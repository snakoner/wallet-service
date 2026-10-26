package key_storage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type KeyStorageService interface {
	GetRoleKey(ctx context.Context, params GetRoleKeyParams) (*WalletKey, error)
	GetRoleKeys(ctx context.Context, params GetRoleKeyParams) ([]WalletKey, error)
	GetOrCreateDepositKey(ctx context.Context, params GetDepositKeyParams) (*WalletKey, error)
	AcquireLease(ctx context.Context, params AcquireLeaseParams) (*Lease, error)
	ExtendLease(ctx context.Context, service string, leaseID uuid.UUID, ttl time.Duration) (time.Time, error)
	ReleaseLease(ctx context.Context, service string, leaseID uuid.UUID) error
}

type Handler struct {
	service KeyStorageService
	log     *logger.Logger
}

func NewHandler(service KeyStorageService, log *logger.Logger) *Handler {
	return &Handler{service: service, log: log}
}
