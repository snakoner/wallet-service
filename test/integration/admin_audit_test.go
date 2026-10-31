//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	adminaudit "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/admin_audit"
	keystorage "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/features/key_storage"
)

type actionView struct {
	Actor           string         `json:"actor"`
	Source          string         `json:"source"`
	Action          string         `json:"action"`
	Service         string         `json:"service"`
	RoleID          *uuid.UUID     `json:"role_id"`
	KeyID           *uuid.UUID     `json:"key_id"`
	ServiceClientID *uuid.UUID     `json:"service_client_id"`
	Details         map[string]any `json:"details"`
}

func (e *env) actions(query string) ([]actionView, int64) {
	e.tb.Helper()
	var page struct {
		Records []actionView `json:"records"`
		Total   int64        `json:"total"`
	}

	if status, res := e.call(http.MethodGet, "/api/v1/admin/audit/actions?"+query, "", nil, &page); status != http.StatusOK {
		e.tb.Fatalf("list actions %s: status = %d, error = %+v", query, status, res.Error)
	}

	return page.Records, page.Total
}

func actionNames(records []actionView) string {
	names := make([]string, len(records))
	for i, r := range records {
		names[i] = r.Action
	}

	return strings.Join(names, ",")
}

func TestAdminAudit(t *testing.T) {
	e := newEnv(t)
	e.actor = "petrov"

	// admin API: actor from X-Admin-Actor
	var role struct {
		ID uuid.UUID `json:"id"`
	}
	if status, res := e.call(http.MethodPost, "/api/v1/admin/roles", "", map[string]any{
		"service": e.service, "purpose": "gas_station", "network": "ethereum", "kind": "single",
	}, &role); status != http.StatusOK {
		t.Fatalf("create role: status = %d, error = %+v", status, res.Error)
	}

	var key, replacement keyView
	e.call(http.MethodPost, "/api/v1/admin/roles/"+role.ID.String()+"/keys", "", map[string]any{}, &key)
	e.call(http.MethodPost, "/api/v1/admin/keys/"+key.ID.String()+"/rotate", "", nil, &replacement)
	if status, res := e.call(http.MethodPost, "/api/v1/admin/keys/"+replacement.ID.String()+"/activate", "", nil, nil); status != http.StatusOK {
		t.Fatalf("activate: status = %d, error = %+v", status, res.Error)
	}

	// history of old key includes its rotation and activation of replacement, newest first
	records, total := e.actions("key_id=" + key.ID.String())
	if total != 3 || actionNames(records) != "key.activate,key.rotate,key.create" {
		t.Fatalf("old key history: total = %d, actions = %s", total, actionNames(records))
	}

	for _, r := range records {
		if r.Actor != "petrov" || r.Source != adminaudit.SourceAPI || r.Service != e.service || r.RoleID == nil || *r.RoleID != role.ID {
			t.Fatalf("record = %+v", r)
		}
	}

	if activate := records[0]; *activate.KeyID != replacement.ID || activate.Details["replaced_key_id"] != key.ID.String() || activate.Details["replaced_key_status"] != "rotating" {
		t.Fatalf("activate = %+v", activate)
	}

	if rotate := records[1]; *rotate.KeyID != key.ID || rotate.Details["replacement_key_id"] != replacement.ID.String() {
		t.Fatalf("rotate = %+v", rotate)
	}

	// service: actor from context
	policy := json.RawMessage(`{"version":1,"rules":[{"op":"native","to":["*"]}]}`)
	if _, err := e.keys.UpdatePolicy(e.ctx, role.ID, policy); err != nil {
		t.Fatal(err)
	}

	records, total = e.actions("role_id=" + role.ID.String() + "&action=role.policy_update")
	if total != 1 || records[0].Actor != "integration-test" || records[0].Source != adminaudit.SourceCLI {
		t.Fatalf("policy update: total = %d, records = %+v", total, records)
	}

	// role without policy has normalized empty policy
	if old := mustJSON(t, records[0].Details["old"]); !strings.Contains(string(old), `"rules":[]`) || !strings.Contains(string(mustJSON(t, records[0].Details["new"])), `"native"`) {
		t.Fatalf("policy update details = %+v", records[0].Details)
	}

	// whole role history with pagination
	records, total = e.actions("role_id=" + role.ID.String() + "&limit=2&offset=1")
	if total != 5 || actionNames(records) != "key.activate,key.rotate" {
		t.Fatalf("role page: total = %d, actions = %s", total, actionNames(records))
	}

	if _, total = e.actions("service=" + e.service + "&actor=petrov"); total != 4 {
		t.Fatalf("by actor: total = %d", total)
	}

	if _, total = e.actions("service=" + e.service + "&from=" + time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); total != 0 {
		t.Fatalf("from future: total = %d", total)
	}

	// service key rotation is found by old key too
	_, rotated, old, err := e.clients.RotateClient(e.ctx, e.service, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if records, total = e.actions("service_client_id=" + old.ID.String()); total != 2 || actionNames(records) != "service_client.rotate,service_client.create" || *records[0].ServiceClientID != rotated.ID {
		t.Fatalf("service client history: total = %d, records = %+v", total, records)
	}
}

func TestAdminAuditRollback(t *testing.T) {
	e := newEnv(t)
	role := e.role("executor", constants.NetworkEthereum, constants.RoleKindPool, "")

	// no actor: change and record are rolled back together
	if _, err := e.keys.CreateKey(context.Background(), keystorage.CreateKeyParams{RoleID: role.ID}); !errors.Is(err, adminaudit.ErrNoActor) {
		t.Fatalf("create key without actor: err = %v", err)
	}

	if _, err := e.keys.CreateRole(context.Background(), keystorage.CreateRoleParams{Service: e.service, Purpose: "owner", Network: constants.NetworkEthereum, Kind: constants.RoleKindSingle}); !errors.Is(err, adminaudit.ErrNoActor) {
		t.Fatalf("create role without actor: err = %v", err)
	}

	keys, total, err := e.keys.ListRoleKeys(e.ctx, role.ID, 0, 10)
	if err != nil || total != 0 || len(keys) != 0 {
		t.Fatalf("keys after rolled back create: %d, %v", total, err)
	}

	roles, err := e.keys.ListRoles(e.ctx, e.service)
	if err != nil || len(roles) != 1 {
		t.Fatalf("roles after rolled back create: %+v, %v", roles, err)
	}

	// newEnv also records service key of test
	if records, total := e.actions("service=" + e.service); total != 2 || actionNames(records) != "role.create,service_client.create" {
		t.Fatalf("records: total = %d, actions = %s", total, actionNames(records))
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return data
}
