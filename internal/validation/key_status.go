package validation

import (
	"github.com/go-playground/validator/v10"

	"gitlab.tve-development.com/backend/gtoken/wallet-service/internal/constants"
)

func ValidateKeyStatus(fl validator.FieldLevel) bool {
	_, ok := constants.ParseKeyStatus(fl.Field().String())
	return ok
}
