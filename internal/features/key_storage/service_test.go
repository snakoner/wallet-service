package key_storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	aes256 "github.com/w3-developers/go-aes256"
	ethereumlib "github.com/w3-developers/go-ethereum-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

// repoStub keeps roles and keys in memory and checks the same unique constraints as db.
type repoStub struct {
	roles []*Role
	keys  []*WalletKey
}

func (r *repoStub) CreateRole(_ context.Context, _ db.DBTX, role *Role) error {
	for _, existing := range r.roles {
		if existing.Service == role.Service && existing.Purpose == role.Purpose && existing.Label == role.Label && existing.Network == role.Network {
			return ErrRoleExists
		}
	}
	r.roles = append(r.roles, role)
	return nil
}

func (r *repoStub) GetRole(_ context.Context, _ db.DBTX, service, purpose, label string, network constants.Network) (*Role, error) {
	for _, role := range r.roles {
		if role.Service == service && role.Purpose == purpose && role.Label == label && role.Network == network {
			return role, nil
		}
	}
	return nil, ErrRoleNotFound
}

func (r *repoStub) GetRoleByID(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Role, error) {
	return r.GetRoleForUpdate(ctx, tx, id)
}

func (r *repoStub) ListRoles(_ context.Context, _ db.DBTX, service string) ([]Role, error) {
	roles := []Role{}
	for _, role := range r.roles {
		if service == "" || role.Service == service {
			roles = append(roles, *role)
		}
	}
	return roles, nil
}

func (r *repoStub) ListRoleKeys(_ context.Context, _ db.DBTX, roleID uuid.UUID) ([]WalletKey, error) {
	keys := []WalletKey{}
	for _, key := range r.keys {
		if key.RoleID == roleID {
			keys = append(keys, *key)
		}
	}
	return keys, nil
}

func (r *repoStub) UpdateRolePolicy(_ context.Context, _ db.DBTX, id uuid.UUID, policy json.RawMessage) error {
	for _, role := range r.roles {
		if role.ID == id {
			role.Policy = policy
			return nil
		}
	}
	return ErrRoleNotFound
}

func (r *repoStub) IsServiceAddress(_ context.Context, _ db.DBTX, service string, network constants.Network, address, purpose string) (bool, error) {
	for _, key := range r.keys {
		usable := key.Status == KeyStatusActive || key.Status == KeyStatusRotating || key.Status == KeyStatusPending
		if usable && key.Role.Service == service && key.Network == network && key.Address == address && (purpose == "" || key.Role.Purpose == purpose) {
			return true, nil
		}
	}
	return false, nil
}

func (r *repoStub) GetRoleForUpdate(_ context.Context, _ db.DBTX, id uuid.UUID) (*Role, error) {
	for _, role := range r.roles {
		if role.ID == id {
			return role, nil
		}
	}
	return nil, ErrRoleNotFound
}

func (r *repoStub) HasActiveKey(_ context.Context, _ db.DBTX, roleID uuid.UUID) (bool, error) {
	for _, key := range r.keys {
		if key.RoleID == roleID && key.Status == KeyStatusActive {
			return true, nil
		}
	}
	return false, nil
}

func (r *repoStub) CreateKey(_ context.Context, _ db.DBTX, key *WalletKey) error {
	for _, existing := range r.keys {
		sameRef := existing.RoleID == key.RoleID && existing.ExternalRef != nil && key.ExternalRef != nil && *existing.ExternalRef == *key.ExternalRef
		if (existing.Network == key.Network && existing.Address == key.Address) || sameRef {
			return ErrKeyExists
		}
	}
	r.keys = append(r.keys, key)
	return nil
}

func (r *repoStub) GetKey(_ context.Context, _ db.DBTX, id uuid.UUID, service string) (*WalletKey, error) {
	for _, key := range r.keys {
		if key.ID == id && key.Role.Service == service {
			return key, nil
		}
	}
	return nil, ErrKeyNotFound
}

func (r *repoStub) GetActiveKey(_ context.Context, _ db.DBTX, roleID uuid.UUID) (*WalletKey, error) {
	for _, key := range r.keys {
		if key.RoleID == roleID && key.Status == KeyStatusActive {
			return key, nil
		}
	}
	return nil, ErrKeyNotFound
}

type txmStub struct{}

func (txmStub) WithTx(ctx context.Context, fn func(ctx context.Context, tx db.DBTX) error) error {
	return fn(ctx, nil)
}

func (txmStub) GetPool() db.DBTX { return nil }

func newTestService(t *testing.T, repo repository) *Service {
	t.Helper()
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	log := logger.New(logger.Config{Level: "error"})
	return NewService(log, repo, txmStub{}, aes256.NewAES256GCM(kek), 1)
}

