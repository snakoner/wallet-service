package service_client

import "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/apperror"

var (
	ErrUnauthorized   = apperror.Unauthorized("UNAUTHORIZED", "unauthorized")
	ErrInvalidService = apperror.BadRequest("INVALID_SERVICE", "service name is required")
)
