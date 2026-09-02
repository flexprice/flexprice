package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateInvoiceRequest_ZeroOutAmounts(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(99),
		Total:     decimal.NewFromInt(99),
		AmountDue: decimal.NewFromInt(99),
		LineItems: []CreateInvoiceLineItemRequest{
			{Amount: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(2)},
			{Amount: decimal.NewFromInt(49), Quantity: decimal.NewFromInt(1)},
		},
	}

	req.ZeroOutAmounts()

	assert.True(t, req.Subtotal.IsZero(), "Subtotal must be zero")
	assert.True(t, req.Total.IsZero(), "Total must be zero")
	assert.True(t, req.AmountDue.IsZero(), "AmountDue must be zero")

	for i, li := range req.LineItems {
		assert.True(t, li.Amount.IsZero(), "line item %d Amount must be zero", i)
		// Quantity is deliberately preserved — it shows the pricing skeleton.
		assert.False(t, li.Quantity.IsZero(), "line item %d Quantity must be preserved", i)
	}
}

func TestCreateInvoiceRequest_ZeroOutAmounts_EmptyLineItems(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(50),
		Total:     decimal.NewFromInt(50),
		AmountDue: decimal.NewFromInt(50),
	}
	req.ZeroOutAmounts() // must not panic on nil/empty LineItems
	assert.True(t, req.Subtotal.IsZero())
	assert.True(t, req.Total.IsZero())
	assert.True(t, req.AmountDue.IsZero())
}

// =============================================================================
// The tax_summary block on an invoice response.
//
// inclusive_tax and exclusive_tax are not persisted columns; they only exist by
// summing the loaded TaxApplied rows by behavior. These pin the three response
// shapes: tax charged, customer exempt, and no tax configured.
// =============================================================================

func taxRow(behavior types.TaxBehavior, amount string) *TaxAppliedResponse {
	return &TaxAppliedResponse{TaxApplied: taxapplied.TaxApplied{
		TaxBehavior: behavior,
		TaxAmount:   decimal.RequireFromString(amount),
	}}
}

func TestBuildTaxSummary(t *testing.T) {
	tests := []struct {
		name          string
		taxes         []*TaxAppliedResponse
		reasonCode    *types.TaxExemptionReasonCode
		wantInclusive string
		wantExclusive string
		wantTotal     string
		wantReason    *types.TaxExemptionReasonCode
		wantLabel     string
		why           string
	}{
		{
			name: "mixed — summed by behavior, exemption nil",
			taxes: []*TaxAppliedResponse{
				taxRow(types.TaxBehaviorInclusive, "90.91"),
				taxRow(types.TaxBehaviorExclusive, "163.64"),
			},
			wantInclusive: "90.91", wantExclusive: "163.64", wantTotal: "254.55",
			why: "nil exemption is the signal that tax was actually charged",
		},
		{
			name: "exclusive only",
			taxes: []*TaxAppliedResponse{
				taxRow(types.TaxBehaviorExclusive, "10.00"),
			},
			wantInclusive: "0", wantExclusive: "10", wantTotal: "10",
		},
		{
			name: "inclusive only",
			taxes: []*TaxAppliedResponse{
				taxRow(types.TaxBehaviorInclusive, "9.09"),
			},
			wantInclusive: "9.09", wantExclusive: "0", wantTotal: "9.09",
		},
		{
			name: "several rows of the same behavior are summed, not overwritten",
			taxes: []*TaxAppliedResponse{
				taxRow(types.TaxBehaviorExclusive, "8.00"),
				taxRow(types.TaxBehaviorExclusive, "2.00"),
			},
			wantInclusive: "0", wantExclusive: "10", wantTotal: "10",
		},
		{
			name: "several rows on both sides",
			taxes: []*TaxAppliedResponse{
				taxRow(types.TaxBehaviorInclusive, "78.95"),
				taxRow(types.TaxBehaviorInclusive, "43.86"),
				taxRow(types.TaxBehaviorExclusive, "70.18"),
				taxRow(types.TaxBehaviorExclusive, "17.54"),
			},
			wantInclusive: "122.81", wantExclusive: "87.72", wantTotal: "210.53",
		},
		{
			name:          "no rows and no reason code",
			taxes:         nil,
			wantInclusive: "0", wantExclusive: "0", wantTotal: "0",
			why: "not a shape the service produces — an untaxed invoice always carries a reason code — but it must not panic",
		},
		{
			name:          "customer exempt",
			reasonCode:    lo.ToPtr(types.TaxExemptionReasonCustomerExempt),
			wantInclusive: "0", wantExclusive: "0", wantTotal: "0",
			wantReason: lo.ToPtr(types.TaxExemptionReasonCustomerExempt),
			wantLabel:  "Customer is tax exempt",
		},
		{
			name:          "no tax configured",
			reasonCode:    lo.ToPtr(types.TaxExemptionReasonNoTaxConfigured),
			wantInclusive: "0", wantExclusive: "0", wantTotal: "0",
			wantReason: lo.ToPtr(types.TaxExemptionReasonNoTaxConfigured),
			wantLabel:  "No tax configured",
			why:        "same zeroed shape as the exempt case, different reason — the two must never be confused",
		},
		{
			name:          "a reason code wins over whatever rows were passed",
			taxes:         []*TaxAppliedResponse{taxRow(types.TaxBehaviorExclusive, "10.00")},
			reasonCode:    lo.ToPtr(types.TaxExemptionReasonCustomerExempt),
			wantInclusive: "0", wantExclusive: "0", wantTotal: "0",
			wantReason: lo.ToPtr(types.TaxExemptionReasonCustomerExempt),
			wantLabel:  "Customer is tax exempt",
			why:        "the reason code is the authority on whether anything was charged, not the row count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := buildTaxSummary(tt.taxes, tt.reasonCode)
			require.NotNil(t, summary)

			assert.True(t, decimal.RequireFromString(tt.wantInclusive).Equal(summary.TotalInclusiveTax),
				"inclusive_tax: want %s, got %s. %s", tt.wantInclusive, summary.TotalInclusiveTax, tt.why)
			assert.True(t, decimal.RequireFromString(tt.wantExclusive).Equal(summary.TotalExclusiveTax),
				"exclusive_tax: want %s, got %s", tt.wantExclusive, summary.TotalExclusiveTax)
			assert.True(t, decimal.RequireFromString(tt.wantTotal).Equal(summary.TotalTax),
				"total_tax: want %s, got %s", tt.wantTotal, summary.TotalTax)

			if tt.wantReason == nil {
				assert.Nil(t, summary.Exemption, "%s", tt.why)
				return
			}
			require.NotNil(t, summary.Exemption)
			assert.Equal(t, *tt.wantReason, summary.Exemption.ReasonCode)
			assert.Equal(t, tt.wantLabel, summary.Exemption.Reason,
				"reason is derived via DisplayLabel(), never stored")
		})
	}
}

