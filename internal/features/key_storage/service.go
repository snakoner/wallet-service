package key_storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	aes256 "github.com/w3-developers/go-aes256"
	ethereumlib "github.com/w3-developers/go-ethereum-lib"
	tronlib "github.com/w3-developers/go-tron-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/policy"
)

const dekSize = 32

type repository interface {
	CreateRole(ctx context.Context, tx db.DBTX, role *Role) error
	GetRole(ctx context.Context, tx db.DBTX, service, purpose, label string, network constants.Network) (*Role, error)
	GetRoleByID(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Role, error)
	ListRoles(ctx context.Context, tx db.DBTX, service string) ([]Role, error)
	GetRoleForUpdate(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Role, error)
	HasActiveKey(ctx context.Context, tx db.DBTX, roleID uuid.UUID) (bool, error)
	CreateKey(ctx context.Context, tx db.DBTX, key *WalletKey) error
	GetKey(ctx context.Context, tx db.DBTX, id uuid.UUID, service string) (*WalletKey, error)
	GetActiveKey(ctx context.Context, tx db.DBTX, roleID uuid.UUID) (*WalletKey, error)
	GetKeyByExternalRef(ctx context.Context, tx db.DBTX, roleID uuid.UUID, externalRef string) (*WalletKey, error)
	ListRoleKeys(ctx context.Context, tx db.DBTX, roleID uuid.UUID) ([]WalletKey, error)
	ListKeys(ctx context.Context, tx db.DBTX, filter ListKeysFilter) ([]WalletKey, error)
	UpdateRolePolicy(ctx context.Context, tx db.DBTX, id uuid.UUID, policy json.RawMessage) error
	IsServiceAddress(ctx context.Context, tx db.DBTX, service string, network constants.Network, address, purpose string) (bool, error)
	GetKeyForUpdate(ctx context.Context, tx db.DBTX, id uuid.UUID) (*WalletKey, error)
	GetReplacedKeyForUpdate(ctx context.Context, tx db.DBTX, replacementID uuid.UUID) (*WalletKey, error)
	UpdateKeyStatus(ctx context.Context, tx db.DBTX, id uuid.UUID, status KeyStatus, now time.Time) error
	SetKeyReplacedBy(ctx context.Context, tx db.DBTX, id uuid.UUID, replacedBy *uuid.UUID, now time.Time) error
	AcquireLease(ctx context.Context, tx db.DBTX, roleID, leaseID uuid.UUID, owner string, ttl time.Duration, exclude []uuid.UUID) (*WalletKey, error)
	ExtendLease(ctx context.Context, tx db.DBTX, leaseID uuid.UUID, service string, ttl time.Duration) (time.Time, error)
	ReleaseLease(ctx context.Context, tx db.DBTX, leaseID uuid.UUID, service string) error
	ForceReleaseLease(ctx context.Context, tx db.DBTX, keyID uuid.UUID) error
}

type KekCipher interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

type networkKeys struct {
	generate func() (string, string, error)          // private key hex, address
	address  func(privateKey string) (string, error) // address of private key hex
}

var keysByNetwork = map[constants.Network]networkKeys{
	constants.NetworkEthereum: {ethereumlib.GenerateAddress, ethereumlib.PrivateKeyToAddress},
	constants.NetworkTron:     {tronlib.GenerateAddress, tronlib.PrivateKeyHexToAddressBase58},
	constants.NetworkArbitrum: {ethereumlib.GenerateAddress, ethereumlib.PrivateKeyToAddress},
}

type Service struct {
	log        *logger.Logger
	repo       repository
	txm        db.TxManager
	kek        KekCipher
	kekVersion int32
}

func NewService(
	log *logger.Logger,
	repo repository,
	txm db.TxManager,
	kek KekCipher,
	kekVersion int32,
) *Service {
	return &Service{
		log:        log,
		repo:       repo,
		txm:        txm,
		kek:        kek,
		kekVersion: kekVersion,
	}
}

