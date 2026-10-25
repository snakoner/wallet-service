package service_client

import "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/apperror"

var (
	ErrUnauthorized          = apperror.Unauthorized("UNAUTHORIZED", "unauthorized")
	ErrInvalidService        = apperror.BadRequest("INVALID_SERVICE", "service name is required")
	ErrServiceClientExists   = apperror.Conflict("SERVICE_CLIENT_EXISTS", "service already has active key, rotate it instead")
	ErrServiceClientNotFound = apperror.NotFound("SERVICE_CLIENT_NOT_FOUND", "service client not found")
	ErrInvalidGrace          = apperror.BadRequest("INVALID_GRACE", "grace period must not be negative")
)
