package key_storage

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/validation"
)

type adminServiceStub struct {
	importParams ImportKeyParams
	keyID        uuid.UUID
	keysFilter   ListKeysFilter
	policy       json.RawMessage
	roleParams   CreateRoleParams
	keyParams    CreateKeyParams
	service      string
	roleID       uuid.UUID
	err          error
}

func (s *adminServiceStub) CreateRole(_ context.Context, params CreateRoleParams) (*Role, error) {
	s.roleParams = params
	if s.err != nil {
		return nil, s.err
	}
	return &Role{ID: uuid.New(), Service: params.Service, Purpose: params.Purpose, Label: params.Label, Network: params.Network, Kind: params.Kind, Policy: json.RawMessage(`{}`), CreatedAt: time.Now()}, nil
}

func (s *adminServiceStub) UpdatePolicy(_ context.Context, roleID uuid.UUID, raw json.RawMessage) (*Role, error) {
	s.roleID, s.policy = roleID, raw
	if s.err != nil {
		return nil, s.err
	}
	return &Role{ID: roleID, Service: "exchange", Network: constants.NetworkTron, Kind: constants.RoleKindSingle, Policy: raw}, nil
}

func (s *adminServiceStub) ListKeys(_ context.Context, filter ListKeysFilter) ([]WalletKey, error) {
	s.keysFilter = filter
	if s.err != nil {
		return nil, s.err
	}
	return []WalletKey{{ID: uuid.New(), Role: Role{Service: "exchange", Purpose: "executor", Kind: constants.RoleKindPool}, Network: constants.NetworkEthereum, Address: "0xA", Status: constants.KeyStatusActive, KeyCipher: []byte("secret")}}, nil
}

func (s *adminServiceStub) keyAction(keyID uuid.UUID, status constants.KeyStatus) (*WalletKey, error) {
	s.keyID = keyID
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{ID: keyID, Role: Role{Service: "exchange", Kind: constants.RoleKindSingle}, Network: constants.NetworkEthereum, Address: "0xA", Status: status, KeyCipher: []byte("secret")}, nil
}

func (s *adminServiceStub) ImportKey(_ context.Context, params ImportKeyParams) (*WalletKey, error) {
	s.importParams = params
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{ID: uuid.New(), RoleID: params.RoleID, Network: constants.NetworkEthereum, Address: "0xA", KeyCipher: []byte("secret")}, nil
}

func (s *adminServiceStub) ForceReleaseLease(_ context.Context, id uuid.UUID) error {
	s.keyID = id
	return s.err
}

func (s *adminServiceStub) RotateKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, constants.KeyStatusPending)
}

func (s *adminServiceStub) ActivateKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, constants.KeyStatusActive)
}

func (s *adminServiceStub) RevokeKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, constants.KeyStatusRevoked)
}

func (s *adminServiceStub) DisableKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, constants.KeyStatusDisabled)
}

func (s *adminServiceStub) EnableKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, constants.KeyStatusActive)
}

func (s *adminServiceStub) ListRoles(_ context.Context, service string) ([]Role, error) {
	s.service = service
	return []Role{{ID: uuid.New(), Service: "exchange", Purpose: "gas_station", Network: constants.NetworkTron, Kind: constants.RoleKindSingle}}, s.err
}

func (s *adminServiceStub) CreateKey(_ context.Context, params CreateKeyParams) (*WalletKey, error) {
	s.keyParams = params
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{ID: uuid.New(), RoleID: params.RoleID, Role: Role{Service: "exchange", Kind: constants.RoleKindDeposit}, Network: constants.NetworkEthereum, Address: "0xAddr", ExternalRef: params.ExternalRef, KeyCipher: []byte("secret")}, nil
}

func (s *adminServiceStub) ListRoleKeys(_ context.Context, roleID uuid.UUID) ([]WalletKey, error) {
	s.roleID = roleID
	if s.err != nil {
		return nil, s.err
	}
	return []WalletKey{{ID: uuid.New(), RoleID: roleID, Network: constants.NetworkTron, Address: "TAddr", KeyCipher: []byte("secret")}}, nil
}

func doAdmin(t *testing.T, svc *adminServiceStub, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := validation.Register(); err != nil {
		t.Fatal(err)
	}
	h := NewAdminHandler(svc, logger.New(logger.Config{Level: "error"}))
	r := gin.New()
	r.POST("/api/v1/admin/roles", h.CreateRole)
	r.GET("/api/v1/admin/roles", h.ListRoles)
	r.PUT("/api/v1/admin/roles/:id/policy", h.UpdatePolicy)
	r.POST("/api/v1/admin/roles/:id/keys", h.CreateKey)
	r.POST("/api/v1/admin/roles/:id/keys/import", h.ImportKey)
	r.GET("/api/v1/admin/roles/:id/keys", h.ListRoleKeys)
	r.GET("/api/v1/admin/keys", h.ListKeys)
	r.POST("/api/v1/admin/keys/:id/rotate", h.RotateKey)
	r.POST("/api/v1/admin/keys/:id/activate", h.ActivateKey)
	r.POST("/api/v1/admin/keys/:id/revoke", h.RevokeKey)
	r.POST("/api/v1/admin/keys/:id/disable", h.DisableKey)
	r.POST("/api/v1/admin/keys/:id/enable", h.EnableKey)
	r.POST("/api/v1/admin/keys/:id/release-lease", h.ReleaseLease)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
