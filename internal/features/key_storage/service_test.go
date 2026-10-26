package key_storage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	aes256 "github.com/w3-developers/go-aes256"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type repoStub struct {
	roles      []*Role
	keys       []*WalletKey
	listFilter ListKeysFilter
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
		usable := key.Status == constants.KeyStatusActive || key.Status == constants.KeyStatusRotating || key.Status == constants.KeyStatusPending
		if usable && key.Role.Service == service && key.Network == network && key.Address == address && (purpose == "" || key.Role.Purpose == purpose) {
			return true, nil
		}
	}
	return false, nil
}

func (r *repoStub) ListKeys(_ context.Context, _ db.DBTX, filter ListKeysFilter) ([]WalletKey, error) {
	r.listFilter = filter
	keys := []WalletKey{}
	for _, key := range r.keys {
		if (filter.Service == nil || key.Role.Service == *filter.Service) &&
			(filter.Purpose == nil || key.Role.Purpose == *filter.Purpose) &&
			(filter.Label == nil || key.Role.Label == *filter.Label) &&
			(filter.Network == nil || key.Network == *filter.Network) &&
			(filter.Status == nil || key.Status == *filter.Status) {
			keys = append(keys, *key)
		}
	}
	return keys, nil
}

func (r *repoStub) AcquireLease(_ context.Context, _ db.DBTX, roleID, leaseID uuid.UUID, owner string, ttl time.Duration, exclude []uuid.UUID) (*WalletKey, error) {
	now := time.Now().UTC()
	for _, key := range r.keys {
		excluded := false
		for _, id := range exclude {
			excluded = excluded || id == key.ID
		}
		if key.RoleID != roleID || key.Status != constants.KeyStatusActive || excluded || (key.LeasedUntil != nil && key.LeasedUntil.After(now)) {
			continue
		}
		until := now.Add(ttl)
		key.LeaseID, key.LeaseOwner, key.LeasedUntil = &leaseID, &owner, &until
		return key, nil
	}
	return nil, ErrNoFreeKey
}

func (r *repoStub) leased(leaseID uuid.UUID) *WalletKey {
	for _, key := range r.keys {
		if key.LeaseID != nil && *key.LeaseID == leaseID {
			return key
		}
	}
	return nil
}

func (r *repoStub) ExtendLease(_ context.Context, _ db.DBTX, leaseID uuid.UUID, _ string, ttl time.Duration) (time.Time, error) {
	key := r.leased(leaseID)
	if key == nil || !key.LeasedUntil.After(time.Now().UTC()) {
		return time.Time{}, ErrLeaseExpired
	}
	until := time.Now().UTC().Add(ttl)
	key.LeasedUntil = &until
	return until, nil
}

func (r *repoStub) ReleaseLease(_ context.Context, _ db.DBTX, leaseID uuid.UUID, _ string) error {
	key := r.leased(leaseID)
	if key == nil {
		return ErrLeaseExpired
	}
	now := time.Now().UTC()
	key.LeaseID, key.LeaseOwner, key.LeasedUntil = nil, nil, &now
	return nil
}

func (r *repoStub) ForceReleaseLease(_ context.Context, _ db.DBTX, keyID uuid.UUID) error {
	key := r.findKey(keyID)
	if key == nil {
		return ErrKeyNotFound
	}
	key.LeaseID, key.LeaseOwner = nil, nil
	return nil
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
		if key.RoleID == roleID && key.Status == constants.KeyStatusActive {
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
			key.ReplacementAddress = nil
			key.LeaseActive = key.LeaseID != nil && key.LeasedUntil != nil && key.LeasedUntil.After(time.Now().UTC())
			if key.ReplacedBy != nil {
				key.ReplacementAddress = &r.findKey(*key.ReplacedBy).Address
			}
			return key, nil
		}
	}
	return nil, ErrKeyNotFound
}

func (r *repoStub) findKey(id uuid.UUID) *WalletKey {
	for _, key := range r.keys {
		if key.ID == id {
			return key
		}
	}
	return nil
}

func (r *repoStub) GetKeyForUpdate(_ context.Context, _ db.DBTX, id uuid.UUID) (*WalletKey, error) {
	if key := r.findKey(id); key != nil {
		return key, nil
	}
	return nil, ErrKeyNotFound
}

func (r *repoStub) GetReplacedKeyForUpdate(_ context.Context, _ db.DBTX, replacementID uuid.UUID) (*WalletKey, error) {
	for _, key := range r.keys {
		if key.ReplacedBy != nil && *key.ReplacedBy == replacementID {
			return key, nil
		}
	}
	return nil, ErrKeyNotFound
}

func (r *repoStub) UpdateKeyStatus(_ context.Context, _ db.DBTX, id uuid.UUID, status constants.KeyStatus, now time.Time) error {
	key := r.findKey(id)
	key.Status = status
	if status == constants.KeyStatusRotating {
		key.RotatedAt = &now
	}
	if status == constants.KeyStatusRevoked {
		key.RevokedAt = &now
	}
	return nil
}

func (r *repoStub) SetKeyReplacedBy(_ context.Context, _ db.DBTX, id uuid.UUID, replacedBy *uuid.UUID, _ time.Time) error {
	r.findKey(id).ReplacedBy = replacedBy
	return nil
}

func (r *repoStub) GetActiveKey(_ context.Context, _ db.DBTX, roleID uuid.UUID) (*WalletKey, error) {
	for _, key := range r.keys {
		if key.RoleID == roleID && key.Status == constants.KeyStatusActive {
			return key, nil
		}
	}
	return nil, ErrKeyNotFound
}

func (r *repoStub) GetKeyByExternalRef(_ context.Context, _ db.DBTX, roleID uuid.UUID, externalRef string) (*WalletKey, error) {
	for _, key := range r.keys {
		if key.RoleID == roleID && key.ExternalRef != nil && *key.ExternalRef == externalRef {
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

func createTestRole(t *testing.T, srv *Service, network constants.Network, kind constants.RoleKind) *Role {
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

func TestDecryptPrivateKeyErrors(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
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
