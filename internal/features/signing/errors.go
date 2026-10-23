package signing

import "gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/apperror"

var (
	ErrInvalidTransaction = apperror.BadRequest("INVALID_TRANSACTION", "invalid transaction")
	ErrPolicyViolation    = apperror.Forbidden("POLICY_VIOLATION", "transaction is not allowed by role policy")
)
