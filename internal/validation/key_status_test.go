package validation

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

type keyStatusValidationRequest struct {
	Status string `validate:"omitempty,key_status"`
}

func TestValidateKeyStatus(t *testing.T) {
	v := validator.New()
	if err := v.RegisterValidation("key_status", ValidateKeyStatus); err != nil {
		t.Fatal(err)
	}

	for _, status := range []string{"active", "rotating", "pending", "disabled", "revoked", ""} {
		if err := v.Struct(keyStatusValidationRequest{Status: status}); err != nil {
			t.Fatalf("%q: err = %v", status, err)
		}
	}

	for _, status := range []string{"deleted", "Active"} {
		if err := v.Struct(keyStatusValidationRequest{Status: status}); err == nil {
			t.Fatalf("%q: expected error", status)
		}
	}
}
