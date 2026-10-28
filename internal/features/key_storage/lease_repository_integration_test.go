//go:build integration

package key_storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestIntegrationLease(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()

	role := createIntegrationRole(t, srv, uniqueService(), "executor", constants.NetworkEthereum, constants.RoleKindPool, "")
	for range 3 {
		if _, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID}); err != nil {
			t.Fatal(err)
		}
	}
	params := AcquireLeaseParams{Service: role.Service, Purpose: role.Purpose, Network: role.Network, Owner: "pod-1", TTL: time.Minute}

	// 20 instances race for 3 keys: each key goes to exactly one of them
	const workers = 20
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		leased = map[uuid.UUID]int{}
		noFree int
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := srv.AcquireLease(ctx, params)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				leased[lease.Key.ID]++
			case errors.Is(err, ErrNoFreeKey):
				noFree++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(leased) != 3 || noFree != workers-3 {
		t.Fatalf("leased keys = %v, no free = %d", leased, noFree)
	}
	for id, n := range leased {
		if n != 1 {
			t.Fatalf("key %s leased %d times", id, n)
		}
	}
}

func TestIntegrationLeaseOnlyActiveKeys(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()

	role := createIntegrationRole(t, srv, uniqueService(), "executor", constants.NetworkEthereum, constants.RoleKindPool, "")
	keys := make([]*WalletKey, 3)
	for i := range keys {
		key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = key
	}
	if _, err := srv.DisableKey(ctx, keys[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RevokeKey(ctx, keys[1].ID); err != nil {
		t.Fatal(err)
	}
	pending, err := srv.RotateKey(ctx, keys[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	params := AcquireLeaseParams{Service: role.Service, Purpose: role.Purpose, Network: role.Network, TTL: time.Minute}

	// disabled, revoked and pending keys are skipped
	lease, err := srv.AcquireLease(ctx, params)
	if err != nil || lease.Key.ID != keys[2].ID {
		t.Fatalf("lease = %+v, err = %v", lease, err)
	}
	if _, err := srv.AcquireLease(ctx, params); !errors.Is(err, ErrNoFreeKey) {
		t.Fatalf("only active key is leased: err = %v", err)
	}

	// rotating key is skipped too
	if err := srv.ReleaseLease(ctx, role.Service, lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ActivateKey(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	lease, err = srv.AcquireLease(ctx, params)
	if err != nil || lease.Key.ID != pending.ID {
		t.Fatalf("lease after activate = %+v, err = %v", lease, err)
	}
	if _, err := srv.AcquireLease(ctx, params); !errors.Is(err, ErrNoFreeKey) {
		t.Fatalf("rotating key is leased: err = %v", err)
	}
}

func TestIntegrationLeaseExpiresAndReleases(t *testing.T) {
	srv, _ := newIntegrationService(t)
	ctx := context.Background()

	role := createIntegrationRole(t, srv, uniqueService(), "executor", constants.NetworkEthereum, constants.RoleKindPool, "")
	key, err := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	if err != nil {
		t.Fatal(err)
	}
	params := AcquireLeaseParams{Service: role.Service, Purpose: role.Purpose, Network: role.Network, TTL: time.Second}

	lease, err := srv.AcquireLease(ctx, params)
	if err != nil || lease.Key.ID != key.ID {
		t.Fatalf("lease = %+v, err = %v", lease, err)
	}
	if _, err := srv.AcquireLease(ctx, params); !errors.Is(err, ErrNoFreeKey) {
		t.Fatalf("leased key: err = %v", err)
	}

	time.Sleep(1200 * time.Millisecond)
	if _, err := srv.ExtendLease(ctx, role.Service, lease.ID, time.Minute); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("extend expired lease: err = %v", err)
	}
	next, err := srv.AcquireLease(ctx, params)
	if err != nil || next.Key.ID != key.ID || next.ID == lease.ID {
		t.Fatalf("lease after expiry = %+v, err = %v", next, err)
	}

	if _, err := srv.ExtendLease(ctx, role.Service, next.ID, time.Minute); err != nil {
		t.Fatalf("extend live lease: err = %v", err)
	}
	if err := srv.ReleaseLease(ctx, uniqueService(), next.ID); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("release by other service: err = %v", err)
	}
	if err := srv.ReleaseLease(ctx, role.Service, next.ID); err != nil {
		t.Fatalf("release: err = %v", err)
	}
	params.TTL = time.Minute
	if _, err := srv.AcquireLease(ctx, params); err != nil {
		t.Fatalf("lease after release: err = %v", err)
	}
}
