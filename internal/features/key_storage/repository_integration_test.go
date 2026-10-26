//go:build integration

package key_storage

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	aes256 "github.com/w3-developers/go-aes256"
	ethereumlib "github.com/w3-developers/go-ethereum-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WALLET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("WALLET_TEST_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func newIntegrationService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	pool := openTestPool(t)
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}

	srv := NewService(logger.New(logger.Config{Level: "error"}), NewRepository(), db.NewPgxTxManager(pool), aes256.NewAES256GCM(kek), 1)
	return srv, pool
}

func uniqueService() string {
	return "it-" + uuid.NewString()[:8]
}

func createIntegrationRole(t *testing.T, srv *Service, service, purpose string, network constants.Network, kind RoleKind, policy string) *Role {
	t.Helper()
	role, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: purpose, Network: network, Kind: kind, Policy: []byte(policy)})
	if err != nil {
		t.Fatal(err)
	}
	return role
}

func TestIntegrationRoleUnique(t *testing.T) {
	srv, _ := newIntegrationService(t)
	service := uniqueService()
	createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, RoleKindSingle, "")

	_, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: "gas_station", Network: constants.NetworkEthereum, Kind: RoleKindPool})
	if !errors.Is(err, ErrRoleExists) {
		t.Fatalf("duplicate role: err = %v", err)
	}

	if _, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: "gas_station", Network: constants.NetworkTron, Kind: RoleKindSingle}); err != nil {
		t.Fatalf("other network: err = %v", err)
	}
	if _, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: "gas_station", Label: "atoken", Network: constants.NetworkEthereum, Kind: RoleKindSingle}); err != nil {
		t.Fatalf("other label: err = %v", err)
	}
}

func TestIntegrationSingleRoleConcurrentCreate(t *testing.T) {
	srv, _ := newIntegrationService(t)
	role := createIntegrationRole(t, srv, uniqueService(), "gas_station", constants.NetworkEthereum, RoleKindSingle, "")

	const workers = 20
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		exists  int
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.CreateKey(context.Background(), CreateKeyParams{RoleID: role.ID})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrActiveKeyExists):
				exists++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if created != 1 || exists != workers-1 {
		t.Fatalf("created = %d, active exists = %d", created, exists)
	}
}

func TestIntegrationKeyUniqueConstraints(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	pool := createIntegrationRole(t, srv, service, "executor", constants.NetworkEthereum, RoleKindPool, "")
	privateKey, _, err := ethereumlib.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ImportKey(ctx, ImportKeyParams{RoleID: pool.ID, PrivateKey: []byte(privateKey)}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ImportKey(ctx, ImportKeyParams{RoleID: pool.ID, PrivateKey: []byte(privateKey)}); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("same address twice: err = %v", err)
	}

	deposit := createIntegrationRole(t, srv, service, "deposit", constants.NetworkEthereum, RoleKindDeposit, "")
	ref := "order-" + uuid.NewString()
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: &ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: &ref}); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("same external_ref twice: err = %v", err)
	}
}

func TestIntegrationUseSigningKey(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	role := createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, RoleKindSingle, `{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`)
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}

	err = srv.UseSigningKey(ctx, service, key.ID, constants.NetworkEthereum,
		func(got *WalletKey) error {
			if got.Role.Service != service || got.Role.Kind != RoleKindSingle || len(got.Role.Policy) == 0 {
				t.Fatalf("role loaded from db = %+v", got.Role)
			}
			return nil
		},
		func(got *WalletKey, privateKey []byte) error {
			address, err := ethereumlib.PrivateKeyToAddress(string(privateKey))
			if err != nil || address != got.Address {
				t.Fatalf("decrypted key address = %s, err = %v, want %s", address, err, got.Address)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("UseSigningKey() error = %v", err)
	}

	noop := func(*WalletKey) error { return nil }
	if err := srv.UseSigningKey(ctx, uniqueService(), key.ID, constants.NetworkEthereum, noop, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("other service: err = %v", err)
	}
}

func TestIntegrationIsServiceAddress(t *testing.T) {
	srv, pool := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	role := createIntegrationRole(t, srv, service, "deposit", constants.NetworkEthereum, RoleKindDeposit, "")
	ref := "order-1"
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID, ExternalRef: &ref})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, service, purpose string
		network                constants.Network
		want                   bool
	}{
		{"any purpose", service, "", constants.NetworkEthereum, true},
		{"same purpose", service, "deposit", constants.NetworkEthereum, true},
		{"other purpose", service, "executor", constants.NetworkEthereum, false},
		{"other service", uniqueService(), "", constants.NetworkEthereum, false},
		{"other network", service, "", constants.NetworkArbitrum, false},
	} {
		if got, err := srv.IsServiceAddress(ctx, tc.service, tc.network, key.Address, tc.purpose); err != nil || got != tc.want {
			t.Fatalf("%s: got %v, err = %v", tc.name, got, err)
		}
	}

	for status, want := range map[KeyStatus]bool{KeyStatusRotating: true, KeyStatusPending: true, KeyStatusDisabled: false, KeyStatusRevoked: false} {
		if _, err := pool.Exec(ctx, `UPDATE wallet_keys SET status = $2 WHERE id = $1`, key.ID, status); err != nil {
			t.Fatal(err)
		}
		if got, _ := srv.IsServiceAddress(ctx, service, constants.NetworkEthereum, key.Address, ""); got != want {
			t.Fatalf("status %s: got %v, want %v", status, got, want)
		}
	}
}

