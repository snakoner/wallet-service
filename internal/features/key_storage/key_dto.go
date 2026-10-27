package key_storage

import (
	"time"

	"github.com/google/uuid"
)

type ListKeysQuery struct {
	Service     *string `form:"service"`
	Purpose     *string `form:"purpose"`
	Label       *string `form:"label"`
	Network     string  `form:"network" binding:"omitempty,network"`
	Status      string  `form:"status" binding:"omitempty,key_status" enums:"active,rotating,pending,disabled,revoked"`
	ExternalRef *string `form:"external_ref"`
}

type GetDepositKeyRequest struct {
	Network     string  `json:"network" binding:"required,network"`
	Label       string  `json:"label"`
	ExternalRef string  `json:"external_ref" binding:"required,max=128"`
	Alias       *string `json:"alias" binding:"omitempty,max=128"`
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
	ExternalRef *string    `json:"external_ref,omitempty"`
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
		ExternalRef: key.ExternalRef,
		Status:      key.Status.String(),
		ReplacedBy:  key.ReplacedBy,
		LeaseOwner:  key.LeaseOwner,
		LeasedUntil: key.LeasedUntil,
		CreatedAt:   key.CreatedAt,
	}
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

func NewKeysResponse(keys []WalletKey) []KeyResponse {
	resp := make([]KeyResponse, len(keys))
	for i := range keys {
		resp[i] = NewKeyResponse(&keys[i])
	}

	return resp
}
