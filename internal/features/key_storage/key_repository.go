package key_storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

func (r *Repository) IsServiceAddress(
	ctx context.Context,
	tx db.DBTX,
	service string,
	network constants.Network,
	address, purpose string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM wallet_keys k
			JOIN wallet_roles r ON r.id = k.role_id
			WHERE r.service = $1
				AND k.network = $2
				AND k.address = $3
				AND k.status IN ($4, $5, $6)
				AND ($7 = '' OR r.purpose = $7)
		)`,
		service,
		network,
		address,
		KeyStatusActive,
		KeyStatusRotating,
		KeyStatusPending,
		purpose,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("select service address: %w", err)
	}

	return exists, nil
}

func (r *Repository) HasActiveKey(ctx context.Context, tx db.DBTX, roleID uuid.UUID) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM wallet_keys WHERE role_id = $1 AND status = $2)`,
		roleID,
		KeyStatusActive,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("select active wallet key: %w", err)
	}

	return exists, nil
}

func (r *Repository) CreateKey(ctx context.Context, tx db.DBTX, key *WalletKey) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO wallet_keys (
			id,
			role_id,
			network,
			alias,
			address,
			external_ref,
			status,
			key_cipher,
			dek_cipher,
			kek_version,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		key.ID,
		key.RoleID,
		key.Network,
		key.Alias,
		key.Address,
		key.ExternalRef,
		key.Status,
		key.KeyCipher,
		key.DEKCipher,
		key.KEKVersion,
		key.CreatedAt,
		key.UpdatedAt,
	)
	if db.IsUniqueViolation(err, keyAddressUniqueConstraint) || db.IsUniqueViolation(err, keyExternalRefUniqueConstraint) {
		return ErrKeyExists
	}

	if err != nil {
		return fmt.Errorf("insert wallet key: %w", err)
	}

	return nil
}

func (r *Repository) GetKey(ctx context.Context, tx db.DBTX, id uuid.UUID, service string) (*WalletKey, error) {
	var key WalletKey
	err := tx.QueryRow(ctx, `
		SELECT
			k.id,
			k.role_id,
			k.network,
			k.alias,
			k.address,
			k.external_ref,
			k.status,
			k.key_cipher,
			k.dek_cipher,
			k.kek_version,
			k.replaced_by,
			k.created_at,
			k.updated_at,
			k.rotated_at,
			k.revoked_at,
			rk.address,
			k.lease_id,
			k.lease_id IS NOT NULL AND k.leased_until > `+nowUTC+`,`+roleColumns+`
		FROM 
			wallet_keys k
		JOIN wallet_roles r ON r.id = k.role_id
		LEFT JOIN wallet_keys rk ON rk.id = k.replaced_by
		WHERE 
			k.id = $1 
		AND 
			r.service = $2`,
		id,
		service,
	).Scan(
		&key.ID,
		&key.RoleID,
		&key.Network,
		&key.Alias,
		&key.Address,
		&key.ExternalRef,
		&key.Status,
		&key.KeyCipher,
		&key.DEKCipher,
		&key.KEKVersion,
		&key.ReplacedBy,
		&key.CreatedAt,
		&key.UpdatedAt,
		&key.RotatedAt,
		&key.RevokedAt,
		&key.ReplacementAddress,
		&key.LeaseID,
		&key.LeaseActive,
		&key.Role.ID,
		&key.Role.Service,
		&key.Role.Purpose,
		&key.Role.Label,
		&key.Role.Network,
		&key.Role.Kind,
		&key.Role.Policy,
		&key.Role.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKeyNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("select wallet key: %w", err)
	}

	return &key, nil
}

