package service_client

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
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
	if err != nil {
		return fmt.Errorf("insert service client: %w", err)
	}

	return nil
}

func (r *Repository) GetActiveByKeyHash(ctx context.Context, tx db.DBTX, keyHash []byte) (*ServiceClient, error) {
	var client ServiceClient
	err := tx.QueryRow(ctx, `
		SELECT id, service, key_hash, is_active, created_at, revoked_at
		FROM service_clients
		WHERE key_hash = $1 AND is_active`,
		keyHash,
	).Scan(
		&client.ID,
		&client.Service,
		&client.KeyHash,
		&client.IsActive,
		&client.CreatedAt,
		&client.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthorized
	}

	if err != nil {
		return nil, fmt.Errorf("select service client: %w", err)
	}

	return &client, nil
}
