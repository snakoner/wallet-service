package key_storage

import (
	"time"

	"github.com/google/uuid"
)

type AcquireLeaseRequest struct {
	Network    string      `json:"network" binding:"required,network"`
	Label      string      `json:"label"`
	Owner      string      `json:"owner" binding:"max=128"`
	TTLSeconds int         `json:"ttl_seconds" binding:"omitempty,min=30"`
	Exclude    []uuid.UUID `json:"exclude"`
}

type ExtendLeaseRequest struct {
	TTLSeconds int `json:"ttl_seconds" binding:"omitempty,min=30"`
}

type LeaseResponse struct {
	LeaseID   uuid.UUID   `json:"lease_id"`
	ExpiresAt time.Time   `json:"expires_at"`
	Key       KeyResponse `json:"key"`
}

type ExtendLeaseResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
}
