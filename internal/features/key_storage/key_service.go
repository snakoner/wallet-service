package key_storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

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

		if (role.Kind == constants.RoleKindDeposit) != (externalRef != nil && strings.TrimSpace(*externalRef) != "") {
			return ErrInvalidParams.WithMessage("external_ref is required only for deposit role")
		}

		if role.Kind == constants.RoleKindSingle {
			exists, err := s.repo.HasActiveKey(ctx, tx, role.ID)
			if err != nil {
				return err
			}

			if exists {
				return ErrActiveKeyExists
			}
		}

		key, err = s.insertKey(ctx, tx, role, alias, externalRef, constants.KeyStatusActive, makeKey)
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
	status constants.KeyStatus,
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

		if old.Role.Kind == constants.RoleKindDeposit {
			return ErrInvalidParams.WithMessage("deposit keys are not rotated")
		}

		if old.Status != constants.KeyStatusActive {
			return ErrInvalidKeyStatus.WithMessage("only active key can be rotated")
		}

		if old.ReplacedBy != nil {
			return ErrRotationInProgress
		}

		role, err := s.repo.GetRoleForUpdate(ctx, tx, old.RoleID)
		if err != nil {
			return err
		}

		replacement, err = s.insertKey(ctx, tx, role, old.Alias, nil, constants.KeyStatusPending, generateKey)
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

		if key.Status != constants.KeyStatusPending {
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

		if old != nil && old.Status == constants.KeyStatusActive {
			if err := s.repo.UpdateKeyStatus(ctx, tx, old.ID, constants.KeyStatusRotating, now); err != nil {
				return err
			}
		}

		if key.Role.Kind == constants.RoleKindSingle {
			exists, err := s.repo.HasActiveKey(ctx, tx, key.RoleID)
			if err != nil {
				return err
			}

			if exists {
				return ErrActiveKeyExists
			}
		}

		key.Status = constants.KeyStatusActive
		return s.repo.UpdateKeyStatus(ctx, tx, key.ID, constants.KeyStatusActive, now)
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

		if key.Status == constants.KeyStatusRevoked {
			return ErrInvalidKeyStatus.WithMessage("key is already revoked")
		}

		now := time.Now().UTC()
		if key.Status == constants.KeyStatusPending {
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

		key.Status = constants.KeyStatusRevoked
		return s.repo.UpdateKeyStatus(ctx, tx, key.ID, constants.KeyStatusRevoked, now)
	})
	if err != nil {
		return nil, err
	}

	s.logKey(key, "wallet key revoked")

	return key, nil
}

func (s *Service) DisableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	return s.switchKey(ctx, keyID, constants.KeyStatusActive, constants.KeyStatusDisabled, "wallet key disabled")
}

func (s *Service) EnableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error) {
	return s.switchKey(ctx, keyID, constants.KeyStatusDisabled, constants.KeyStatusActive, "wallet key enabled")
}

func (s *Service) switchKey(ctx context.Context, keyID uuid.UUID, from, to constants.KeyStatus, message string) (*WalletKey, error) {
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

		if to == constants.KeyStatusActive && key.Role.Kind == constants.RoleKindSingle {
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

func (s *Service) GetRoleKeys(ctx context.Context, params GetRoleKeyParams) ([]WalletKey, error) {
	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind == constants.RoleKindDeposit {
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

	if role.Kind != constants.RoleKindSingle {
		return nil, ErrInvalidParams.WithMessage("role is not single")
	}

	key, err := s.repo.GetActiveKey(ctx, s.txm.GetPool(), role.ID)
	if err != nil {
		return nil, err
	}

	key.Role = *role

	return key, nil
}

// GetOrCreateDepositKey returns key of own deposit role for external_ref, creating it on first call.
func (s *Service) GetOrCreateDepositKey(ctx context.Context, params GetDepositKeyParams) (*WalletKey, error) {
	externalRef := strings.TrimSpace(params.ExternalRef)
	if externalRef == "" {
		return nil, ErrInvalidParams.WithMessage("external_ref is required")
	}

	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind != constants.RoleKindDeposit {
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
		// concurrent
		key, err = s.repo.GetKeyByExternalRef(ctx, s.txm.GetPool(), role.ID, externalRef)
		if err != nil {
			return nil, err
		}

		key.Role = *role
	}

	return key, err
}

func isSigningAllowed(status constants.KeyStatus) bool {
	switch status {
	case constants.KeyStatusActive, constants.KeyStatusRotating, constants.KeyStatusPending:
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
