//go:build integration

package key_storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	ethereumlib "github.com/w3-developers/go-ethereum-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestIntegrationSingleRoleConcurrentCreate(t *testing.T) {
	srv, _ := newIntegrationService(t)
	role := createIntegrationRole(t, srv, uniqueService(), "gas_station", constants.NetworkEthereum, constants.RoleKindSingle, "")

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

	pool := createIntegrationRole(t, srv, service, "executor", constants.NetworkEthereum, constants.RoleKindPool, "")
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

	deposit := createIntegrationRole(t, srv, service, "deposit", constants.NetworkEthereum, constants.RoleKindDeposit, "")
	ref := "order-" + uuid.NewString()
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: &ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: deposit.ID, ExternalRef: &ref}); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("same external_ref twice: err = %v", err)
	}
}

func TestIntegrationUseSigningKey(t *testing.T) {
	srv, pool := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	role := createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, constants.RoleKindSingle, `{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`)
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := srv.AcquireLease(ctx, AcquireLeaseParams{Service: service, Purpose: role.Purpose, Network: role.Network})
	if err != nil {
		t.Fatal(err)
	}

	err = srv.UseSigningKey(ctx, service, key.ID, constants.NetworkEthereum, &lease.ID,
		func(got *WalletKey) error {
			if got.Role.Service != service || got.Role.Kind != constants.RoleKindSingle || len(got.Role.Policy) == 0 {
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
	if err := srv.UseSigningKey(ctx, uniqueService(), key.ID, constants.NetworkEthereum, nil, noop, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("other service: err = %v", err)
	}
	if err := srv.UseSigningKey(ctx, service, key.ID, constants.NetworkEthereum, nil, noop, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrLeaseRequired) {
		t.Fatalf("without lease: err = %v", err)
	}

	// last 10 seconds of lease are not used for signing
	if _, err := pool.Exec(ctx, `UPDATE wallet_keys SET leased_until = (now() AT TIME ZONE 'UTC') + interval '5 seconds' WHERE id = $1`, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := srv.UseSigningKey(ctx, service, key.ID, constants.NetworkEthereum, &lease.ID, noop, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("ending lease: err = %v", err)
	}
	if err := srv.UseSigningKey(ctx, service, key.ID, constants.NetworkEthereum, nil, noop, func(*WalletKey, []byte) error { return nil }); !errors.Is(err, ErrLeaseRequired) {
		t.Fatalf("ending lease, no lease id: err = %v", err)
	}
}

func TestIntegrationIsServiceAddress(t *testing.T) {
	srv, pool := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	role := createIntegrationRole(t, srv, service, "deposit", constants.NetworkEthereum, constants.RoleKindDeposit, "")
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

	for status, want := range map[constants.KeyStatus]bool{constants.KeyStatusRotating: true, constants.KeyStatusPending: true, constants.KeyStatusDisabled: false, constants.KeyStatusRevoked: false} {
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

	single := createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, constants.RoleKindSingle, "")
	labeled, err := srv.CreateRole(ctx, CreateRoleParams{Service: service, Purpose: "executor", Label: "atoken", Network: constants.NetworkTron, Kind: constants.RoleKindPool})
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
	if _, err := pool.Exec(ctx, `UPDATE wallet_keys SET status = $2 WHERE id = $1`, gas.ID, constants.KeyStatusRevoked); err != nil {
		t.Fatal(err)
	}

	empty, atoken, tron, revoked := "", "atoken", constants.NetworkTron, constants.KeyStatusRevoked
	for _, tc := range []struct {
		name   string
		filter ListKeysFilter
		want   int
	}{
		{"service", ListKeysFilter{Service: &service, Limit: 100}, 3},
		{"empty label", ListKeysFilter{Service: &service, Label: &empty, Limit: 100}, 1},
		{"label", ListKeysFilter{Service: &service, Label: &atoken, Limit: 100}, 2},
		{"network", ListKeysFilter{Service: &service, Network: &tron, Limit: 100}, 2},
		{"status", ListKeysFilter{Service: &service, Status: &revoked, Limit: 100}, 1},
		{"limit", ListKeysFilter{Service: &service, Limit: 1}, 1},
		{"offset", ListKeysFilter{Service: &service, Offset: 2, Limit: 100}, 1},
		{"offset past end", ListKeysFilter{Service: &service, Offset: 3, Limit: 100}, 0},
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

func TestIntegrationRotation(t *testing.T) {
	srv, pool := newIntegrationService(t)
	ctx := context.Background()

	role := createIntegrationRole(t, srv, uniqueService(), "gas_station", constants.NetworkEthereum, constants.RoleKindSingle, "")
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
		status               constants.KeyStatus
		replacedBy           *uuid.UUID
		rotatedAt, revokedAt *time.Time
	)
	if err := pool.QueryRow(ctx, `SELECT status, replaced_by, rotated_at, revoked_at FROM wallet_keys WHERE id = $1`, old.ID).
		Scan(&status, &replacedBy, &rotatedAt, &revokedAt); err != nil {
		t.Fatal(err)
	}
	if status != constants.KeyStatusRevoked || replacedBy == nil || *replacedBy != replacement.ID || rotatedAt == nil || revokedAt == nil {
		t.Fatalf("old key: status = %s, replaced_by = %v, rotated_at = %v, revoked_at = %v", status, replacedBy, rotatedAt, revokedAt)
	}

	active, err := srv.GetRoleKey(ctx, GetRoleKeyParams{Service: role.Service, Purpose: role.Purpose, Network: role.Network})
	if err != nil || active.ID != replacement.ID {
		t.Fatalf("active = %+v, err = %v", active, err)
	}
}