func createTestRole(t *testing.T, srv *Service, network constants.Network, kind RoleKind) *Role {
	t.Helper()
	role, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: "exchange", Purpose: "gas_station", Label: kind.String(), Network: network, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return role
}

func ptr(s string) *string { return &s }

func addressOf(t *testing.T, network constants.Network, privateKey []byte) string {
	t.Helper()
	address, err := keysByNetwork[network].address(string(privateKey))
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestCreateRole(t *testing.T) {
	srv := newTestService(t, &repoStub{})

	role, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: " exchange ", Purpose: "gas_station", Label: " atoken ", Network: constants.NetworkTron, Kind: RoleKindSingle})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if role.Service != "exchange" || role.Label != "atoken" || string(role.Policy) != `{"version":1,"rules":[]}` {
		t.Fatalf("role = %+v", role)
	}

	if _, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: "exchange", Purpose: "gas_station", Label: "atoken", Network: constants.NetworkTron, Kind: RoleKindSingle}); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("duplicate: err = %v", err)
	}

	cases := map[string]CreateRoleParams{
		"no service":     {Purpose: "executor", Network: constants.NetworkTron, Kind: RoleKindPool},
		"no purpose":     {Service: "exchange", Network: constants.NetworkTron, Kind: RoleKindPool},
		"bad network":    {Service: "exchange", Purpose: "executor", Network: 9, Kind: RoleKindPool},
		"bad kind":       {Service: "exchange", Purpose: "executor", Network: constants.NetworkTron},
		"invalid policy": {Service: "exchange", Purpose: "executor", Network: constants.NetworkTron, Kind: RoleKindPool, Policy: []byte("{")},
		"policy rule":    {Service: "exchange", Purpose: "executor", Network: constants.NetworkTron, Kind: RoleKindPool, Policy: []byte(`{"version":1,"rules":[{"op":"native"}]}`)},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := srv.CreateRole(context.Background(), params); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCreateKey(t *testing.T) {
	for _, network := range []constants.Network{constants.NetworkEthereum, constants.NetworkTron, constants.NetworkArbitrum} {
		t.Run(network.String(), func(t *testing.T) {
			repo := &repoStub{}
			srv := newTestService(t, repo)
			role := createTestRole(t, srv, network, RoleKindSingle)

			key, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID})
			if err != nil {
				t.Fatalf("CreateKey() error = %v", err)
			}
			if key.RoleID != role.ID || key.Role.Service != "exchange" || key.Network != network || key.Status != KeyStatusActive || len(repo.keys) != 1 {
				t.Fatalf("unexpected key: %+v", key)
			}

			privateKey, err := srv.decryptPrivateKey(key)
			if err != nil {
				t.Fatalf("decryptPrivateKey() error = %v", err)
			}
			if address := addressOf(t, network, privateKey); address != key.Address {
				t.Fatalf("address = %q, want %q", address, key.Address)
			}
		})
	}
}

func TestCreateKeyByKind(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	single := createTestRole(t, srv, constants.NetworkEthereum, RoleKindSingle)
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: single.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: single.ID}); !errors.Is(err, ErrActiveKeyExists) {
		t.Fatalf("single second key: err = %v", err)
	}

	pool := createTestRole(t, srv, constants.NetworkEthereum, RoleKindPool)
	for range 2 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID}); err != nil {
			t.Fatalf("pool key: err = %v", err)
		}
	}

	deposit := createTestRole(t, srv, constants.NetworkEthereum, RoleKindDeposit)
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("deposit without ref: err = %v", err)
	}
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: ptr("order-1")})
	if err != nil || *key.ExternalRef != "order-1" {
		t.Fatalf("deposit key = %+v, err = %v", key, err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: ptr("order-1")}); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("deposit same ref: err = %v", err)
	}

	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID, ExternalRef: ptr("order-2")}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("pool with ref: err = %v", err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: uuid.New()}); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}

