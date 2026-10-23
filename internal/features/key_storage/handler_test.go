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
	params GetRoleKeyParams
	err    error
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
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
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
