package key_storage

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type AdminService interface {
	CreateRole(ctx context.Context, params CreateRoleParams) (*Role, error)
	ListRoles(ctx context.Context, service string) ([]Role, error)
	UpdatePolicy(ctx context.Context, roleID uuid.UUID, raw json.RawMessage) (*Role, error)
	CreateKey(ctx context.Context, params CreateKeyParams) (*WalletKey, error)
	ImportKey(ctx context.Context, params ImportKeyParams) (*WalletKey, error)
	ListRoleKeys(ctx context.Context, roleID uuid.UUID) ([]WalletKey, error)
	ListKeys(ctx context.Context, filter ListKeysFilter) ([]WalletKey, error)
	RotateKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	ActivateKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	RevokeKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	DisableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	EnableKey(ctx context.Context, keyID uuid.UUID) (*WalletKey, error)
	ForceReleaseLease(ctx context.Context, keyID uuid.UUID) error
}

type AdminHandler struct {
	service AdminService
	log     *logger.Logger
}

func NewAdminHandler(service AdminService, log *logger.Logger) *AdminHandler {
	return &AdminHandler{service: service, log: log}
}
