package validator

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func NewValidator() *validator.Validate {
	validate = validator.New()
	return validate
}

func GetValidator() *validator.Validate {
	return validate
}

func ValidateRequest(req interface{}) error {
	if validate == nil {
		return ierr.NewError("validator not initialized").
			WithHint("Validator must be initialized before using it").
			Mark(ierr.ErrSystem)
	}

	if err := validate.Struct(req); err != nil {
		details := make(map[string]any)
		var validateErrs validator.ValidationErrors
		if ierr.As(err, &validateErrs) {
			for _, err := range validateErrs {
				details[err.Field()] = err.Error()
			}
		}
		return ierr.WithError(err).
			WithHint("Request validation failed").
			WithReportableDetails(details).
			Mark(ierr.ErrValidation)
	}
	return nil
}
