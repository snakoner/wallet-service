package key_storage

import (
	"context"
	"fmt"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

const rewrapBatch = 100

func (s *Service) RewrapKeys(ctx context.Context) (rewrapped int, left int64, err error) {
	for {
		var n int
		err = s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
			keys, err := s.repo.ListKeysToRewrap(ctx, tx, s.kekVersion, rewrapBatch)
			if err != nil {
				return err
			}

			for i := range keys {
				if err := s.rewrapKey(ctx, tx, &keys[i]); err != nil {
					return err
				}
			}
			n = len(keys)
			return nil
		})
		if err != nil {
			return rewrapped, 0, err
		}

		rewrapped += n
		if n < rewrapBatch {
			break
		}
	}

	left, err = s.repo.CountKeysToRewrap(ctx, s.txm.GetPool(), s.kekVersion)
	return rewrapped, left, err
}

func (s *Service) rewrapKey(ctx context.Context, tx db.DBTX, key *WalletKey) error {
	kek, ok := s.keks[key.KEKVersion]
	if !ok {
		return fmt.Errorf("key %s: unknown kek version %d", key.ID, key.KEKVersion)
	}

	dek, err := kek.Decrypt(key.DEKCipher)
	if err != nil {
		return fmt.Errorf("key %s: decrypt dek: %w", key.ID, err)
	}
	defer clear(dek)

	dekCipher, err := s.keks[s.kekVersion].Encrypt(dek)
	if err != nil {
		return fmt.Errorf("key %s: encrypt dek: %w", key.ID, err)
	}

	return s.repo.UpdateKeyDEK(ctx, tx, key.ID, dekCipher, s.kekVersion)
}
