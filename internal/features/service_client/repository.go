package service_client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

const primaryClientIndex = "service_clients_one_primary"

const clientColumns = `id, service, key_hash, is_active, created_at, revoked_at, expires_at, replaced_by`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func scanClient(row pgx.Row) (*ServiceClient, error) {
	var client ServiceClient
	err := row.Scan(
		&client.ID,
		&client.Service,
		&client.KeyHash,
		&client.IsActive,
		&client.CreatedAt,
		&client.RevokedAt,
		&client.ExpiresAt,
		&client.ReplacedBy,
	)
	if err != nil {
		return nil, err
	}

	return &client, nil
}

func (r *Repository) CreateClient(ctx context.Context, tx db.DBTX, client *ServiceClient) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO service_clients (
			id,
			service,
			key_hash,
			is_active,
			created_at
		) VALUES ($1, $2, $3, $4, $5)`,
		client.ID,
		client.Service,
		client.KeyHash,
		client.IsActive,
		client.CreatedAt,
	)
	if db.IsUniqueViolation(err, primaryClientIndex) {
		return ErrServiceClientExists
	}

	if err != nil {
		return fmt.Errorf("insert service client: %w", err)
	}

	return nil
}

// GetActiveByKeyHash returns client whose key is active and not expired at now.
func (r *Repository) GetActiveByKeyHash(ctx context.Context, tx db.DBTX, keyHash []byte, now time.Time) (*ServiceClient, error) {
	client, err := scanClient(tx.QueryRow(ctx, `
		SELECT `+clientColumns+`
		FROM service_clients
		WHERE key_hash = $1 AND is_active AND (expires_at IS NULL OR expires_at > $2)`,
		keyHash,
		now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthorized
	}

	if err != nil {
		return nil, fmt.Errorf("select service client: %w", err)
	}

	return client, nil
}

// GetPrimaryForUpdate locks primary key of service: active and without expiration.
func (r *Repository) GetPrimaryForUpdate(ctx context.Context, tx db.DBTX, service string) (*ServiceClient, error) {
	client, err := scanClient(tx.QueryRow(ctx, `
		SELECT `+clientColumns+`
		FROM service_clients
		WHERE service = $1 AND is_active AND expires_at IS NULL
		FOR UPDATE`,
		service,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceClientNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("select primary service client: %w", err)
	}

	return client, nil
}

func (r *Repository) ExpireClient(ctx context.Context, tx db.DBTX, id uuid.UUID, expiresAt time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE service_clients SET expires_at = $2 WHERE id = $1`, id, expiresAt); err != nil {
		return fmt.Errorf("expire service client: %w", err)
	}

	return nil
}

func (r *Repository) SetReplacedBy(ctx context.Context, tx db.DBTX, id, replacedBy uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE service_clients SET replaced_by = $2 WHERE id = $1`, id, replacedBy); err != nil {
		return fmt.Errorf("set service client replaced_by: %w", err)
	}

	return nil
}

func (r *Repository) RevokeClient(ctx context.Context, tx db.DBTX, id uuid.UUID, revokedAt time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE service_clients
		SET is_active = FALSE, revoked_at = $2
		WHERE id = $1 AND is_active`,
		id,
		revokedAt,
	)
	if err != nil {
		return fmt.Errorf("revoke service client: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrServiceClientNotFound
	}

	return nil
}

// ListClients returns clients of service, all clients when service is empty, newest first.
func (r *Repository) ListClients(ctx context.Context, tx db.DBTX, service string) ([]ServiceClient, error) {
	rows, err := tx.Query(ctx, `
		SELECT `+clientColumns+`
		FROM service_clients
		WHERE $1 = '' OR service = $1
		ORDER BY service, created_at DESC`,
		service,
	)
	if err != nil {
		return nil, fmt.Errorf("select service clients: %w", err)
	}
	defer rows.Close()

	clients := []ServiceClient{}
	for rows.Next() {
		client, err := scanClient(rows)
		if err != nil {
			return nil, fmt.Errorf("scan service client: %w", err)
		}

		clients = append(clients, *client)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service clients: %w", err)
	}

	return clients, nil
}
