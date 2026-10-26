package key_storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

type CreateRoleParams struct {
	Service string
	Purpose string
	Label   string
	Network constants.Network
	Kind    RoleKind
	Policy  json.RawMessage
}

type CreateKeyParams struct {
	RoleID      uuid.UUID
	Alias       *string
	ExternalRef *string
}

type ImportKeyParams struct {
	RoleID      uuid.UUID
	PrivateKey  []byte // hex, cleared after import
	Alias       *string
	ExternalRef *string
}

type ListKeysFilter struct {
	Service     *string
	Purpose     *string
	Label       *string
	Network     *constants.Network
	Status      *KeyStatus
	ExternalRef *string
	Limit       int
}

type ListKeysQuery struct {
	Service     *string `form:"service"`
	Purpose     *string `form:"purpose"`
	Label       *string `form:"label"`
	Network     string  `form:"network" binding:"omitempty,network"`
	Status      string  `form:"status" binding:"omitempty,oneof=active rotating pending disabled revoked"`
	ExternalRef *string `form:"external_ref"`
	Limit       int     `form:"limit" binding:"omitempty,min=1,max=1000"`
}

type AcquireLeaseParams struct {
	Service string
	Purpose string
	Label   string
	Network constants.Network
	Owner   string
	TTL     time.Duration
	Exclude []uuid.UUID
}

type AcquireLeaseRequest struct {
	Network    string      `json:"network" binding:"required,network"`
	Label      string      `json:"label"`
	Owner      string      `json:"owner" binding:"max=128"`
	TTLSeconds int         `json:"ttl_seconds" binding:"omitempty,min=1"`
	Exclude    []uuid.UUID `json:"exclude"`
}

type ExtendLeaseRequest struct {
	TTLSeconds int `json:"ttl_seconds" binding:"omitempty,min=1"`
}

type LeaseResponse struct {
	LeaseID   uuid.UUID   `json:"lease_id"`
	ExpiresAt time.Time   `json:"expires_at"`
	Key       KeyResponse `json:"key"`
}

type ExtendLeaseResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
}

type GetRoleKeyParams struct {
	Service string
	Purpose string
	Label   string
	Network constants.Network
}

type GetRoleKeyQuery struct {
	Label   string `form:"label"`
	Network string `form:"network" binding:"required,network"`
}

type KeyResponse struct {
	ID          uuid.UUID  `json:"id"`
	Service     string     `json:"service"`
	Purpose     string     `json:"purpose"`
	Label       string     `json:"label"`
	Network     string     `json:"network"`
	Kind        string     `json:"kind"`
	Alias       *string    `json:"alias,omitempty"`
	Address     string     `json:"address"`
	Status      string     `json:"status"`
	ReplacedBy  *uuid.UUID `json:"replaced_by,omitempty"`
	LeaseOwner  *string    `json:"lease_owner,omitempty"`
	LeasedUntil *time.Time `json:"leased_until,omitempty"` // new key after rotation
	CreatedAt   time.Time  `json:"created_at"`
}

func NewKeyResponse(key *WalletKey) KeyResponse {
	return KeyResponse{
		ID:          key.ID,
		Service:     key.Role.Service,
		Purpose:     key.Role.Purpose,
		Label:       key.Role.Label,
		Network:     key.Network.String(),
		Kind:        key.Role.Kind.String(),
		Alias:       key.Alias,
		Address:     key.Address,
		Status:      key.Status.String(),
		ReplacedBy:  key.ReplacedBy,
		LeaseOwner:  key.LeaseOwner,
		LeasedUntil: key.LeasedUntil,
		CreatedAt:   key.CreatedAt,
	}
}

type CreateRoleRequest struct {
	Service string          `json:"service" binding:"required"`
	Purpose string          `json:"purpose" binding:"required"`
	Label   string          `json:"label"`
	Network string          `json:"network" binding:"required,network"`
	Kind    string          `json:"kind" binding:"required,oneof=single pool deposit"`
	Policy  json.RawMessage `json:"policy" swaggertype:"object"`
}

type UpdatePolicyRequest struct {
	Policy json.RawMessage `json:"policy" binding:"required" swaggertype:"object"`
}

type ListRolesQuery struct {
	Service string `form:"service"`
}

type ImportKeyRequest struct {
	PrivateKey  string  `json:"private_key" binding:"required"` // hex, with or without 0x
	Alias       *string `json:"alias"`
	ExternalRef *string `json:"external_ref"` // only for deposit role
}

type CreateRoleKeyRequest struct {
	Alias       *string `json:"alias"`
	ExternalRef *string `json:"external_ref"` // only for deposit role
}

type RoleResponse struct {
	ID        uuid.UUID       `json:"id"`
	Service   string          `json:"service"`
	Purpose   string          `json:"purpose"`
	Label     string          `json:"label"`
	Network   string          `json:"network"`
	Kind      string          `json:"kind"`
	Policy    json.RawMessage `json:"policy" swaggertype:"object"`
	CreatedAt time.Time       `json:"created_at"`
}

func NewRoleResponse(role *Role) RoleResponse {
	return RoleResponse{
		ID:        role.ID,
		Service:   role.Service,
		Purpose:   role.Purpose,
		Label:     role.Label,
		Network:   role.Network.String(),
		Kind:      role.Kind.String(),
		Policy:    role.Policy,
		CreatedAt: role.CreatedAt,
	}
}

func NewRolesResponse(roles []Role) []RoleResponse {
	resp := make([]RoleResponse, len(roles))
	for i := range roles {
		resp[i] = NewRoleResponse(&roles[i])
	}

	return resp
}

func NewKeysResponse(keys []WalletKey) []KeyResponse {
	resp := make([]KeyResponse, len(keys))
	for i := range keys {
		resp[i] = NewKeyResponse(&keys[i])
	}

	return resp
}
