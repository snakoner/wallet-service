package key_storage

import (
	"context"
	"crypto/rand"
	"testing"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	adminaudit "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/admin_audit"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

func randomKEK(t *testing.T) []byte {
	t.Helper()
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	return kek
}

func TestRewrapKeys(t *testing.T) {
	ctx := adminaudit.WithActor(context.Background(), "test", adminaudit.SourceCLI)
	log := logger.New(logger.Config{Level: "error"})
	repo := &repoStub{}
	oldKEK, newKEK := randomKEK(t), randomKEK(t)

	// more keys than one batch
	old := NewService(log, repo, txmStub{}, NewKEKs(map[int32][]byte{1: oldKEK}), 1, &adminAuditStub{})
	role := createTestRole(t, old, constants.NetworkEthereum, constants.RoleKindPool)
	for range rewrapBatch + 5 {
		if _, err := old.CreateKey(ctx, CreateKeyParams{RoleID: role.ID}); err != nil {
			t.Fatal(err)
		}
	}

	// new key before rewrap gets current version
	srv := NewService(log, repo, txmStub{}, NewKEKs(map[int32][]byte{1: oldKEK, 2: newKEK}), 2, &adminAuditStub{})
	if key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID}); err != nil || key.KEKVersion != 2 {
		t.Fatalf("new key = %+v, err = %v", key, err)
	}

	// without old KEK rewrap stops on first old key
	onlyNew := NewService(log, repo, txmStub{}, NewKEKs(map[int32][]byte{2: newKEK}), 2, &adminAuditStub{})
	if rewrapped, _, err := onlyNew.RewrapKeys(ctx); err == nil || rewrapped != 0 {
		t.Fatalf("rewrap without old kek: rewrapped = %d, err = %v", rewrapped, err)
	}

	rewrapped, left, err := srv.RewrapKeys(ctx)
	if err != nil || rewrapped != rewrapBatch+5 || left != 0 {
		t.Fatalf("rewrapped = %d, left = %d, err = %v", rewrapped, left, err)
	}

	// every key decrypts with new KEK alone and gives its address
	for _, key := range repo.keys {
		privateKey, err := onlyNew.decryptPrivateKey(key)
		if err != nil || key.KEKVersion != 2 {
			t.Fatalf("key %s: version = %d, err = %v", key.ID, key.KEKVersion, err)
		}
		if address := addressOf(t, key.Network, privateKey); address != key.Address {
			t.Fatalf("key %s: address = %s, want %s", key.ID, address, key.Address)
		}
	}
	if _, err := old.decryptPrivateKey(repo.keys[0]); err == nil {
		t.Fatal("old KEK alone must not decrypt rewrapped key")
	}

	if rewrapped, left, err := srv.RewrapKeys(ctx); err != nil || rewrapped != 0 || left != 0 {
		t.Fatalf("second rewrap: rewrapped = %d, left = %d, err = %v", rewrapped, left, err)
	}
}
