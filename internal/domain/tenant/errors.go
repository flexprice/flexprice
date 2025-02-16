package tenant

import (
	"github.com/cockroachdb/errors"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

var (
	ErrNotFound      = errors.WithSafeDetails(errors.New("tenant not found"), ierr.ErrCodeNotFound)
	ErrAlreadyExists = errors.WithSafeDetails(errors.New("tenant already exists"), ierr.ErrCodeAlreadyExists)
)
