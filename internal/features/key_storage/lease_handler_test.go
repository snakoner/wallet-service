package key_storage

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
