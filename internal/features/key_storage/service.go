package key_storage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	aes256 "github.com/w3-developers/go-aes256"
	ethereumlib "github.com/w3-developers/go-ethereum-lib"
	tronlib "github.com/w3-developers/go-tron-lib"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
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
