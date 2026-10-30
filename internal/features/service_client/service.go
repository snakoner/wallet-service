package service_client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

const keySize = 32

type repository interface {
	CreateClient(ctx context.Context, tx db.DBTX, client *ServiceClient) error
	GetActiveByKeyHash(ctx context.Context, tx db.DBTX, keyHash []byte, now time.Time) (*ServiceClient, error)
	GetPrimaryForUpdate(ctx context.Context, tx db.DBTX, service string) (*ServiceClient, error)
	ExpireClient(ctx context.Context, tx db.DBTX, id uuid.UUID, expiresAt time.Time) error
	SetReplacedBy(ctx context.Context, tx db.DBTX, id, replacedBy uuid.UUID) error
	RevokeClient(ctx context.Context, tx db.DBTX, id uuid.UUID, revokedAt time.Time) error
	ListClients(ctx context.Context, tx db.DBTX, service string) ([]ServiceClient, error)
}

type Service struct {
	log  *logger.Logger
	repo repository
	txm  db.TxManager
}

func NewService(log *logger.Logger, repo repository, txm db.TxManager) *Service {
	return &Service{log: log, repo: repo, txm: txm}
}

func (s *Service) CreateClient(ctx context.Context, service string) (string, *ServiceClient, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return "", nil, ErrInvalidService
	}

	key, client, err := newClient(service)
	if err != nil {
		return "", nil, err
	}

	if err := s.repo.CreateClient(ctx, s.txm.GetPool(), client); err != nil {
		return "", nil, err
	}

	s.log.Info().
		Str("service_client_id", client.ID.String()).
		Str("service", client.Service).
		Msg("service client created")

	return key, client, nil
}

func (s *Service) RotateClient(ctx context.Context, service string, grace time.Duration) (string, *ServiceClient, *ServiceClient, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return "", nil, nil, ErrInvalidService
	}

	if grace < 0 {
		return "", nil, nil, ErrInvalidGrace
	}

	key, client, err := newClient(service)
	if err != nil {
		return "", nil, nil, err
	}

	var old *ServiceClient
	err = s.txm.WithTx(ctx, func(ctx context.Context, tx db.DBTX) error {
		if old, err = s.repo.GetPrimaryForUpdate(ctx, tx, service); err != nil {
			return err
		}

		// old key stops being primary first, otherwise insert violates one primary per service
		expiresAt := client.CreatedAt.Add(grace)
		if err := s.repo.ExpireClient(ctx, tx, old.ID, expiresAt); err != nil {
			return err
		}
		old.ExpiresAt = &expiresAt

		if err := s.repo.CreateClient(ctx, tx, client); err != nil {
			return err
		}

		old.ReplacedBy = &client.ID
		return s.repo.SetReplacedBy(ctx, tx, old.ID, client.ID)
	})
	if err != nil {
		return "", nil, nil, err
	}

	s.log.Info().
		Str("service_client_id", client.ID.String()).
		Str("old_service_client_id", old.ID.String()).
		Str("service", service).
		Time("old_expires_at", *old.ExpiresAt).
		Msg("service client rotated")

	return key, client, old, nil
}

func (s *Service) RevokeClient(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.RevokeClient(ctx, s.txm.GetPool(), id, time.Now().UTC()); err != nil {
		return err
	}

	s.log.Info().Str("service_client_id", id.String()).Msg("service client revoked")

	return nil
}

func (s *Service) ListClients(ctx context.Context, service string) ([]ServiceClient, error) {
	return s.repo.ListClients(ctx, s.txm.GetPool(), strings.TrimSpace(service))
}

func (s *Service) Authenticate(ctx context.Context, key string) (*ServiceClient, error) {
	if key == "" {
		return nil, ErrUnauthorized
	}

	return s.repo.GetActiveByKeyHash(ctx, s.txm.GetPool(), hashKey(key), time.Now().UTC())
}

func hashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

func newClient(service string) (string, *ServiceClient, error) {
	raw := make([]byte, keySize)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate service key: %w", err)
	}

	key := hex.EncodeToString(raw)

	id, err := uuid.NewV7()
	if err != nil {
		return "", nil, fmt.Errorf("generate service client id: %w", err)
	}

	return key, &ServiceClient{
		ID:        id,
		Service:   service,
		KeyHash:   hashKey(key),
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	}, nil
}
