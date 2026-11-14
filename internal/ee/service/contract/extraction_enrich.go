package contract

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const DefaultReviewThreshold = 90

const (
	CustomerMatchExisting = "existing"
	CustomerMatchNew      = "new"
)

const (
	MatchedOnEmail       = "billing_email"
	MatchedOnLegalName   = "legal_name"
	MatchedOnEmailDomain = "email_domain"
)

const maxConfidence = 100

var periodMonths = map[string]int{"MONTHLY": 1, "QUARTERLY": 3, "HALF_YEARLY": 6, "ANNUAL": 12}

var genericEmailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true,
	"yahoo.com": true, "icloud.com": true, "proton.me": true, "protonmail.com": true, "aol.com": true,
}

var legalSuffixes = []string{"incorporated", "inc", "llc", "l.l.c", "ltd", "limited", "corp", "corporation",
	"co", "company", "plc", "gmbh", "pvt", "private", "llp", "lp", "sa", "ag", "bv", "pty"}

type CustomerFinder interface {
	GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error)
}

// enrich adds the derived dates and review counts to an extracted result.
func (c *contractIngestService) enrich(result *extraction.Result) error {
	if result == nil {
		return ierr.NewError("result is required").Mark(ierr.ErrValidation)
	}
	result.Derived = c.derive(result)
	result.ToCheck = c.countToCheck(result, c.reviewThreshold)
	return nil
}