func TestImportKey(t *testing.T) {
	for _, network := range []constants.Network{constants.NetworkEthereum, constants.NetworkTron} {
		t.Run(network.String(), func(t *testing.T) {
			srv := newTestService(t, &repoStub{})
			role := createTestRole(t, srv, network, RoleKindPool)

			privateKey, address, err := keysByNetwork[network].generate()
			if err != nil {
				t.Fatal(err)
			}

			input := []byte(" 0x" + privateKey + "\n")
			key, err := srv.ImportKey(context.Background(), ImportKeyParams{RoleID: role.ID, PrivateKey: input})
			if err != nil {
				t.Fatalf("ImportKey() error = %v", err)
			}
			if key.Address != address {
				t.Fatalf("address = %q, want %q", key.Address, address)
			}
			for _, b := range input {
				if b != 0 {
					t.Fatal("input private key must be cleared")
				}
			}

			decrypted, err := srv.decryptPrivateKey(key)
			if err != nil || string(decrypted) != privateKey {
				t.Fatalf("decrypted = %q, err = %v", decrypted, err)
			}

			if _, err := srv.ImportKey(context.Background(), ImportKeyParams{RoleID: role.ID, PrivateKey: []byte(privateKey)}); !errors.Is(err, ErrKeyExists) {
				t.Fatalf("same key twice: err = %v", err)
			}
		})
	}

	srv := newTestService(t, &repoStub{})
	role := createTestRole(t, srv, constants.NetworkEthereum, RoleKindPool)
	for name, privateKey := range map[string]string{"short": "abcd", "not hex": hex.EncodeToString(make([]byte, 31)) + "zz", "zero key": hex.EncodeToString(make([]byte, 32))} {
		t.Run(name, func(t *testing.T) {
			if _, err := srv.ImportKey(context.Background(), ImportKeyParams{RoleID: role.ID, PrivateKey: []byte(privateKey)}); !errors.Is(err, ErrInvalidParams) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestGetRoleKey(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	single := createTestRole(t, srv, constants.NetworkTron, RoleKindSingle)
	params := GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "single", Network: constants.NetworkTron}
	if _, err := srv.GetRoleKey(ctx, params); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("no key: err = %v", err)
	}

	created, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: single.ID})
	if err != nil {
		t.Fatal(err)
	}
	key, err := srv.GetRoleKey(ctx, params)
	if err != nil || key.ID != created.ID || key.Role.Purpose != "gas_station" {
		t.Fatalf("GetRoleKey() = %+v, %v", key, err)
	}

	createTestRole(t, srv, constants.NetworkTron, RoleKindPool)
	if _, err := srv.GetRoleKey(ctx, GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "pool", Network: constants.NetworkTron}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("pool role: err = %v", err)
	}

	if _, err := srv.GetRoleKey(ctx, GetRoleKeyParams{Service: "banknote", Purpose: "gas_station", Label: "single", Network: constants.NetworkTron}); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("other service: err = %v", err)
	}
}

func TestDecryptPrivateKeyErrors(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	role := createTestRole(t, srv, constants.NetworkEthereum, RoleKindSingle)
	key, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}

	wrongVersion := *key
	wrongVersion.KEKVersion = 2
	if _, err := srv.decryptPrivateKey(&wrongVersion); err == nil {
		t.Fatal("expected error for unknown kek version")
	}

	otherKEK := newTestService(t, &repoStub{})
	if _, err := otherKEK.decryptPrivateKey(key); err == nil {
		t.Fatal("expected error for wrong kek")
	}

	tampered := *key
	tampered.KeyCipher = append([]byte(nil), key.KeyCipher...)
	tampered.KeyCipher[len(tampered.KeyCipher)-1] ^= 1
	if _, err := srv.decryptPrivateKey(&tampered); err == nil {
		t.Fatal("expected error for tampered cipher")
	}
}

func newSigningTestKey(t *testing.T, network constants.Network, status KeyStatus) (*Service, *WalletKey) {
	t.Helper()
	repo := &repoStub{}
	srv := newTestService(t, repo)
	role := createTestRole(t, srv, network, RoleKindSingle)
	key, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	key.Status = status

	return srv, key
}

func TestUseSigningKey(t *testing.T) {
	for _, status := range []KeyStatus{KeyStatusActive, KeyStatusRotating} {
		t.Run(status.String(), func(t *testing.T) {
			srv, key := newSigningTestKey(t, constants.NetworkEthereum, status)

			var used []byte
			err := srv.UseSigningKey(context.Background(), "exchange", key.ID, constants.NetworkEthereum, func(*WalletKey) error { return nil }, func(got *WalletKey, privateKey []byte) error {
				address, err := ethereumlib.PrivateKeyToAddress(string(privateKey))
				if err != nil || address != got.Address || got.Role.Service != "exchange" {
					t.Fatalf("address = %q, err = %v, want %q", address, err, got.Address)
				}
				used = privateKey

				return nil
			})
			if err != nil {
				t.Fatalf("UseSigningKey() error = %v", err)
			}

			for _, b := range used {
				if b != 0 {
					t.Fatal("private key must be cleared after fn")
				}
			}
		})
	}
}

