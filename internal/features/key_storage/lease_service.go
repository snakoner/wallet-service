package key_storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	defaultLeaseTTL = time.Minute
	maxLeaseTTL     = 10 * time.Minute
)

func leaseTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultLeaseTTL
	}

	return min(ttl, maxLeaseTTL)
}

// AcquireLease gives free active key of own role to one service instance until lease expires or is released.
func (s *Service) AcquireLease(ctx context.Context, params AcquireLeaseParams) (*Lease, error) {
	role, err := s.repo.GetRole(ctx, s.txm.GetPool(), params.Service, params.Purpose, params.Label, params.Network)
	if err != nil {
		return nil, err
	}

	if role.Kind == RoleKindDeposit {
		return nil, ErrInvalidParams.WithMessage("deposit keys are not leased")
	}

	leaseID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate lease id: %w", err)
	}

	key, err := s.repo.AcquireLease(ctx, s.txm.GetPool(), role.ID, leaseID, params.Owner, leaseTTL(params.TTL), params.Exclude)
	if err != nil {
		return nil, err
	}
	key.Role = *role

	s.logKey(key, "wallet key leased", "lease_id", leaseID.String(), "lease_owner", params.Owner)

	return &Lease{ID: leaseID, ExpiresAt: *key.LeasedUntil, Key: *key}, nil
}

func (s *Service) ExtendLease(ctx context.Context, service string, leaseID uuid.UUID, ttl time.Duration) (time.Time, error) {
	return s.repo.ExtendLease(ctx, s.txm.GetPool(), leaseID, service, leaseTTL(ttl))
}

func (s *Service) ReleaseLease(ctx context.Context, service string, leaseID uuid.UUID) error {
	return s.repo.ReleaseLease(ctx, s.txm.GetPool(), leaseID, service)
}

// ForceReleaseLease frees key held by stuck instance.
func (s *Service) ForceReleaseLease(ctx context.Context, keyID uuid.UUID) error {
	if err := s.repo.ForceReleaseLease(ctx, s.txm.GetPool(), keyID); err != nil {
		return err
	}

	s.log.Info().Str("key_id", keyID.String()).Msg("wallet key lease force released")

	return nil
}

// checkLease: pool key signs only with its live lease; any key under live lease signs only with that lease.
func checkLease(key *WalletKey, leaseID *uuid.UUID) error {
	switch {
	case key.Role.Kind == RoleKindPool && leaseID == nil:
		return ErrLeaseRequired
	case key.LeaseActive && leaseID == nil:
		return ErrKeyLeased
	case leaseID != nil && (!key.LeaseActive || key.LeaseID == nil || *key.LeaseID != *leaseID):
		return ErrLeaseExpired
	}

	return nil
}