func (r *Repository) GetActiveKey(ctx context.Context, tx db.DBTX, roleID uuid.UUID) (*WalletKey, error) {
	var key WalletKey
	err := tx.QueryRow(ctx, `
		SELECT
			id,
			role_id,
			network,
			alias,
			address,
			external_ref,
			status,
			kek_version,
			replaced_by,
			created_at,
			updated_at,
			rotated_at,
			revoked_at
		FROM 
			wallet_keys
		WHERE 
			role_id = $1 
		AND 
			status = $2`,
		roleID,
		KeyStatusActive,
	).Scan(
		&key.ID,
		&key.RoleID,
		&key.Network,
		&key.Alias,
		&key.Address,
		&key.ExternalRef,
		&key.Status,
		&key.KEKVersion,
		&key.ReplacedBy,
		&key.CreatedAt,
		&key.UpdatedAt,
		&key.RotatedAt,
		&key.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKeyNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("select active wallet key: %w", err)
	}

	return &key, nil
}

func (r *Repository) GetKeyByExternalRef(ctx context.Context, tx db.DBTX, roleID uuid.UUID, externalRef string) (*WalletKey, error) {
	var key WalletKey
	err := tx.QueryRow(ctx, `
		SELECT
			id,
			role_id,
			network,
			alias,
			address,
			external_ref,
			status,
			kek_version,
			replaced_by,
			created_at,
			updated_at,
			rotated_at,
			revoked_at
		FROM 
			wallet_keys
		WHERE 
			role_id = $1 
		AND 
			external_ref = $2`,
		roleID,
		externalRef,
	).Scan(
		&key.ID,
		&key.RoleID,
		&key.Network,
		&key.Alias,
		&key.Address,
		&key.ExternalRef,
		&key.Status,
		&key.KEKVersion,
		&key.ReplacedBy,
		&key.CreatedAt,
		&key.UpdatedAt,
		&key.RotatedAt,
		&key.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKeyNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("select wallet key by external ref: %w", err)
	}

	return &key, nil
}

func (r *Repository) ListRoleKeys(ctx context.Context, tx db.DBTX, roleID uuid.UUID) ([]WalletKey, error) {
	rows, err := tx.Query(ctx, `
		SELECT
			id,
			role_id,
			network,
			alias,
			address,
			external_ref,
			status,
			kek_version,
			replaced_by,
			created_at,
			updated_at,
			rotated_at,
			revoked_at,
			lease_owner,
			leased_until
		FROM 
			wallet_keys
		WHERE 
			role_id = $1
		ORDER BY 
			created_at, id`,
		roleID,
	)
	if err != nil {
		return nil, fmt.Errorf("select wallet keys: %w", err)
	}
	defer rows.Close()

	keys := []WalletKey{}
	for rows.Next() {
		var key WalletKey
		if err := rows.Scan(
			&key.ID,
			&key.RoleID,
			&key.Network,
			&key.Alias,
			&key.Address,
			&key.ExternalRef,
			&key.Status,
			&key.KEKVersion,
			&key.ReplacedBy,
			&key.CreatedAt,
			&key.UpdatedAt,
			&key.RotatedAt,
			&key.RevokedAt,
			&key.LeaseOwner,
			&key.LeasedUntil,
		); err != nil {
			return nil, fmt.Errorf("scan wallet key: %w", err)
		}

		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wallet keys: %w", err)
	}

	return keys, nil
}

