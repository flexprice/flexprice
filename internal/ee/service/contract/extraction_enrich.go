package contract

import (
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

const DefaultReviewThreshold = 90

const (
	CustomerMatchPending     = "pending"
	CustomerMatchExisting    = "existing"
	CustomerMatchPossible    = "possible"
	CustomerMatchNew         = "new"
	CustomerMatchUnavailable = "unavailable"
)

const (
	DefaultExistingThreshold = 85
	DefaultPossibleThreshold = 40
)

const maxConfidence = 100

// enrich adds the derived dates and review counts to an extracted result.
func (c *contractIngestService) enrich(result *extraction.Result) error {
	if result == nil {
		return ierr.NewError("result is required").Mark(ierr.ErrValidation)
	}
	result.Derived = c.derive(result)
	result.ToCheck = c.countToCheck(result, c.reviewThreshold)
	return nil
}
