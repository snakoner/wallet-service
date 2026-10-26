package key_storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/validation"
)

type serviceStub struct {
	leaseParams AcquireLeaseParams
	deposit     GetDepositKeyParams
	leaseID     uuid.UUID
	ttl         time.Duration
	params      GetRoleKeyParams
	err         error
}

func (s *serviceStub) GetRoleKey(_ context.Context, params GetRoleKeyParams) (*WalletKey, error) {
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{
		ID:        uuid.New(),
		Role:      Role{Service: params.Service, Purpose: params.Purpose, Label: params.Label, Kind: RoleKindSingle},
		Network:   params.Network,
		Address:   "TAddr",
		Status:    KeyStatusActive,
		KeyCipher: []byte("secret"),
		CreatedAt: time.Now(),
	}, nil
}

func (s *serviceStub) GetRoleKeys(_ context.Context, params GetRoleKeyParams) ([]WalletKey, error) {
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	role := Role{Service: params.Service, Purpose: params.Purpose, Kind: RoleKindPool}
	return []WalletKey{
		{ID: uuid.New(), Role: role, Network: params.Network, Address: "0xA", Status: KeyStatusActive, KeyCipher: []byte("secret")},
		{ID: uuid.New(), Role: role, Network: params.Network, Address: "0xB", Status: KeyStatusRotating, KeyCipher: []byte("secret")},
	}, nil
}

func (s *serviceStub) GetOrCreateDepositKey(_ context.Context, params GetDepositKeyParams) (*WalletKey, error) {
	s.deposit = params
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{ID: uuid.New(), Role: Role{Service: params.Service, Purpose: params.Purpose, Kind: RoleKindDeposit}, Network: params.Network, Address: "TDeposit", ExternalRef: &params.ExternalRef, Status: KeyStatusActive, KeyCipher: []byte("secret")}, nil
}

func (s *serviceStub) AcquireLease(_ context.Context, params AcquireLeaseParams) (*Lease, error) {
	s.leaseParams = params
	if s.err != nil {
		return nil, s.err
	}
	return &Lease{ID: uuid.New(), ExpiresAt: time.Now().Add(time.Minute), Key: WalletKey{ID: uuid.New(), Role: Role{Service: params.Service, Purpose: params.Purpose, Kind: RoleKindPool}, Network: params.Network, Address: "0xA", Status: KeyStatusActive, KeyCipher: []byte("secret")}}, nil
}

func (s *serviceStub) ExtendLease(_ context.Context, service string, leaseID uuid.UUID, ttl time.Duration) (time.Time, error) {
	s.leaseID, s.ttl = leaseID, ttl
	return time.Now().Add(ttl), s.err
}

func (s *serviceStub) ReleaseLease(_ context.Context, service string, leaseID uuid.UUID) error {
	s.leaseID = leaseID
	return s.err
}

func doGetRoleKey(t *testing.T, svc *serviceStub, service, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := validation.Register(); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	setService := func(c *gin.Context) {
		if service != "" {
			c.Set(middleware.ContextServiceKey, service)
		}
		c.Next()
	}
	r.GET("/api/v1/roles/:purpose/key", setService, NewHandler(svc, logger.New(logger.Config{Level: "error"})).GetRoleKey)
	r.GET("/api/v1/roles/:purpose/keys", setService, NewHandler(svc, logger.New(logger.Config{Level: "error"})).GetRoleKeys)
	h := NewHandler(svc, logger.New(logger.Config{Level: "error"}))
	r.POST("/api/v1/roles/:purpose/lease", setService, h.AcquireLease)
	r.POST("/api/v1/roles/:purpose/deposit-key", setService, h.GetOrCreateDepositKey)
	r.POST("/api/v1/leases/:id/extend", setService, h.ExtendLease)
	r.DELETE("/api/v1/leases/:id", setService, h.ReleaseLease)
	w := httptest.NewRecorder()
	method, body := http.MethodGet, ""
	if parts := strings.SplitN(path, " ", 3); len(parts) > 1 {
		method, path = parts[0], parts[1]
		if len(parts) == 3 {
			body = parts[2]
		}
	}
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
