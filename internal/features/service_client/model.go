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
