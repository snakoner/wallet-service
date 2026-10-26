package service_client

import (
	"time"

	"github.com/google/uuid"
)

const defaultRotationGrace = 24 * time.Hour

type CreateClientRequest struct {
	Service string `json:"service" binding:"required"`
}

type RotateClientRequest struct {
	Service      string `json:"service" binding:"required"`
	GraceSeconds *int   `json:"grace_seconds" binding:"omitempty,min=0"` // default 24h, 0 rejects old key at once
}

type ListClientsQuery struct {
	Service string `form:"service"`
}

type ClientResponse struct {
	ID         uuid.UUID  `json:"id"`
	Service    string     `json:"service"`
	State      string     `json:"state"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	ReplacedBy *uuid.UUID `json:"replaced_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type IssuedKeyResponse struct {
	ServiceKey string          `json:"service_key"` // shown once, only hash is stored
	Client     ClientResponse  `json:"client"`
	OldClient  *ClientResponse `json:"old_client,omitempty"`
}

func NewClientResponse(client *ServiceClient, now time.Time) ClientResponse {
	return ClientResponse{
		ID:         client.ID,
		Service:    client.Service,
		State:      client.State(now),
		ExpiresAt:  client.ExpiresAt,
		ReplacedBy: client.ReplacedBy,
		CreatedAt:  client.CreatedAt,
		RevokedAt:  client.RevokedAt,
	}
}