func (s *Service) CreateRole(ctx context.Context, params CreateRoleParams) (*Role, error) {
	if strings.TrimSpace(params.Service) == "" || strings.TrimSpace(params.Purpose) == "" {
		return nil, ErrInvalidParams
	}

	if _, ok := keysByNetwork[params.Network]; !ok {
		return nil, ErrUnsupportedNetwork
	}

	if _, ok := RoleKindStrings[params.Kind]; !ok {
		return nil, ErrInvalidParams.WithMessage("unsupported role kind")
	}

	rolePolicy, err := policy.Parse(params.Policy, params.Network)
	if err != nil {
		return nil, ErrInvalidParams.WithMessage("invalid policy: " + err.Error())
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate role id: %w", err)
	}

	role := &Role{
		ID:        id,
		Service:   strings.TrimSpace(params.Service),
		Purpose:   strings.TrimSpace(params.Purpose),
		Label:     strings.TrimSpace(params.Label),
		Network:   params.Network,
		Kind:      params.Kind,
		Policy:    rolePolicy.JSON(),
		CreatedAt: time.Now().UTC(),
	}

	if err := s.repo.CreateRole(ctx, s.txm.GetPool(), role); err != nil {
		return nil, err
	}

	s.log.Info().
		Str("role_id", role.ID.String()).
		Str("service", role.Service).
		Str("purpose", role.Purpose).
		Str("label", role.Label).
		Str("network", role.Network.String()).
		Str("kind", role.Kind.String()).
		Msg("wallet role created")

	return role, nil
}

func (s *Service) ListRoles(ctx context.Context, service string) ([]Role, error) {
	return s.repo.ListRoles(ctx, s.txm.GetPool(), strings.TrimSpace(service))
}

func (s *Service) ListRoleKeys(ctx context.Context, roleID uuid.UUID) ([]WalletKey, error) {
	role, err := s.repo.GetRoleByID(ctx, s.txm.GetPool(), roleID)
	if err != nil {
		return nil, err
	}

	keys, err := s.repo.ListRoleKeys(ctx, s.txm.GetPool(), role.ID)
	if err != nil {
		return nil, err
	}

	for i := range keys {
		keys[i].Role = *role
	}

	return keys, nil
}

func (s *Service) UpdatePolicy(ctx context.Context, roleID uuid.UUID, raw json.RawMessage) (*Role, error) {
	role, err := s.repo.GetRoleByID(ctx, s.txm.GetPool(), roleID)
	if err != nil {
		return nil, err
	}

	rolePolicy, err := policy.Parse(raw, role.Network)
	if err != nil {
		return nil, ErrInvalidParams.WithMessage("invalid policy: " + err.Error())
	}

	role.Policy = rolePolicy.JSON()
	if err := s.repo.UpdateRolePolicy(ctx, s.txm.GetPool(), role.ID, role.Policy); err != nil {
		return nil, err
	}

	s.log.Info().
		Str("role_id", role.ID.String()).
		Str("service", role.Service).
		Str("purpose", role.Purpose).
		RawJSON("policy", role.Policy).
		Msg("wallet role policy updated")

	return role, nil
}

func (s *Service) IsServiceAddress(ctx context.Context, service string, network constants.Network, address, purpose string) (bool, error) {
	return s.repo.IsServiceAddress(ctx, s.txm.GetPool(), service, network, address, purpose)
}

const (
	defaultListKeysLimit = 100
	maxListKeysLimit     = 1000
)

func (s *Service) ListKeys(ctx context.Context, filter ListKeysFilter) ([]WalletKey, error) {
	if filter.Limit <= 0 {
		filter.Limit = defaultListKeysLimit
	}
	filter.Limit = min(filter.Limit, maxListKeysLimit)

	return s.repo.ListKeys(ctx, s.txm.GetPool(), filter)
}

func (s *Service) CreateKey(ctx context.Context, params CreateKeyParams) (*WalletKey, error) {
	return s.addKey(ctx, params.RoleID, params.Alias, params.ExternalRef, generateKey)
}

func (s *Service) ImportKey(ctx context.Context, params ImportKeyParams) (*WalletKey, error) {
	defer clear(params.PrivateKey)

	privateKey := bytes.TrimPrefix(bytes.TrimSpace(params.PrivateKey), []byte("0x"))
	if len(privateKey) != 64 {
		return nil, ErrInvalidParams.WithMessage("private key must be 32 bytes in hex")
	}

	return s.addKey(ctx, params.RoleID, params.Alias, params.ExternalRef, func(keys networkKeys) ([]byte, string, error) {
		address, err := keys.address(string(privateKey))
		if err != nil {
			return nil, "", ErrInvalidParams.WithMessage("invalid private key")
		}

		return bytes.Clone(privateKey), address, nil
	})
}

func (s *Service) addKey(
	ctx context.Context,
	roleID uuid.UUID,
	alias, externalRef *string,
	makeKey func(keys networkKeys) ([]byte, string, error),
) (*WalletKey, error) {
	var key *WalletKey
	err := s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
		role, err := s.repo.GetRoleForUpdate(ctx, tx, roleID)
		if err != nil {
			return err
		}

		if (role.Kind == RoleKindDeposit) != (externalRef != nil && strings.TrimSpace(*externalRef) != "") {
			return ErrInvalidParams.WithMessage("external_ref is required only for deposit role")
		}

		if role.Kind == RoleKindSingle {
			exists, err := s.repo.HasActiveKey(ctx, tx, role.ID)
			if err != nil {
				return err
			}

			if exists {
				return ErrActiveKeyExists
			}
		}

		key, err = s.insertKey(ctx, tx, role, alias, externalRef, KeyStatusActive, makeKey)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.logKey(key, "wallet key created")

	return key, nil
}

