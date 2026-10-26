package key_storage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestCreateRole(t *testing.T) {
	srv := newTestService(t, &repoStub{})

	role, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: " exchange ", Purpose: "gas_station", Label: " atoken ", Network: constants.NetworkTron, Kind: constants.RoleKindSingle})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if role.Service != "exchange" || role.Label != "atoken" || string(role.Policy) != `{"version":1,"rules":[]}` {
		t.Fatalf("role = %+v", role)
	}

	if _, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: "exchange", Purpose: "gas_station", Label: "atoken", Network: constants.NetworkTron, Kind: constants.RoleKindSingle}); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("duplicate: err = %v", err)
	}

	cases := map[string]CreateRoleParams{
		"no service":     {Purpose: "executor", Network: constants.NetworkTron, Kind: constants.RoleKindPool},
		"no purpose":     {Service: "exchange", Network: constants.NetworkTron, Kind: constants.RoleKindPool},
		"bad network":    {Service: "exchange", Purpose: "executor", Network: 9, Kind: constants.RoleKindPool},
		"bad kind":       {Service: "exchange", Purpose: "executor", Network: constants.NetworkTron},
		"invalid policy": {Service: "exchange", Purpose: "executor", Network: constants.NetworkTron, Kind: constants.RoleKindPool, Policy: []byte("{")},
		"policy rule":    {Service: "exchange", Purpose: "executor", Network: constants.NetworkTron, Kind: constants.RoleKindPool, Policy: []byte(`{"version":1,"rules":[{"op":"native"}]}`)},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := srv.CreateRole(context.Background(), params); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestListRolesAndKeys(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	pool := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
	if _, err := srv.CreateRole(ctx, CreateRoleParams{Service: "banknote", Purpose: "executor", Network: constants.NetworkTron, Kind: constants.RoleKindPool}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID}); err != nil {
			t.Fatal(err)
		}
	}

	if roles, err := srv.ListRoles(ctx, ""); err != nil || len(roles) != 2 {
		t.Fatalf("all roles = %d, err = %v", len(roles), err)
	}
	if roles, err := srv.ListRoles(ctx, "banknote"); err != nil || len(roles) != 1 || roles[0].Service != "banknote" {
		t.Fatalf("banknote roles = %+v, err = %v", roles, err)
	}

	keys, err := srv.ListRoleKeys(ctx, pool.ID)
	if err != nil || len(keys) != 2 || keys[0].Role.Purpose != "gas_station" {
		t.Fatalf("keys = %+v, err = %v", keys, err)
	}
	if _, err := srv.ListRoleKeys(ctx, uuid.New()); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}

func TestUpdatePolicy(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)

	updated, err := srv.UpdatePolicy(context.Background(), role.ID, []byte(`{"version":1,"rules":[{"op":"native","to":["internal:executor"]}]}`))
	if err != nil {
		t.Fatalf("UpdatePolicy() error = %v", err)
	}
	if want := `{"version":1,"rules":[{"op":"native","to":["internal:executor"]}]}`; string(updated.Policy) != want || string(repo.roles[0].Policy) != want {
		t.Fatalf("policy = %s, stored = %s", updated.Policy, repo.roles[0].Policy)
	}

	if _, err := srv.UpdatePolicy(context.Background(), role.ID, []byte(`{"version":1,"rules":[{"op":"token","to":["*"]}]}`)); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("invalid policy: err = %v", err)
	}
	if _, err := srv.UpdatePolicy(context.Background(), uuid.New(), []byte(`{}`)); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}
