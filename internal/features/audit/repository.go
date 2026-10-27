package audit

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

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

func (r *Repository) ListSigningRecords(ctx context.Context, tx db.DBTX, filter ListSigningFilter) ([]SigningRecord, error) {
	conditions := []string{"TRUE"}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	if filter.KeyID != uuid.Nil {
		add("a.key_id = $%d", filter.KeyID)
	}

	if filter.Service != "" {
		add("c.service = $%d", filter.Service)
	}

	if filter.TxHash != "" {
		add("a.tx_hash = $%d", filter.TxHash)
	}

	if filter.ErrorCode != "" {
		add("a.error_code = $%d", filter.ErrorCode)
	}

	if filter.Failed != nil {
		if *filter.Failed {
			conditions = append(conditions, "a.error_code IS NOT NULL")
		} else {
			conditions = append(conditions, "a.error_code IS NULL")
		}
	}

	// created_at is UTC without time zone
	if !filter.From.IsZero() {
		add("a.created_at >= $%d", filter.From.UTC())
	}

	if !filter.To.IsZero() {
		add("a.created_at < $%d", filter.To.UTC())
	}

	args = append(args, filter.Limit, filter.Offset)

	rows, err := tx.Query(ctx, `
		SELECT
			a.id,
			a.created_at,
			a.service_client_id,
			COALESCE(c.service, ''),
			a.key_id,
			a.network,
			a.raw_tx,
			a.tx_hash,
			a.error_code
		FROM 
			signing_audit a
		LEFT JOIN service_clients c ON c.id = a.service_client_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("select signing audit: %w", err)
	}
	defer rows.Close()

	records := []SigningRecord{}
	for rows.Next() {
		var record SigningRecord
		if err := rows.Scan(
			&record.ID,
			&record.CreatedAt,
			&record.ServiceClientID,
			&record.Service,
			&record.KeyID,
			&record.Network,
			&record.RawTx,
			&record.TxHash,
			&record.ErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan signing audit: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate signing audit: %w", err)
	}

	return records, nil
}
