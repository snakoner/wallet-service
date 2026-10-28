//go:build integration

package key_storage

import (
	"context"
	"testing"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

// Other tests share database with keys on KEK version 1 and own random KEKs, so this test moves its keys
// from version 7 to version 1: rewrap selects only keys not on current version and does not touch keys of other tests.
func TestIntegrationRewrapKeys(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	log := logger.New(logger.Config{Level: "error"})
	txm := db.NewPgxTxManager(pool)
	oldKEK, newKEK := randomKEK(t), randomKEK(t)

	old := NewService(log, NewRepository(), txm, NewKEKs(map[int32][]byte{7: oldKEK}), 7)
	service := uniqueService()
	role := createIntegrationRole(t, old, service, "executor", constants.NetworkEthereum, constants.RoleKindPool, "")
	keys := make([]*WalletKey, 3)
	for i := range keys {
		key, err := old.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = key
	}

	// key locked by other operation is skipped, rewrap does not wait for it
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM wallet_keys WHERE id = $1 FOR UPDATE`, keys[0].ID); err != nil {
		t.Fatal(err)
	}
	srv := NewService(log, NewRepository(), txm, NewKEKs(map[int32][]byte{1: newKEK, 7: oldKEK}), 1)
	rewrapped, left, err := srv.RewrapKeys(ctx)
	if err != nil || rewrapped != 2 || left != 1 {
		t.Fatalf("with locked key: rewrapped = %d, left = %d, err = %v", rewrapped, left, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	rewrapped, left, err = srv.RewrapKeys(ctx)
	if err != nil || rewrapped != 1 || left != 0 {
		t.Fatalf("after unlock: rewrapped = %d, left = %d, err = %v", rewrapped, left, err)
	}

	// keys decrypt with new KEK alone
	onlyNew := NewService(log, NewRepository(), txm, NewKEKs(map[int32][]byte{1: newKEK}), 1)
	for _, key := range keys {
		got, err := onlyNew.repo.GetKey(ctx, pool, key.ID, service)
		if err != nil || got.KEKVersion != 1 {
			t.Fatalf("key %s: %+v, err = %v", key.ID, got, err)
		}
		privateKey, err := onlyNew.decryptPrivateKey(got)
		if err != nil {
			t.Fatalf("key %s: decrypt: %v", key.ID, err)
		}
		if address := addressOf(t, got.Network, privateKey); address != key.Address {
			t.Fatalf("key %s: address = %s, want %s", key.ID, address, key.Address)
		}
	}
}
