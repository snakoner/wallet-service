package admin_audit

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
	filter ListFilter
}

func (s *adminServiceStub) List(_ context.Context, filter ListFilter) ([]Record, int64, error) {
	s.filter = filter
	keyID := uuid.New()
	return []Record{{ID: uuid.New(), Actor: "ivanov", Source: SourceCLI, Action: ActionKeyRevoke, Service: "exchange", KeyID: &keyID, Details: map[string]any{"from": "active"}}}, 42, nil
}

func doList(svc *adminServiceStub, query string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/admin/audit/actions", NewAdminHandler(svc, logger.New(logger.Config{Level: "error"})).List)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/actions"+query, nil))
	return w
}

func TestAdminList(t *testing.T) {
	roleID, keyID, clientID := uuid.New(), uuid.New(), uuid.New()
	svc := &adminServiceStub{}
	w := doList(svc, "?actor=ivanov&action=key.revoke&service=exchange&role_id="+roleID.String()+"&key_id="+keyID.String()+"&service_client_id="+clientID.String()+"&from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&offset=20&limit=10")
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"total":42`) || !strings.Contains(body, `"actor":"ivanov"`) || !strings.Contains(body, `"details":{"from":"active"}`) || strings.Contains(body, `"role_id"`) {
		t.Fatalf("code = %d, body = %s", w.Code, body)
	}
	f := svc.filter
	if f.Actor != "ivanov" || f.Action != "key.revoke" || f.Service != "exchange" || f.RoleID != roleID || f.KeyID != keyID || f.ServiceClientID != clientID ||
		!f.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !f.To.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) || f.Offset != 20 || f.Limit != 10 {
		t.Fatalf("filter = %+v", f)
	}

	svc = &adminServiceStub{}
	if w := doList(svc, ""); w.Code != http.StatusOK || svc.filter != (ListFilter{Limit: 100}) {
		t.Fatalf("no filters: code = %d, filter = %+v", w.Code, svc.filter)
	}

	for name, query := range map[string]string{"bad role_id": "?role_id=1", "bad key_id": "?key_id=x", "bad service_client_id": "?service_client_id=1", "bad to": "?to=yesterday"} {
		t.Run(name, func(t *testing.T) {
			if w := doList(&adminServiceStub{}, query); w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body)
			}
		})
	}
}