func TestIntegrationListKeys(t *testing.T) {
	srv, pool := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	single := createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, RoleKindSingle, "")
	labeled, err := srv.CreateRole(ctx, CreateRoleParams{Service: service, Purpose: "executor", Label: "atoken", Network: constants.NetworkTron, Kind: RoleKindPool})
	if err != nil {
		t.Fatal(err)
	}
	gas, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: single.ID})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: labeled.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE wallet_keys SET status = $2 WHERE id = $1`, gas.ID, KeyStatusRevoked); err != nil {
		t.Fatal(err)
	}

	empty, atoken, tron, revoked := "", "atoken", constants.NetworkTron, KeyStatusRevoked
	for _, tc := range []struct {
		name   string
		filter ListKeysFilter
		want   int
	}{
		{"service", ListKeysFilter{Service: &service}, 3},
		{"empty label", ListKeysFilter{Service: &service, Label: &empty}, 1},
		{"label", ListKeysFilter{Service: &service, Label: &atoken}, 2},
		{"network", ListKeysFilter{Service: &service, Network: &tron}, 2},
		{"status", ListKeysFilter{Service: &service, Status: &revoked}, 1},
		{"limit", ListKeysFilter{Service: &service, Limit: 1}, 1},
	} {
		keys, err := srv.ListKeys(ctx, tc.filter)
		if err != nil || len(keys) != tc.want {
			t.Fatalf("%s: keys = %d, err = %v, want %d", tc.name, len(keys), err, tc.want)
		}
		for _, key := range keys {
			if key.Role.Service != service || len(key.KeyCipher) != 0 {
				t.Fatalf("%s: key = %+v", tc.name, key)
			}
		}
	}
}

func TestIntegrationUpdatePolicy(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	role := createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, RoleKindSingle, "")
	if _, err := srv.UpdatePolicy(ctx, role.ID, []byte(`{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`)); err != nil {
		t.Fatal(err)
	}

	roles, err := srv.ListRoles(ctx, service)
	if err != nil || len(roles) != 1 {
		t.Fatalf("roles = %v, err = %v", roles, err)
	}
	if _, err := srv.UpdatePolicy(ctx, uuid.New(), []byte(`{}`)); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}

func TestIntegrationRotation(t *testing.T) {
	srv, pool := newIntegrationService(t)
	ctx := context.Background()

	role := createIntegrationRole(t, srv, uniqueService(), "gas_station", constants.NetworkEthereum, RoleKindSingle, "")
	old, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 10
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		replacements []*WalletKey
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := srv.RotateKey(ctx, old.ID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				replacements = append(replacements, key)
			case errors.Is(err, ErrRotationInProgress):
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(replacements) != 1 {
		t.Fatalf("replacements = %d", len(replacements))
	}
	replacement := replacements[0]

	if _, err := srv.ActivateKey(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RevokeKey(ctx, old.ID); err != nil {
		t.Fatal(err)
	}

	var (
		status               KeyStatus
		replacedBy           *uuid.UUID
		rotatedAt, revokedAt *time.Time
	)
	if err := pool.QueryRow(ctx, `SELECT status, replaced_by, rotated_at, revoked_at FROM wallet_keys WHERE id = $1`, old.ID).
		Scan(&status, &replacedBy, &rotatedAt, &revokedAt); err != nil {
		t.Fatal(err)
	}
	if status != KeyStatusRevoked || replacedBy == nil || *replacedBy != replacement.ID || rotatedAt == nil || revokedAt == nil {
		t.Fatalf("old key: status = %s, replaced_by = %v, rotated_at = %v, revoked_at = %v", status, replacedBy, rotatedAt, revokedAt)
	}

	active, err := srv.GetRoleKey(ctx, GetRoleKeyParams{Service: role.Service, Purpose: role.Purpose, Network: role.Network})
	if err != nil || active.ID != replacement.ID {
		t.Fatalf("active = %+v, err = %v", active, err)
	}
}