// A row with an unrecognised behavior contributes to neither side rather than being counted
// as one of them — total_tax must never claim tax the response cannot attribute.
func TestBuildTaxSummary_UnknownBehaviorIsNotCounted(t *testing.T) {
	summary := buildTaxSummary([]*TaxAppliedResponse{
		taxRow(types.TaxBehaviorExclusive, "10.00"),
		taxRow(types.TaxBehavior("something_else"), "999.00"),
	}, nil)

	assert.True(t, decimal.NewFromInt(10).Equal(summary.TotalTax),
		"only the attributable row counts, got %s", summary.TotalTax)
}

// WithTaxes is what actually wires the summary onto a response: it must derive the summary
// from the rows plus the reason code already on the invoice, not leave it nil.
func TestInvoiceResponse_WithTaxes(t *testing.T) {
	t.Run("tax charged", func(t *testing.T) {
		r := &InvoiceResponse{}
		r.WithTaxes([]*TaxAppliedResponse{taxRow(types.TaxBehaviorExclusive, "10.00")})

		require.NotNil(t, r.TaxSummary)
		assert.Len(t, r.Taxes, 1)
		assert.True(t, decimal.NewFromInt(10).Equal(r.TaxSummary.TotalTax))
		assert.Nil(t, r.TaxSummary.Exemption)
	})

	t.Run("reason code already on the invoice is carried into the summary", func(t *testing.T) {
		r := &InvoiceResponse{}
		r.TaxExemptionReasonCode = lo.ToPtr(types.TaxExemptionReasonCustomerExempt)
		r.WithTaxes([]*TaxAppliedResponse{taxRow(types.TaxBehaviorExclusive, "0")})

		require.NotNil(t, r.TaxSummary)
		require.NotNil(t, r.TaxSummary.Exemption)
		assert.Equal(t, types.TaxExemptionReasonCustomerExempt, r.TaxSummary.Exemption.ReasonCode)
		assert.Len(t, r.Taxes, 1, "the $0 audit row is still surfaced")
	})

	t.Run("no taxes at all", func(t *testing.T) {
		r := &InvoiceResponse{}
		r.TaxExemptionReasonCode = lo.ToPtr(types.TaxExemptionReasonNoTaxConfigured)
		r.WithTaxes(nil)

		require.NotNil(t, r.TaxSummary)
		require.NotNil(t, r.TaxSummary.Exemption)
		assert.Equal(t, types.TaxExemptionReasonNoTaxConfigured, r.TaxSummary.Exemption.ReasonCode)
		assert.Empty(t, r.Taxes)
	})
}

// reason is derived from the stored code, never stored itself, so every code must
// produce a human-readable label.
func TestTaxExemptionReasonCode_DisplayLabel(t *testing.T) {
	tests := []struct {
		code types.TaxExemptionReasonCode
		want string
	}{
		{types.TaxExemptionReasonCustomerExempt, "Customer is tax exempt"},
		{types.TaxExemptionReasonNoTaxConfigured, "No tax configured"},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code.DisplayLabel())
		})
	}

	assert.Equal(t, "future_code", types.TaxExemptionReasonCode("future_code").DisplayLabel(),
		"an unmapped code falls back to itself rather than rendering as empty")
}
