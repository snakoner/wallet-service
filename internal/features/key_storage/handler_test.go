package key_storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/app/middleware"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/validation"
)

type serviceStub struct {
	leaseParams AcquireLeaseParams
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

func TestHandlerGetRoleKey(t *testing.T) {
	svc := &serviceStub{}
	w := doGetRoleKey(t, svc, "exchange", "/api/v1/roles/gas_station/key?label=atoken&network=tron")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	p := svc.params
	if p.Service != "exchange" || p.Purpose != "gas_station" || p.Label != "atoken" || p.Network != constants.NetworkTron {
		t.Fatalf("params = %+v", p)
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data["address"] != "TAddr" || resp.Data["network"] != "tron" || resp.Data["kind"] != "single" || resp.Data["label"] != "atoken" {
		t.Fatalf("data = %v", resp.Data)
	}
	if strings.Contains(w.Body.String(), "cipher") {
		t.Fatal("response must not contain ciphers")
	}
}

func TestHandlerGetRoleKeyErrors(t *testing.T) {
	cases := []struct {
		name    string
		service string
		path    string
		err     error
		want    int
	}{
		{"no service in context", "", "/api/v1/roles/gas_station/key?network=tron", nil, http.StatusUnauthorized},
		{"no network", "x", "/api/v1/roles/gas_station/key", nil, http.StatusBadRequest},
		{"unknown network", "x", "/api/v1/roles/gas_station/key?network=btc", nil, http.StatusBadRequest},
		{"role not found", "x", "/api/v1/roles/gas_station/key?network=tron", ErrRoleNotFound, http.StatusNotFound},
		{"no active key", "x", "/api/v1/roles/gas_station/key?network=tron", ErrKeyNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := doGetRoleKey(t, &serviceStub{err: tc.err}, tc.service, tc.path); w.Code != tc.want {
				t.Fatalf("code = %d, want %d, body = %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

func TestHandlerGetRoleKeys(t *testing.T) {
	svc := &serviceStub{}
	w := doGetRoleKey(t, svc, "exchange", "/api/v1/roles/executor/keys?network=ethereum")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	if p := svc.params; p.Service != "exchange" || p.Purpose != "executor" || p.Label != "" || p.Network != constants.NetworkEthereum {
		t.Fatalf("params = %+v", p)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 2 || resp.Data[0]["address"] != "0xA" || resp.Data[1]["status"] != "rotating" || resp.Data[0]["kind"] != "pool" {
		t.Fatalf("data = %v", resp.Data)
	}
	if strings.Contains(w.Body.String(), "cipher") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("response must not contain ciphers")
	}

	for name, tc := range map[string]struct {
		service, path string
		err           error
		want          int
	}{
		"no service":     {"", "/api/v1/roles/executor/keys?network=ethereum", nil, http.StatusUnauthorized},
		"no network":     {"x", "/api/v1/roles/executor/keys", nil, http.StatusBadRequest},
		"role not found": {"x", "/api/v1/roles/executor/keys?network=ethereum", ErrRoleNotFound, http.StatusNotFound},
		"deposit role":   {"x", "/api/v1/roles/deposit/keys?network=ethereum", ErrInvalidParams, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			if w := doGetRoleKey(t, &serviceStub{err: tc.err}, tc.service, tc.path); w.Code != tc.want {
				t.Fatalf("code = %d, want %d, body = %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

func TestHandlerLease(t *testing.T) {
	svc := &serviceStub{}
	exclude := uuid.New()
	w := doGetRoleKey(t, svc, "exchange", `POST /api/v1/roles/executor/lease {"network":"ethereum","owner":"pod-1","ttl_seconds":30,"exclude":["`+exclude.String()+`"]}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"lease_id"`) || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	p := svc.leaseParams
	if p.Service != "exchange" || p.Purpose != "executor" || p.Owner != "pod-1" || p.TTL != 30*time.Second || len(p.Exclude) != 1 || p.Exclude[0] != exclude {
		t.Fatalf("params = %+v", p)
	}

	w = doGetRoleKey(t, &serviceStub{err: ErrNoFreeKey}, "exchange", `POST /api/v1/roles/executor/lease {"network":"ethereum"}`)
	if w.Code != http.StatusConflict || w.Header().Get("Retry-After") == "" {
		t.Fatalf("no free key: code = %d, retry-after = %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := doGetRoleKey(t, &serviceStub{}, "exchange", `POST /api/v1/roles/executor/lease {}`); w.Code != http.StatusBadRequest {
		t.Fatalf("no network: code = %d", w.Code)
	}

	leaseID := uuid.New()
	svc = &serviceStub{}
	if w := doGetRoleKey(t, svc, "exchange", `POST /api/v1/leases/`+leaseID.String()+`/extend {"ttl_seconds":120}`); w.Code != http.StatusOK || svc.leaseID != leaseID || svc.ttl != 2*time.Minute {
		t.Fatalf("extend: code = %d, lease = %s, ttl = %s", w.Code, svc.leaseID, svc.ttl)
	}
	if w := doGetRoleKey(t, &serviceStub{err: ErrLeaseExpired}, "exchange", `POST /api/v1/leases/`+leaseID.String()+`/extend`); w.Code != http.StatusConflict {
		t.Fatalf("extend expired: code = %d", w.Code)
	}
	if w := doGetRoleKey(t, &serviceStub{}, "exchange", `DELETE /api/v1/leases/`+leaseID.String()); w.Code != http.StatusOK {
		t.Fatalf("release: code = %d", w.Code)
	}
	if w := doGetRoleKey(t, &serviceStub{}, "", `DELETE /api/v1/leases/`+leaseID.String()); w.Code != http.StatusUnauthorized {
		t.Fatalf("release without service: code = %d", w.Code)
	}
}