func (r *Repository) ListKeys(ctx context.Context, tx db.DBTX, filter ListKeysFilter) ([]WalletKey, error) {
	conditions := []string{"TRUE"}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}

	if filter.Service != nil {
		add("r.service = $%d", *filter.Service)
	}

	if filter.Purpose != nil {
		add("r.purpose = $%d", *filter.Purpose)
	}

	if filter.Label != nil {
		add("r.label = $%d", *filter.Label)
	}

	if filter.Network != nil {
		add("k.network = $%d", *filter.Network)
	}

	if filter.Status != nil {
		add("k.status = $%d", *filter.Status)
	}

	if filter.ExternalRef != nil {
		add("k.external_ref = $%d", *filter.ExternalRef)
	}

	args = append(args, filter.Limit)

	rows, err := tx.Query(ctx, `
		SELECT
			k.id,
			k.role_id,
			k.network,
			k.alias,
			k.address,
			k.external_ref,
			k.status,
			k.kek_version,
			k.replaced_by,
			k.created_at,
			k.updated_at,
			k.rotated_at,
			k.revoked_at,
			k.lease_owner,
			k.leased_until,`+roleColumns+`
		FROM 
			wallet_keys k
		JOIN wallet_roles r ON r.id = k.role_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY k.created_at DESC, k.id
		LIMIT $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("select wallet keys: %w", err)
	}
	defer rows.Close()

	keys := []WalletKey{}
	for rows.Next() {
		var key WalletKey
		if err := rows.Scan(
			&key.ID,
			&key.RoleID,
			&key.Network,
			&key.Alias,
			&key.Address,
			&key.ExternalRef,
			&key.Status,
			&key.KEKVersion,
			&key.ReplacedBy,
			&key.CreatedAt,
			&key.UpdatedAt,
			&key.RotatedAt,
			&key.RevokedAt,
			&key.LeaseOwner,
			&key.LeasedUntil,
			&key.Role.ID,
			&key.Role.Service,
			&key.Role.Purpose,
			&key.Role.Label,
			&key.Role.Network,
			&key.Role.Kind,
			&key.Role.Policy,
			&key.Role.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wallet key: %w", err)
		}

		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wallet keys: %w", err)
	}

	return keys, nil
}

func (r *Repository) GetKeyForUpdate(ctx context.Context, tx db.DBTX, id uuid.UUID) (*WalletKey, error) {
	return scanLockedKey(tx.QueryRow(ctx, `
		SELECT
			k.id,
			k.role_id,
			k.network,
			k.alias,
			k.address,
			k.status,
			k.replaced_by,`+roleColumns+`
		FROM 
			wallet_keys k
		JOIN 
			wallet_roles r ON r.id = k.role_id
		WHERE 
			k.id = $1
		FOR UPDATE OF k`,
		id,
	))
}

func (r *Repository) GetReplacedKeyForUpdate(ctx context.Context, tx db.DBTX, replacementID uuid.UUID) (*WalletKey, error) {
	return scanLockedKey(tx.QueryRow(ctx, `
		SELECT
			k.id,
			k.role_id,
			k.network,
			k.alias,
			k.address,
			k.status,
			k.replaced_by,`+roleColumns+`
		FROM 
			wallet_keys k
		JOIN 
			wallet_roles r ON r.id = k.role_id
		WHERE
			k.replaced_by = $1
		FOR UPDATE OF k`,
		replacementID,
	))
}

func scanLockedKey(row pgx.Row) (*WalletKey, error) {
	var key WalletKey
	err := row.Scan(
		&key.ID,
		&key.RoleID,
		&key.Network,
		&key.Alias,
		&key.Address,
		&key.Status,
		&key.ReplacedBy,
		&key.Role.ID,
		&key.Role.Service,
		&key.Role.Purpose,
		&key.Role.Label,
		&key.Role.Network,
		&key.Role.Kind,
		&key.Role.Policy,
		&key.Role.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKeyNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("select wallet key: %w", err)
	}

	return &key, nil
}

func (r *Repository) UpdateKeyStatus(ctx context.Context, tx db.DBTX, id uuid.UUID, status KeyStatus, now time.Time) error {
	var rotatedAt, revokedAt *time.Time
	switch status {
	case KeyStatusRotating:
		rotatedAt = &now
	case KeyStatusRevoked:
		revokedAt = &now
	}

	if _, err := tx.Exec(ctx, `
		UPDATE 
			wallet_keys
		SET 
			status = $2,
			updated_at = $3,
			rotated_at = COALESCE($4, rotated_at),
			revoked_at = COALESCE($5, revoked_at)
		WHERE 
			id = $1`,
		id,
		status,
		now,
		rotatedAt,
		revokedAt,
	); err != nil {
		return fmt.Errorf("update wallet key status: %w", err)
	}

	return nil
}

func (r *Repository) SetKeyReplacedBy(ctx context.Context, tx db.DBTX, id uuid.UUID, replacedBy *uuid.UUID, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE wallet_keys SET replaced_by = $2, updated_at = $3 WHERE id = $1`, id, replacedBy, now); err != nil {
		return fmt.Errorf("set wallet key replaced_by: %w", err)
	}

	return nil
}
