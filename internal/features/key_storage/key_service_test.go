package key_storage

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	ethereumlib "github.com/w3-developers/go-ethereum-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestCreateKey(t *testing.T) {
	for _, network := range []constants.Network{constants.NetworkEthereum, constants.NetworkTron, constants.NetworkArbitrum} {
		t.Run(network.String(), func(t *testing.T) {
			repo := &repoStub{}
			srv := newTestService(t, repo)
			role := createTestRole(t, srv, network, constants.RoleKindSingle)

			key, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID})
			if err != nil {
				t.Fatalf("CreateKey() error = %v", err)
			}
			if key.RoleID != role.ID || key.Role.Service != "exchange" || key.Network != network || key.Status != constants.KeyStatusActive || len(repo.keys) != 1 {
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

	single := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: single.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: single.ID}); !errors.Is(err, ErrActiveKeyExists) {
		t.Fatalf("single second key: err = %v", err)
	}

	pool := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
	for range 2 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID}); err != nil {
			t.Fatalf("pool key: err = %v", err)
		}
	}

	deposit := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindDeposit)
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
			role := createTestRole(t, srv, network, constants.RoleKindPool)

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
	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
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

	single := createTestRole(t, srv, constants.NetworkTron, constants.RoleKindSingle)
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

	createTestRole(t, srv, constants.NetworkTron, constants.RoleKindPool)
	if _, err := srv.GetRoleKey(ctx, GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "pool", Network: constants.NetworkTron}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("pool role: err = %v", err)
	}

	if _, err := srv.GetRoleKey(ctx, GetRoleKeyParams{Service: "banknote", Purpose: "gas_station", Label: "single", Network: constants.NetworkTron}); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("other service: err = %v", err)
	}
}

