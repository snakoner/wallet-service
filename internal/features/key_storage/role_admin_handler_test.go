package key_storage

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

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
