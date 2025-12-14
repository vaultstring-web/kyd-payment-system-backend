// ==============================================================================
// VALIDATOR PACKAGE - pkg/validator/validator.go
// ==============================================================================
package validator

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

type Validator struct {
	validate *validator.Validate
}

func New() *Validator {
	v := &Validator{
		validate: validator.New(),
	}
	v.registerCustomValidations()
	return v
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

func (v *Validator) registerCustomValidations() {
	_ = v.validate.RegisterValidation("phone_by_country", func(fl validator.FieldLevel) bool {
		phone := strings.TrimSpace(fl.Field().String())
		// Basic E.164 format (+ then up to 15 digits)
		e164 := regexp.MustCompile(`^\+[1-9]\d{1,14}$`)
		if !e164.MatchString(phone) {
			return false
		}
		// Attempt to read CountryCode from the parent struct
		parent := fl.Parent()
		if parent.IsValid() && parent.Kind() == reflect.Struct {
			cf := parent.FieldByName("CountryCode")
			if cf.IsValid() && cf.Kind() == reflect.String {
				cc := strings.ToUpper(strings.TrimSpace(cf.String()))
				switch cc {
				case "CN":
					return strings.HasPrefix(phone, "+86")
				case "MW":
					return strings.HasPrefix(phone, "+265")
				default:
					// For other countries, accept any E.164 number
					return true
				}
			}
		}
		// If no country found, require E.164 only
		return true
	})
}