func TestGetOrCreateDepositKey(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	createTestRole(t, srv, constants.NetworkTron, constants.RoleKindDeposit)
	params := GetDepositKeyParams{Service: "exchange", Purpose: "gas_station", Label: "deposit", Network: constants.NetworkTron, ExternalRef: " order-1 "}

	created, err := srv.GetOrCreateDepositKey(ctx, params)
	if err != nil {
		t.Fatalf("GetDepositKey() error = %v", err)
	}
	if *created.ExternalRef != "order-1" || created.Status != constants.KeyStatusActive || created.Role.Kind != constants.RoleKindDeposit {
		t.Fatalf("key = %+v", created)
	}

	again, err := srv.GetOrCreateDepositKey(ctx, params)
	if err != nil || again.ID != created.ID || again.Role.Service != "exchange" || len(repo.keys) != 1 {
		t.Fatalf("repeated call: key = %+v, err = %v, keys = %d", again, err, len(repo.keys))
	}

	params.ExternalRef = "order-2"
	other, err := srv.GetOrCreateDepositKey(ctx, params)
	if err != nil || other.ID == created.ID || other.Address == created.Address {
		t.Fatalf("other ref: key = %+v, err = %v", other, err)
	}

	createTestRole(t, srv, constants.NetworkTron, constants.RoleKindPool)
	cases := map[string]struct {
		params GetDepositKeyParams
		err    error
	}{
		"empty ref":     {GetDepositKeyParams{Service: "exchange", Purpose: "gas_station", Label: "deposit", Network: constants.NetworkTron, ExternalRef: " "}, ErrInvalidParams},
		"not deposit":   {GetDepositKeyParams{Service: "exchange", Purpose: "gas_station", Label: "pool", Network: constants.NetworkTron, ExternalRef: "order-1"}, ErrInvalidParams},
		"other service": {GetDepositKeyParams{Service: "banknote", Purpose: "gas_station", Label: "deposit", Network: constants.NetworkTron, ExternalRef: "order-1"}, ErrRoleNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := srv.GetOrCreateDepositKey(ctx, tc.params); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func newSigningTestKey(t *testing.T, network constants.Network, status constants.KeyStatus) (*Service, *WalletKey) {
	t.Helper()
	repo := &repoStub{}
	srv := newTestService(t, repo)
	// deposit key signs without lease
	role := createTestRole(t, srv, network, constants.RoleKindDeposit)
	key, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID, ExternalRef: ptr("order-1")})
	if err != nil {
		t.Fatal(err)
	}
	key.Status = status

	return srv, key
}

func TestUseSigningKey(t *testing.T) {
	for _, status := range []constants.KeyStatus{constants.KeyStatusActive, constants.KeyStatusRotating} {
		t.Run(status.String(), func(t *testing.T) {
			srv, key := newSigningTestKey(t, constants.NetworkEthereum, status)

			var used []byte
			err := srv.UseSigningKey(context.Background(), "exchange", key.ID, constants.NetworkEthereum, nil, func(*WalletKey) error { return nil }, func(got *WalletKey, privateKey []byte) error {
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
		status     constants.KeyStatus
		want       error
	}{
		{"key network mismatch", tron, eth, constants.KeyStatusActive, ErrInvalidParams},
		{"disabled key", eth, eth, constants.KeyStatusDisabled, ErrKeyNotActive},
		{"revoked key", eth, eth, constants.KeyStatusRevoked, ErrKeyNotActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, key := newSigningTestKey(t, tc.keyNetwork, tc.status)
			err := srv.UseSigningKey(context.Background(), "exchange", key.ID, tc.network, nil, func(*WalletKey) error { return nil }, func(*WalletKey, []byte) error {
				t.Fatal("fn must not be called")
				return nil
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	srv, key := newSigningTestKey(t, eth, constants.KeyStatusActive)
	if err := srv.UseSigningKey(context.Background(), "banknote", key.ID, eth, nil, func(*WalletKey) error { return nil }, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("other service: err = %v", err)
	}

	fnErr := errors.New("fn failed")
	if err := srv.UseSigningKey(context.Background(), "exchange", key.ID, eth, nil, func(*WalletKey) error { return nil }, func(*WalletKey, []byte) error { return fnErr }); !errors.Is(err, fnErr) {
		t.Fatalf("fn error: err = %v", err)
	}
}

func TestUseSigningKeyCheckBeforeDecrypt(t *testing.T) {
	srv, key := newSigningTestKey(t, constants.NetworkEthereum, constants.KeyStatusActive)
	key.KeyCipher = []byte("broken")

	checkErr := errors.New("denied")
	err := srv.UseSigningKey(context.Background(), "exchange", key.ID, constants.NetworkEthereum, nil,
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

func TestIsServiceAddress(t *testing.T) {
	srv, key := newSigningTestKey(t, constants.NetworkEthereum, constants.KeyStatusActive)
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

	key.Status = constants.KeyStatusRevoked
	if got, _ := srv.IsServiceAddress(ctx, "exchange", constants.NetworkEthereum, key.Address, ""); got {
		t.Fatal("revoked key must not be internal")
	}
}

func TestGetRoleKeys(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	pool := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
	for range 3 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID}); err != nil {
			t.Fatal(err)
		}
	}

	params := GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "pool", Network: constants.NetworkEthereum}
	keys, err := srv.GetRoleKeys(ctx, params)
	if err != nil || len(keys) != 3 || keys[0].Role.Kind != constants.RoleKindPool || keys[0].Role.Service != "exchange" {
		t.Fatalf("keys = %+v, err = %v", keys, err)
	}

	single := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	if keys, err := srv.GetRoleKeys(ctx, GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "single", Network: constants.NetworkEthereum}); err != nil || len(keys) != 0 {
		t.Fatalf("single role %s without keys: keys = %v, err = %v", single.ID, keys, err)
	}

	createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindDeposit)
	if _, err := srv.GetRoleKeys(ctx, GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "deposit", Network: constants.NetworkEthereum}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("deposit role: err = %v", err)
	}

	params.Service = "banknote"
	if _, err := srv.GetRoleKeys(ctx, params); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("other service: err = %v", err)
	}
}

func TestListKeys(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	single := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	pool := createTestRole(t, srv, constants.NetworkTron, constants.RoleKindPool)
	for _, roleID := range []uuid.UUID{single.ID, pool.ID, pool.ID} {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: roleID}); err != nil {
			t.Fatal(err)
		}
	}

	if keys, err := srv.ListKeys(ctx, ListKeysFilter{Offset: 5, Limit: 10}); err != nil || len(keys) != 3 || repo.listFilter.Offset != 5 || repo.listFilter.Limit != 10 {
		t.Fatalf("all: keys = %d, filter = %+v, err = %v", len(keys), repo.listFilter, err)
	}

	tron := constants.NetworkTron
	if keys, _ := srv.ListKeys(ctx, ListKeysFilter{Network: &tron}); len(keys) != 2 {
		t.Fatalf("tron: keys = %d", len(keys))
	}

	label := "single"
	if keys, _ := srv.ListKeys(ctx, ListKeysFilter{Service: ptr("exchange"), Label: &label}); len(keys) != 1 || keys[0].Role.Kind != constants.RoleKindSingle {
		t.Fatalf("label: keys = %+v", keys)
	}
}

func TestRotateKeyLifecycle(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	old, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID, Alias: ptr("gas")})
	if err != nil {
		t.Fatal(err)
	}
	params := GetRoleKeyParams{Service: "exchange", Purpose: "gas_station", Label: "single", Network: constants.NetworkEthereum}

	replacement, err := srv.RotateKey(ctx, old.ID)
	if err != nil {
		t.Fatalf("RotateKey() error = %v", err)
	}
	if replacement.Status != constants.KeyStatusPending || replacement.RoleID != role.ID || *replacement.Alias != "gas" || *old.ReplacedBy != replacement.ID {
		t.Fatalf("replacement = %+v, old = %+v", replacement, old)
	}
	if _, err := srv.RotateKey(ctx, old.ID); !errors.Is(err, ErrRotationInProgress) {
		t.Fatalf("second rotation: err = %v", err)
	}

	if key, _ := srv.GetRoleKey(ctx, params); key.ID != old.ID {
		t.Fatalf("active before activation = %s", key.ID)
	}
	noop := func(*WalletKey) error { return nil }
	if err := srv.UseSigningKey(ctx, "exchange", replacement.ID, constants.NetworkEthereum, nil, noop, func(*WalletKey, []byte) error { return nil }); err != nil {
		t.Fatalf("pending key must sign: err = %v", err)
	}

	lease, err := srv.AcquireLease(ctx, AcquireLeaseParams{Service: "exchange", Purpose: role.Purpose, Label: role.Label, Network: role.Network})
	if err != nil || lease.Key.ID != old.ID {
		t.Fatalf("lease = %+v, err = %v", lease, err)
	}
	err = srv.UseSigningKey(ctx, "exchange", old.ID, constants.NetworkEthereum, &lease.ID, func(key *WalletKey) error {
		if key.ReplacementAddress == nil || *key.ReplacementAddress != replacement.Address {
			t.Fatalf("replacement address = %v", key.ReplacementAddress)
		}
		return nil
	}, func(*WalletKey, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	if _, err := srv.ActivateKey(ctx, replacement.ID); err != nil {
		t.Fatalf("ActivateKey() error = %v", err)
	}
	if old.Status != constants.KeyStatusRotating || old.RotatedAt == nil || replacement.Status != constants.KeyStatusActive {
		t.Fatalf("old = %s, replacement = %s", old.Status, replacement.Status)
	}
	if key, _ := srv.GetRoleKey(ctx, params); key.ID != replacement.ID {
		t.Fatalf("active after activation = %s", key.ID)
	}
	if err := srv.UseSigningKey(ctx, "exchange", old.ID, constants.NetworkEthereum, &lease.ID, noop, func(*WalletKey, []byte) error { return nil }); err != nil {
		t.Fatalf("rotating key must sign under its lease: err = %v", err)
	}

	if _, err := srv.RevokeKey(ctx, old.ID); err != nil || old.Status != constants.KeyStatusRevoked || old.RevokedAt == nil {
		t.Fatalf("RevokeKey() = %v, old = %+v", err, old)
	}
	if err := srv.UseSigningKey(ctx, "exchange", old.ID, constants.NetworkEthereum, nil, noop, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrKeyNotActive) {
		t.Fatalf("revoked key: err = %v", err)
	}
}

func TestRotateKeyErrors(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	deposit := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindDeposit)
	depositKey, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: ptr("order-1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RotateKey(ctx, depositKey.ID); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("deposit key: err = %v", err)
	}

	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ActivateKey(ctx, key.ID); !errors.Is(err, ErrInvalidKeyStatus) {
		t.Fatalf("activate active key: err = %v", err)
	}
	if _, err := srv.DisableKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RotateKey(ctx, key.ID); !errors.Is(err, ErrInvalidKeyStatus) {
		t.Fatalf("rotate disabled key: err = %v", err)
	}
	if _, err := srv.RevokeKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RevokeKey(ctx, key.ID); !errors.Is(err, ErrInvalidKeyStatus) {
		t.Fatalf("revoke twice: err = %v", err)
	}
	if _, err := srv.RotateKey(ctx, uuid.New()); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("unknown key: err = %v", err)
	}
}

