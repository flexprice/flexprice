package validator

import (
	"errors"
	"net/url"
	"strings"
	"sync"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/go-playground/validator/v10"
)

var (
	validate *validator.Validate
	once     sync.Once
)

// initValidator initializes the validator exactly once
func initValidator() {
	once.Do(func() {
		validate = validator.New()
	})
}

func NewValidator() *validator.Validate {
	initValidator()
	return validate
}

func GetValidator() *validator.Validate {
	initValidator()
	return validate
}

func ValidateRequest(req interface{}) error {
	initValidator()

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

func ValidateURL(raw *string) error {
	if raw == nil {
		return nil
	}

	if strings.TrimSpace(*raw) == "" {
		return nil
	}

	u, err := url.ParseRequestURI(*raw)
	if err != nil {
		return errors.New("url must be a valid URL")
	}

	if u.Scheme != "https" {
		return errors.New("url must start with https://")
	}

	if u.Host == "" {
		return errors.New("url must have a valid host")
	}

	return nil
}
