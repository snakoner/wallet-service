package httputil

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/infra/apperror"
)

type ErrorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type Response struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type ErrorResponse struct {
	Success bool      `json:"success" example:"false"`
	Error   ErrorBody `json:"error"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

func Fail(c *gin.Context, err error) {
	ae := apperror.FromError(err)
	if ae == nil {
		c.JSON(http.StatusInternalServerError, Response{Success: false, Error: &ErrorBody{
			Code: apperror.CodeInternal, Message: "internal error",
		}})
		return
	}

	c.JSON(ae.HTTPStatus, Response{Success: false, Error: &ErrorBody{
		Code:    ae.Code,
		Message: ae.Message,
		Fields:  ae.Fields,
	}})
}

func renderCoded(c *gin.Context, status int, defaultCode string, err error) {
	if ae, ok := apperror.As(err); ok {
		c.JSON(ae.HTTPStatus, Response{Success: false, Error: &ErrorBody{
			Code: ae.Code, Message: ae.Message, Fields: ae.Fields,
		}})
		return
	}

	msg := "error"
	if err != nil {
		msg = err.Error()
	}

	c.JSON(status, Response{Success: false, Error: &ErrorBody{
		Code: defaultCode, Message: msg,
	}})
}

func BadRequest(c *gin.Context, err error) {
	renderCoded(c, http.StatusBadRequest, apperror.CodeBadRequest, err)
}

func Internal(c *gin.Context, err error) {
	renderCoded(c, http.StatusInternalServerError, apperror.CodeInternal, err)
}

func Forbidden(c *gin.Context, err error) {
	renderCoded(c, http.StatusForbidden, apperror.CodeForbidden, err)
}

func Unauthorized(c *gin.Context, err error) {
	renderCoded(c, http.StatusUnauthorized, apperror.CodeUnauthorized, err)
}

func Conflict(c *gin.Context, err error) {
	renderCoded(c, http.StatusConflict, apperror.CodeConflict, err)
}

func NotFound(c *gin.Context, err error) {
	renderCoded(c, http.StatusNotFound, apperror.CodeNotFound, err)
}

func UnprocessableEntity(c *gin.Context, err error) {
	renderCoded(c, http.StatusUnprocessableEntity, apperror.CodeUnprocessable, err)
}

func Redirect(c *gin.Context, url string) {
	c.Redirect(http.StatusFound, url)
}

func TooManyRequests(c *gin.Context, err error) {
	renderCoded(c, http.StatusTooManyRequests, apperror.CodeTooManyRequests, err)
}

func ServiceUnavailable(c *gin.Context, err error) {
	renderCoded(c, http.StatusServiceUnavailable, apperror.CodeServiceUnavailable, err)
}

func BadRequestValidation(c *gin.Context, err error) {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		c.JSON(http.StatusBadRequest, Response{Success: false, Error: &ErrorBody{
			Code:    apperror.CodeBadRequest,
			Message: "invalid request",
		}})
		return
	}

	fields := make(map[string]string, len(ve))
	for _, fe := range ve {
		fieldName := validationFieldName(fe)
		fields[fieldName] = validationMessage(fe, fieldName)
	}

	c.JSON(http.StatusBadRequest, Response{Success: false, Error: &ErrorBody{
		Code:    apperror.CodeValidation,
		Message: "validation failed",
		Fields:  fields,
	}})
}

var validationMessages = map[string]string{
	"required": "is required",
	"address":  "must be a valid address",
	"uuid":     "must be a valid uuid",
	"amount":   "must be a valid amount",
	"network":  "unsupported network",
}

func validationMessage(fe validator.FieldError, fieldName string) string {
	if msg, ok := validationMessages[fe.Tag()]; ok {
		return msg
	}

	switch fe.Tag() {
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	case "len":
		return fmt.Sprintf("must be exactly %s characters", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	default:
		return "is invalid"
	}
}

func validationFieldName(fe validator.FieldError) string {
	return fe.Field()
}
