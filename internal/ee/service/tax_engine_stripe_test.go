package service

import (
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v82"
)

// ---------------------------------------------------------------------------
// Request construction. calculationParams is the whole request, so the shape
// Stripe receives can be asserted without a network call.
// ---------------------------------------------------------------------------

// stripeTestInvoice carries a taxable base, which is all the calculation request reads.
func stripeTestInvoice(currency, subtotal, discount string) *invoice.Invoice {
	return &invoice.Invoice{
		ID:            "inv_stripe_test",
		Currency:      currency,
		Subtotal:      decimal.RequireFromString(subtotal),
		TotalDiscount: decimal.RequireFromString(discount),
	}
}

func TestCalculationParams_TheWholeTaxableBaseIsSentAsOneLine(t *testing.T) {
	inv := stripeTestInvoice("USD", "197.00", "0")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	require.Len(t, params.LineItems, 1,
		"tax is resolved, stored and reported at invoice level, so the base is sent as one line")
	assert.Equal(t, int64(19700), *params.LineItems[0].Amount)
	require.Len(t, params.Expand, 1)
	assert.Equal(t, "line_items.data.tax_breakdown", *params.Expand[0],
		"the line-level breakdown is the only one carrying the jurisdiction and display name")
	assert.Equal(t, "usd", *params.Currency, "Stripe expects a lowercase currency")
	assert.Equal(t, "cus_stripe", *params.Customer,
		"the customer reference is sent rather than an address snapshot, so the two cannot disagree")
}

func TestCalculationParams_BaseIsSentNetOfDiscounts(t *testing.T) {
	inv := stripeTestInvoice("USD", "200.00", "30.00")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	assert.Equal(t, int64(17000), *params.LineItems[0].Amount,
		"Stripe has no discount parameter and expects them applied already")
}

func TestCalculationParams_BaseIsTheSameOneTheNativeEngineWouldTax(t *testing.T) {
	inv := stripeTestInvoice("USD", "200.00", "30.00")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	assert.Equal(t, types.ToSmallestUnit(taxableAmount(inv), inv.Currency), *params.LineItems[0].Amount,
		"the two engines must charge against the same base or they disagree on the same invoice")
}

func TestCalculationParams_PrepaidCreditsAreNotSubtracted(t *testing.T) {
	inv := stripeTestInvoice("USD", "100.00", "0")
	inv.TotalPrepaidCreditsApplied = decimal.RequireFromString("40.00")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	assert.Equal(t, int64(10000), *params.LineItems[0].Amount,
		"a prepaid credit is a payment method, not a discount, so it moves what is payable and not what is taxable")
}

func TestCalculationParams_OverDiscountedInvoiceIsClampedAtZeroNotNegative(t *testing.T) {
	inv := stripeTestInvoice("USD", "40.00", "50.00")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	assert.Equal(t, int64(0), *params.LineItems[0].Amount,
		"a negative amount would be rejected and would corrupt the reconciliation")
}

func TestCalculationParams_LineReferenceIsTheInvoiceID(t *testing.T) {
	inv := stripeTestInvoice("USD", "10.00", "0")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	assert.Equal(t, inv.ID, *params.LineItems[0].Reference,
		"one line carries the whole invoice, so the invoice is what it points at")
}

func TestCalculationParams_ZeroDecimalCurrencyIsNotScaled(t *testing.T) {
	inv := stripeTestInvoice("JPY", "1500", "0")

	params := calculationParams(invoiceTaxRequest(inv), "cus_stripe")

	assert.Equal(t, int64(1500), *params.LineItems[0].Amount,
		"yen has no minor unit, so its smallest unit is the major one")
}

// ---------------------------------------------------------------------------
// Response handling. resultFrom maps the line-level breakdown onto
// records. That breakdown is the only one carrying the jurisdiction, the
// display name and the sourcing.
// ---------------------------------------------------------------------------

func breakdown(amount, taxable int64, reason stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReason) *stripe.TaxCalculationLineItemTaxBreakdown {
	return &stripe.TaxCalculationLineItemTaxBreakdown{
		Amount:           amount,
		TaxableAmount:    taxable,
		TaxabilityReason: reason,
	}
}

