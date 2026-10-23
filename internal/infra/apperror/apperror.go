package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type AppError struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	HTTPStatus int               `json:"-"`
	Fields     map[string]string `json:"fields,omitempty"`
	Cause      error             `json:"-"`
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}

	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %s", e.Code, e.Message, e.Cause.Error())
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

func (e *AppError) WithCause(cause error) *AppError {
	if e == nil {
		return nil
	}

	cp := *e
	cp.Cause = cause
	return &cp
}

func (e *AppError) WithFields(fields map[string]string) *AppError {
	if e == nil {
		return nil
	}

	cp := *e
	cp.Fields = fields
	return &cp
}

func (e *AppError) WithMessage(msg string) *AppError {
	if e == nil {
		return nil
	}

	cp := *e
	cp.Message = msg
	return &cp
}

func (e *AppError) Is(target error) bool {
	var t *AppError
	if !errors.As(target, &t) {
		return false
	}

	return e != nil && t != nil && e.Code == t.Code
}

func As(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}

	return nil, false
}

func New(httpStatus int, code, message string) *AppError {
	return &AppError{HTTPStatus: httpStatus, Code: code, Message: message}
}

func Wrap(err error, httpStatus int, code, message string) *AppError {
	if err == nil {
		return nil
	}

	return &AppError{HTTPStatus: httpStatus, Code: code, Message: message, Cause: err}
}

func FromError(err error) *AppError {
	if err == nil {
		return nil
	}

	if ae, ok := As(err); ok {
		return ae
	}

	return &AppError{
		HTTPStatus: http.StatusInternalServerError,
		Code:       CodeInternal,
		Message:    "internal error",
		Cause:      err,
	}
}

func BadRequest(code, message string) *AppError {
	return New(http.StatusBadRequest, code, message)
}

func Unauthorized(code, message string) *AppError {
	return New(http.StatusUnauthorized, code, message)
}

func Forbidden(code, message string) *AppError {
	return New(http.StatusForbidden, code, message)
}

func NotFound(code, message string) *AppError {
	return New(http.StatusNotFound, code, message)
}

func Conflict(code, message string) *AppError {
	return New(http.StatusConflict, code, message)
}

func Unprocessable(code, message string) *AppError {
	return New(http.StatusUnprocessableEntity, code, message)
}

func TooManyRequests(code, message string) *AppError {
	return New(http.StatusTooManyRequests, code, message)
}

func ServiceUnavailable(code, message string) *AppError {
	return New(http.StatusServiceUnavailable, code, message)
}

func Internal(code, message string) *AppError {
	return New(http.StatusInternalServerError, code, message)
}
