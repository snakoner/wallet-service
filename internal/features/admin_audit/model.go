package admin_audit

import (
	"time"

	"github.com/google/uuid"
)

const (
	SourceCLI = "cli"
	SourceAPI = "api"
)

const (
	ActionRoleCreate          = "role.create"
	ActionRolePolicyUpdate    = "role.policy_update"
	ActionKeyCreate           = "key.create"
	ActionKeyImport           = "key.import"
	ActionKeyRotate           = "key.rotate"
	ActionKeyActivate         = "key.activate"
	ActionKeyRevoke           = "key.revoke"
	ActionKeyDisable          = "key.disable"
	ActionKeyEnable           = "key.enable"
	ActionKeyReleaseLease     = "key.release_lease"
	ActionKEKRewrap           = "kek.rewrap"
	ActionServiceClientCreate = "service_client.create"
	ActionServiceClientRotate = "service_client.rotate"
	ActionServiceClientRevoke = "service_client.revoke"
)

type Record struct {
	ID              uuid.UUID
	CreatedAt       time.Time
	Actor           string
	Source          string
	Action          string
	Service         string // '' - not related to service
	RoleID          *uuid.UUID
	KeyID           *uuid.UUID
	ServiceClientID *uuid.UUID
	Details         map[string]any
}

type ListFilter struct {
	Actor           string
	Action          string
	Service         string
	RoleID          uuid.UUID
	KeyID           uuid.UUID // also records where key is replacement or replaced key
	ServiceClientID uuid.UUID
	From            time.Time
	To              time.Time // exclusive
	Offset          int
	Limit           int
}
