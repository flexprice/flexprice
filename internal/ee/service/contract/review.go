package contract

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

const (
	reviewChoiceExisting = "existing"
	reviewChoiceNew      = "new"
)

// reviewDoc is the stored review: the reviewer's copy of the extracted terms, the term paths they
// confirmed and their customer decision. The match, derived values and counts are not
// stored; they are worked out each time the review is read.
type reviewDoc struct {
	Result    *extraction.Result          `json:"result"`
	Confirmed []string                    `json:"confirmed"`
	Customer  *dto.ContractReviewCustomer `json:"customer,omitempty"`
}

// GetReview returns the working copy of the contract terms; before the first save it is the extraction.
func (c *contractIngestService) GetReview(ctx context.Context, contractID string) (*dto.ContractReviewResponse, error) {
	contract, out, doc, err := c.loadReview(ctx, contractID)
	if err != nil {
		return nil, err
	}
	return c.reviewView(contract, out, doc), nil
}

// SaveReviewStep applies one step's edits, confirmations and customer decision to the review.
// It fails with a version conflict when the review changed since req.Version.
func (c *contractIngestService) SaveReviewStep(ctx context.Context, contractID, step string, req *dto.SaveContractReviewRequest) (*dto.ContractReviewResponse, error) {
	if err := c.validStep(step); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	contract, out, doc, err := c.loadReview(ctx, contractID)
	if err != nil {
		return nil, err
	}

	terms := c.termsByPath(doc.Result)
	for path, value := range req.Values {
		t, err := c.stepTerm(terms, step, path)
		if err != nil {
			return nil, err
		}
		if value != "" && value != t.Value && !c.validValue(path, value) {
			return nil, c.invalidValue(path, value)
		}
		t.Value = value
	}
	if req.Confirmed != nil {
		for _, path := range req.Confirmed {
			if _, err := c.stepTerm(terms, step, path); err != nil {
				return nil, err
			}
		}
		kept := lo.Filter(doc.Confirmed, func(path string, _ int) bool { return c.stepFor(path) != step })
		doc.Confirmed = lo.Uniq(append(kept, req.Confirmed...))
		sort.Strings(doc.Confirmed)
	}
	if req.Customer != nil {
		if err := c.applyCustomerChoice(ctx, step, doc, req.Customer); err != nil {
			return nil, err
		}
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to save the contract review").
			Mark(ierr.ErrInternal)
	}
	version, err := c.contracts.UpdateReview(ctx, contractID, raw, req.Version)
	if err != nil {
		return nil, err
	}
	saved := domainContract.NewBuilder(contract).WithReview(raw).WithReviewVersion(version).Build()
	return c.reviewView(saved, out, doc), nil
}

// loadReview reads the contract, its extraction and its review, starting the review from the
// extraction when none is saved yet.
func (c *contractIngestService) loadReview(ctx context.Context, contractID string) (*domainContract.Contract, *extraction.Extraction, *reviewDoc, error) {
	contract, out, err := c.loadExtraction(ctx, contractID)
	if err != nil {
		return nil, nil, nil, err
	}
	if out == nil {
		return nil, nil, nil, ierr.NewError("contract terms are not extracted yet").
			WithHint("Wait until extraction_status is completed").
			Mark(ierr.ErrInvalidOperation)
	}

	raw := contract.Review()
	if len(raw) == 0 {
		if raw, err = json.Marshal(reviewDoc{Result: out.Result, Confirmed: []string{}}); err != nil {
			return nil, nil, nil, ierr.WithError(err).Mark(ierr.ErrInternal)
		}
	}
	var doc reviewDoc
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Result == nil {
		return nil, nil, nil, ierr.NewError("stored review could not be read").
			WithHint("The saved contract review could not be read").
			Mark(ierr.ErrInternal)
	}
	doc.Result.CustomerMatch, doc.Result.Derived, doc.Result.ToCheck = nil, nil, nil
	if doc.Confirmed == nil {
		doc.Confirmed = []string{}
	}
	return contract, out, &doc, nil
}

