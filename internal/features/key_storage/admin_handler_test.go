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
	return &Role{ID: roleID, Service: "exchange", Network: constants.NetworkTron, Kind: RoleKindSingle, Policy: raw}, nil
}

func (s *adminServiceStub) ListKeys(_ context.Context, filter ListKeysFilter) ([]WalletKey, error) {
	s.keysFilter = filter
	if s.err != nil {
		return nil, s.err
	}
	return []WalletKey{{ID: uuid.New(), Role: Role{Service: "exchange", Purpose: "executor", Kind: RoleKindPool}, Network: constants.NetworkEthereum, Address: "0xA", Status: KeyStatusActive, KeyCipher: []byte("secret")}}, nil
}

func (s *adminServiceStub) keyAction(keyID uuid.UUID, status KeyStatus) (*WalletKey, error) {
	s.keyID = keyID
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{ID: keyID, Role: Role{Service: "exchange", Kind: RoleKindSingle}, Network: constants.NetworkEthereum, Address: "0xA", Status: status, KeyCipher: []byte("secret")}, nil
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
	return s.keyAction(id, KeyStatusPending)
}

func (s *adminServiceStub) ActivateKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, KeyStatusActive)
}

func (s *adminServiceStub) RevokeKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, KeyStatusRevoked)
}

func (s *adminServiceStub) DisableKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, KeyStatusDisabled)
}

func (s *adminServiceStub) EnableKey(_ context.Context, id uuid.UUID) (*WalletKey, error) {
	return s.keyAction(id, KeyStatusActive)
}

func (s *adminServiceStub) ListRoles(_ context.Context, service string) ([]Role, error) {
	s.service = service
	return []Role{{ID: uuid.New(), Service: "exchange", Purpose: "gas_station", Network: constants.NetworkTron, Kind: RoleKindSingle}}, s.err
}

func (s *adminServiceStub) CreateKey(_ context.Context, params CreateKeyParams) (*WalletKey, error) {
	s.keyParams = params
	if s.err != nil {
		return nil, s.err
	}
	return &WalletKey{ID: uuid.New(), RoleID: params.RoleID, Role: Role{Service: "exchange", Kind: RoleKindDeposit}, Network: constants.NetworkEthereum, Address: "0xAddr", ExternalRef: params.ExternalRef, KeyCipher: []byte("secret")}, nil
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

func TestAdminCreateRole(t *testing.T) {
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/roles", `{"service":"exchange","purpose":"gas_station","label":"atoken","network":"tron","kind":"single","policy":{"rules":[]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	p := svc.roleParams
	if p.Service != "exchange" || p.Purpose != "gas_station" || p.Label != "atoken" || p.Network != constants.NetworkTron || p.Kind != RoleKindSingle || string(p.Policy) != `{"rules":[]}` {
		t.Fatalf("params = %+v", p)
	}

	cases := map[string]string{
		"no service":      `{"purpose":"x","network":"tron","kind":"single"}`,
		"unknown network": `{"service":"x","purpose":"x","network":"btc","kind":"single"}`,
		"unknown kind":    `{"service":"x","purpose":"x","network":"tron","kind":"many"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/roles", body); w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body)
			}
		})
	}

	if w := doAdmin(t, &adminServiceStub{err: ErrRoleExists}, http.MethodPost, "/api/v1/admin/roles", `{"service":"x","purpose":"x","network":"tron","kind":"pool"}`); w.Code != http.StatusConflict {
		t.Fatalf("exists: code = %d", w.Code)
	}
}

func TestAdminListRoles(t *testing.T) {
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodGet, "/api/v1/admin/roles?service=exchange", "")
	if w.Code != http.StatusOK || svc.service != "exchange" || !strings.Contains(w.Body.String(), `"kind":"single"`) {
		t.Fatalf("code = %d, service = %q, body = %s", w.Code, svc.service, w.Body)
	}
}

func TestAdminCreateKey(t *testing.T) {
	roleID := uuid.New()

	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/roles/"+roleID.String()+"/keys", `{"external_ref":"order-1"}`)
	if w.Code != http.StatusOK || svc.keyParams.RoleID != roleID || *svc.keyParams.ExternalRef != "order-1" {
		t.Fatalf("code = %d, params = %+v, body = %s", w.Code, svc.keyParams, w.Body)
	}
	if strings.Contains(w.Body.String(), "cipher") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("response must not contain ciphers")
	}

	svc = &adminServiceStub{}
	if w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/roles/"+roleID.String()+"/keys", ""); w.Code != http.StatusOK || svc.keyParams.ExternalRef != nil {
		t.Fatalf("empty body: code = %d, body = %s", w.Code, w.Body)
	}

	if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/roles/abc/keys", ""); w.Code != http.StatusNotFound {
		t.Fatalf("bad role id: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrActiveKeyExists}, http.MethodPost, "/api/v1/admin/roles/"+roleID.String()+"/keys", ""); w.Code != http.StatusConflict {
		t.Fatalf("active exists: code = %d", w.Code)
	}
}

func TestAdminListRoleKeys(t *testing.T) {
	roleID := uuid.New()
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodGet, "/api/v1/admin/roles/"+roleID.String()+"/keys", "")
	if w.Code != http.StatusOK || svc.roleID != roleID || !strings.Contains(w.Body.String(), "TAddr") || strings.Contains(w.Body.String(), "cipher") {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}

	if w := doAdmin(t, &adminServiceStub{err: ErrRoleNotFound}, http.MethodGet, "/api/v1/admin/roles/"+roleID.String()+"/keys", ""); w.Code != http.StatusNotFound {
		t.Fatalf("not found: code = %d", w.Code)
	}
}

func TestAdminUpdatePolicy(t *testing.T) {
	roleID := uuid.New()
	path := "/api/v1/admin/roles/" + roleID.String() + "/policy"

	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodPut, path, `{"policy":{"version":1,"rules":[{"op":"native","to":["*"]}]}}`)
	if w.Code != http.StatusOK || svc.roleID != roleID || string(svc.policy) != `{"version":1,"rules":[{"op":"native","to":["*"]}]}` {
		t.Fatalf("code = %d, policy = %s, body = %s", w.Code, svc.policy, w.Body)
	}

	if w := doAdmin(t, &adminServiceStub{}, http.MethodPut, path, `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("no policy: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrInvalidParams}, http.MethodPut, path, `{"policy":{"rules":[]}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrRoleNotFound}, http.MethodPut, path, `{"policy":{}}`); w.Code != http.StatusNotFound {
		t.Fatalf("not found: code = %d", w.Code)
	}
}

func TestAdminListKeys(t *testing.T) {
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodGet, "/api/v1/admin/keys?service=exchange&purpose=executor&label=&network=ethereum&status=active&external_ref=o-1&limit=10", "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), `"address":"0xA"`) {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	f := svc.keysFilter
	if f.Service == nil || *f.Service != "exchange" || f.Purpose == nil || *f.Purpose != "executor" || f.Label == nil || *f.Label != "" ||
		f.Network == nil || *f.Network != constants.NetworkEthereum || f.Status == nil || *f.Status != KeyStatusActive ||
		f.ExternalRef == nil || *f.ExternalRef != "o-1" || f.Limit != 10 {
		t.Fatalf("filter = %+v", f)
	}

	svc = &adminServiceStub{}
	if w := doAdmin(t, svc, http.MethodGet, "/api/v1/admin/keys", ""); w.Code != http.StatusOK || svc.keysFilter != (ListKeysFilter{}) {
		t.Fatalf("no filters: code = %d, filter = %+v", w.Code, svc.keysFilter)
	}

	for name, query := range map[string]string{"bad network": "?network=btc", "bad status": "?status=deleted", "bad limit": "?limit=5000"} {
		t.Run(name, func(t *testing.T) {
			if w := doAdmin(t, &adminServiceStub{}, http.MethodGet, "/api/v1/admin/keys"+query, ""); w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body)
			}
		})
	}
}

