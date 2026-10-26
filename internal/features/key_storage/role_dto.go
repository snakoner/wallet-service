package key_storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

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
