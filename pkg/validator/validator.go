// ==============================================================================
// VALIDATOR PACKAGE - pkg/validator/validator.go
// ==============================================================================
package validator

import (
	"fmt"
	"html"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
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

// ValidateStructured returns a map of field -> error message for frontend usage
func (v *Validator) ValidateStructured(i interface{}) map[string]string {
	errs := make(map[string]string)
	if err := v.validate.Struct(i); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			for _, e := range validationErrors {
				msg := fmt.Sprintf("failed validation on '%s'", e.Tag())
				switch e.Tag() {
				case "required":
					msg = "This field is required"
				case "email":
					msg = "Invalid email address"
				case "min":
					msg = fmt.Sprintf("Must be at least %s characters", e.Param())
				case "max":
					msg = fmt.Sprintf("Must be at most %s characters", e.Param())
				case "e164":
					msg = "Invalid phone number format (E.164 required)"
				case "phone_by_country":
					msg = "Invalid phone number for the selected country"
				}
				errs[e.Field()] = msg
			}
		} else {
			errs["_global"] = err.Error()
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (v *Validator) registerCustomValidations() {
	// Register decimal.Decimal to be validated as float64 for gt/lt checks
	v.validate.RegisterCustomTypeFunc(func(field reflect.Value) interface{} {
		if val, ok := field.Interface().(decimal.Decimal); ok {
			f, _ := val.Float64()
			return f
		}
		return nil
	}, decimal.Decimal{})

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

// Sanitize cleans string input to prevent XSS attacks
func Sanitize(input string) string {
	return html.EscapeString(strings.TrimSpace(input))
}
