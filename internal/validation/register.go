package validation

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

func Register() error {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return fmt.Errorf("failed to get validator engine")
	}

	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		for _, tag := range []string{"json", "form", "uri"} {
			name := fld.Tag.Get(tag)
			if name == "" || name == "-" {
				continue
			}

			if idx := strings.Index(name, ","); idx >= 0 {
				name = name[:idx]
			}

			if name != "" {
				return name
			}
		}

		return fld.Name
	})

	if err := v.RegisterValidation("network", ValidateNetwork); err != nil {
		return err
	}

	return nil
}
