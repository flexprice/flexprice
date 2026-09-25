package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	stripeintegration "github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
)

// calculationTimeout bounds the calculation, which runs inside the transaction that settles
// the invoice. A stall fails the transaction, which rolls back and is retried, rather than
// holding the invoice row lock for the SDK's own eighty second default.
const calculationTimeout = 5 * time.Second

// stripeTaxEngine delegates calculation to Stripe Tax and records the result as a Stripe
// tax transaction at commit.
type stripeTaxEngine struct {
	ServiceParams
}

func (e *stripeTaxEngine) GetProvider() types.TaxProvider {
	return types.TaxProviderStripe
}

func (e *stripeTaxEngine) Calculate(ctx context.Context, req dto.TaxCalculationRequest) (*dto.TaxCalculationResult, error) {
	client := stripeintegration.NewClient(e.ConnectionRepo, e.EncryptionService, e.Logger)
	stripeClient, _, err := client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Stripe reads the address, IP, exemption and tax IDs off its own Customer object, so
	// the reference is sent rather than a snapshot and the two cannot disagree.
	stripeCustomerID, err := e.stripeCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}

	// An invoice calculation runs inside the transaction that settles it, so the SDK's own
	// eighty second default would hold the invoice row lock for that long on a Stripe stall.
	// The deadline covers the SDK's internal retries as well as the first attempt.
	ctx, cancel := context.WithTimeout(ctx, calculationTimeout)
	defer cancel()

	calculation, err := stripeClient.V1TaxCalculations.Create(ctx, calculationParams(req, stripeCustomerID))
	if err != nil {
		details := map[string]any{
			"entity_type":        req.EntityType,
			"entity_id":          req.EntityID,
			"reference":          req.Reference,
			"stripe_customer_id": stripeCustomerID,
		}
		mark := stripeFailureMark(err, details)

		return nil, ierr.WithError(err).
			WithHint("Stripe could not calculate tax").
			WithReportableDetails(details).
			Mark(mark)
	}

	// Without a reference the tax can never be filed. Charging it anyway would bill the
	// customer for something the tenant cannot remit, so refuse before any row is written.
	if calculation.ID == "" {
		return nil, ierr.NewError("stripe returned a tax calculation with no reference").
			WithHint("The tax could not be filed, so none was applied").
			WithReportableDetails(map[string]any{"entity_type": req.EntityType, "entity_id": req.EntityID}).
			Mark(ierr.ErrHTTPClient)
	}

	// The breakdown is only ever read off the expanded line, so without it there is no tax
	// to apply rather than no tax due.
	if calculation.LineItems == nil || len(calculation.LineItems.Data) == 0 {
		return nil, ierr.NewError("stripe returned a tax calculation with no line items").
			WithHint("The tax could not be read back, so none was applied").
			WithReportableDetails(map[string]any{
				"entity_type":    req.EntityType,
				"entity_id":      req.EntityID,
				"calculation_id": calculation.ID,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	return e.resultFrom(calculation, req), nil
}

// Commit records this invoice's tax in Stripe's books, from the calculation that produced
// its records. Nothing is recalculated, so what is filed is what the invoice was sealed with.
func (e *stripeTaxEngine) Commit(ctx context.Context, inv *invoice.Invoice, appliedTaxes []*taxapplied.TaxApplied) (string, error) {
	calculationID := calculationHandle(appliedTaxes)
	if calculationID == "" {
		return "", ierr.NewError("no stripe tax calculation to record").
			WithHint("The invoice's tax records carry no Stripe calculation, so the tax cannot be filed").
			WithReportableDetails(map[string]any{"invoice_id": inv.ID}).
			Mark(ierr.ErrValidation)
	}

	client := stripeintegration.NewClient(e.ConnectionRepo, e.EncryptionService, e.Logger)
	stripeClient, _, err := client.GetStripeClient(ctx)
	if err != nil {
		return "", err
	}

	// The reference must be unique across every transaction the account holds, reversals
	// included, so a retried commit is rejected by Stripe rather than filed twice.
	transaction, err := stripeClient.V1TaxTransactions.CreateFromCalculation(ctx,
		&stripe.TaxTransactionCreateFromCalculationParams{
			Calculation: stripe.String(calculationID),
			Reference:   stripe.String(inv.ID),
		})
	if err != nil {
		details := map[string]any{"invoice_id": inv.ID, "calculation_id": calculationID}
		mark := stripeFailureMark(err, details)

		return "", ierr.WithError(err).
			WithHint("Stripe could not record the tax transaction").
			WithReportableDetails(details).
			Mark(mark)
	}

	return transaction.ID, nil
}

// Reverse records the undoing of tax Stripe already filed. A partial reversal states the gross
// amount coming back, tax included, and Stripe works out how much of it was tax by apportioning
// it across the original transaction's lines at the rates those lines were filed at. It posts
// to the original's tax date, not today's, so the correction lands in the period it belongs to.
func (e *stripeTaxEngine) Reverse(ctx context.Context, req dto.TaxReversalRequest) (string, error) {
	if req.OriginalTransactionID == "" {
		return "", ierr.NewError("no stripe tax transaction to reverse").
			WithHint("The tax being reversed was never filed, so there is nothing to undo").
			WithReportableDetails(map[string]any{"reference": req.Reference}).
			Mark(ierr.ErrValidation)
	}

	client := stripeintegration.NewClient(e.ConnectionRepo, e.EncryptionService, e.Logger)
	stripeClient, _, err := client.GetStripeClient(ctx)
	if err != nil {
		return "", err
	}

	params := &stripe.TaxTransactionCreateReversalParams{
		Mode:                stripe.String(string(req.Mode)),
		OriginalTransaction: stripe.String(req.OriginalTransactionID),
		Reference:           stripe.String(req.Reference),
	}

	// Stripe wants the refund as a negative, and ToSmallestUnit floors at zero, so the sign
	// goes on after the conversion rather than before it.
	if req.Mode == types.TaxReversalModePartial {
		params.FlatAmount = stripe.Int64(-types.ToSmallestUnit(req.Amount, req.Currency))
	}

	transaction, err := stripeClient.V1TaxTransactions.CreateReversal(ctx, params)
	if err != nil {
		details := map[string]any{
			"original_transaction_id": req.OriginalTransactionID,
			"reference":               req.Reference,
			"mode":                    string(req.Mode),
		}
		mark := stripeFailureMark(err, details)

		return "", ierr.WithError(err).
			WithHint("Stripe could not reverse the tax transaction").
			WithReportableDetails(details).
			Mark(mark)
	}

	return transaction.ID, nil
}

// stripeFailureMark reports whether another attempt could help, and records Stripe's own
// code and status on the details so a failure is diagnosable. Anything Stripe rejected
// outright is the tenant's configuration to fix rather than ours to retry.
func stripeFailureMark(err error, details map[string]any) error {
	var stripeErr *stripe.Error
	if !errors.As(err, &stripeErr) {
		return ierr.ErrHTTPClient
	}

	details["stripe_message"] = stripeErr.Msg
	details["stripe_error_code"] = string(stripeErr.Code)
	details["stripe_error_type"] = string(stripeErr.Type)
	details["stripe_status"] = stripeErr.HTTPStatusCode

	if stripeErr.HTTPStatusCode >= 400 && stripeErr.HTTPStatusCode < 500 &&
		stripeErr.HTTPStatusCode != http.StatusTooManyRequests {
		return ierr.ErrValidation
	}
	return ierr.ErrHTTPClient
}

// calculationParams is what Stripe is asked. Tax is resolved, stored and reported at document
// level, so the whole taxable base goes as one line rather than one per invoice line. The base
// is net of discounts, which is what the native engine taxes too; prepaid credits are a payment
// method rather than a discount, so they are not subtracted.
func calculationParams(req dto.TaxCalculationRequest, stripeCustomerID string) *stripe.TaxCalculationCreateParams {
	params := &stripe.TaxCalculationCreateParams{
		Currency: stripe.String(strings.ToLower(req.Currency)),
		Customer: stripe.String(stripeCustomerID),
		LineItems: []*stripe.TaxCalculationCreateLineItemParams{
			{
				Amount:    stripe.Int64(types.ToSmallestUnit(req.Amount, req.Currency)),
				Reference: stripe.String(req.Reference),
			},
		},
	}
	// The line-level breakdown is the only one carrying the jurisdiction, the display name
	// and the sourcing, and it is not returned without this.
	params.AddExpand("line_items.data.tax_breakdown")

	return params
}

// TaxIdentifiers reads both parties' registered tax numbers straight from Stripe, which is
// where the tenant keeps them. They are not stored: the rendered PDF is kept, so it freezes
// them itself, and regenerating an old invoice is rare enough to accept reading them again.
func (e *stripeTaxEngine) TaxIdentifiers(ctx context.Context, customerID string) ([]types.TaxIdentifier, []types.TaxIdentifier, error) {
	client := stripeintegration.NewClient(e.ConnectionRepo, e.EncryptionService, e.Logger)
	stripeClient, _, err := client.GetStripeClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	// The account's own numbers are static configuration, so they are cached rather than
	// read again for every invoice rendered.
	cacheKey := cache.GenerateKey(ctx, cache.PrefixBillerTaxIDs)
	cacheClient := cache.GetInMemoryCache()

	var biller []types.TaxIdentifier
	if cached, found := cacheClient.ForceCacheGet(ctx, cacheKey); found {
		biller, _ = cached.([]types.TaxIdentifier)
	}
	if biller == nil {
		// A nil customer asks for the account's own numbers rather than a customer's.
		biller, err = collectTaxIDs(ctx, stripeClient.V1TaxIDs.List(ctx, &stripe.TaxIDListParams{}),
			stripe.TaxIDOwnerTypeSelf)
		if err != nil {
			return nil, nil, err
		}
		cacheClient.ForceCacheSet(ctx, cacheKey, biller, cache.ExpiryBillerTaxIDs)
	}

	stripeCustomerID, err := e.stripeCustomerID(ctx, customerID)
	if err != nil {
		return biller, nil, err
	}

	customer, err := collectTaxIDs(ctx,
		stripeClient.V1TaxIDs.List(ctx, &stripe.TaxIDListParams{Customer: stripe.String(stripeCustomerID)}),
		stripe.TaxIDOwnerTypeCustomer)
	if err != nil {
		return biller, nil, err
	}

	return biller, customer, nil
}

// collectTaxIDs drains a tax id listing, keeping only the owner asked for. The same endpoint
// serves the account's numbers and a customer's, so the owner is what tells them apart.
func collectTaxIDs(ctx context.Context, iter func(func(*stripe.TaxID, error) bool), owner stripe.TaxIDOwnerType) ([]types.TaxIdentifier, error) {
	taxIDs := make([]types.TaxIdentifier, 0)

	for taxID, err := range iter {
		if err != nil {
			details := map[string]any{"owner": string(owner)}
			mark := stripeFailureMark(err, details)

			return nil, ierr.WithError(err).
				WithHint("Stripe could not list the registered tax numbers").
				WithReportableDetails(details).
				Mark(mark)
		}
		if taxID.Owner == nil || taxID.Owner.Type != owner {
			continue
		}
		taxIDs = append(taxIDs, types.TaxIdentifier{
			Type:    string(taxID.Type),
			Value:   taxID.Value,
			Country: taxID.Country,
		})
	}

	return taxIDs, nil
}

// calculationHandle reads the Stripe calculation these records were written from.
func calculationHandle(appliedTaxes []*taxapplied.TaxApplied) string {
	for _, applied := range appliedTaxes {
		if id := applied.ExternalTaxDetails.GetCalculationID(); id != "" {
			return id
		}
	}
	return ""
}

// resultFrom maps the line-level breakdown onto one applied-tax row per entry. That breakdown
// is the only one carrying the jurisdiction, the display name and the sourcing, and because the
// whole taxable base is sent as a single line its entries cover all of it. Every entry becomes a
// row, including one a jurisdiction imposed nothing for: the filing has to match what the engine
// returned, and the document decides separately what to show.
func (e *stripeTaxEngine) resultFrom(calculation *stripe.TaxCalculation, req dto.TaxCalculationRequest) *dto.TaxCalculationResult {
	currency := req.Currency
	line := calculation.LineItems.Data[0]

	result := &dto.TaxCalculationResult{
		InclusiveTax:      types.FromSmallestUnit(calculation.TaxAmountInclusive, currency),
		ExclusiveTax:      types.FromSmallestUnit(calculation.TaxAmountExclusive, currency),
		Provider:          types.TaxProviderStripe,
		TaxAppliedRecords: make([]*dto.TaxAppliedResponse, 0, len(line.TaxBreakdown)),
		// Read off the engine's own total rather than added up here, so what a credit note
		// returns is what the engine says the transaction came to.
		AmountTotal: types.FromSmallestUnit(calculation.AmountTotal, currency),
	}
	result.TotalTaxAmount = result.InclusiveTax.Add(result.ExclusiveTax)

	expiry := ""
	if calculation.ExpiresAt > 0 {
		expiry = time.Unix(calculation.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}

	// Behavior belongs to the line rather than to each tax on it, so every row shares it.
	behavior := types.TaxBehaviorExclusive
	if line.TaxBehavior == stripe.TaxCalculationLineItemTaxBehaviorInclusive {
		behavior = types.TaxBehaviorInclusive
	}

	for _, entry := range line.TaxBreakdown {
		// Commit reads the calculation back from here, and the invoice renders from the rest:
		// the engine resolved the rate, so this row is the only record of what it was.
		details := &types.ExternalTaxDetails{
			CalculationID:        calculation.ID,
			CalculationExpiresAt: expiry,
			TaxCode:              line.TaxCode,
			TaxabilityReason:     string(entry.TaxabilityReason),
			Sourcing:             string(entry.Sourcing),
			// What this row is filed under, and what a later reversal must not reuse.
			Reference: req.Reference,
		}

		// Null whenever the jurisdiction imposed no tax, which still produces a row.
		if rate := entry.TaxRateDetails; rate != nil {
			details.DisplayName = rate.DisplayName
			details.TaxType = string(rate.TaxType)
			details.Percentage = rate.PercentageDecimal
		}

		if jurisdiction := entry.Jurisdiction; jurisdiction != nil {
			details.Jurisdiction = &types.TaxJurisdiction{
				Country:     jurisdiction.Country,
				State:       jurisdiction.State,
				DisplayName: jurisdiction.DisplayName,
				Level:       string(jurisdiction.Level),
			}
		}

		result.TaxAppliedRecords = append(result.TaxAppliedRecords, &dto.TaxAppliedResponse{
			TaxApplied: taxapplied.TaxApplied{
				EntityType:         req.EntityType,
				EntityID:           req.EntityID,
				TaxableAmount:      types.FromSmallestUnit(entry.TaxableAmount, currency),
				TaxAmount:          types.FromSmallestUnit(entry.Amount, currency),
				TaxBehavior:        behavior,
				Currency:           currency,
				Provider:           types.TaxProviderStripe,
				ExternalTaxDetails: details,
			},
		})
	}

	// Reverse charge obliges the invoice to carry a statement whatever else was taxed, so it
	// is recorded either way. The other reasons describe a document that charged nothing, and
	// one jurisdiction imposing nothing beside another that taxed is not that: a state adding
	// no tax of its own is ordinary, not an exemption.
	if reason := stripeExemptionReason(line.TaxBreakdown); reason != nil {
		if result.TotalTaxAmount.IsZero() || *reason == types.TaxExemptionReasonReverseCharge {
			result.ExemptionReason = reason
		}
	}

	return result
}

// stripeExemptionReason maps Stripe's reason for charging zero onto ours. Five of its
// fifteen values mean no tax was charged, each for a materially different reason, and the
// difference decides whether an operator needs to act. Anything else leaves it unset.
func stripeExemptionReason(breakdown []*stripe.TaxCalculationLineItemTaxBreakdown) *types.TaxExemptionReasonCode {
	var first *types.TaxExemptionReasonCode

	for _, entry := range breakdown {
		var mapped types.TaxExemptionReasonCode

		switch entry.TaxabilityReason {
		case stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonReverseCharge:
			mapped = types.TaxExemptionReasonReverseCharge
		case stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonCustomerExempt:
			mapped = types.TaxExemptionReasonCustomerExempt
		case stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotCollecting:
			mapped = types.TaxExemptionReasonNotCollecting
		case stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSubjectToTax:
			mapped = types.TaxExemptionReasonNotSubjectToTax
		case stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSupported:
			mapped = types.TaxExemptionReasonNotSupported
		default:
			continue
		}

		// Reverse charge obliges the invoice to carry a statement, so it outranks any other
		// reason on the same calculation.
		if mapped == types.TaxExemptionReasonReverseCharge {
			return &mapped
		}
		if first == nil {
			first = lo.ToPtr(mapped)
		}
	}

	return first
}

func (e *stripeTaxEngine) stripeCustomerID(ctx context.Context, customerID string) (string, error) {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = customerID
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.ProviderTypes = []string{string(types.SecretProviderStripe)}

	mappings, err := e.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", err
	}

	for _, mapping := range mappings {
		if mapping.ProviderEntityID != "" {
			return mapping.ProviderEntityID, nil
		}
	}

	return "", ierr.NewError("customer is not synced to stripe").
		WithHint("The customer must exist in Stripe before tax can be calculated for them").
		WithReportableDetails(map[string]any{"customer_id": customerID}).
		Mark(ierr.ErrNotFound)
}
