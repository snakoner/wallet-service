package service_client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type adminServiceStub struct {
	service string
	grace   time.Duration
	id      uuid.UUID
	err     error
}

func (s *adminServiceStub) CreateClient(_ context.Context, service string) (string, *ServiceClient, error) {
	s.service = service
	if s.err != nil {
		return "", nil, s.err
	}
	return "secret-key", &ServiceClient{ID: uuid.New(), Service: service, KeyHash: []byte("hash"), IsActive: true}, nil
}

func (s *adminServiceStub) RotateClient(_ context.Context, service string, grace time.Duration) (string, *ServiceClient, *ServiceClient, error) {
	s.service, s.grace = service, grace
	if s.err != nil {
		return "", nil, nil, s.err
	}
	expires := time.Now().Add(grace)
	return "new-key", &ServiceClient{ID: uuid.New(), Service: service, IsActive: true}, &ServiceClient{ID: uuid.New(), Service: service, IsActive: true, ExpiresAt: &expires}, nil
}

func (s *adminServiceStub) RevokeClient(_ context.Context, id uuid.UUID) error {
	s.id = id
	return s.err
}

func (s *adminServiceStub) ListClients(_ context.Context, service string) ([]ServiceClient, error) {
	s.service = service
	past := time.Now().Add(-time.Minute)
	return []ServiceClient{
		{ID: uuid.New(), Service: "exchange", IsActive: true, KeyHash: []byte("hash")},
		{ID: uuid.New(), Service: "exchange", IsActive: true, ExpiresAt: &past},
		{ID: uuid.New(), Service: "exchange", IsActive: false},
	}, s.err
}

func doAdmin(t *testing.T, svc *adminServiceStub, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewAdminHandler(svc, logger.New(logger.Config{Level: "error"}))
	r := gin.New()
	r.POST("/api/v1/admin/service-clients", h.CreateClient)
	r.GET("/api/v1/admin/service-clients", h.ListClients)
	r.POST("/api/v1/admin/service-clients/rotate", h.RotateClient)
	r.POST("/api/v1/admin/service-clients/:id/revoke", h.RevokeClient)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func TestAdminCreateClient(t *testing.T) {
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/service-clients", `{"service":"exchange"}`)
	if w.Code != http.StatusOK || svc.service != "exchange" || !strings.Contains(w.Body.String(), `"service_key":"secret-key"`) || !strings.Contains(w.Body.String(), `"state":"primary"`) {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "hash") {
		t.Fatal("response must not contain key hash")
	}

	if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/service-clients", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("no service: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrServiceClientExists}, http.MethodPost, "/api/v1/admin/service-clients", `{"service":"exchange"}`); w.Code != http.StatusConflict {
		t.Fatalf("exists: code = %d", w.Code)
	}
}

func TestAdminRotateClient(t *testing.T) {
	svc := &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/service-clients/rotate", `{"service":"exchange"}`)
	if w.Code != http.StatusOK || svc.grace != 24*time.Hour || !strings.Contains(w.Body.String(), `"old_client"`) || !strings.Contains(w.Body.String(), `"state":"expiring"`) {
		t.Fatalf("default grace: code = %d, grace = %s, body = %s", w.Code, svc.grace, w.Body)
	}

	svc = &adminServiceStub{}
	if w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/service-clients/rotate", `{"service":"exchange","grace_seconds":0}`); w.Code != http.StatusOK || svc.grace != 0 {
		t.Fatalf("grace 0: code = %d, grace = %s", w.Code, svc.grace)
	}
	if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/service-clients/rotate", `{"service":"exchange","grace_seconds":-1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("negative grace: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{err: ErrServiceClientNotFound}, http.MethodPost, "/api/v1/admin/service-clients/rotate", `{"service":"exchange"}`); w.Code != http.StatusNotFound {
		t.Fatalf("not found: code = %d", w.Code)
	}
}

func TestAdminRevokeAndListClients(t *testing.T) {
	id := uuid.New()
	svc := &adminServiceStub{}
	if w := doAdmin(t, svc, http.MethodPost, "/api/v1/admin/service-clients/"+id.String()+"/revoke", ""); w.Code != http.StatusOK || svc.id != id {
		t.Fatalf("revoke: code = %d", w.Code)
	}
	if w := doAdmin(t, &adminServiceStub{}, http.MethodPost, "/api/v1/admin/service-clients/abc/revoke", ""); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: code = %d", w.Code)
	}

	svc = &adminServiceStub{}
	w := doAdmin(t, svc, http.MethodGet, "/api/v1/admin/service-clients?service=exchange", "")
	body := w.Body.String()
	if w.Code != http.StatusOK || svc.service != "exchange" || !strings.Contains(body, `"primary"`) || !strings.Contains(body, `"expired"`) || !strings.Contains(body, `"revoked"`) || strings.Contains(body, "hash") {
		t.Fatalf("list: code = %d, body = %s", w.Code, body)
	}
}
