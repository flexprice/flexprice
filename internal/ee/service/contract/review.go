package contract

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
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

// reviewDoc is the stored review: only the reviewer's changes to the extraction. Values holds the
// edited term values by path; the reviewed terms are the extraction with Values applied.
type reviewDoc struct {
	Values    map[string]string                    `json:"values,omitempty"`
	Confirmed []string                             `json:"confirmed,omitempty"`
	Customer  *dto.ContractReviewCustomer          `json:"customer,omitempty"`
	Products  map[string]dto.ContractReviewProduct `json:"products,omitempty"`
}

// GetReview returns the working copy of the contract terms; before the first save it is the extraction.
func (c *contractIngestService) GetReview(ctx context.Context, contractID string) (*dto.ContractReviewResponse, error) {
	contract, out, doc, err := c.loadReview(ctx, contractID)
	if err != nil {
		return nil, err
	}
	return c.reviewView(ctx, contract, out, doc)
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

	terms := c.termsByPath(out.Result)
	for path, value := range req.Values {
		t, err := c.stepTerm(terms, step, path)
		if err != nil {
			return nil, err
		}
		current, edited := doc.Values[path]
		unchanged := value == t.Value || (edited && value == current)
		if value != "" && !unchanged && !c.validValue(path, value) {
			return nil, c.invalidValue(path, value)
		}
		if value == t.Value {
			delete(doc.Values, path)
			continue
		}
		doc.Values[path] = value
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
	if req.Products != nil {
		if err := c.applyProductChoices(ctx, step, doc, len(out.Result.Charges), req.Products); err != nil {
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
	return c.reviewView(ctx, saved, out, doc)
}

// loadReview reads the contract, its extraction and the reviewer's saved changes.
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

	var doc reviewDoc
	if raw := contract.Review(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, nil, nil, ierr.WithError(err).
				WithHint("The saved contract review could not be read").
				Mark(ierr.ErrInternal)
		}
	}
	if doc.Values == nil {
		doc.Values = map[string]string{}
	}
	if doc.Confirmed == nil {
		doc.Confirmed = []string{}
	}
	if doc.Products == nil {
		doc.Products = map[string]dto.ContractReviewProduct{}
	}
	return contract, out, &doc, nil
}

// reviewed returns a copy of the extracted terms with the reviewer's values applied. A saved value
// for a term the extraction no longer has is ignored.
func (c *contractIngestService) reviewed(extracted *extraction.Result, values map[string]string) (*extraction.Result, error) {
	raw, err := json.Marshal(extracted)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrInternal)
	}
	var r extraction.Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrInternal)
	}
	r.CustomerMatch, r.Derived, r.ToCheck = nil, nil, nil
	terms := c.termsByPath(&r)
	for path, value := range values {
		if t, ok := terms[path]; ok {
			t.Value = value
		}
	}
	return &r, nil
}

// reviewView adds the current customer and product matches, the derived values and the counts left
// to check. A term the reviewer changed or confirmed is no longer left to check.
func (c *contractIngestService) reviewView(ctx context.Context, contract *domainContract.Contract, out *extraction.Extraction, doc *reviewDoc) (*dto.ContractReviewResponse, error) {
	reviewed, err := c.reviewed(out.Result, doc.Values)
	if err != nil {
		return nil, err
	}
	edited := c.editedPaths(out.Result, reviewed)
	settled := lo.SliceToMap(append(append([]string{}, doc.Confirmed...), edited...), func(path string) (string, bool) { return path, true })
	if doc.Customer != nil {
		settled[customerMatchPath] = true
	}

	result := *reviewed
	result.CustomerMatch = out.Result.CustomerMatch
	result.Derived = c.derive(&result)
	result.ToCheck = c.countToCheck(&result, c.reviewThreshold, func(path string) bool { return settled[path] })
	matches := c.productMatches(ctx, contract.ID(), result.Charges)
	c.countProductsToCheck(result.ToCheck, matches, doc.Products)

	return &dto.ContractReviewResponse{
		ContractID:     contract.ID(),
		Version:        contract.ReviewVersion(),
		Result:         &result,
		Confirmed:      doc.Confirmed,
		Edited:         edited,
		Customer:       doc.Customer,
		Products:       doc.Products,
		ProductMatches: matches,
	}, nil
}

func (c *contractIngestService) productMatches(ctx context.Context, contractID string, charges []extraction.Charge) map[string]*dto.ContractProductMatch {
	matches, err := c.catalog.Match(ctx, charges)
	if err != nil {
		c.logger.Error(ctx, "contract product match failed", "error", err, "contract_id", contractID)
		return c.catalog.Unavailable(charges)
	}
	return matches
}

// countProductsToCheck adds each charge with no saved product decision and no single exact match.
func (c *contractIngestService) countProductsToCheck(tc *extraction.ToCheck, matches map[string]*dto.ContractProductMatch, decisions map[string]dto.ContractReviewProduct) {
	for path, m := range matches {
		if _, decided := decisions[path]; decided || m.Status == ProductMatchMatched {
			continue
		}
		tc.Charges++
		tc.Total++
	}
}

// applyProductChoices replaces the product decisions; every key must be a charge of this contract.
func (c *contractIngestService) applyProductChoices(ctx context.Context, step string, doc *reviewDoc, charges int, choices map[string]dto.ContractReviewProduct) error {
	if step != stepCharges {
		return ierr.NewError("products saved on the wrong step").
			WithHint("Save product decisions on the charges step").
			Mark(ierr.ErrValidation)
	}
	paths := lo.Keys(choices)
	sort.Strings(paths)
	for _, path := range paths {
		if !c.isChargePath(path, charges) {
			return ierr.NewError("unknown charge for a product decision").
				WithHintf("%s is not a charge of this contract", path).
				WithReportableDetails(map[string]any{"path": path}).
				Mark(ierr.ErrValidation)
		}
		if err := c.validProductChoice(ctx, path, choices[path]); err != nil {
			return err
		}
	}
	doc.Products = lo.Assign(choices)
	return nil
}

func (c *contractIngestService) validProductChoice(ctx context.Context, path string, choice dto.ContractReviewProduct) error {
	switch choice.Choice {
	case reviewChoiceNew:
		if choice.ProductID != "" {
			return ierr.NewError("product_id set for a new product").
				WithHintf("Leave product_id empty for %s when creating a new product", path).
				WithReportableDetails(map[string]any{"path": path}).
				Mark(ierr.ErrValidation)
		}
	case reviewChoiceExisting:
		if choice.ProductID == "" {
			return ierr.NewError("product_id is required").
				WithHintf("Choose the existing product for %s", path).
				WithReportableDetails(map[string]any{"path": path}).
				Mark(ierr.ErrValidation)
		}
		_, err := c.catalog.Get(ctx, choice.ProductID)
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("The chosen product was not found").
				WithReportableDetails(map[string]any{"path": path, "product_id": choice.ProductID}).
				Mark(ierr.ErrValidation)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *contractIngestService) isChargePath(path string, charges int) bool {
	index, ok := strings.CutPrefix(path, "charges[")
	if !ok {
		return false
	}
	index, ok = strings.CutSuffix(index, "]")
	if !ok {
		return false
	}
	i, err := strconv.Atoi(index)
	return err == nil && i >= 0 && i < charges && strconv.Itoa(i) == index
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