func TestAdminKeyActions(t *testing.T) {
	keyID := uuid.New()
	for action, status := range map[string]string{"rotate": "pending", "activate": "active", "revoke": "revoked", "disable": "disabled", "enable": "active"} {
		t.Run(action, func(t *testing.T) {
			svc := &adminServiceStub{}
			w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/keys/"+keyID.String()+"/"+action, "")
			if w.Code != http.StatusOK || svc.keyID != keyID || !strings.Contains(w.Body.String(), `"status":"`+status+`"`) || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body)
			}
		})
	}

	if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/keys/abc/rotate", ""); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrRotationInProgress}, http.MethodPost, "/api/v1/admin/keys/"+keyID.String()+"/rotate", ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "ROTATION_IN_PROGRESS") {
		t.Fatalf("in progress: code = %d, body = %s", w.Code, w.Body)
	}
}

func TestAdminReleaseLease(t *testing.T) {
	keyID := uuid.New()
	svc := &adminServiceStub{}
	if w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/keys/"+keyID.String()+"/release-lease", ""); w.Code != http.StatusOK || svc.keyID != keyID {
		t.Fatalf("code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrKeyNotFound}, http.MethodPost, "/api/v1/admin/keys/"+keyID.String()+"/release-lease", ""); w.Code != http.StatusNotFound {
		t.Fatalf("not found: code = %d", w.Code)
	}
}

func TestAdminImportKey(t *testing.T) {
	roleID := uuid.New()
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/roles/"+roleID.String()+"/keys/import", `{"private_key":"0xabc","alias":"old gas"}`)
	if w.Code != http.StatusOK || svc.importParams.RoleID != roleID || string(svc.importParams.PrivateKey) != "0xabc" || *svc.importParams.Alias != "old gas" {
		t.Fatalf("code = %d, params = %+v, body = %s", w.Code, svc.importParams, w.Body)
	}
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "0xabc") {
		t.Fatal("response must not contain private key")
	}

	if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/roles/"+roleID.String()+"/keys/import", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("no private key: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrKeyExists}, http.MethodPost, "/api/v1/admin/roles/"+roleID.String()+"/keys/import", `{"private_key":"0xabc"}`); w.Code != http.StatusConflict {
		t.Fatalf("exists: code = %d", w.Code)
	}
}
