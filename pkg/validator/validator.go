// ==============================================================================
// VALIDATOR PACKAGE - pkg/validator/validator.go
// ==============================================================================
package validator

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

type Validator struct {
	validate *validator.Validate
}

func New() *Validator {
	return &Validator{
		validate: validator.New(),
	}
}

func (v *Validator) Validate(i interface{}) error {
	if err := v.validate.Struct(i); err != nil {
		// Format validation errors
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errMessages []string
			for _, e := range validationErrors {
				errMessages = append(errMessages, fmt.Sprintf(
					"Field '%s' failed validation '%s'",
					e.Field(),
					e.Tag(),
				))
			}
			return fmt.Errorf("validation failed: %v", errMessages)
		}
		return err
	}
	return nil
}