func TestUseSigningKeyErrors(t *testing.T) {
	eth, tron := constants.NetworkEthereum, constants.NetworkTron
	cases := []struct {
		name       string
		keyNetwork constants.Network
		network    constants.Network
		status     KeyStatus
		want       error
	}{
		{"key network mismatch", tron, eth, KeyStatusActive, ErrInvalidParams},
		{"disabled key", eth, eth, KeyStatusDisabled, ErrKeyNotActive},
		{"revoked key", eth, eth, KeyStatusRevoked, ErrKeyNotActive},
		{"pending key", eth, eth, KeyStatusPending, ErrKeyNotActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, key := newSigningTestKey(t, tc.keyNetwork, tc.status)
			err := srv.UseSigningKey(context.Background(), "exchange", key.ID, tc.network, func(*WalletKey) error { return nil }, func(*WalletKey, []byte) error {
				t.Fatal("fn must not be called")
				return nil
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	srv, key := newSigningTestKey(t, eth, KeyStatusActive)
	if err := srv.UseSigningKey(context.Background(), "banknote", key.ID, eth, func(*WalletKey) error { return nil }, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("other service: err = %v", err)
	}

	fnErr := errors.New("fn failed")
	if err := srv.UseSigningKey(context.Background(), "exchange", key.ID, eth, func(*WalletKey) error { return nil }, func(*WalletKey, []byte) error { return fnErr }); !errors.Is(err, fnErr) {
		t.Fatalf("fn error: err = %v", err)
	}
}

func TestListRolesAndKeys(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	pool := createTestRole(t, srv, constants.NetworkEthereum, RoleKindPool)
	if _, err := srv.CreateRole(ctx, CreateRoleParams{Service: "banknote", Purpose: "executor", Network: constants.NetworkTron, Kind: RoleKindPool}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID}); err != nil {
			t.Fatal(err)
		}
	}

	if roles, err := srv.ListRoles(ctx, ""); err != nil || len(roles) != 2 {
		t.Fatalf("all roles = %d, err = %v", len(roles), err)
	}
	if roles, err := srv.ListRoles(ctx, "banknote"); err != nil || len(roles) != 1 || roles[0].Service != "banknote" {
		t.Fatalf("banknote roles = %+v, err = %v", roles, err)
	}

	keys, err := srv.ListRoleKeys(ctx, pool.ID)
	if err != nil || len(keys) != 2 || keys[0].Role.Purpose != "gas_station" {
		t.Fatalf("keys = %+v, err = %v", keys, err)
	}
	if _, err := srv.ListRoleKeys(ctx, uuid.New()); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}

func TestUseSigningKeyCheckBeforeDecrypt(t *testing.T) {
	srv, key := newSigningTestKey(t, constants.NetworkEthereum, KeyStatusActive)
	key.KeyCipher = []byte("broken") // decryption would fail if it ran

	checkErr := errors.New("denied")
	err := srv.UseSigningKey(context.Background(), "exchange", key.ID, constants.NetworkEthereum,
		func(got *WalletKey) error {
			if got.ID != key.ID || got.Role.Service != "exchange" {
				t.Fatalf("check got key = %+v", got)
			}
			return checkErr
		},
		func(*WalletKey, []byte) error {
			t.Fatal("fn must not be called")
			return nil
		},
	)
	if !errors.Is(err, checkErr) {
		t.Fatalf("err = %v, want check error", err)
	}
}

func TestUpdatePolicy(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	role := createTestRole(t, srv, constants.NetworkEthereum, RoleKindSingle)

	updated, err := srv.UpdatePolicy(context.Background(), role.ID, []byte(`{"version":1,"rules":[{"op":"native","to":["internal:executor"]}]}`))
	if err != nil {
		t.Fatalf("UpdatePolicy() error = %v", err)
	}
	if want := `{"version":1,"rules":[{"op":"native","to":["internal:executor"]}]}`; string(updated.Policy) != want || string(repo.roles[0].Policy) != want {
		t.Fatalf("policy = %s, stored = %s", updated.Policy, repo.roles[0].Policy)
	}

	if _, err := srv.UpdatePolicy(context.Background(), role.ID, []byte(`{"version":1,"rules":[{"op":"token","to":["*"]}]}`)); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("invalid policy: err = %v", err)
	}
	if _, err := srv.UpdatePolicy(context.Background(), uuid.New(), []byte(`{}`)); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}

func TestIsServiceAddress(t *testing.T) {
	srv, key := newSigningTestKey(t, constants.NetworkEthereum, KeyStatusActive)
	ctx := context.Background()

	for _, tc := range []struct {
		service, purpose string
		network          constants.Network
		want             bool
	}{
		{"exchange", "", constants.NetworkEthereum, true},
		{"exchange", "gas_station", constants.NetworkEthereum, true},
		{"exchange", "executor", constants.NetworkEthereum, false},
		{"banknote", "", constants.NetworkEthereum, false},
		{"exchange", "", constants.NetworkArbitrum, false},
	} {
		if got, err := srv.IsServiceAddress(ctx, tc.service, tc.network, key.Address, tc.purpose); err != nil || got != tc.want {
			t.Fatalf("%+v: got %v, err = %v", tc, got, err)
		}
	}

	key.Status = KeyStatusRevoked
	if got, _ := srv.IsServiceAddress(ctx, "exchange", constants.NetworkEthereum, key.Address, ""); got {
		t.Fatal("revoked key must not be internal")
	}
}
