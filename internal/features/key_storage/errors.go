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
)
