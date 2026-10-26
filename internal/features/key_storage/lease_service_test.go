package key_storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func TestLease(t *testing.T) {
	repo := &repoStub{}
	srv := newTestService(t, repo)
	ctx := context.Background()

	pool := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindPool)
	first, _ := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID})
	second, _ := srv.CreateKey(ctx, CreateKeyParams{RoleID: pool.ID})
	params := AcquireLeaseParams{Service: "exchange", Purpose: "gas_station", Label: "pool", Network: constants.NetworkEthereum, Owner: "pod-1"}

	lease, err := srv.AcquireLease(ctx, params)
	if err != nil || lease.Key.ID != first.ID || lease.Key.Role.Kind != constants.RoleKindPool || !lease.ExpiresAt.After(time.Now()) {
		t.Fatalf("lease = %+v, err = %v", lease, err)
	}
	if lease.ExpiresAt.After(time.Now().Add(defaultLeaseTTL + time.Second)) {
		t.Fatalf("default ttl: expires at %s", lease.ExpiresAt)
	}

	params.Exclude = []uuid.UUID{second.ID}
	if _, err := srv.AcquireLease(ctx, params); !errors.Is(err, ErrNoFreeKey) {
		t.Fatalf("all leased or excluded: err = %v", err)
	}

	params.Exclude, params.TTL = nil, time.Hour
	if other, err := srv.AcquireLease(ctx, params); err != nil || other.Key.ID != second.ID || other.ExpiresAt.After(time.Now().Add(maxLeaseTTL+time.Second)) {
		t.Fatalf("second lease = %+v, err = %v", other, err)
	}

	noop := func(*WalletKey) error { return nil }
	sign := func(keyID uuid.UUID, leaseID *uuid.UUID) error {
		return srv.UseSigningKey(ctx, "exchange", keyID, constants.NetworkEthereum, leaseID, noop, func(*WalletKey, []byte) error { return nil })
	}
	stranger := uuid.New()
	if err := sign(first.ID, &lease.ID); err != nil {
		t.Fatalf("sign with own lease: err = %v", err)
	}
	if err := sign(first.ID, nil); !errors.Is(err, ErrLeaseRequired) {
		t.Fatalf("pool key without lease: err = %v", err)
	}
	if err := sign(first.ID, &stranger); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("pool key with other lease: err = %v", err)
	}

	if _, err := srv.ExtendLease(ctx, "exchange", lease.ID, 0); err != nil {
		t.Fatalf("extend: err = %v", err)
	}
	if err := srv.ReleaseLease(ctx, "exchange", lease.ID); err != nil {
		t.Fatalf("release: err = %v", err)
	}
	if err := sign(first.ID, &lease.ID); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("sign with released lease: err = %v", err)
	}
	if _, err := srv.ExtendLease(ctx, "exchange", lease.ID, 0); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("extend released: err = %v", err)
	}

	deposit := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindDeposit)
	if _, err := srv.AcquireLease(ctx, AcquireLeaseParams{Service: "exchange", Purpose: deposit.Purpose, Label: deposit.Label, Network: constants.NetworkEthereum}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("deposit lease: err = %v", err)
	}
}

func TestLeaseSingleKey(t *testing.T) {
	srv := newTestService(t, &repoStub{})
	ctx := context.Background()

	role := createTestRole(t, srv, constants.NetworkEthereum, constants.RoleKindSingle)
	key, _ := srv.CreateKey(ctx, CreateKeyParams{RoleID: role.ID})
	noop := func(*WalletKey) error { return nil }
	sign := func(leaseID *uuid.UUID) error {
		return srv.UseSigningKey(ctx, "exchange", key.ID, constants.NetworkEthereum, leaseID, noop, func(*WalletKey, []byte) error { return nil })
	}

	if err := sign(nil); err != nil {
		t.Fatalf("single key without lease: err = %v", err)
	}

	lease, err := srv.AcquireLease(ctx, AcquireLeaseParams{Service: "exchange", Purpose: role.Purpose, Label: role.Label, Network: role.Network})
	if err != nil {
		t.Fatal(err)
	}
	if err := sign(nil); !errors.Is(err, ErrKeyLeased) {
		t.Fatalf("leased single key without lease: err = %v", err)
	}
	if err := sign(&lease.ID); err != nil {
		t.Fatalf("single key with lease: err = %v", err)
	}

	if err := srv.ForceReleaseLease(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := sign(nil); err != nil {
		t.Fatalf("after force release: err = %v", err)
	}
}
