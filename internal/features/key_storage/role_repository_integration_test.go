//go:build integration

package key_storage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestIntegrationRoleUnique(t *testing.T) {
	srv, _ := newIntegrationService(t)
	service := uniqueService()
	createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, RoleKindSingle, "")

	_, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: "gas_station", Network: constants.NetworkEthereum, Kind: RoleKindPool})
	if !errors.Is(err, ErrRoleExists) {
		t.Fatalf("duplicate role: err = %v", err)
	}

	if _, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: "gas_station", Network: constants.NetworkTron, Kind: RoleKindSingle}); err != nil {
		t.Fatalf("other network: err = %v", err)
	}
	if _, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: "gas_station", Label: "atoken", Network: constants.NetworkEthereum, Kind: RoleKindSingle}); err != nil {
		t.Fatalf("other label: err = %v", err)
	}
}

func TestIntegrationUpdatePolicy(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	role := createIntegrationRole(t, srv, service, "gas_station", constants.NetworkEthereum, RoleKindSingle, "")
	if _, err := srv.UpdatePolicy(ctx, role.ID, []byte(`{"version":1,"rules":[{"op":"native","to":["internal:deposit"]}]}`)); err != nil {
		t.Fatal(err)
	}

	roles, err := srv.ListRoles(ctx, service)
	if err != nil || len(roles) != 1 {
		t.Fatalf("roles = %v, err = %v", roles, err)
	}
	if _, err := srv.UpdatePolicy(ctx, uuid.New(), []byte(`{}`)); !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}
