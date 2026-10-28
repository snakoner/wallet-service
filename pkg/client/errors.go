package client

import (
	"fmt"
	"time"
)

type Error struct {
	Status     int               `json:"-"`
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Fields     map[string]string `json:"fields,omitempty"` // VALIDATION_FAILED: field -> problem
	RetryAfter time.Duration     `json:"-"`                // NO_FREE_KEY: when to retry
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("status %d", e.Status)
	}

	return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code != "" && t.Code == e.Code
}

var (
	ErrUnauthorized       = &Error{Code: "UNAUTHORIZED"}
	ErrValidationFailed   = &Error{Code: "VALIDATION_FAILED"}
	ErrInvalidParams      = &Error{Code: "INVALID_PARAMS"}
	ErrRoleNotFound       = &Error{Code: "ROLE_NOT_FOUND"}
	ErrKeyNotFound        = &Error{Code: "KEY_NOT_FOUND"}
	ErrKeyNotActive       = &Error{Code: "KEY_NOT_ACTIVE"}
	ErrNoFreeKey          = &Error{Code: "NO_FREE_KEY"}
	ErrLeaseRequired      = &Error{Code: "LEASE_REQUIRED"}
	ErrLeaseExpired       = &Error{Code: "LEASE_EXPIRED"}
	ErrKeyLeased          = &Error{Code: "KEY_LEASED"}
	ErrInvalidTransaction = &Error{Code: "INVALID_TRANSACTION"}
	ErrUnsupportedNetwork = &Error{Code: "UNSUPPORTED_NETWORK"}
	ErrPolicyViolation    = &Error{Code: "POLICY_VIOLATION"}
)
