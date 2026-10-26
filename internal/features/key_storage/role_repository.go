package key_storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

const roleColumns = `
	r.id,
	r.service,
	r.purpose,
	r.label,
	r.network,
	r.kind,
	r.policy,
	r.created_at`

func scanRole(row pgx.Row) (*Role, error) {
	var role Role
	err := row.Scan(
		&role.ID,
		&role.Service,
		&role.Purpose,
		&role.Label,
		&role.Network,
		&role.Kind,
		&role.Policy,
		&role.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("select wallet role: %w", err)
	}

	return &role, nil
}

func (r *Repository) CreateRole(ctx context.Context, tx db.DBTX, role *Role) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO wallet_roles (
			id,
			service,
			purpose,
			label,
			network,
			kind,
			policy,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		role.ID,
		role.Service,
		role.Purpose,
		role.Label,
		role.Network,
		role.Kind,
		role.Policy,
		role.CreatedAt,
	)
	if db.IsUniqueViolation(err, roleUniqueConstraint) {
		return ErrRoleExists
	}

	if err != nil {
		return fmt.Errorf("insert wallet role: %w", err)
	}

	return nil
}

func (r *Repository) GetRole(
	ctx context.Context,
	tx db.DBTX,
	service, purpose, label string,
	network constants.Network,
) (*Role, error) {
	return scanRole(tx.QueryRow(ctx, `
		SELECT`+roleColumns+`
		FROM 
			wallet_roles r
		WHERE 
			r.service = $1 
		AND 
			r.purpose = $2 
		AND 
			r.label = $3 
		AND 
			r.network = $4`,
		service,
		purpose,
		label,
		network,
	))
}

func (r *Repository) GetRoleByID(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Role, error) {
	return scanRole(tx.QueryRow(ctx, `
		SELECT`+roleColumns+`
		FROM 
			wallet_roles r
		WHERE 
			r.id = $1`,
		id,
	))
}

func (r *Repository) ListRoles(ctx context.Context, tx db.DBTX, service string) ([]Role, error) {
	rows, err := tx.Query(ctx, `
		SELECT`+roleColumns+`
		FROM 
			wallet_roles r
		WHERE 
			$1 = '' OR r.service = $1
		ORDER BY 
			r.service, r.purpose, r.label, r.network`,
		service,
	)
	if err != nil {
		return nil, fmt.Errorf("select wallet roles: %w", err)
	}
	defer rows.Close()

	roles := []Role{}
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}

		roles = append(roles, *role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wallet roles: %w", err)
	}

	return roles, nil
}

func (r *Repository) UpdateRolePolicy(ctx context.Context, tx db.DBTX, id uuid.UUID, policy json.RawMessage) error {
	tag, err := tx.Exec(ctx, `UPDATE wallet_roles SET policy = $2 WHERE id = $1`, id, policy)
	if err != nil {
		return fmt.Errorf("update wallet role policy: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}

	return nil
}

func (r *Repository) GetRoleForUpdate(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Role, error) {
	return scanRole(tx.QueryRow(ctx, `
		SELECT`+roleColumns+`
		FROM wallet_roles r
		WHERE r.id = $1
		FOR UPDATE`,
		id,
	))
}