// rated attaches the rate detail Stripe returns whenever a jurisdiction imposed something.
func rated(entry *stripe.TaxCalculationLineItemTaxBreakdown, displayName, taxType, percentage string) *stripe.TaxCalculationLineItemTaxBreakdown {
	entry.TaxRateDetails = &stripe.TaxCalculationLineItemTaxBreakdownTaxRateDetails{
		DisplayName:       displayName,
		TaxType:           stripe.TaxCalculationLineItemTaxBreakdownTaxRateDetailsTaxType(taxType),
		PercentageDecimal: percentage,
	}
	return entry
}

func calculationWith(id string, inclusiveTax, exclusiveTax int64, entries ...*stripe.TaxCalculationLineItemTaxBreakdown) *stripe.TaxCalculation {
	return &stripe.TaxCalculation{
		ID:                 id,
		TaxAmountInclusive: inclusiveTax,
		TaxAmountExclusive: exclusiveTax,
		CustomerDetails:    &stripe.TaxCalculationCustomerDetails{},
		LineItems: &stripe.TaxCalculationLineItemList{
			Data: []*stripe.TaxCalculationLineItem{{
				TaxBehavior:  stripe.TaxCalculationLineItemTaxBehaviorExclusive,
				TaxCode:      "txcd_10000000",
				TaxBreakdown: entries,
			}},
		},
	}
}

func TestResultFromCalculation_SingleRateGroupBecomesOneRecord(t *testing.T) {
	engine := &stripeTaxEngine{}
	inv := stripeTestInvoice("USD", "0", "0")
	calc := calculationWith("taxcalc_1", 0, 875,
		breakdown(875, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated))

	result := engine.resultFrom(calc, invoiceTaxRequest(inv))

	require.Len(t, result.TaxAppliedRecords, 1)
	rec := result.TaxAppliedRecords[0]
	assert.True(t, decimal.RequireFromString("8.75").Equal(rec.TaxAmount))
	assert.True(t, decimal.RequireFromString("100").Equal(rec.TaxableAmount))
	assert.True(t, decimal.RequireFromString("8.75").Equal(result.TotalTaxAmount))
}

func TestResultFromCalculation_EveryEntryBecomesARowIncludingOneThatImposedNothing(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_in", 0, 180,
		rated(breakdown(180, 1000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
			"Integrated goods and services tax (IGST)", "igst", "18.0"),
		breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSubjectToTax))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("INR", "0", "0")))

	require.Len(t, result.TaxAppliedRecords, 2,
		"the filing has to match what the engine returned, so a jurisdiction that imposed nothing is still recorded")
	assert.Equal(t, "not_subject_to_tax", result.TaxAppliedRecords[1].ExternalTaxDetails.GetTaxabilityReason())
	assert.True(t, result.TaxAppliedRecords[1].TaxAmount.IsZero())
}

