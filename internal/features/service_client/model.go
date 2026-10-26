package service_client

import (
	"time"

	"github.com/google/uuid"
)

type ServiceClient struct {
	ID         uuid.UUID
	Service    string
	KeyHash    []byte
	IsActive   bool
	CreatedAt  time.Time
	RevokedAt  *time.Time
	ExpiresAt  *time.Time // old key after rotation is accepted until this time
	ReplacedBy *uuid.UUID // new key after rotation
}

const (
	StatePrimary  = "primary"
	StateExpiring = "expiring"
	StateExpired  = "expired"
	StateRevoked  = "revoked"
)

func (c *ServiceClient) State(now time.Time) string {
	switch {
	case !c.IsActive:
		return StateRevoked
	case c.ExpiresAt != nil && !c.ExpiresAt.After(now):
		return StateExpired
	case c.ExpiresAt != nil:
		return StateExpiring
	}

	return StatePrimary
}
