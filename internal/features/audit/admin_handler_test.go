package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

type adminServiceStub struct {
	filter ListSigningFilter
	err    error
}

func (s *adminServiceStub) ListSigning(_ context.Context, filter ListSigningFilter) ([]SigningRecord, int64, error) {
	s.filter = filter
	if s.err != nil {
		return nil, 0, s.err
	}
	hash := "0xhash"
	return []SigningRecord{{ID: uuid.New(), Service: "exchange", Network: constants.NetworkTron, RawTx: []byte{0xab, 0xcd}, TxHash: &hash}}, 42, nil
}

func doAudit(svc *adminServiceStub, query string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/admin/audit", NewAdminHandler(svc, logger.New(logger.Config{Level: "error"})).ListSigning)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit"+query, nil))
	return w
}

func TestAdminListSigning(t *testing.T) {
	keyID := uuid.New()
	svc := &adminServiceStub{}
	w := doAudit(svc, "?key_id="+keyID.String()+"&service=exchange&tx_hash=0xh&error_code=POLICY_VIOLATION&failed=true&from=2026-10-01T00:00:00Z&to=2026-10-02T03:00:00%2B03:00&offset=20&limit=10")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"records":[{`) || !strings.Contains(w.Body.String(), `"total":42`) || !strings.Contains(w.Body.String(), `"raw_tx":"0xabcd"`) || !strings.Contains(w.Body.String(), `"network":"tron"`) || !strings.Contains(w.Body.String(), `"tx_hash":"0xhash"`) {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body)
	}
	f := svc.filter
	if f.KeyID != keyID || f.Service != "exchange" || f.TxHash != "0xh" || f.ErrorCode != "POLICY_VIOLATION" || f.Failed == nil || !*f.Failed ||
		!f.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !f.To.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) || f.Offset != 20 || f.Limit != 10 {
		t.Fatalf("filter = %+v", f)
	}

	svc = &adminServiceStub{}
	if w := doAudit(svc, ""); w.Code != http.StatusOK || svc.filter != (ListSigningFilter{Limit: 100}) {
		t.Fatalf("no filters: code = %d, filter = %+v", w.Code, svc.filter)
	}

	svc = &adminServiceStub{}
	if w := doAudit(svc, "?failed=false&offset=-5&limit=5000"); w.Code != http.StatusOK || svc.filter.Failed == nil || *svc.filter.Failed || svc.filter.Offset != 0 || svc.filter.Limit != 1000 {
		t.Fatalf("signed only: code = %d, filter = %+v", w.Code, svc.filter)
	}

	for name, query := range map[string]string{"bad key_id": "?key_id=1", "bad from": "?from=2026-10-01", "bad failed": "?failed=maybe"} {
		t.Run(name, func(t *testing.T) {
			if w := doAudit(&adminServiceStub{}, query); w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body)
			}
		})
	}

	if w := doAudit(&adminServiceStub{err: errors.New("db down")}, ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("service error: code = %d", w.Code)
	}
}