func TestResultFromCalculation_SeveralRateGroupsSumToTheInvoiceTotalTax(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_2", 0, 1500,
		breakdown(900, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
		breakdown(600, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	require.Len(t, result.TaxAppliedRecords, 2)
	sum := decimal.Zero
	for _, rec := range result.TaxAppliedRecords {
		sum = sum.Add(rec.TaxAmount)
	}
	assert.True(t, sum.Equal(result.TotalTaxAmount), "the rows must reconcile to the invoice total tax")
}

func TestResultFromCalculation_BehaviourIsReadFromTheLineNotTheEntry(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_inc", 909, 0,
		breakdown(909, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
		breakdown(0, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonZeroRated))
	calc.LineItems.Data[0].TaxBehavior = stripe.TaxCalculationLineItemTaxBehaviorInclusive

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	require.Len(t, result.TaxAppliedRecords, 2)
	for _, rec := range result.TaxAppliedRecords {
		assert.Equal(t, types.TaxBehaviorInclusive, rec.TaxBehavior,
			"behaviour belongs to the line's amount, so every tax on it shares it")
	}
	assert.True(t, decimal.RequireFromString("9.09").Equal(result.InclusiveTax))
}

func TestResultFromCalculation_CarriesNoFlexpriceRateID(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_5", 0, 100,
		breakdown(100, 1000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	assert.Nil(t, result.TaxAppliedRecords[0].TaxRateID,
		"an engine holds the rate, so there is no Flexprice rate to point at")
}

func TestResultFromCalculation_RecordsCarryTheCalculationHandle(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_6", 0, 100,
		breakdown(100, 1000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated))
	calc.ExpiresAt = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC).Unix()

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	details := result.TaxAppliedRecords[0].ExternalTaxDetails
	assert.Equal(t, "taxcalc_6", details.GetCalculationID(), "commit reads the calculation back from here")
	assert.Equal(t, "2026-09-23T12:00:00Z", details.GetCalculationExpiresAt())
}

func TestResultFromCalculation_NoExpiryLeavesTheDeadlineUnset(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_7", 0, 100,
		breakdown(100, 1000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	assert.Empty(t, result.TaxAppliedRecords[0].ExternalTaxDetails.GetCalculationExpiresAt(),
		"an absent expiry must not be recorded as the zero time")
}

func TestResultFromCalculation_RecordsCarryTheResolvedRateAndJurisdiction(t *testing.T) {
	engine := &stripeTaxEngine{}
	entry := rated(breakdown(1800, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
		"Integrated goods and services tax (IGST)", "igst", "18.0")
	entry.Sourcing = stripe.TaxCalculationLineItemTaxBreakdownSourcingDestination
	entry.Jurisdiction = &stripe.TaxCalculationLineItemTaxBreakdownJurisdiction{
		Country:     "IN",
		DisplayName: "India",
		Level:       stripe.TaxCalculationLineItemTaxBreakdownJurisdictionLevelCountry,
	}
	calc := calculationWith("taxcalc_detail", 0, 1800, entry)

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("INR", "0", "0")))

	details := result.TaxAppliedRecords[0].ExternalTaxDetails
	assert.Equal(t, "Integrated goods and services tax (IGST)", details.GetDisplayName(),
		"the engine resolved the rate, so this row is the only record of it")
	assert.Equal(t, "igst", details.GetTaxType())
	assert.Equal(t, "18.0", details.GetPercentage())
	assert.Equal(t, "standard_rated", details.GetTaxabilityReason())
	assert.Equal(t, "India", details.GetJurisdictionName())
	assert.Equal(t, "destination", details.Sourcing)
	assert.Equal(t, "country", details.Jurisdiction.Level)
	assert.Equal(t, "txcd_10000000", details.GetTaxCode(),
		"the code belongs to the line, so every row on this invoice carries the same one")
}

func TestResultFromCalculation_AnEntryWithNoRateDetailStillBecomesARow(t *testing.T) {
	engine := &stripeTaxEngine{}
	entry := breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSubjectToTax)
	entry.Jurisdiction = &stripe.TaxCalculationLineItemTaxBreakdownJurisdiction{
		Country:     "IN",
		State:       "HR",
		DisplayName: "Haryāna",
		Level:       stripe.TaxCalculationLineItemTaxBreakdownJurisdictionLevelState,
	}
	calc := calculationWith("taxcalc_bare", 0, 0, entry)

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("INR", "0", "0")))

	require.Len(t, result.TaxAppliedRecords, 1)
	details := result.TaxAppliedRecords[0].ExternalTaxDetails
	assert.Equal(t, "taxcalc_bare", details.GetCalculationID(), "commit depends on the handle landing")
	assert.Empty(t, details.GetPercentage(), "no rate was imposed, so none is recorded")
	assert.Equal(t, "Haryāna", details.GetJurisdictionName())
}

func TestResultFromCalculation_AReverseChargedLineIsRecordedEvenWhenTaxWasCharged(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_8", 0, 500,
		breakdown(500, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
		breakdown(0, 2000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonReverseCharge))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	require.NotNil(t, result.ExemptionReason,
		"one jurisdiction reverse charged, and that invoice still owes the statement")
	assert.Equal(t, types.TaxExemptionReasonReverseCharge, *result.ExemptionReason)
	assert.True(t, result.TotalTaxAmount.IsPositive(), "the other jurisdiction was taxed normally")
}

