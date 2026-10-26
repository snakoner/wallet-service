package key_storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

func (r *Repository) AcquireLease(
	ctx context.Context,
	tx db.DBTX,
	roleID, leaseID uuid.UUID,
	owner string,
	ttl time.Duration,
	exclude []uuid.UUID,
) (*WalletKey, error) {
	if exclude == nil {
		exclude = []uuid.UUID{}
	}

	var key WalletKey
	err := tx.QueryRow(ctx, `
		UPDATE wallet_keys
		SET 
			lease_id = $2,
			lease_owner = $3,
			leased_until = `+nowUTC+` + make_interval(secs => $4)
		WHERE id = (
			SELECT id
			FROM wallet_keys
			WHERE role_id = $1
				AND status = $5
				AND (leased_until IS NULL OR leased_until <= `+nowUTC+`)
				AND NOT (id = ANY($6))
			ORDER BY leased_until NULLS FIRST, created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, 
				role_id, 
				network, 
				alias, 
				address, 
				external_ref, 
				status, 
				replaced_by, 
				created_at, 
				updated_at, 
				lease_id, 
				lease_owner, 
				leased_until`,
		roleID,
		leaseID,
		owner,
		ttl.Seconds(),
		KeyStatusActive,
		exclude,
	).Scan(
		&key.ID,
		&key.RoleID,
		&key.Network,
		&key.Alias,
		&key.Address,
		&key.ExternalRef,
		&key.Status,
		&key.ReplacedBy,
		&key.CreatedAt,
		&key.UpdatedAt,
		&key.LeaseID,
		&key.LeaseOwner,
		&key.LeasedUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoFreeKey
	}

	if err != nil {
		return nil, fmt.Errorf("acquire lease: %w", err)
	}

	return &key, nil
}

func (r *Repository) ExtendLease(ctx context.Context, tx db.DBTX, leaseID uuid.UUID, service string, ttl time.Duration) (time.Time, error) {
	var leasedUntil time.Time
	err := tx.QueryRow(ctx, `
		UPDATE 
			wallet_keys k
		SET 
			leased_until = `+nowUTC+` + make_interval(secs => $3)
		FROM 
			wallet_roles r
		WHERE 
			r.id = k.role_id 
		AND 
			k.lease_id = $1 
		AND 
			r.service = $2 
		AND 
			k.leased_until > `+nowUTC+`
		RETURNING k.leased_until`,
		leaseID,
		service,
		ttl.Seconds(),
	).Scan(&leasedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrLeaseExpired
	}

	if err != nil {
		return time.Time{}, fmt.Errorf("extend lease: %w", err)
	}

	return leasedUntil, nil
}

// ReleaseLease keeps leased_until in the past, so the least recently used key is leased first.
func (r *Repository) ReleaseLease(ctx context.Context, tx db.DBTX, leaseID uuid.UUID, service string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE 
			wallet_keys k
		SET 
			lease_id = NULL, 
			lease_owner = NULL, 
			leased_until = LEAST(k.leased_until, `+nowUTC+`)
		FROM 
			wallet_roles r
		WHERE 
			r.id = k.role_id 
		AND 
			k.lease_id = $1 
		AND 
			r.service = $2`,
		leaseID,
		service,
	)
	if err != nil {
		return fmt.Errorf("release lease: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrLeaseExpired
	}

	return nil
}

func (r *Repository) ForceReleaseLease(ctx context.Context, tx db.DBTX, keyID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE 
			wallet_keys
		SET 
			lease_id = NULL, 
			lease_owner = NULL, 
			leased_until = LEAST(leased_until, `+nowUTC+`)
		WHERE 
			id = $1`,
		keyID,
	)
	if err != nil {
		return fmt.Errorf("force release lease: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}

	return nil
}
