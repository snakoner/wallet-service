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
