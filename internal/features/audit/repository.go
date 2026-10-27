package audit

import (
	"context"
	"fmt"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) CreateSigningRecord(ctx context.Context, tx db.DBTX, record *SigningRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO signing_audit (
			id,
			created_at,
			service_client_id,
			key_id,
			network,
			raw_tx,
			tx_hash,
			error_code
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		record.ID,
		record.CreatedAt,
		record.ServiceClientID,
		record.KeyID,
		record.Network,
		record.RawTx,
		record.TxHash,
		record.ErrorCode,
	)
	if err != nil {
		return fmt.Errorf("insert signing audit: %w", err)
	}

	return nil
}
