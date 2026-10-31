package admin_audit

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

func (r *Repository) CreateRecord(ctx context.Context, tx db.DBTX, record *Record) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO admin_audit (
			id,
			created_at,
			actor,
			source,
			action,
			service,
			role_id,
			key_id,
			service_client_id,
			details
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10)`,
		record.ID,
		record.CreatedAt,
		record.Actor,
		record.Source,
		record.Action,
		record.Service,
		record.RoleID,
		record.KeyID,
		record.ServiceClientID,
		record.Details,
	)
	if err != nil {
		return fmt.Errorf("insert admin audit: %w", err)
	}

	return nil
}

func listWhere(filter ListFilter) (string, []any) {
	conditions := []string{"TRUE"}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.ReplaceAll(condition, "$n", "$"+strconv.Itoa(len(args))))
	}

	if filter.Actor != "" {
		add("actor = $n", filter.Actor)
	}

	if filter.Action != "" {
		add("action = $n", filter.Action)
	}

	if filter.Service != "" {
		add("service = $n", filter.Service)
	}

	if filter.RoleID != uuid.Nil {
		add("role_id = $n", filter.RoleID)
	}

	// rotation and its cancel are recorded on one key, other one is in details
	if filter.KeyID != uuid.Nil {
		add("(key_id = $n OR details->>'replacement_key_id' = $n::text OR details->>'replaced_key_id' = $n::text)", filter.KeyID)
	}

	if filter.ServiceClientID != uuid.Nil {
		add("(service_client_id = $n OR details->>'old_service_client_id' = $n::text)", filter.ServiceClientID)
	}

	if !filter.From.IsZero() {
		add("created_at >= $n", filter.From.UTC())
	}

	if !filter.To.IsZero() {
		add("created_at < $n", filter.To.UTC())
	}

	return strings.Join(conditions, " AND "), args
}

func (r *Repository) CountRecords(ctx context.Context, tx db.DBTX, filter ListFilter) (int64, error) {
	where, args := listWhere(filter)

	var total int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit WHERE `+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count admin audit: %w", err)
	}

	return total, nil
}

func (r *Repository) ListRecords(ctx context.Context, tx db.DBTX, filter ListFilter) ([]Record, error) {
	where, args := listWhere(filter)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := tx.Query(ctx, `
		SELECT
			id,
			created_at,
			actor,
			source,
			action,
			COALESCE(service, ''),
			role_id,
			key_id,
			service_client_id,
			details
		FROM
			admin_audit
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("select admin audit: %w", err)
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		var record Record
		if err := rows.Scan(
			&record.ID,
			&record.CreatedAt,
			&record.Actor,
			&record.Source,
			&record.Action,
			&record.Service,
			&record.RoleID,
			&record.KeyID,
			&record.ServiceClientID,
			&record.Details,
		); err != nil {
			return nil, fmt.Errorf("scan admin audit: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin audit: %w", err)
	}

	return records, nil
}
