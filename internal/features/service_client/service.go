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
	GetActiveByKeyHash(ctx context.Context, tx db.DBTX, keyHash []byte) (*ServiceClient, error)
}

type Service struct {
	log  *logger.Logger
	repo repository
	txm  db.TxManager
}

func NewService(log *logger.Logger, repo repository, txm db.TxManager) *Service {
	return &Service{log: log, repo: repo, txm: txm}
}

// CreateClient registers service and returns its X-Service-Key. The key is not stored and is shown only once.
func (s *Service) CreateClient(ctx context.Context, service string) (string, *ServiceClient, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return "", nil, ErrInvalidService
	}

	raw := make([]byte, keySize)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate service key: %w", err)
	}
	key := hex.EncodeToString(raw)

	id, err := uuid.NewV7()
	if err != nil {
		return "", nil, fmt.Errorf("generate service client id: %w", err)
	}

	client := &ServiceClient{
		ID:        id,
		Service:   service,
		KeyHash:   hashKey(key),
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
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

func (s *Service) Authenticate(ctx context.Context, key string) (*ServiceClient, error) {
	if key == "" {
		return nil, ErrUnauthorized
	}

	return s.repo.GetActiveByKeyHash(ctx, s.txm.GetPool(), hashKey(key))
}

func hashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}