func TestResultFromCalculation_AJurisdictionThatImposedNothingIsNotAnExemption(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_in_mixed", 0, 2700,
		rated(breakdown(2700, 15000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
			"Integrated goods and services tax (IGST)", "igst", "18.0"),
		breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSubjectToTax))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("INR", "0", "0")))

	assert.Nil(t, result.ExemptionReason,
		"a state adding no tax of its own beside a country that taxed is ordinary, and calling it "+
			"an exemption reports a taxed invoice as untaxed")
	assert.True(t, decimal.RequireFromString("27").Equal(result.TotalTaxAmount))
}

func TestResultFromCalculation_AFullyTaxedInvoiceRecordsNoReason(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_8b", 0, 500,
		breakdown(500, 10000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated))

	result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

	assert.Nil(t, result.ExemptionReason,
		"standard rating is not a reason anything was exempt")
}

func TestResultFromCalculation_ZeroTaxRecordsTheReason(t *testing.T) {
	cases := []struct {
		name   string
		reason stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReason
		want   types.TaxExemptionReasonCode
	}{
		{"reverse charge", stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonReverseCharge, types.TaxExemptionReasonReverseCharge},
		{"customer exempt", stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonCustomerExempt, types.TaxExemptionReasonCustomerExempt},
		{"not collecting", stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotCollecting, types.TaxExemptionReasonNotCollecting},
		{"not subject to tax", stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSubjectToTax, types.TaxExemptionReasonNotSubjectToTax},
		{"not supported", stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotSupported, types.TaxExemptionReasonNotSupported},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := &stripeTaxEngine{}
			calc := calculationWith("taxcalc_zero", 0, 0, breakdown(0, 10000, tc.reason))

			result := engine.resultFrom(calc, invoiceTaxRequest(stripeTestInvoice("USD", "0", "0")))

			require.NotNil(t, result.ExemptionReason)
			assert.Equal(t, tc.want, *result.ExemptionReason)
		})
	}
}

func TestStripeExemptionReason_ReverseChargeOutranksTheRest(t *testing.T) {
	reason := stripeExemptionReason([]*stripe.TaxCalculationLineItemTaxBreakdown{
		breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonNotCollecting),
		breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonReverseCharge),
	})

	require.NotNil(t, reason)
	assert.Equal(t, types.TaxExemptionReasonReverseCharge, *reason,
		"reverse charge obliges the invoice to carry a statement, so it outranks any other reason")
	assert.True(t, reason.RequiresReverseChargeStatement())
}

func TestStripeExemptionReason_UnmappedReasonsAreIgnored(t *testing.T) {
	reason := stripeExemptionReason([]*stripe.TaxCalculationLineItemTaxBreakdown{
		breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
		breakdown(0, 0, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonZeroRated),
	})

	assert.Nil(t, reason, "only the reasons that mean nothing was charged are ours to report")
}

func TestStripeExemptionReason_EmptyBreakdownIsNotAReason(t *testing.T) {
	assert.Nil(t, stripeExemptionReason(nil))
	assert.Nil(t, stripeExemptionReason([]*stripe.TaxCalculationLineItemTaxBreakdown{}))
}

// ---------------------------------------------------------------------------
// The handle commit records from, and the deadline a failure is logged with.
// ---------------------------------------------------------------------------

func appliedWithDetails(id string, details *types.ExternalTaxDetails) *taxapplied.TaxApplied {
	return &taxapplied.TaxApplied{ID: id, ExternalTaxDetails: details}
}

func TestCalculationHandle_ReadsTheCalculationOffTheRecords(t *testing.T) {
	handle := calculationHandle([]*taxapplied.TaxApplied{
		appliedWithDetails("ta_1", &types.ExternalTaxDetails{CalculationID: "taxcalc_9"}),
	})

	assert.Equal(t, "taxcalc_9", handle)
}

func TestCalculationHandle_MissingOrEmptyDetailsYieldsNothing(t *testing.T) {
	assert.Empty(t, calculationHandle(nil))
	assert.Empty(t, calculationHandle([]*taxapplied.TaxApplied{appliedWithDetails("ta_1", nil)}),
		"a native row carries no details at all and must read as absent rather than panic")
	assert.Empty(t, calculationHandle([]*taxapplied.TaxApplied{
		appliedWithDetails("ta_1", &types.ExternalTaxDetails{}),
	}))
}

// ---------------------------------------------------------------------------
// Failure classification. A tenant configuration problem and a transient one
// must not read the same, because only one of them is worth retrying.
// ---------------------------------------------------------------------------

