package validation

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

type roleKindValidationRequest struct {
	Kind string `validate:"required,role_kind"`
}

func TestValidateRoleKind(t *testing.T) {
	v := validator.New()
	if err := v.RegisterValidation("role_kind", ValidateRoleKind); err != nil {
		t.Fatal(err)
	}

	for _, kind := range []string{"single", "pool", "deposit"} {
		if err := v.Struct(roleKindValidationRequest{Kind: kind}); err != nil {
			t.Fatalf("%s: err = %v", kind, err)
		}
	}

	for _, kind := range []string{"multi", "Single", ""} {
		if err := v.Struct(roleKindValidationRequest{Kind: kind}); err == nil {
			t.Fatalf("%q: expected error", kind)
		}
	}
}
