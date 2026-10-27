package key_storage

import "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/apperror"

var (
	ErrUnsupportedNetwork = apperror.BadRequest("UNSUPPORTED_NETWORK", "unsupported network")
	ErrInvalidParams      = apperror.BadRequest("INVALID_PARAMS", "invalid params")
	ErrActiveKeyExists    = apperror.Conflict("ACTIVE_KEY_EXISTS", "active key already exists")
	ErrUnsupportedStatus  = apperror.BadRequest("UNSUPPORTED_STATUS", "unsupported key status")
	ErrKeyNotFound        = apperror.NotFound("KEY_NOT_FOUND", "key not found")
	ErrKeyNotActive       = apperror.Conflict("KEY_NOT_ACTIVE", "key is not active")
	ErrKeyExists          = apperror.Conflict("KEY_EXISTS", "key already exists")
	ErrRoleNotFound       = apperror.NotFound("ROLE_NOT_FOUND", "role not found")
	ErrRoleExists         = apperror.Conflict("ROLE_EXISTS", "role already exists")
	ErrInvalidKeyStatus   = apperror.Conflict("INVALID_KEY_STATUS", "operation is not allowed in current key status")
	ErrRotationInProgress = apperror.Conflict("ROTATION_IN_PROGRESS", "key already has pending replacement")
	ErrNoFreeKey          = apperror.Conflict("NO_FREE_KEY", "no free key in role, retry later")
	ErrLeaseExpired       = apperror.Conflict("LEASE_EXPIRED", "lease expired or belongs to other instance")
	ErrLeaseRequired      = apperror.Conflict("LEASE_REQUIRED", "active key requires lease")
	ErrKeyLeased          = apperror.Conflict("KEY_LEASED", "key is leased by other instance")
)