// Caller holds role lock.
func (s *Service) insertKey(
	ctx context.Context,
	tx db.DBTX,
	role *Role,
	alias, externalRef *string,
	status KeyStatus,
	makeKey func(keys networkKeys) ([]byte, string, error),
) (*WalletKey, error) {
	privateKey, address, err := makeKey(keysByNetwork[role.Network])
	if err != nil {
		return nil, err
	}

	keyCipher, dekCipher, err := s.encryptPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate key id: %w", err)
	}

	now := time.Now().UTC()
	key := &WalletKey{
		ID:          id,
		RoleID:      role.ID,
		Role:        *role,
		Network:     role.Network,
		Alias:       alias,
		Address:     address,
		ExternalRef: externalRef,
		Status:      status,
		KeyCipher:   keyCipher,
		DEKCipher:   dekCipher,
		KEKVersion:  s.kekVersion,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateKey(ctx, tx, key); err != nil {
		return nil, err
	}

	return key, nil
}

func generateKey(keys networkKeys) ([]byte, string, error) {
	privateKey, address, err := keys.generate()
	if err != nil {
		return nil, "", fmt.Errorf("generate address: %w", err)
	}

	return []byte(privateKey), address, nil
}

func (s *Service) RotateKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	var replacement *WalletKey
	err := s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
		old, err := s.repo.GetKeyForUpdate(ctx, tx, keyID)
		if err != nil {
			return err
		}

		if old.Role.Kind == RoleKindDeposit {
			return ErrInvalidParams.WithMessage("deposit keys are not rotated")
		}

		if old.Status != KeyStatusActive {
			return ErrInvalidKeyStatus.WithMessage("only active key can be rotated")
		}

		if old.ReplacedBy != nil {
			return ErrRotationInProgress
		}

		role, err := s.repo.GetRoleForUpdate(ctx, tx, old.RoleID)
		if err != nil {
			return err
		}

		replacement, err = s.insertKey(ctx, tx, role, old.Alias, nil, KeyStatusPending, generateKey)
		if err != nil {
			return err
		}

		return s.repo.SetKeyReplacedBy(ctx, tx, old.ID, &replacement.ID, replacement.CreatedAt)
	})
	if err != nil {
		return nil, err
	}

	s.logKey(replacement, "wallet key rotation started", "old_key_id", keyID.String())

	return replacement, nil
}

func (s *Service) ActivateKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	var key *WalletKey
	err := s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if key, err = s.repo.GetKeyForUpdate(ctx, tx, keyID); err != nil {
			return err
		}

		if key.Status != KeyStatusPending {
			return ErrInvalidKeyStatus.WithMessage("only pending key can be activated")
		}

		if _, err := s.repo.GetRoleForUpdate(ctx, tx, key.RoleID); err != nil {
			return err
		}

		now := time.Now().UTC()
		old, err := s.repo.GetReplacedKeyForUpdate(ctx, tx, key.ID)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}

		if old != nil && old.Status == KeyStatusActive {
			if err := s.repo.UpdateKeyStatus(ctx, tx, old.ID, KeyStatusRotating, now); err != nil {
				return err
			}
		}

		if key.Role.Kind == RoleKindSingle {
			exists, err := s.repo.HasActiveKey(ctx, tx, key.RoleID)
			if err != nil {
				return err
			}

			if exists {
				return ErrActiveKeyExists
			}
		}

		key.Status = KeyStatusActive
		return s.repo.UpdateKeyStatus(ctx, tx, key.ID, KeyStatusActive, now)
	})
	if err != nil {
		return nil, err
	}

	s.logKey(key, "wallet key activated")

	return key, nil
}

func (s *Service) RevokeKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	var key *WalletKey
	err := s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if key, err = s.repo.GetKeyForUpdate(ctx, tx, keyID); err != nil {
			return err
		}

		if key.Status == KeyStatusRevoked {
			return ErrInvalidKeyStatus.WithMessage("key is already revoked")
		}

		now := time.Now().UTC()
		if key.Status == KeyStatusPending {
			old, err := s.repo.GetReplacedKeyForUpdate(ctx, tx, key.ID)
			if err != nil && !errors.Is(err, ErrKeyNotFound) {
				return err
			}

			if old != nil {
				if err := s.repo.SetKeyReplacedBy(ctx, tx, old.ID, nil, now); err != nil {
					return err
				}
			}
		}

		key.Status = KeyStatusRevoked
		return s.repo.UpdateKeyStatus(ctx, tx, key.ID, KeyStatusRevoked, now)
	})
	if err != nil {
		return nil, err
	}

	s.logKey(key, "wallet key revoked")

	return key, nil
}