func TestStripeFailureMark_RejectionIsTheTenantsToFix(t *testing.T) {
	// The real shape of "The provided `customer` object does not have any associated addresses."
	err := &stripe.Error{
		HTTPStatusCode: 400,
		Type:           stripe.ErrorTypeInvalidRequest,
		Msg:            "The provided `customer` object does not have any associated addresses.",
		Param:          "customer",
	}
	details := map[string]any{}

	mark := stripeFailureMark(err, details)

	assert.Equal(t, ierr.ErrValidation, mark,
		"a 400 will be rejected identically every time, so it is not ours to retry")
	assert.Equal(t, 400, details["stripe_status"])
	assert.Equal(t, string(stripe.ErrorTypeInvalidRequest), details["stripe_error_type"])
}

func TestStripeFailureMark_ServerSideAndRateLimitsAreRetryable(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503} {
		details := map[string]any{}

		mark := stripeFailureMark(&stripe.Error{HTTPStatusCode: status}, details)

		assert.Equal(t, ierr.ErrHTTPClient, mark, "status %d should be retried", status)
		assert.Equal(t, status, details["stripe_status"])
	}
}

func TestStripeFailureMark_ANonStripeErrorIsTreatedAsTransport(t *testing.T) {
	details := map[string]any{}

	mark := stripeFailureMark(errors.New("connection reset by peer"), details)

	assert.Equal(t, ierr.ErrHTTPClient, mark)
	assert.Empty(t, details, "there is no provider detail to record")
}

// ---------------------------------------------------------------------------
// A credit note sends the same request as an invoice. Only the entity it is
// stamped with, the amount and the reference differ.
// ---------------------------------------------------------------------------

func creditNoteTaxRequest(amount, reference string) dto.TaxCalculationRequest {
	return dto.TaxCalculationRequest{
		EntityType: types.TaxRateEntityTypeCreditNote,
		EntityID:   "cn_stripe_test",
		CustomerID: "cust_stripe_test",
		Currency:   "USD",
		Amount:     decimal.RequireFromString(amount),
		Reference:  reference,
	}
}

func TestCalculationParams_CreditNoteSendsItsOwnAmountAndReference(t *testing.T) {
	params := calculationParams(creditNoteTaxRequest("20.00", "idem_key_abc"), "cus_stripe")

	require.Len(t, params.LineItems, 1)
	assert.Equal(t, int64(2000), *params.LineItems[0].Amount,
		"the credited amount is the taxable base, not the invoice's")
	assert.Equal(t, "idem_key_abc", *params.LineItems[0].Reference)
	assert.Equal(t, "usd", *params.Currency)
	assert.Equal(t, "cus_stripe", *params.Customer)
}

func TestResultFromCalculation_CreditNoteRowsCarryTheCreditNote(t *testing.T) {
	engine := &stripeTaxEngine{}
	calc := calculationWith("taxcalc_cn", 0, 360,
		rated(breakdown(360, 2000, stripe.TaxCalculationLineItemTaxBreakdownTaxabilityReasonStandardRated),
			"Integrated goods and services tax (IGST)", "igst", "18.0"))
	calc.AmountTotal = 2360

	result := engine.resultFrom(calc, creditNoteTaxRequest("20.00", "idem_key_abc"))

	require.Len(t, result.TaxAppliedRecords, 1)
	rec := result.TaxAppliedRecords[0]
	assert.Equal(t, types.TaxRateEntityTypeCreditNote, rec.EntityType)
	assert.Equal(t, "cn_stripe_test", rec.EntityID)
	assert.Equal(t, "idem_key_abc", rec.ExternalTaxDetails.Reference,
		"the row records what it was calculated under")
	assert.Equal(t, "taxcalc_cn", rec.ExternalTaxDetails.GetCalculationID())

	// Read off the engine's own total rather than added up here, so what the customer is
	// credited is what the engine says the transaction came to.
	assert.True(t, decimal.RequireFromString("23.60").Equal(result.AmountTotal),
		"the gross is the engine's total, got %s", result.AmountTotal)
	assert.True(t, decimal.RequireFromString("3.60").Equal(result.TotalTaxAmount))
}
