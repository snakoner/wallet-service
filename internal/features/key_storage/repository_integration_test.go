//go:build integration

package key_storage

import (
	"context"
	"crypto/rand"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WALLET_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("WALLET_TEST_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func newIntegrationService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	pool := openTestPool(t)
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}

	srv := NewService(logger.New(logger.Config{Level: "error"}), NewRepository(), db.NewPgxTxManager(pool), NewKEKs(map[int32][]byte{1: kek}), 1)
	return srv, pool
}

func uniqueService() string {
	return "it-" + uuid.NewString()[:8]
}

func createIntegrationRole(t *testing.T, srv *Service, service, purpose string, network constants.Network, kind constants.RoleKind, policy string) *Role {
	t.Helper()
	role, err := srv.CreateRole(context.Background(), CreateRoleParams{Service: service, Purpose: purpose, Network: network, Kind: kind, Policy: []byte(policy)})
	if err != nil {
		t.Fatal(err)
	}
	return role
}