func TestCancelRotation(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	old, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := srv.RotateKey(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := srv.RevokeKey(ctx, replacement.ID); err != nil {
		t.Fatalf("revoke pending: err = %v", err)
	}
	if old.ReplacedBy != nil || old.Status != constants.KeyStatusActive {
		t.Fatalf("old after cancel = %+v", old)
	}
	if _, err := srv.RotateKey(ctx, old.ID); err != nil {
		t.Fatalf("rotate again after cancel: err = %v", err)
	}
}

func TestRotatePoolKey(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	pool := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
	first, _ := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID})
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID}); err != nil {
		t.Fatal(err)
	}

	replacement, err := srv.RotateKey(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ActivateKey(ctx, replacement.ID); err != nil {
		t.Fatalf("activate pool key: err = %v", err)
	}

	statuses := map[constants.KeyStatus]int{}
	for _, key := range repo.keys {
		statuses[key.Status]++
	}
	if statuses[constants.KeyStatusActive] != 2 || statuses[constants.KeyStatusRotating] != 1 {
		t.Fatalf("statuses = %v", statuses)
	}
}

func TestDisableEnableKey(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := srv.DisableKey(ctx, key.ID); err != nil || key.Status != constants.KeyStatusDisabled {
		t.Fatalf("disable: err = %v, status = %s", err, key.Status)
	}
	if _, err := srv.DisableKey(ctx, key.ID); !errors.Is(err, ErrInvalidKeyStatus) {
		t.Fatalf("disable twice: err = %v", err)
	}
	if _, err := srv.EnableKey(ctx, key.ID); err != nil || key.Status != constants.KeyStatusActive {
		t.Fatalf("enable: err = %v, status = %s", err, key.Status)
	}

	if _, err := srv.DisableKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.EnableKey(ctx, key.ID); !errors.Is(err, ErrActiveKeyExists) {
		t.Fatalf("enable with other active: err = %v", err)
	}
}
