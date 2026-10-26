//go:build integration

package service_client

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/db"
	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/logger"
)

func newIntegrationService(t *testing.T) *Service {
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

	return NewService(logger.New(logger.Config{Level: "error"}), NewRepository(), db.NewPgxTxManager(pool))
}

func uniqueService() string {
	return "it-" + uuid.NewString()[:8]
}

func TestIntegrationServiceClientLifecycle(t *testing.T) {
	srv := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()

	firstKey, first, err := srv.CreateClient(ctx, service)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.CreateClient(ctx, service); !errors.Is(err, ErrServiceClientExists) {
		t.Fatalf("second create: err = %v", err)
	}

	secondKey, second, old, err := srv.RotateClient(ctx, service, time.Hour)
	if err != nil {
		t.Fatalf("rotate: err = %v", err)
	}
	if old.ID != first.ID || *old.ReplacedBy != second.ID {
		t.Fatalf("old = %+v", old)
	}
	for name, key := range map[string]string{"old key in grace": firstKey, "new key": secondKey} {
		if client, err := srv.Authenticate(ctx, key); err != nil || client.Service != service {
			t.Fatalf("%s: client = %+v, err = %v", name, client, err)
		}
	}

	thirdKey, _, _, err := srv.RotateClient(ctx, service, 0)
	if err != nil {
		t.Fatalf("rotate grace 0: err = %v", err)
	}
	if _, err := srv.Authenticate(ctx, secondKey); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("key rotated with grace 0: err = %v", err)
	}

	if err := srv.RevokeClient(ctx, first.ID); err != nil {
		t.Fatalf("revoke: err = %v", err)
	}
	if _, err := srv.Authenticate(ctx, firstKey); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked key: err = %v", err)
	}
	if _, err := srv.Authenticate(ctx, thirdKey); err != nil {
		t.Fatalf("current key: err = %v", err)
	}

	clients, err := srv.ListClients(ctx, service)
	if err != nil || len(clients) != 3 {
		t.Fatalf("clients = %d, err = %v", len(clients), err)
	}
}

// Concurrent rotations must leave exactly one primary key: the losers see that primary changed and fail.
func TestIntegrationConcurrentRotate(t *testing.T) {
	srv := newIntegrationService(t)
	ctx := context.Background()
	service := uniqueService()
	if _, _, err := srv.CreateClient(ctx, service); err != nil {
		t.Fatal(err)
	}

	const workers = 10
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		rotated int
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := srv.RotateClient(ctx, service, time.Hour)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				rotated++
			case errors.Is(err, ErrServiceClientNotFound):
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	clients, err := srv.ListClients(ctx, service)
	if err != nil {
		t.Fatal(err)
	}

	primary := 0
	for _, client := range clients {
		if client.IsActive && client.ExpiresAt == nil {
			primary++
		}
	}
	if rotated == 0 || primary != 1 || len(clients) != rotated+1 {
		t.Fatalf("rotated = %d, primary = %d, clients = %d", rotated, primary, len(clients))
	}
}