// reviewView adds the current customer match, the derived values and the counts left
// to check. A term the reviewer changed or confirmed is no longer left to check.
func (c *contractIngestService) reviewView(contract *domainContract.Contract, out *extraction.Extraction, doc *reviewDoc) *dto.ContractReviewResponse {
	edited := c.editedPaths(out.Result, doc.Result)
	settled := lo.SliceToMap(append(append([]string{}, doc.Confirmed...), edited...), func(path string) (string, bool) { return path, true })
	if doc.Customer != nil {
		settled[customerMatchPath] = true
	}

	result := *doc.Result
	result.CustomerMatch = out.Result.CustomerMatch
	result.Derived = c.derive(&result)
	result.ToCheck = c.countToCheck(&result, c.reviewThreshold, func(path string) bool { return settled[path] })

	return &dto.ContractReviewResponse{
		ContractID: contract.ID(),
		Version:    contract.ReviewVersion(),
		Result:     &result,
		Confirmed:  doc.Confirmed,
		Edited:     edited,
		Customer:   doc.Customer,
	}
}

func (c *contractIngestService) applyCustomerChoice(ctx context.Context, step string, doc *reviewDoc, choice *dto.ContractReviewCustomer) error {
	if step != stepCustomer {
		return ierr.NewError("customer saved on the wrong step").
			WithHint("Save the customer decision on the customer step").
			Mark(ierr.ErrValidation)
	}
	switch choice.Choice {
	case reviewChoiceNew:
		if choice.CustomerID != "" {
			return ierr.NewError("customer_id set for a new customer").
				WithHint("Leave customer_id empty when creating a new customer").
				Mark(ierr.ErrValidation)
		}
	case reviewChoiceExisting:
		if choice.CustomerID == "" {
			return ierr.NewError("customer_id is required").
				WithHint("Choose the existing customer this contract is for").
				Mark(ierr.ErrValidation)
		}
		_, err := c.directory.Get(ctx, choice.CustomerID)
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("The chosen customer was not found").
				WithReportableDetails(map[string]any{"customer_id": choice.CustomerID}).
				Mark(ierr.ErrValidation)
		}
		if err != nil {
			return err
		}
	}
	doc.Customer = &dto.ContractReviewCustomer{Choice: choice.Choice, CustomerID: choice.CustomerID}
	return nil
}

func (c *contractIngestService) termsByPath(r *extraction.Result) map[string]*extraction.Term {
	terms := map[string]*extraction.Term{}
	c.eachTerm(r, func(path string, t *extraction.Term) { terms[path] = t })
	return terms
}

func (c *contractIngestService) stepTerm(terms map[string]*extraction.Term, step, path string) (*extraction.Term, error) {
	t, ok := terms[path]
	if !ok || c.stepFor(path) != step {
		return nil, ierr.NewError("unknown term for this step").
			WithHintf("%s is not a term of the %s step", path, step).
			WithReportableDetails(map[string]any{"path": path, "step": step}).
			Mark(ierr.ErrValidation)
	}
	return t, nil
}

// editedPaths lists the terms whose reviewed value differs from the extracted one.
func (c *contractIngestService) editedPaths(extracted, reviewed *extraction.Result) []string {
	before := c.termsByPath(extracted)
	edited := []string{}
	c.eachTerm(reviewed, func(path string, t *extraction.Term) {
		if orig, ok := before[path]; ok && orig.Value != t.Value {
			edited = append(edited, path)
		}
	})
	sort.Strings(edited)
	return edited
}

func (c *contractIngestService) validStep(step string) error {
	switch step {
	case stepCustomer, stepCharges, stepBilling:
		return nil
	}
	return ierr.NewError("unknown review step").
		WithHint("The step must be customer, charges or billing").
		WithReportableDetails(map[string]any{"step": step}).
		Mark(ierr.ErrValidation)
}

// validValue checks a term value against the format the extraction uses for that field.
func (c *contractIngestService) validValue(path, value string) bool {
	switch path[strings.LastIndex(path, ".")+1:] {
	case "signed_date", "effective_date", "expiry":
		return c.validDate(value)
	case "term_months", "renewal_term_months", "notice_days":
		return c.validCount(value)
	case "auto_renew":
		return value == "true" || value == "false"
	case "amount", "included_quantity":
		return c.validAmount(value)
	case "amount_period", "billing_period", "period":
		return c.validPeriod(value)
	case "billing_timing":
		return c.validTiming(value)
	case "tier_mode":
		return value == contractagent.TierModeGraduated || value == contractagent.TierModeVolume
	case "currency":
		return len(value) == 3 && strings.Trim(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
	}
	return true
}

func (c *contractIngestService) invalidValue(path, value string) error {
	return ierr.NewError("invalid term value").
		WithHintf("%s has an invalid value", path).
		WithReportableDetails(map[string]any{"field": path, "value": value}).
		Mark(ierr.ErrValidation)
}
