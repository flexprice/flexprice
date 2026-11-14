package contract

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	unverifiedEvidencePenalty = 15
	runnerUpMargin            = 10
)

// MatchCustomer runs the matching agent for a completed extraction and stores its verified answer.
// It does nothing once the match has left the pending state, so retries are safe.
func (c *contractIngestService) MatchCustomer(ctx context.Context, contractID string) error {
	contract, out, err := c.loadExtraction(ctx, contractID)
	if err != nil || out == nil {
		return err
	}
	if out.Result.CustomerMatch != nil && out.Result.CustomerMatch.Status != CustomerMatchPending {
		return nil
	}

	match, err := c.findCustomer(ctx, out.Result.Customer)
	if err != nil {
		return err
	}
	if err := c.saveMatch(ctx, contract, out, match); err != nil {
		return err
	}
	c.logger.Info(ctx, "contract customer matched",
		"contract_id", contractID,
		"status", match.Status,
		"customer_id", match.CustomerID,
		"candidates", len(match.Candidates),
		"steps", match.Steps)
	return nil
}

// MarkCustomerMatchUnavailable records that matching could not run, so the reviewer picks the customer by hand.
func (c *contractIngestService) MarkCustomerMatchUnavailable(ctx context.Context, contractID, reason string) error {
	contract, out, err := c.loadExtraction(ctx, contractID)
	if err != nil || out == nil {
		return err
	}
	if out.Result.CustomerMatch != nil && out.Result.CustomerMatch.Status != CustomerMatchPending {
		return nil
	}
	return c.saveMatch(ctx, contract, out, &extraction.CustomerMatch{
		Status: CustomerMatchUnavailable,
		Reason: c.truncate(reason, maxErrorLength),
	})
}

func (c *contractIngestService) initialMatch() *extraction.CustomerMatch {
	if !c.matchingEnabled() {
		return &extraction.CustomerMatch{Status: CustomerMatchUnavailable, Reason: "Customer matching is turned off"}
	}
	return &extraction.CustomerMatch{Status: CustomerMatchPending}
}

func (c *contractIngestService) findCustomer(ctx context.Context, cust extraction.Customer) (*extraction.CustomerMatch, error) {
	req := &customermatch.MatchRequest{
		LegalName:      strings.TrimSpace(cust.LegalName.Value),
		BillingEmail:   strings.TrimSpace(cust.BillingEmail.Value),
		BillingAddress: strings.TrimSpace(cust.BillingAddress.Value),
		PrimaryContact: strings.TrimSpace(cust.PrimaryContact.Value),
	}
	if req.LegalName == "" && req.BillingEmail == "" {
		return &extraction.CustomerMatch{Status: CustomerMatchNew, Reason: "No customer name or email in the contract"}, nil
	}

	res, err := c.matcher.Match(ctx, req, c.directory)
	if err != nil {
		return nil, err
	}
	return c.verify(ctx, req, res)
}

