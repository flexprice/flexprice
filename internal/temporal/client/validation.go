package client

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// validateService checks if service is initialized - this is the only validation we really need
func validateService(initialized bool) error {
	if !initialized {
		return ierr.NewError("temporal service not initialized").
			WithHint("Call InitTemporalService() first").
			Mark(ierr.ErrInternal)
	}
	return nil
}