func (s *Service) DisableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	return s.switchKey(ctx, keyID, KeyStatusActive, KeyStatusDisabled, "wallet key disabled")
}

func (s *Service) EnableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	return s.switchKey(ctx, keyID, KeyStatusDisabled, KeyStatusActive, "wallet key enabled")
}

func (s *Service) switchKey(ctx context.Context, keyID uuid.UUID, from, to KeyStatus, message string) (*WalletKey, error) {
	var key *WalletKey
	err := s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if key, err = s.repo.GetKeyForUpdate(ctx, tx, keyID); err != nil {
			return err
		}

		if key.Status != from {
			return ErrInvalidKeyStatus.WithMessage("key must be " + from.String())
		}

		if _, err := s.repo.GetRoleForUpdate(ctx, tx, key.RoleID); err != nil {
			return err
		}

		if to == KeyStatusActive && key.Role.Kind == RoleKindSingle {
			exists, err := s.repo.HasActiveKey(ctx, tx, key.RoleID)
			if err != nil {
				return err
			}

			if exists {
				return ErrActiveKeyExists
			}
		}

		key.Status = to
		return s.repo.UpdateKeyStatus(ctx, tx, key.ID, to, time.Now().UTC())
	})
	if err != nil {
		return nil, err
	}

	s.logKey(key, message)

	return key, nil
}

func (s *Service) logKey(key *WalletKey, message string, fields ...string) {
	event := s.log.Info().
		Str("key_id", key.ID.String()).
		Str("role_id", key.RoleID.String()).
		Str("service", key.Role.Service).
		Str("purpose", key.Role.Purpose).
		Str("label", key.Role.Label).
		Str("network", key.Network.String()).
		Str("address", key.Address).
		Str("status", key.Status.String())
	for i := 0; i+1 < len(fields); i += 2 {
		event = event.Str(fields[i], fields[i+1])
	}
	event.Msg(message)
}

func (s *Service) GetRoleKeys(ctx context.Context, params GetRoleKeyParams) ([]WalletKey, error) {
	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind == RoleKindDeposit {
		return nil, ErrInvalidParams.WithMessage("deposit role keys are not listed")
	}

	keys, err := s.repo.ListRoleKeys(ctx, s.txm.GetPool(), role.ID)
	if err != nil {
		return nil, err
	}

	for i := range keys {
		keys[i].Role = *role
	}

	return keys, nil
}

func (s *Service) GetRoleKey(ctx context.Context, params GetRoleKeyParams) (*WalletKey, error) {
	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind != RoleKindSingle {
		return nil, ErrInvalidParams.WithMessage("role is not single")
	}

	key, err := s.repo.GetActiveKey(ctx, s.txm.GetPool(), role.ID)
	if err != nil {
		return nil, err
	}

	key.Role = *role

	return key, nil
}

// GetDepositKey returns key of own deposit role for external_ref, creating it on first call.
func (s *Service) GetDepositKey(ctx context.Context, params GetDepositKeyParams) (*WalletKey, error) {
	externalRef := strings.TrimSpace(params.ExternalRef)
	if externalRef == "" {
		return nil, ErrInvalidParams.WithMessage("external_ref is required")
	}

	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind != RoleKindDeposit {
		return nil, ErrInvalidParams.WithMessage("role is not deposit")
	}

	key, err := s.repo.GetKeyByExternalRef(ctx, s.txm.GetPool(), role.ID, externalRef)
	if err == nil {
		key.Role = *role
		return key, nil
	}

	if !errors.Is(err, ErrKeyNotFound) {
		return nil, err
	}

	key, err = s.addKey(ctx, role.ID, params.Alias, &externalRef, generateKey)
	if errors.Is(err, ErrKeyExists) {
		// concurrent request created key for same external_ref
		key, err = s.repo.GetKeyByExternalRef(ctx, s.txm.GetPool(), role.ID, externalRef)
		if err != nil {
			return nil, err
		}
		key.Role = *role
	}

	return key, err
}

func isSigningAllowed(status KeyStatus) bool {
	switch status {
	case KeyStatusActive, KeyStatusRotating, KeyStatusPending:
		return true
	}

	return false
}