// verify re-reads every candidate and re-checks the evidence that can be checked, lowering the score
// of claims that do not hold, then decides the status from the thresholds. A candidate is linked
// automatically only when every claim it makes holds.
func (c *contractIngestService) verify(ctx context.Context, req *customermatch.MatchRequest, res *customermatch.MatchResult) (*extraction.CustomerMatch, error) {
	candidates := []extraction.MatchCandidate{}
	unsupported := map[string]bool{}
	for _, cand := range res.Candidates {
		record, err := c.directory.Get(ctx, cand.CustomerID)
		if ierr.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		evidence, dropped := c.checkEvidence(req, record, cand.Evidence)
		unsupported[record.ID] = dropped > 0 || len(evidence) == 0
		confidence := max(0, cand.Confidence-dropped*unverifiedEvidencePenalty)
		if confidence < c.possibleThreshold() {
			continue
		}
		candidates = append(candidates, extraction.MatchCandidate{
			CustomerID: record.ID,
			Name:       record.Name,
			Email:      record.Email,
			Confidence: confidence,
			MatchedOn:  evidence,
			Reason:     cand.Reason,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Confidence > candidates[j].Confidence })

	match := &extraction.CustomerMatch{Status: CustomerMatchNew, Candidates: candidates, Model: res.Model, Steps: res.Steps}
	if len(candidates) == 0 {
		match.Reason = "No existing customer matched"
		return match, nil
	}

	top := candidates[0]
	clearWinner := len(candidates) == 1 || candidates[1].Confidence < top.Confidence-runnerUpMargin
	if top.CustomerID == res.BestCustomerID && top.Confidence >= c.existingThreshold() && clearWinner && !unsupported[top.CustomerID] {
		match.Status = CustomerMatchExisting
		match.CustomerID = top.CustomerID
		match.Name = top.Name
		match.Confidence = top.Confidence
		match.MatchedOn = top.MatchedOn
		match.Reason = top.Reason
		return match, nil
	}
	match.Status = CustomerMatchPossible
	match.Confidence = top.Confidence
	match.Reason = "More than one customer could match, or the best match is not certain"
	return match, nil
}

// checkEvidence keeps the evidence that holds against the stored customer and counts the claims that do not.
func (c *contractIngestService) checkEvidence(req *customermatch.MatchRequest, record *customermatch.CustomerRecord, claimed []string) ([]string, int) {
	kept := []string{}
	dropped := 0
	for _, e := range lo.Uniq(claimed) {
		if c.evidenceHolds(req, record, e) {
			kept = append(kept, e)
		} else {
			dropped++
		}
	}
	return kept, dropped
}

func (c *contractIngestService) evidenceHolds(req *customermatch.MatchRequest, record *customermatch.CustomerRecord, evidence string) bool {
	switch evidence {
	case customermatch.EvidenceExactName:
		name := c.directory.normalizeName(req.LegalName)
		return name != "" && name == c.directory.normalizeName(record.Name)
	case customermatch.EvidenceEmailExact:
		return req.BillingEmail != "" && strings.EqualFold(req.BillingEmail, record.Email)
	case customermatch.EvidenceEmailDomain:
		domain := c.directory.companyDomain(req.BillingEmail)
		return domain != "" && domain == c.directory.companyDomain(record.Email)
	case customermatch.EvidenceAddress:
		return c.addressMatches(req.BillingAddress, record)
	default:
		return true
	}
}

// addressMatches reports whether the contract address names the stored customer's city as whole words.
// A customer with no stored city cannot support an address claim.
func (c *contractIngestService) addressMatches(address string, record *customermatch.CustomerRecord) bool {
	city := c.directory.normalizeName(record.City)
	if city == "" {
		return false
	}
	return strings.Contains(" "+c.directory.normalizeName(address)+" ", " "+city+" ")
}

func (c *contractIngestService) loadExtraction(ctx context.Context, contractID string) (*domainContract.Contract, *extraction.Extraction, error) {
	contract, err := c.contracts.GetByID(ctx, contractID)
	if err != nil {
		return nil, nil, err
	}
	if contract.ExtractionStatus() != types.ContractExtractionStatusCompleted || len(contract.Extraction()) == 0 {
		return contract, nil, nil
	}
	var out extraction.Extraction
	if err := json.Unmarshal(contract.Extraction(), &out); err != nil {
		return nil, nil, ierr.WithError(err).
			WithHint("The stored extraction could not be read").
			Mark(ierr.ErrInternal)
	}
	if out.Result == nil {
		return contract, nil, nil
	}
	return contract, &out, nil
}

func (c *contractIngestService) saveMatch(ctx context.Context, contract *domainContract.Contract, out *extraction.Extraction, match *extraction.CustomerMatch) error {
	out.Result.CustomerMatch = match
	out.Result.ToCheck = c.countToCheck(out.Result, c.reviewThreshold)
	raw, err := json.Marshal(out)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to save the customer match").
			Mark(ierr.ErrInternal)
	}
	return c.contracts.Update(ctx, domainContract.NewBuilder(contract).WithExtraction(raw).Build())
}

func (c *contractIngestService) matchingEnabled() bool {
	return c.config != nil && c.config.CustomerMatch.Enabled && c.matcher != nil && c.matcher.IsConfigured()
}

func (c *contractIngestService) existingThreshold() int {
	if c.config != nil && c.config.CustomerMatch.ExistingThreshold > 0 {
		return c.config.CustomerMatch.ExistingThreshold
	}
	return DefaultExistingThreshold
}

func (c *contractIngestService) possibleThreshold() int {
	if c.config != nil && c.config.CustomerMatch.PossibleThreshold > 0 {
		return c.config.CustomerMatch.PossibleThreshold
	}
	return DefaultPossibleThreshold
}
