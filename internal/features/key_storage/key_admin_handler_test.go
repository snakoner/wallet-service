package key_storage

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

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
