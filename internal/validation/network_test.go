package validation

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

type networkValidationRequest struct {
	Network string `validate:"required,network"`
}

func TestValidateNetwork(t *testing.T) {
	v := validator.New()
	if err := v.RegisterValidation("network", ValidateNetwork); err != nil {
		t.Fatal(err)
	}

	for _, network := range []string{"ethereum", "tron"} {
		if err := v.Struct(networkValidationRequest{Network: network}); err != nil {
			t.Fatalf("%s: err = %v", network, err)
		}
	}

	for _, network := range []string{"btc", "Ethereum", ""} {
		if err := v.Struct(networkValidationRequest{Network: network}); err == nil {
			t.Fatalf("%q: expected error", network)
		}
	}
}

type enumValidationRequest struct {
	Kind   string `validate:"required,role_kind"`
	Status string `validate:"omitempty,key_status"`
}

func TestValidateRoleKindAndKeyStatus(t *testing.T) {
	v := validator.New()
	if err := v.RegisterValidation("role_kind", ValidateRoleKind); err != nil {
		t.Fatal(err)
	}
	if err := v.RegisterValidation("key_status", ValidateKeyStatus); err != nil {
		t.Fatal(err)
	}

	for _, req := range []enumValidationRequest{{"single", ""}, {"pool", "active"}, {"deposit", "revoked"}} {
		if err := v.Struct(req); err != nil {
			t.Fatalf("%+v: err = %v", req, err)
		}
	}

	for _, req := range []enumValidationRequest{{"", ""}, {"multi", ""}, {"Single", ""}, {"single", "deleted"}} {
		if err := v.Struct(req); err == nil {
			t.Fatalf("%+v: expected error", req)
		}
	}
}
