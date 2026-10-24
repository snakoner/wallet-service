package key_storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
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
	ListRoleKeys(ctx context.Context, tx db.DBTX, roleID uuid.UUID) ([]WalletKey, error)
	ListKeys(ctx context.Context, tx db.DBTX, filter ListKeysFilter) ([]WalletKey, error)
	UpdateRolePolicy(ctx context.Context, tx db.DBTX, id uuid.UUID, policy json.RawMessage) error
	IsServiceAddress(ctx context.Context, tx db.DBTX, service string, network constants.Network, address, purpose string) (bool, error)
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

// UpdatePolicy replaces signing policy of role, it applies to all keys of role at once.
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

// IsServiceAddress reports whether address is a usable key of service in network; purpose "" - any purpose.
func (s *Service) IsServiceAddress(ctx context.Context, service string, network constants.Network, address, purpose string) (bool, error) {
	return s.repo.IsServiceAddress(ctx, s.txm.GetPool(), service, network, address, purpose)
}

const (
	defaultListKeysLimit = 100
	maxListKeysLimit     = 1000
)

// ListKeys returns keys of all services for admin, newest first.
func (s *Service) ListKeys(ctx context.Context, filter ListKeysFilter) ([]WalletKey, error) {
	if filter.Limit <= 0 {
		filter.Limit = defaultListKeysLimit
	}
	filter.Limit = min(filter.Limit, maxListKeysLimit)

	return s.repo.ListKeys(ctx, s.txm.GetPool(), filter)
}

// CreateKey generates new key of role.
func (s *Service) CreateKey(ctx context.Context, params CreateKeyParams) (*WalletKey, error) {
	return s.addKey(ctx, params.RoleID, params.Alias, params.ExternalRef, func(keys networkKeys) ([]byte, string, error) {
		privateKey, address, err := keys.generate()
		if err != nil {
			return nil, "", fmt.Errorf("generate address: %w", err)
		}

		return []byte(privateKey), address, nil
	})
}

// ImportKey adds existing private key to role, e.g. key moved from service env.
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

// addKey creates key in locked role: single role has one active key, deposit key has external_ref.
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

		privateKey, address, err := makeKey(keysByNetwork[role.Network])
		if err != nil {
			return err
		}

		keyCipher, dekCipher, err := s.encryptPrivateKey(privateKey)
		if err != nil {
			return fmt.Errorf("encrypt private key: %w", err)
		}

		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate key id: %w", err)
		}

		now := time.Now().UTC()
		key = &WalletKey{
			ID:          id,
			RoleID:      role.ID,
			Role:        *role,
			Network:     role.Network,
			Alias:       alias,
			Address:     address,
			ExternalRef: externalRef,
			Status:      KeyStatusActive,
			KeyCipher:   keyCipher,
			DEKCipher:   dekCipher,
			KEKVersion:  s.kekVersion,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		return s.repo.CreateKey(ctx, tx, key)
	})
	if err != nil {
		return nil, err
	}

	s.log.Info().
		Str("key_id", key.ID.String()).
		Str("role_id", key.RoleID.String()).
		Str("service", key.Role.Service).
		Str("purpose", key.Role.Purpose).
		Str("label", key.Role.Label).
		Str("network", key.Network.String()).
		Str("address", key.Address).
		Msg("wallet key created")

	return key, nil
}

// GetRoleKeys returns all keys of own single or pool role, e.g. executors. Deposit keys are looked up by external_ref.
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

// GetRoleKey returns active key of single role, e.g. gas station of service.
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

func isSigningAllowed(status KeyStatus) bool {
	switch status {
	case KeyStatusActive, KeyStatusRotating:
		return true
	}

	return false
}

func (s *Service) UseSigningKey(
	ctx context.Context,
	service string,
	keyID uuid.UUID,
	network constants.Network,
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

	// check runs before decryption: denied tx never touches private key
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

// encryptPrivateKey encrypts private key with a new DEK and DEK with KEK.
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

// decryptPrivateKey decrypts DEK with KEK and private key with DEK.
// Caller must clear the returned private key after use.
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