func (s *Service) UseSigningKey(
	ctx context.Context,
	service string,
	keyID uuid.UUID,
	network constants.Network,
	leaseID *uuid.UUID,
	check func(key *WalletKey) error,
	fn func(key *WalletKey, privateKey []byte) error,
) error {
	key, err := s.repo.GetKey(ctx, s.txm.GetPool(), keyID, service)
	if err != nil {
		return err
	}

	if key.Network != network {
		return ErrInvalidParams.WithMessage("key network mismatch")
	}

	if !isSigningAllowed(key.Status) {
		return ErrKeyNotActive
	}

	if err := checkLease(key, leaseID); err != nil {
		return err
	}

	// before decryption: denied tx never touches private key
	if err := check(key); err != nil {
		return err
	}

	privateKey, err := s.decryptPrivateKey(key)
	if err != nil {
		return fmt.Errorf("decrypt private key: %w", err)
	}
	defer clear(privateKey)

	return fn(key, privateKey)
}

func (s *Service) encryptPrivateKey(privateKey []byte) ([]byte, []byte, error) {
	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return nil, nil, fmt.Errorf("generate dek: %w", err)
	}
	defer clear(dek)
	defer clear(privateKey)

	keyCipher, err := aes256.NewAES256GCM(dek).Encrypt(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt key: %w", err)
	}

	dekCipher, err := s.kek.Encrypt(dek)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt dek: %w", err)
	}

	return keyCipher, dekCipher, nil
}

// Caller must clear the returned private key.
func (s *Service) decryptPrivateKey(key *WalletKey) ([]byte, error) {
	if key.KEKVersion != s.kekVersion {
		return nil, fmt.Errorf("unknown kek version %d", key.KEKVersion)
	}

	dek, err := s.kek.Decrypt(key.DEKCipher)
	if err != nil {
		return nil, fmt.Errorf("decrypt dek: %w", err)
	}
	defer clear(dek)

	privateKey, err := aes256.NewAES256GCM(dek).Decrypt(key.KeyCipher)
	if err != nil {
		return nil, fmt.Errorf("decrypt key: %w", err)
	}

	return privateKey, nil
}

const (
	defaultLeaseTTL = time.Minute
	maxLeaseTTL     = 10 * time.Minute
)

func leaseTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultLeaseTTL
	}

	return min(ttl, maxLeaseTTL)
}

// AcquireLease gives free active key of own role to one service instance until lease expires or is released.
func (s *Service) AcquireLease(ctx context.Context, params AcquireLeaseParams) (*Lease, error) {
	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind == RoleKindDeposit {
		return nil, ErrInvalidParams.WithMessage("deposit keys are not leased")
	}

	leaseID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate lease id: %w", err)
	}

	key, err := s.repo.AcquireLease(ctx, s.txm.GetPool(), role.ID, leaseID, params.Owner, leaseTTL(params.TTL), params.Exclude)
	if err != nil {
		return nil, err
	}
	key.Role = *role

	s.logKey(key, "wallet key leased", "lease_id", leaseID.String(), "lease_owner", params.Owner)

	return &Lease{ID: leaseID, ExpiresAt: *key.LeasedUntil, Key: *key}, nil
}

func (s *Service) ExtendLease(ctx context.Context, service string, leaseID uuid.UUID, ttl time.Duration) (time.Time, error) {
	return s.repo.ExtendLease(ctx, s.txm.GetPool(), leaseID, service, leaseTTL(ttl))
}

func (s *Service) ReleaseLease(ctx context.Context, service string, leaseID uuid.UUID) error {
	return s.repo.ReleaseLease(ctx, s.txm.GetPool(), leaseID, service)
}

// ForceReleaseLease frees key held by stuck instance.
func (s *Service) ForceReleaseLease(ctx context.Context, keyID uuid.UUID) error {
	if err := s.repo.ForceReleaseLease(ctx, s.txm.GetPool(), keyID); err != nil {
		return err
	}

	s.log.Info().Str("key_id", keyID.String()).Msg("wallet key lease force released")

	return nil
}

// checkLease: pool key signs only with its live lease; any key under live lease signs only with that lease.
func checkLease(key *WalletKey, leaseID *uuid.UUID) error {
	switch {
	case key.Role.Kind == RoleKindPool && leaseID == nil:
		return ErrLeaseRequired
	case key.LeaseActive && leaseID == nil:
		return ErrKeyLeased
	case leaseID != nil && (!key.LeaseActive || key.LeaseID == nil || *key.LeaseID != *leaseID):
		return ErrLeaseExpired
	}

	return nil
}
