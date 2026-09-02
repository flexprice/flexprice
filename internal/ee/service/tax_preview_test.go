package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// =============================================================================
// calculateTaxBreakdown: the tax formula itself.
//
// Pure function, no DB and no service — these tests pin the arithmetic from the
// design doc directly, including its worked examples. Everything about exemption
// lives one layer up and never reaches here.
// =============================================================================

// pct builds one rate line at the given whole-number percentage.
func pct(id string, value int64, behavior types.TaxBehavior) taxRateLine {
	return taxRateLine{id: id, percentageValue: lo.ToPtr(decimal.NewFromInt(value)), taxBehavior: behavior}
}

// pctDec is pct for rates that are not whole numbers.
func pctDec(id string, value string, behavior types.TaxBehavior) taxRateLine {
	return taxRateLine{id: id, percentageValue: lo.ToPtr(decimal.RequireFromString(value)), taxBehavior: behavior}
}

// amountByRateID indexes a breakdown's lines so assertions can name a rate instead of
// depending on the order lines happen to come back in.
func amountByRateID(b *taxCalculationBreakdown) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal, len(b.lines))
	for _, l := range b.lines {
		out[l.rateID] = l.taxAmount
	}
	return out
}

func TestCalculateTaxBreakdown_Totals(t *testing.T) {
	tests := []struct {
		name          string
		taxableAmount string
		currency      string
		rates         []taxRateLine
		wantInclusive string
		wantExclusive string
		why           string
	}{
		{
			name:          "no rates at all",
			taxableAmount: "100", currency: "usd",
			rates:         nil,
			wantInclusive: "0", wantExclusive: "0",
			why: "nothing resolved means nothing charged — falls out of the general path, it is not special-cased",
		},
		{
			name:          "single exclusive rate is added on top",
			taxableAmount: "100", currency: "usd",
			rates:         []taxRateLine{pct("vat", 10, types.TaxBehaviorExclusive)},
			wantInclusive: "0", wantExclusive: "10",
			why: "10% of 100",
		},
		{
			name:          "exclusive rates are independent and summed",
			taxableAmount: "100", currency: "usd",
			rates: []taxRateLine{
				pct("state", 8, types.TaxBehaviorExclusive),
				pct("county", 2, types.TaxBehaviorExclusive),
			},
			wantInclusive: "0", wantExclusive: "10",
			why: "8 + 2 — order irrelevant, base*r1/100 + base*r2/100 == base*(r1+r2)/100",
		},
		{
			name:          "single inclusive rate is extracted, not added",
			taxableAmount: "100", currency: "usd",
			rates:         []taxRateLine{pct("gst", 10, types.TaxBehaviorInclusive)},
			wantInclusive: "9.09", wantExclusive: "0",
			why: "100 * 10/110 — the tax already inside 100, never added to it",
		},
		{
			name:          "two inclusive rates combine before extraction",
			taxableAmount: "1000", currency: "usd",
			rates: []taxRateLine{
				pct("nine", 9, types.TaxBehaviorInclusive),
				pct("five", 5, types.TaxBehaviorInclusive),
			},
			wantInclusive: "122.81", wantExclusive: "0",
			why: "combined 14% extracted once: 1000 * 14/114. Extracting each independently would give 82.57 + 47.62 = 130.19, implying two contradictory net amounts",
		},
		{
			name:          "exclusive runs against net, not the taxable amount",
			taxableAmount: "1000", currency: "usd",
			rates: []taxRateLine{
				pct("gst", 10, types.TaxBehaviorInclusive),
				pct("vat", 18, types.TaxBehaviorExclusive),
			},
			wantInclusive: "90.91", wantExclusive: "163.64",
			why: "18% of net (909.09) = 163.64, NOT 18% of 1000 = 180 — the inclusive tax has already claimed part of that money",
		},
		{
			name:          "combined inclusive rate above 100% is legal and still leaves something behind",
			taxableAmount: "1000", currency: "usd",
			rates: []taxRateLine{
				pct("sixty", 60, types.TaxBehaviorInclusive),
				pct("fifty", 50, types.TaxBehaviorInclusive),
			},
			wantInclusive: "523.81", wantExclusive: "0",
			why: "1000 * 110/210 — only the per-rate value is capped at 100, never the combined figure",
		},
		{
			name:          "a 0% inclusive rate does not divide by zero",
			taxableAmount: "100", currency: "usd",
			rates:         []taxRateLine{pct("zero", 0, types.TaxBehaviorInclusive)},
			wantInclusive: "0", wantExclusive: "0",
			why: "combinedInclusiveRate is zero here, so both the extraction and the per-rate split must be guarded",
		},
		{
			name:          "a 0% exclusive rate charges nothing",
			taxableAmount: "100", currency: "usd",
			rates:         []taxRateLine{pct("zero", 0, types.TaxBehaviorExclusive)},
			wantInclusive: "0", wantExclusive: "0",
		},
		{
			name:          "zero taxable amount yields zero tax of either kind",
			taxableAmount: "0", currency: "usd",
			rates: []taxRateLine{
				pct("gst", 10, types.TaxBehaviorInclusive),
				pct("vat", 18, types.TaxBehaviorExclusive),
			},
			wantInclusive: "0", wantExclusive: "0",
			why: "a fully discounted invoice still resolves its rates; they just have nothing to compute against",
		},
		{
			name:          "fractional rate rounds to currency precision",
			taxableAmount: "100", currency: "usd",
			rates:         []taxRateLine{pctDec("nyc", "8.875", types.TaxBehaviorExclusive)},
			wantInclusive: "0", wantExclusive: "8.88",
			why: "100 * 0.08875 = 8.875 -> 8.88 at 2dp",
		},
		{
			name:          "JPY has no minor unit, so tax rounds to whole yen",
			taxableAmount: "999", currency: "jpy",
			rates:         []taxRateLine{pctDec("consumption", "10.5", types.TaxBehaviorExclusive)},
			wantInclusive: "0", wantExclusive: "105",
			why: "999 * 0.105 = 104.895 -> 105 at 0dp",
		},
		{
			name:          "JPY inclusive extraction also rounds to whole yen",
			taxableAmount: "1000", currency: "jpy",
			rates:         []taxRateLine{pct("consumption", 10, types.TaxBehaviorInclusive)},
			wantInclusive: "91", wantExclusive: "0",
			why: "1000 * 10/110 = 90.909... -> 91 at 0dp",
		},
		{
			name:          "sub-minor-unit tax rounds away entirely",
			taxableAmount: "1.00", currency: "usd",
			rates:         []taxRateLine{pctDec("tiny", "0.1", types.TaxBehaviorExclusive)},
			wantInclusive: "0", wantExclusive: "0",
			why: "1.00 * 0.001 = 0.001 -> 0.00; a rate can legitimately charge nothing on a small enough amount",
		},
		{
			name:          "mixed with several rates on each side",
			taxableAmount: "1000", currency: "usd",
			rates: []taxRateLine{
				pct("gst", 9, types.TaxBehaviorInclusive),
				pct("cess", 5, types.TaxBehaviorInclusive),
				pct("state", 8, types.TaxBehaviorExclusive),
				pct("county", 2, types.TaxBehaviorExclusive),
			},
			wantInclusive: "122.81", wantExclusive: "87.72",
			why: "inclusive 14% combined -> 122.81; net 877.19; exclusive 8% (70.18) + 2% (17.54) against net = 87.72",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTaxBreakdown(decimal.RequireFromString(tt.taxableAmount), tt.rates, tt.currency)

			assert.True(t, decimal.RequireFromString(tt.wantInclusive).Equal(got.inclusiveTax),
				"inclusive_tax: want %s, got %s. %s", tt.wantInclusive, got.inclusiveTax, tt.why)
			assert.True(t, decimal.RequireFromString(tt.wantExclusive).Equal(got.exclusiveTax),
				"exclusive_tax: want %s, got %s. %s", tt.wantExclusive, got.exclusiveTax, tt.why)
			assert.Len(t, got.lines, len(tt.rates), "every rate passed in must produce exactly one line")
		})
	}
}

// the split across simultaneous inclusive rates is proportional to each rate's share
// of the combined rate, never an equal division. An equal split of the 14% example would give
// 61.41 each; the 9% line must carry more, in the ratio 9:5.
func TestCalculateTaxBreakdown_InclusiveSplitIsProportionalNotEqual(t *testing.T) {
	got := calculateTaxBreakdown(decimal.NewFromInt(1000), []taxRateLine{
		pct("nine", 9, types.TaxBehaviorInclusive),
		pct("five", 5, types.TaxBehaviorInclusive),
	}, "usd")

	byID := amountByRateID(got)
	assert.True(t, decimal.RequireFromString("78.95").Equal(byID["nine"]),
		"9%% carries 9/14 of 122.81 = 78.95, got %s", byID["nine"])
	assert.True(t, decimal.RequireFromString("43.86").Equal(byID["five"]),
		"5%% carries 5/14 of 122.81 = 43.86, got %s", byID["five"])
	assert.True(t, byID["nine"].Add(byID["five"]).Equal(got.inclusiveTax),
		"per-rate shares must sum back to the combined figure exactly")
}

// three inclusive rates (1%, 1%, 4%) on 100 round each share independently and land a
// cent short of the rounded whole. That stray cent must go to one deterministic line — the
// largest share — so the lines still reconcile to inclusiveTax exactly.
func TestCalculateTaxBreakdown_RoundingRemainderGoesToLargestShare(t *testing.T) {
	got := calculateTaxBreakdown(decimal.NewFromInt(100), []taxRateLine{
		pct("one_a", 1, types.TaxBehaviorInclusive),
		pct("one_b", 1, types.TaxBehaviorInclusive),
		pct("four", 4, types.TaxBehaviorInclusive),
	}, "usd")

	assert.True(t, decimal.RequireFromString("5.66").Equal(got.inclusiveTax),
		"combined 6%% of 100 is 5.66, got %s", got.inclusiveTax)

	byID := amountByRateID(got)
	assert.True(t, decimal.RequireFromString("0.94").Equal(byID["one_a"]), "got %s", byID["one_a"])
	assert.True(t, decimal.RequireFromString("0.94").Equal(byID["one_b"]), "got %s", byID["one_b"])
	assert.True(t, decimal.RequireFromString("3.78").Equal(byID["four"]),
		"rounded on its own this line is 3.77; the stray cent lands here as the largest share, got %s", byID["four"])

	assert.True(t, byID["one_a"].Add(byID["one_b"]).Add(byID["four"]).Equal(got.inclusiveTax),
		"lines must sum to inclusive_tax exactly — that is what the remainder assignment is for")
}

// the same invoice recomputed must produce identical per-rate lines. A remainder rule
// that could vary between runs would silently change a TaxApplied row on every recompute.
func TestCalculateTaxBreakdown_IsDeterministicAcrossRuns(t *testing.T) {
	rates := []taxRateLine{
		pct("one_a", 1, types.TaxBehaviorInclusive),
		pct("one_b", 1, types.TaxBehaviorInclusive),
		pct("four", 4, types.TaxBehaviorInclusive),
	}

	first := amountByRateID(calculateTaxBreakdown(decimal.NewFromInt(100), rates, "usd"))
	for i := 0; i < 5; i++ {
		again := amountByRateID(calculateTaxBreakdown(decimal.NewFromInt(100), rates, "usd"))
		for id, amount := range first {
			assert.True(t, amount.Equal(again[id]),
				"run %d moved rate %s from %s to %s", i, id, amount, again[id])
		}
	}
}

// the per-line taxable_amount is the cascade made visible in the response: an
// inclusive line reports the invoice's taxable amount, an exclusive line reports net.
func TestCalculateTaxBreakdown_LineTaxableAmountsShowTheCascade(t *testing.T) {
	got := calculateTaxBreakdown(decimal.NewFromInt(1000), []taxRateLine{
		pct("gst", 10, types.TaxBehaviorInclusive),
		pct("vat", 18, types.TaxBehaviorExclusive),
	}, "usd")

	byID := make(map[string]*taxLineResult, len(got.lines))
	for _, l := range got.lines {
		byID[l.rateID] = l
	}

	assert.True(t, decimal.NewFromInt(1000).Equal(byID["gst"].taxableAmount),
		"an inclusive line is computed against the full taxable amount, got %s", byID["gst"].taxableAmount)
	assert.True(t, decimal.RequireFromString("909.09").Equal(byID["vat"].taxableAmount),
		"an exclusive line is computed against net, got %s", byID["vat"].taxableAmount)
	assert.Equal(t, types.TaxBehaviorInclusive, byID["gst"].taxBehavior)
	assert.Equal(t, types.TaxBehaviorExclusive, byID["vat"].taxBehavior)
}

// inclusive_tax can never reach the taxable amount, because R/(100+R) < 1 for every
// R >= 0. This is what removed the clamp and the overflow error path, so it is worth
// pinning across a wide spread of rates rather than trusting the algebra alone.
func TestCalculateTaxBreakdown_InclusiveTaxNeverReachesTaxableAmount(t *testing.T) {
	taxableAmount := decimal.NewFromInt(1000)
	for _, rate := range []int64{0, 1, 18, 99, 100, 150, 500, 10000} {
		got := calculateTaxBreakdown(taxableAmount, []taxRateLine{
			pct("r", rate, types.TaxBehaviorInclusive),
		}, "usd")

		assert.True(t, got.inclusiveTax.LessThan(taxableAmount),
			"at %d%% inclusive_tax was %s, which is not below the taxable amount %s", rate, got.inclusiveTax, taxableAmount)
	}
}

// =============================================================================
// taxableAmount — what tax is computed from.
// =============================================================================

func TestTaxableAmount(t *testing.T) {
	tests := []struct {
		name     string
		subtotal string
		discount string
		want     string
		why      string
	}{
		{name: "no discount", subtotal: "100", discount: "0", want: "100"},
		{name: "discount applies before tax", subtotal: "100", discount: "10", want: "90",
			why: "tax is computed on what is left after the discount, not on the list price"},
		{name: "discount equal to subtotal", subtotal: "100", discount: "100", want: "0"},
		{name: "discount larger than subtotal clamps to zero", subtotal: "100", discount: "150", want: "0",
			why: "a negative taxable amount would produce negative tax"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := taxableAmount(&invoice.Invoice{
				Subtotal:      decimal.RequireFromString(tt.subtotal),
				TotalDiscount: decimal.RequireFromString(tt.discount),
			})
			assert.True(t, decimal.RequireFromString(tt.want).Equal(got),
				"want %s, got %s. %s", tt.want, got, tt.why)
		})
	}
}

// =============================================================================
// applyTaxResultToInvoice: turning a computed result into invoice totals.
//
// Pure function over an in-memory invoice, so every combination of totals and
// every reason code can be pinned without touching the DB.
// =============================================================================

func TestApplyTaxResultToInvoice_Totals(t *testing.T) {
	tests := []struct {
		name         string
		subtotal     string
		discount     string
		prepaid      string
		inclusiveTax string
		exclusiveTax string
		exempt       bool
		wantTotal    string
		wantTotalTax string
		why          string
	}{
		{
			name:     "taxable, exclusive only — today's behavior, unchanged",
			subtotal: "100", discount: "0", prepaid: "0",
			inclusiveTax: "0", exclusiveTax: "10",
			wantTotal: "110", wantTotalTax: "10",
		},
		{
			name:     "taxable, inclusive only — the total does not move",
			subtotal: "100", discount: "0", prepaid: "0",
			inclusiveTax: "9.09", exclusiveTax: "0",
			wantTotal: "100", wantTotalTax: "9.09",
			why: "the 9.09 was always inside the 100; extracting it is reporting, not a charge",
		},
		{
			name:     "taxable, mixed — only the exclusive portion moves the total",
			subtotal: "1000", discount: "0", prepaid: "0",
			inclusiveTax: "90.91", exclusiveTax: "163.64",
			wantTotal: "1163.64", wantTotalTax: "254.55",
			why: "1000 at 10% inclusive plus 18% exclusive, end to end",
		},
		{
			name:     "exempt, exclusive only — nothing was ever added, so nothing comes off",
			subtotal: "100", discount: "0", prepaid: "0",
			inclusiveTax: "0", exclusiveTax: "10", exempt: true,
			wantTotal: "100", wantTotalTax: "0",
		},
		{
			name:     "exempt, inclusive only — the baked-in tax is backed out",
			subtotal: "100", discount: "0", prepaid: "0",
			inclusiveTax: "9.09", exclusiveTax: "0", exempt: true,
			wantTotal: "90.91", wantTotalTax: "0",
			why: "the one case where total drops BELOW the taxable amount — total = subtotal + total_tax no longer holds",
		},
		{
			name:     "exempt, mixed — inclusive backed out, exclusive simply not added",
			subtotal: "1000", discount: "0", prepaid: "0",
			inclusiveTax: "90.91", exclusiveTax: "163.64", exempt: true,
			wantTotal: "909.09", wantTotalTax: "0",
		},
		{
			name:     "discount is subtracted before tax is applied on top",
			subtotal: "100", discount: "10", prepaid: "0",
			inclusiveTax: "0", exclusiveTax: "9",
			wantTotal: "99", wantTotalTax: "9",
			why: "taxable amount 90, 10% of 90 = 9, total 99",
		},
		{
			name:     "prepaid credits reduce the total alongside the discount",
			subtotal: "100", discount: "10", prepaid: "20",
			inclusiveTax: "0", exclusiveTax: "9",
			wantTotal: "79", wantTotalTax: "9",
			why: "100 - 20 prepaid - 10 discount + 9 tax",
		},
		{
			name:     "a total driven negative is clamped to zero",
			subtotal: "100", discount: "0", prepaid: "500",
			inclusiveTax: "0", exclusiveTax: "10",
			wantTotal: "0", wantTotalTax: "10",
			why: "an invoice can never owe a negative amount",
		},
		{
			name:     "exempt with a large inclusive tax still clamps at zero, never below",
			subtotal: "5", discount: "0", prepaid: "10",
			inclusiveTax: "0.45", exclusiveTax: "0", exempt: true,
			wantTotal: "0", wantTotalTax: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &invoice.Invoice{
				Subtotal:                   decimal.RequireFromString(tt.subtotal),
				TotalDiscount:              decimal.RequireFromString(tt.discount),
				TotalPrepaidCreditsApplied: decimal.RequireFromString(tt.prepaid),
				AmountPaid:                 decimal.Zero,
			}

			applyTaxResultToInvoice(inv, &TaxCalculationResult{
				InclusiveTax:      decimal.RequireFromString(tt.inclusiveTax),
				ExclusiveTax:      decimal.RequireFromString(tt.exclusiveTax),
				TotalTaxAmount:    decimal.RequireFromString(tt.wantTotalTax),
				Exempt:            tt.exempt,
				TaxAppliedRecords: []*dto.TaxAppliedResponse{{}},
			})

			assert.True(t, decimal.RequireFromString(tt.wantTotal).Equal(inv.Total),
				"total: want %s, got %s. %s", tt.wantTotal, inv.Total, tt.why)
			assert.True(t, decimal.RequireFromString(tt.wantTotalTax).Equal(inv.TotalTax),
				"total_tax: want %s, got %s", tt.wantTotalTax, inv.TotalTax)
			assert.True(t, inv.Total.Equal(inv.AmountDue), "amount_due must track total")
		})
	}
}

// amount_remaining is what is still owed after payments already made — distinct from
// amount_due, which is the full computed total.
func TestApplyTaxResultToInvoice_AmountRemainingSubtractsWhatWasPaid(t *testing.T) {
	inv := &invoice.Invoice{
		Subtotal:   decimal.NewFromInt(100),
		AmountPaid: decimal.NewFromInt(40),
	}

	applyTaxResultToInvoice(inv, &TaxCalculationResult{
		ExclusiveTax:      decimal.NewFromInt(10),
		TotalTaxAmount:    decimal.NewFromInt(10),
		TaxAppliedRecords: []*dto.TaxAppliedResponse{{}},
	})

	assert.True(t, decimal.NewFromInt(110).Equal(inv.AmountDue), "amount_due is the full total, got %s", inv.AmountDue)
	assert.True(t, decimal.NewFromInt(70).Equal(inv.AmountRemaining), "110 owed less 40 paid = 70, got %s", inv.AmountRemaining)
}

// an untaxed invoice must say why. The reason code is null in exactly one situation:
// tax was actually charged. Without it, "no tax configured" and "exempt customer" are
// byte-identical on the wire.
func TestApplyTaxResultToInvoice_ExemptionReasonCode(t *testing.T) {
	tests := []struct {
		name    string
		exempt  bool
		records []*dto.TaxAppliedResponse
		want    *types.TaxExemptionReasonCode
		why     string
	}{
		{
			name:   "exempt customer, rates resolved and zeroed",
			exempt: true, records: []*dto.TaxAppliedResponse{{}},
			want: lo.ToPtr(types.TaxExemptionReasonCustomerExempt),
		},
		{
			name:   "exempt customer, nothing resolved at all",
			exempt: true, records: nil,
			want: lo.ToPtr(types.TaxExemptionReasonCustomerExempt),
			why:  "exemption outranks no_tax_configured — the customer being exempt is the more specific truth",
		},
		{
			name:   "taxable customer, nothing configured",
			exempt: false, records: nil,
			want: lo.ToPtr(types.TaxExemptionReasonNoTaxConfigured),
		},
		{
			name:   "taxable customer, tax charged",
			exempt: false, records: []*dto.TaxAppliedResponse{{}},
			want: nil,
			why:  "null reason_code is the signal that tax was genuinely charged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &invoice.Invoice{Subtotal: decimal.NewFromInt(100)}
			applyTaxResultToInvoice(inv, &TaxCalculationResult{
				TotalTaxAmount:    decimal.Zero,
				Exempt:            tt.exempt,
				TaxAppliedRecords: tt.records,
			})

			if tt.want == nil {
				assert.Nil(t, inv.TaxExemptionReasonCode, "%s", tt.why)
				return
			}
			require.NotNil(t, inv.TaxExemptionReasonCode)
			assert.Equal(t, *tt.want, *inv.TaxExemptionReasonCode, "%s", tt.why)
		})
	}
}

// A recompute must land on the same numbers, not accumulate. applyTaxResultToInvoice rebuilds
// total from subtotal every time rather than adjusting whatever was already there.
func TestApplyTaxResultToInvoice_IsIdempotent(t *testing.T) {
	inv := &invoice.Invoice{Subtotal: decimal.NewFromInt(100)}
	result := &TaxCalculationResult{
		ExclusiveTax:      decimal.NewFromInt(10),
		TotalTaxAmount:    decimal.NewFromInt(10),
		TaxAppliedRecords: []*dto.TaxAppliedResponse{{}},
	}

	applyTaxResultToInvoice(inv, result)
	first := inv.Total
	applyTaxResultToInvoice(inv, result)

	assert.True(t, first.Equal(inv.Total), "recomputing must not double-tax: first %s, second %s", first, inv.Total)
	assert.True(t, decimal.NewFromInt(110).Equal(inv.Total))
}

// =============================================================================
// Service-level suite — everything that needs repositories.
// =============================================================================

type TaxCalculationSuite struct {
	testutil.BaseServiceTestSuite
	svc TaxService
}

func TestTaxCalculation(t *testing.T) {
	suite.Run(t, new(TaxCalculationSuite))
}

func (s *TaxCalculationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	s.svc = NewTaxService(ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		TaxRateRepo:        st.TaxRateRepo,
		TaxAppliedRepo:     st.TaxAppliedRepo,
		TaxAssociationRepo: st.TaxAssociationRepo,
		InvoiceRepo:        st.InvoiceRepo,
		CustomerRepo:       st.CustomerRepo,
		SubRepo:            st.SubscriptionRepo,
	})
}

func (s *TaxCalculationSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// ---------- fixtures ----------

func (s *TaxCalculationSuite) newCustomer(taxTreatment types.TaxTreatment) *customer.Customer {
	cust := &customer.Customer{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID:   types.GenerateUUIDWithPrefix("ext"),
		Name:         "Tax Test Customer",
		TaxTreatment: taxTreatment,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))
	return cust
}

// newSubscription persists the minimal subscription CreateTaxAssociation's resolution needs:
// a customer and a concrete currency. Nothing here is ever invoiced.
func (s *TaxCalculationSuite) newSubscription(customerID, currency string) *subscription.Subscription {
	now := time.Now().UTC()
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         customerID,
		PlanID:             types.GenerateUUIDWithPrefix("plan"),
		Currency:           currency,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

// persistedRate creates a saved percentage rate under a unique code, for tests that go through
// CreateTaxAssociation (which resolves rates by code, not by object).
func (s *TaxCalculationSuite) persistedRate(name string, percentage int64) *taxrate.TaxRate {
	return s.persistedRateWithStatus(name, percentage, types.TaxRateStatusActive)
}

func (s *TaxCalculationSuite) persistedRateWithStatus(name string, percentage int64, status types.TaxRateStatus) *taxrate.TaxRate {
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            name,
		Code:            name + "_" + types.GenerateUUIDWithPrefix("u"),
		TaxRateStatus:   status,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromInt(percentage)),
		EnvironmentID:   types.GetEnvironmentID(s.GetContext()),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(s.GetContext(), tr))
	return tr
}

// association writes a subscription-level association straight to the repo, bypassing
// CreateTaxAssociation — the only way to produce rows CreateTaxAssociation would skip
// or never stamp (a null tax_behavior, an exempt customer's row) so resolution can be
// tested against them.
func (s *TaxCalculationSuite) association(taxRateID, subscriptionID string, behavior *types.TaxBehavior) *taxassociation.TaxAssociation {
	assoc := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     taxRateID,
		EntityType:    types.TaxRateEntityTypeSubscription,
		EntityID:      subscriptionID,
		AutoApply:     true,
		Currency:      "usd",
		TaxBehavior:   behavior,
		StartDate:     time.Now().UTC().Add(-24 * time.Hour),
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(s.GetContext(), assoc))
	return assoc
}

// invoiceFor builds an unsaved invoice at the given subtotal for a customer of the given
// tax treatment — the shared fixture for the scenarios below.
func (s *TaxCalculationSuite) invoiceFor(taxTreatment types.TaxTreatment, subtotal decimal.Decimal) *invoice.Invoice {
	cust := s.newCustomer(taxTreatment)
	return &invoice.Invoice{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID: cust.ID,
		Currency:   "usd",
		Subtotal:   subtotal,
	}
}

// rate builds one resolved rate in the shape Calculate/ApplyTaxesOnInvoice expect.
func (s *TaxCalculationSuite) rate(code string, percentage int64, behavior types.TaxBehavior) *dto.TaxRateWithBehavior {
	return &dto.TaxRateWithBehavior{
		TaxRateResponse: &dto.TaxRateResponse{TaxRate: &taxrate.TaxRate{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
			Name:            code,
			Code:            code,
			TaxRateType:     types.TaxRateTypePercentage,
			PercentageValue: lo.ToPtr(decimal.NewFromInt(percentage)),
		}},
		TaxBehavior: behavior,
	}
}

func (s *TaxCalculationSuite) countTaxApplied() int {
	filter := types.NewNoLimitTaxAppliedFilter()
	records, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return len(records)
}

func (s *TaxCalculationSuite) taxAppliedFor(invoiceID string) []*dto.TaxAppliedResponse {
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = invoiceID
	records, err := s.GetStores().TaxAppliedRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)

	out := make([]*dto.TaxAppliedResponse, len(records))
	for i, r := range records {
		out[i] = &dto.TaxAppliedResponse{TaxApplied: *r}
	}
	return out
}

// =============================================================================
// CalculateTaxesOnInvoice / ApplyTaxesOnInvoice
// =============================================================================

// A quote is not a charge. Applying writes one tax_applied row per rate, and a previewed
// invoice is never created — those rows would point at nothing forever.
func (s *TaxCalculationSuite) TestCalculateWritesNothingWhileApplyPersists() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	rates := &dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{s.rate("vat_10", 10, types.TaxBehaviorExclusive)}}

	quoted := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NotNil(quoted)
	s.True(decimal.NewFromInt(10).Equal(quoted.TotalTaxAmount), "10%% of 100 is 10, got %s", quoted.TotalTaxAmount)
	s.Len(quoted.TaxAppliedRecords, 1, "a quote still synthesises the per-rate breakdown, in memory only")
	s.Equal(0, s.countTaxApplied(), "a quote must not touch the tax_applied table")

	charged, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)
	s.True(charged.TotalTaxAmount.Equal(quoted.TotalTaxAmount),
		"quoting and charging must agree: quoted %s, charged %s", quoted.TotalTaxAmount, charged.TotalTaxAmount)
	s.Equal(1, s.countTaxApplied(), "charging is what records the tax")
}

// The seven rate arrangements, all run against subtotal 1000 with a 100 discount so the
// discount actually participates: both kinds of tax are computed on the discounted amount.
type taxShape struct {
	name  string
	rates func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior
}

const (
	shapeSubtotal = 1000
	shapeDiscount = 100
)

func taxShapes() []taxShape {
	return []taxShape{
		{"no tax", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior { return nil }},
		{"one inclusive", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior {
			return []*dto.TaxRateWithBehavior{s.rate("gst", 10, types.TaxBehaviorInclusive)}
		}},
		{"one exclusive", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior {
			return []*dto.TaxRateWithBehavior{s.rate("vat", 18, types.TaxBehaviorExclusive)}
		}},
		{"one of each", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior {
			return []*dto.TaxRateWithBehavior{
				s.rate("gst", 10, types.TaxBehaviorInclusive),
				s.rate("vat", 18, types.TaxBehaviorExclusive),
			}
		}},
		{"several inclusive", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior {
			return []*dto.TaxRateWithBehavior{
				s.rate("gst", 9, types.TaxBehaviorInclusive),
				s.rate("cess", 5, types.TaxBehaviorInclusive),
			}
		}},
		{"several exclusive", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior {
			return []*dto.TaxRateWithBehavior{
				s.rate("state", 8, types.TaxBehaviorExclusive),
				s.rate("county", 2, types.TaxBehaviorExclusive),
			}
		}},
		{"several of each", func(s *TaxCalculationSuite) []*dto.TaxRateWithBehavior {
			return []*dto.TaxRateWithBehavior{
				s.rate("gst", 9, types.TaxBehaviorInclusive),
				s.rate("cess", 5, types.TaxBehaviorInclusive),
				s.rate("state", 8, types.TaxBehaviorExclusive),
				s.rate("county", 2, types.TaxBehaviorExclusive),
			}
		}},
	}
}

func (s *TaxCalculationSuite) shapeInvoice(taxTreatment types.TaxTreatment) *invoice.Invoice {
	inv := s.invoiceFor(taxTreatment, decimal.NewFromInt(shapeSubtotal))
	inv.TotalDiscount = decimal.NewFromInt(shapeDiscount)
	return inv
}

// Inclusive tax is recovered from subtotal and only reported; exclusive tax runs on net and is
// the only thing that moves the total.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_TaxableCustomer() {
	want := map[string]struct{ inclusive, exclusive, total, totalTax string }{
		"no tax":            {"0", "0", "900", "0"},
		"one inclusive":     {"81.82", "0", "900", "81.82"},
		"one exclusive":     {"0", "162", "1062", "162"},
		"one of each":       {"81.82", "147.27", "1047.27", "229.09"},
		"several inclusive": {"110.53", "0", "900", "110.53"},
		"several exclusive": {"0", "90", "990", "90"},
		"several of each":   {"110.53", "78.95", "978.95", "189.48"},
	}

	for _, shape := range taxShapes() {
		s.Run(shape.name, func() {
			exp := want[shape.name]
			inv := s.shapeInvoice(types.TaxTreatmentTaxable)

			result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv,
				&dto.InvoiceTaxRates{Rates: shape.rates(s)})
			applyTaxResultToInvoice(inv, result)

			s.True(decimal.RequireFromString(exp.inclusive).Equal(result.InclusiveTax), "inclusive_tax got %s", result.InclusiveTax)
			s.True(decimal.RequireFromString(exp.exclusive).Equal(result.ExclusiveTax), "exclusive_tax got %s", result.ExclusiveTax)
			s.True(decimal.RequireFromString(exp.total).Equal(inv.Total), "total got %s", inv.Total)
			s.True(decimal.RequireFromString(exp.totalTax).Equal(inv.TotalTax), "total_tax got %s", inv.TotalTax)
			s.False(result.Exempt)
		})
	}
}

// Nothing is charged, and the inclusive portion comes back out of what is owed. The gross
// amounts are still computed: exemption is one override at the end, not a branch in the maths.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_ExemptCustomer() {
	want := map[string]struct{ inclusive, total string }{
		"no tax":            {"0", "900"},
		"one inclusive":     {"81.82", "818.18"},
		"one exclusive":     {"0", "900"},
		"one of each":       {"81.82", "818.18"},
		"several inclusive": {"110.53", "789.47"},
		"several exclusive": {"0", "900"},
		"several of each":   {"110.53", "789.47"},
	}

	for _, shape := range taxShapes() {
		s.Run(shape.name, func() {
			exp := want[shape.name]
			inv := s.shapeInvoice(types.TaxTreatmentExempt)

			result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv,
				&dto.InvoiceTaxRates{Exempt: true, Rates: shape.rates(s)})
			applyTaxResultToInvoice(inv, result)

			s.True(result.Exempt)
			s.True(decimal.RequireFromString(exp.inclusive).Equal(result.InclusiveTax),
				"inclusive_tax is still computed when exempt, got %s", result.InclusiveTax)
			s.True(result.TotalTaxAmount.IsZero(), "charged %s", result.TotalTaxAmount)
			s.True(decimal.RequireFromString(exp.total).Equal(inv.Total), "total got %s", inv.Total)
			s.Require().NotNil(inv.TaxExemptionReasonCode)
			s.Equal(types.TaxExemptionReasonCustomerExempt, *inv.TaxExemptionReasonCode)
		})
	}
}

// Discounting a tax-inclusive price reduces the tax inside it, because the tax rate has to stay
// correct against what the customer actually pays. Stripe: "we recalculate taxes based on the
// remaining amount. This reduction has the side effect of reducing the tax amount due."
//
// Their illustration: a 5.00 item at 5% inclusive with a 10% discount is taxed 0.21, not 0.24.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_DiscountReducesInclusiveTax() {
	tests := []struct {
		name      string
		subtotal  string
		discount  string
		wantTax   string
		wantTotal string
	}{
		{"undiscounted", "5.00", "0", "0.24", "5.00"},
		{"10% discount", "5.00", "0.50", "0.21", "4.50"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.RequireFromString(tt.subtotal))
			inv.TotalDiscount = decimal.RequireFromString(tt.discount)

			result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv,
				&dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{s.rate("gst", 5, types.TaxBehaviorInclusive)}})
			applyTaxResultToInvoice(inv, result)

			s.True(decimal.RequireFromString(tt.wantTax).Equal(result.InclusiveTax), "inclusive tax, got %s", result.InclusiveTax)
			s.True(decimal.RequireFromString(tt.wantTotal).Equal(inv.Total),
				"an inclusive rate never moves the total off the discounted amount, got %s", inv.Total)
		})
	}
}

// Stripe's exempt/reverse table: for an inclusive rate the customer pays the price minus the tax
// that would have been due; for an exclusive rate they pay the price unchanged.
// 10% on 100 -> 90.91 inclusive, 100 exclusive.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_MatchesStripeExemptTable() {
	tests := []struct {
		behavior  types.TaxBehavior
		wantTotal string
	}{
		{types.TaxBehaviorInclusive, "90.91"},
		{types.TaxBehaviorExclusive, "100"},
	}

	for _, tt := range tests {
		s.Run(string(tt.behavior), func() {
			inv := s.invoiceFor(types.TaxTreatmentExempt, decimal.NewFromInt(100))

			result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv, &dto.InvoiceTaxRates{
				Exempt: true,
				Rates:  []*dto.TaxRateWithBehavior{s.rate("tax", 10, tt.behavior)},
			})
			applyTaxResultToInvoice(inv, result)

			s.True(result.TotalTaxAmount.IsZero(), "tax due must be zero, got %s", result.TotalTaxAmount)
			s.True(decimal.RequireFromString(tt.wantTotal).Equal(inv.Total), "total, got %s", inv.Total)
		})
	}
}

// Stripe's own worked example for inclusive + exclusive + discount, at invoice level:
// 15.00 subtotal, 10%% discount, 5%% inclusive, 7%% exclusive.
// https://docs.stripe.com/tax/tax-rates#both-inclusive-and-exclusive-tax-with-discount-example
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_MatchesStripeWorkedExample() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.RequireFromString("15.00"))
	inv.TotalDiscount = decimal.RequireFromString("1.50")

	result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv, &dto.InvoiceTaxRates{
		Rates: []*dto.TaxRateWithBehavior{
			s.rate("gst", 5, types.TaxBehaviorInclusive),
			s.rate("vat", 7, types.TaxBehaviorExclusive),
		},
	})
	applyTaxResultToInvoice(inv, result)

	s.True(decimal.RequireFromString("0.64").Equal(result.InclusiveTax), "inclusive tax, got %s", result.InclusiveTax)
	s.True(decimal.RequireFromString("0.90").Equal(result.ExclusiveTax), "exclusive tax, got %s", result.ExclusiveTax)
	s.True(decimal.RequireFromString("14.40").Equal(inv.Total), "total, got %s", inv.Total)

	// "Post Discount, Less Incl. Tax" — the base the exclusive rate ran against.
	for _, line := range result.TaxAppliedRecords {
		if line.TaxBehavior == types.TaxBehaviorExclusive {
			s.True(decimal.RequireFromString("12.86").Equal(line.TaxableAmount),
				"exclusive base, got %s", line.TaxableAmount)
		}
	}
}

// an exempt customer's rows are written at zero, never omitted, so the audit trail
// records which rates were evaluated rather than looking identical to "nothing configured".
func (s *TaxCalculationSuite) TestApplyTaxesOnInvoice_ExemptCustomerPersistsZeroRows() {
	inv := s.invoiceFor(types.TaxTreatmentExempt, decimal.NewFromInt(100))
	rates := &dto.InvoiceTaxRates{Exempt: true, Rates: []*dto.TaxRateWithBehavior{
		s.rate("vat", 10, types.TaxBehaviorExclusive),
		s.rate("gst", 5, types.TaxBehaviorInclusive),
	}}

	charged, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)

	s.Require().Len(charged.TaxAppliedRecords, 2, "both associations were evaluated, so both must have a row")
	for _, record := range charged.TaxAppliedRecords {
		s.True(record.TaxAmount.IsZero(), "exempt rows persist at zero, got %s", record.TaxAmount)
		s.NotEmpty(record.TaxBehavior, "the row must still record which behavior was evaluated")
	}
	s.Equal(2, s.countTaxApplied(), "the zeroed rows must actually be persisted, not just returned")
}

// The persisted row is the audit trail, so every field on it has to be right — not just the
// amount. taxable_amount in particular differs per behavior.
func (s *TaxCalculationSuite) TestApplyTaxesOnInvoice_PersistedRowFields() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(1000))
	inclusive := s.rate("gst", 10, types.TaxBehaviorInclusive)
	exclusive := s.rate("vat", 18, types.TaxBehaviorExclusive)

	_, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv,
		&dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{inclusive, exclusive}})
	s.Require().NoError(err)

	byRateID := make(map[string]*dto.TaxAppliedResponse)
	for _, r := range s.taxAppliedFor(inv.ID) {
		byRateID[r.TaxRateID] = r
	}
	s.Require().Len(byRateID, 2)

	inc := byRateID[inclusive.ID]
	s.Require().NotNil(inc)
	s.Equal(types.TaxRateEntityTypeInvoice, inc.EntityType)
	s.Equal(inv.ID, inc.EntityID)
	s.Equal("usd", inc.Currency)
	s.Equal(types.TaxBehaviorInclusive, inc.TaxBehavior)
	s.True(decimal.NewFromInt(1000).Equal(inc.TaxableAmount), "an inclusive row reports the full taxable amount, got %s", inc.TaxableAmount)
	s.True(decimal.RequireFromString("90.91").Equal(inc.TaxAmount))

	exc := byRateID[exclusive.ID]
	s.Require().NotNil(exc)
	s.Equal(types.TaxBehaviorExclusive, exc.TaxBehavior)
	s.True(decimal.RequireFromString("909.09").Equal(exc.TaxableAmount), "an exclusive row reports net, got %s", exc.TaxableAmount)
	s.True(decimal.RequireFromString("163.64").Equal(exc.TaxAmount))
}

// Applying twice must update the existing row rather than writing a second one — invoices get
// recomputed, and a duplicate row would double-count in every rollup that reads them.
func (s *TaxCalculationSuite) TestApplyTaxesOnInvoice_IsIdempotent() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	rates := &dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{s.rate("vat", 10, types.TaxBehaviorExclusive)}}

	first, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)
	s.Equal(1, s.countTaxApplied())

	second, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)

	s.Equal(1, s.countTaxApplied(), "re-applying must update the existing row, not append another")
	s.True(first.TotalTaxAmount.Equal(second.TotalTaxAmount),
		"the amount must not drift on recompute: %s then %s", first.TotalTaxAmount, second.TotalTaxAmount)
}

// Recomputing after the taxable amount changes must rewrite the row to the new figure —
// idempotency must not mean "frozen at whatever was written first".
func (s *TaxCalculationSuite) TestApplyTaxesOnInvoice_RecomputeUpdatesTheAmount() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	rates := &dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{s.rate("vat", 10, types.TaxBehaviorExclusive)}}

	_, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)

	inv.TotalDiscount = decimal.NewFromInt(50)
	updated, err := s.svc.ApplyTaxesOnInvoice(s.GetContext(), inv, rates)
	s.Require().NoError(err)

	s.Equal(1, s.countTaxApplied())
	s.True(decimal.NewFromInt(5).Equal(updated.TotalTaxAmount),
		"10%% of the now-discounted 50 is 5, got %s", updated.TotalTaxAmount)

	persisted := s.taxAppliedFor(inv.ID)
	s.Require().Len(persisted, 1)
	s.True(decimal.NewFromInt(5).Equal(persisted[0].TaxAmount), "the persisted row must be rewritten too, got %s", persisted[0].TaxAmount)
}

// Empty and nil rate sets reach the same place as everything else — outcome falls out
// of the general path rather than being special-cased ahead of it.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_NoRates() {
	tests := []struct {
		name       string
		taxRates   *dto.InvoiceTaxRates
		exempt     bool
		wantReason types.TaxExemptionReasonCode
	}{
		{name: "nil InvoiceTaxRates", taxRates: nil, wantReason: types.TaxExemptionReasonNoTaxConfigured},
		{name: "empty rate slice", taxRates: &dto.InvoiceTaxRates{}, wantReason: types.TaxExemptionReasonNoTaxConfigured},
		{name: "exempt customer, nothing resolved", taxRates: &dto.InvoiceTaxRates{Exempt: true}, exempt: true, wantReason: types.TaxExemptionReasonCustomerExempt},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			taxTreatment := types.TaxTreatmentTaxable
			if tt.exempt {
				taxTreatment = types.TaxTreatmentExempt
			}
			inv := s.invoiceFor(taxTreatment, decimal.NewFromInt(100))

			result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv, tt.taxRates)
			applyTaxResultToInvoice(inv, result)

			s.Require().NotNil(result, "a nil rate set must not panic")
			s.True(result.TotalTaxAmount.IsZero())
			s.Empty(result.TaxAppliedRecords, "no rates means no rows at all — distinct from the exempt-with-rates case")
			s.True(decimal.NewFromInt(100).Equal(inv.Total), "total stays at the taxable amount, got %s", inv.Total)
			s.Require().NotNil(inv.TaxExemptionReasonCode)
			s.Equal(tt.wantReason, *inv.TaxExemptionReasonCode)
		})
	}
}

// A rate with no percentage_value cannot be computed with. It is skipped and logged (L11)
// rather than treated as zero, and the rates around it still apply.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_SkipsRateMissingPercentageValue() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))

	broken := s.rate("broken", 10, types.TaxBehaviorExclusive)
	broken.PercentageValue = nil
	good := s.rate("vat", 10, types.TaxBehaviorExclusive)

	result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv,
		&dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{broken, good}})

	s.Len(result.TaxAppliedRecords, 1, "the unusable rate must be dropped, not charged at zero")
	s.Equal(good.ID, result.TaxAppliedRecords[0].TaxRateID)
	s.True(decimal.NewFromInt(10).Equal(result.TotalTaxAmount), "the usable rate still applies, got %s", result.TotalTaxAmount)
}

// If every rate is unusable the invoice reads as untaxed, with the reason code to say so.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_AllRatesUnusable() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	broken := s.rate("broken", 10, types.TaxBehaviorExclusive)
	broken.PercentageValue = nil

	result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv,
		&dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{broken}})
	applyTaxResultToInvoice(inv, result)

	s.Empty(result.TaxAppliedRecords)
	s.True(result.TotalTaxAmount.IsZero())
	s.Require().NotNil(inv.TaxExemptionReasonCode)
	s.Equal(types.TaxExemptionReasonNoTaxConfigured, *inv.TaxExemptionReasonCode)
}

// discounts apply before tax, so a discount changes what tax is computed against.
func (s *TaxCalculationSuite) TestCalculateTaxesOnInvoice_TaxIsComputedAfterDiscount() {
	inv := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	inv.TotalDiscount = decimal.NewFromInt(10)

	result := s.svc.CalculateTaxesOnInvoice(s.GetContext(), inv,
		&dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{s.rate("vat", 10, types.TaxBehaviorExclusive)}})
	applyTaxResultToInvoice(inv, result)

	s.True(decimal.NewFromInt(9).Equal(result.TotalTaxAmount), "10%% of the discounted 90 is 9, got %s", result.TotalTaxAmount)
	s.True(decimal.NewFromInt(99).Equal(inv.Total), "90 + 9, got %s", inv.Total)
}

// =============================================================================
// / PrepareTaxRatesForInvoice: which rates apply and what behavior each carries.
// =============================================================================

// overrides on the request fully replace the subscription's own associations rather than
// merging with them.
func (s *TaxCalculationSuite) TestPrepareTaxRates_OverridesWinOverSubscriptionAssociations() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")

	subRate := s.persistedRate("sub_level", 10)
	s.association(subRate.ID, sub.ID, lo.ToPtr(types.TaxBehaviorExclusive))

	overrideRate := s.persistedRate("override_level", 18)

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "usd",
		SubscriptionID: &sub.ID,
		TaxRateOverrides: []*dto.TaxRateOverride{
			{TaxRateCode: overrideRate.Code, Currency: "usd", AutoApply: true, TaxBehavior: lo.ToPtr(types.TaxBehaviorInclusive)},
		},
	})

	s.Require().NoError(err)
	s.Require().Len(resolved.GetRates(), 1, "an override replaces the subscription's associations, it does not add to them")
	s.Equal(overrideRate.ID, resolved.GetRates()[0].ID)
	s.Equal(types.TaxBehaviorInclusive, resolved.GetRates()[0].TaxBehavior)
}

// an override with no explicit behavior falls back to the currency default, resolved
// against the override's own currency.
func (s *TaxCalculationSuite) TestPrepareTaxRates_OverrideWithoutBehaviorUsesCurrencyDefault() {
	tests := []struct {
		name     string
		currency string
		want     types.TaxBehavior
	}{
		{name: "USD is in the exclusive list", currency: "usd", want: types.TaxBehaviorExclusive},
		{name: "CAD is in the exclusive list", currency: "cad", want: types.TaxBehaviorExclusive},
		{name: "INR is not, so it defaults inclusive", currency: "inr", want: types.TaxBehaviorInclusive},
		{name: "EUR is not, so it defaults inclusive", currency: "eur", want: types.TaxBehaviorInclusive},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentTaxable)
			rate := s.persistedRate("override_"+tt.currency, 10)

			resolved, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
				CustomerID: cust.ID,
				Currency:   tt.currency,
				TaxRateOverrides: []*dto.TaxRateOverride{
					{TaxRateCode: rate.Code, Currency: tt.currency, AutoApply: true},
				},
			})

			s.Require().NoError(err)
			s.Require().Len(resolved.GetRates(), 1)
			s.Equal(tt.want, resolved.GetRates()[0].TaxBehavior)
		})
	}
}

// raw tax_rate IDs have no association behind them, so only the invoice's own currency
// decides the behavior. A customer-level template with an explicit behavior is resolved
// through a different path and is deliberately not consulted here: the documented gap.
func (s *TaxCalculationSuite) TestPrepareTaxRates_RawTaxRatesUseCurrencyOnly() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	rate := s.persistedRate("raw_rate_gap", 10)

	// A customer-level template saying inclusive — what the hierarchy would resolve to if it
	// were consulted on this path.
	_, err := s.svc.CreateTaxAssociation(ctx, &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    cust.ID,
		AutoApply:   true,
		TaxBehavior: lo.ToPtr(types.TaxBehaviorInclusive),
	})
	s.Require().NoError(err)

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID: cust.ID,
		Currency:   "usd",
		TaxRates:   []string{rate.ID},
	})

	s.Require().NoError(err)
	s.Require().Len(resolved.GetRates(), 1)
	s.Equal(types.TaxBehaviorExclusive, resolved.GetRates()[0].TaxBehavior,
		"raw tax_rates resolve purely from the invoice currency, ignoring the customer-level inclusive template")
}

// An unknown rate ID on the raw path is a hard failure, not a silently dropped rate — a
// mistyped ID must not quietly produce an untaxed invoice.
func (s *TaxCalculationSuite) TestPrepareTaxRates_RawTaxRatesUnknownIDFails() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)

	_, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
		CustomerID: cust.ID,
		Currency:   "usd",
		TaxRates:   []string{"taxrate_does_not_exist"},
	})

	s.Require().Error(err)
}

// a subscription's associations carry their own stamped behavior, and two associations
// on the same subscription can legitimately disagree.
func (s *TaxCalculationSuite) TestPrepareTaxRates_SubscriptionAssociationsKeepTheirOwnBehavior() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")

	inclusiveRate := s.persistedRate("assoc_inclusive", 10)
	exclusiveRate := s.persistedRate("assoc_exclusive", 18)
	s.association(inclusiveRate.ID, sub.ID, lo.ToPtr(types.TaxBehaviorInclusive))
	s.association(exclusiveRate.ID, sub.ID, lo.ToPtr(types.TaxBehaviorExclusive))

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "usd",
		SubscriptionID: &sub.ID,
	})

	s.Require().NoError(err)
	s.Require().Len(resolved.GetRates(), 2)

	behaviorByID := make(map[string]types.TaxBehavior)
	for _, r := range resolved.GetRates() {
		behaviorByID[r.ID] = r.TaxBehavior
	}
	s.Equal(types.TaxBehaviorInclusive, behaviorByID[inclusiveRate.ID])
	s.Equal(types.TaxBehaviorExclusive, behaviorByID[exclusiveRate.ID],
		"each association keeps its own behavior — collapsing to a bare rate list would lose this")
}

// a subscription-level row with a null tax_behavior should not exist (creation
// always stamps one). If one is found anyway it is logged as an anomaly and falls back to the
// same currency default every other unstamped resolution uses, rather than a value special to
// this branch.
func (s *TaxCalculationSuite) TestPrepareTaxRates_AssociationWithNullBehaviorFallsBackToCurrencyDefault() {
	tests := []struct {
		name     string
		currency string
		want     types.TaxBehavior
	}{
		{name: "USD falls back to exclusive", currency: "usd", want: types.TaxBehaviorExclusive},
		{name: "INR falls back to inclusive", currency: "inr", want: types.TaxBehaviorInclusive},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentTaxable)
			sub := s.newSubscription(cust.ID, tt.currency)
			rate := s.persistedRate("null_behavior_"+tt.currency, 10)
			s.association(rate.ID, sub.ID, nil) // written directly: CreateTaxAssociation would never produce this

			resolved, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
				CustomerID:     cust.ID,
				Currency:       tt.currency,
				SubscriptionID: &sub.ID,
			})

			s.Require().NoError(err)
			s.Require().Len(resolved.GetRates(), 1)
			s.Equal(tt.want, resolved.GetRates()[0].TaxBehavior)
		})
	}
}

// a subscription with no associations resolves to no rates, without error. The customer
// still has to be looked up, because the exemption flag rides along with the rates.
func (s *TaxCalculationSuite) TestPrepareTaxRates_SubscriptionWithNoAssociations() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")

	resolved, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "usd",
		SubscriptionID: &sub.ID,
	})

	s.Require().NoError(err)
	s.Empty(resolved.GetRates())
	s.False(resolved.IsExempt())
}

// Associations that are not auto_apply are not picked up by invoice resolution.
func (s *TaxCalculationSuite) TestPrepareTaxRates_SkipsAssociationsThatAreNotAutoApply() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")
	rate := s.persistedRate("manual_only", 10)

	assoc := s.association(rate.ID, sub.ID, lo.ToPtr(types.TaxBehaviorExclusive))
	assoc.AutoApply = false
	s.Require().NoError(s.GetStores().TaxAssociationRepo.Update(ctx, assoc))

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "usd",
		SubscriptionID: &sub.ID,
	})

	s.Require().NoError(err)
	s.Empty(resolved.GetRates(), "auto_apply=false associations are not applied automatically")
}

// the exemption flag is resolved alongside the rates, from the customer's live
// tax treatment, on every path.
func (s *TaxCalculationSuite) TestPrepareTaxRates_ExemptionFlagTracksTheCustomer() {
	tests := []struct {
		name         string
		taxTreatment types.TaxTreatment
		want         bool
	}{
		{name: "taxable customer", taxTreatment: types.TaxTreatmentTaxable, want: false},
		{name: "exempt customer", taxTreatment: types.TaxTreatmentExempt, want: true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(tt.taxTreatment)
			rate := s.persistedRate("exempt_flag", 10)

			resolved, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
				CustomerID: cust.ID,
				Currency:   "usd",
				TaxRates:   []string{rate.ID},
			})

			s.Require().NoError(err)
			s.Equal(tt.want, resolved.IsExempt())
		})
	}
}

// An unknown customer fails rather than quoting an invoice whose exemption status is unknown.
func (s *TaxCalculationSuite) TestPrepareTaxRates_UnknownCustomerFails() {
	_, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
		CustomerID: "cust_does_not_exist",
		Currency:   "usd",
	})

	s.Require().Error(err)
}

// No subscription, no overrides, no raw rates: nothing to resolve, and that is not an error.
func (s *TaxCalculationSuite) TestPrepareTaxRates_NothingToResolve() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)

	resolved, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
		CustomerID: cust.ID,
		Currency:   "usd",
	})

	s.Require().NoError(err)
	s.Empty(resolved.GetRates())
	s.False(resolved.IsExempt())
}

// The nil-safe getters exist so callers never have to nil-check the container itself.
func (s *TaxCalculationSuite) TestInvoiceTaxRates_NilReceiverIsSafe() {
	var nilRates *dto.InvoiceTaxRates
	s.Nil(nilRates.GetRates())
	s.False(nilRates.IsExempt())
}

// =============================================================================
// / / CreateTaxAssociation
// =============================================================================

// a subscription-level association with no explicit behavior is stamped from the
// subscription's currency, at creation, once.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_SubscriptionBehaviorDefaultsFromCurrency() {
	tests := []struct {
		name     string
		currency string
		want     types.TaxBehavior
	}{
		{name: "USD is in the exclusive list", currency: "usd", want: types.TaxBehaviorExclusive},
		{name: "CAD is in the exclusive list", currency: "cad", want: types.TaxBehaviorExclusive},
		{name: "INR is not, so it defaults inclusive", currency: "inr", want: types.TaxBehaviorInclusive},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentTaxable)
			sub := s.newSubscription(cust.ID, tt.currency)
			rate := s.persistedRate("assoc_"+tt.currency, 10)

			resp, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
				TaxRateCode: rate.Code,
				EntityType:  types.TaxRateEntityTypeSubscription,
				EntityID:    sub.ID,
				Currency:    tt.currency,
				AutoApply:   true,
			})

			s.Require().NoError(err)
			s.Require().NotNil(resp.TaxBehavior, "a subscription-level association always resolves a concrete behavior")
			s.Equal(tt.want, *resp.TaxBehavior)
		})
	}
}

// An explicit behavior on the request wins over the currency default, in both directions —
// including the case where it contradicts what the currency would have chosen.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_ExplicitBehaviorOverridesCurrencyDefault() {
	tests := []struct {
		name     string
		currency string
		explicit types.TaxBehavior
	}{
		{name: "inclusive on USD, against the currency default", currency: "usd", explicit: types.TaxBehaviorInclusive},
		{name: "exclusive on INR, against the currency default", currency: "inr", explicit: types.TaxBehaviorExclusive},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentTaxable)
			sub := s.newSubscription(cust.ID, tt.currency)
			rate := s.persistedRate("explicit_"+tt.currency, 10)

			resp, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
				TaxRateCode: rate.Code,
				EntityType:  types.TaxRateEntityTypeSubscription,
				EntityID:    sub.ID,
				Currency:    tt.currency,
				AutoApply:   true,
				TaxBehavior: lo.ToPtr(tt.explicit),
			})

			s.Require().NoError(err)
			s.Require().NotNil(resp.TaxBehavior)
			s.Equal(tt.explicit, *resp.TaxBehavior)
		})
	}
}

// only subscription-level associations resolve a concrete behavior. The other levels
// have no single currency to resolve against and keep whatever the request gave, including
// nothing; they are resolved later, when copied down to a subscription.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_NonSubscriptionLevelsAreNotStamped() {
	tests := []struct {
		name       string
		entityType types.TaxRateEntityType
	}{
		{name: "customer level", entityType: types.TaxRateEntityTypeCustomer},
		{name: "tenant level", entityType: types.TaxRateEntityTypeTenant},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentTaxable)
			rate := s.persistedRate("template_"+string(tt.entityType), 10)

			resp, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
				TaxRateCode: rate.Code,
				EntityType:  tt.entityType,
				EntityID:    cust.ID,
				AutoApply:   true,
			})

			s.Require().NoError(err, "a %s association must not require a subscription", tt.entityType)
			s.Nil(resp.TaxBehavior, "no currency to resolve against, so nothing is stamped")
		})
	}
}

// A template created with an explicit behavior keeps it verbatim — "not stamped" means the
// currency default is not consulted, not that the request's own value is discarded.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_CustomerLevelKeepsExplicitBehavior() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	rate := s.persistedRate("template_explicit", 10)

	resp, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    cust.ID,
		AutoApply:   true,
		TaxBehavior: lo.ToPtr(types.TaxBehaviorInclusive),
	})

	s.Require().NoError(err)
	s.Require().NotNil(resp.TaxBehavior)
	s.Equal(types.TaxBehaviorInclusive, *resp.TaxBehavior)
}

// an exempt customer's subscription never gets a tax association, whatever behavior
// it would have resolved to. The create is skipped (not rejected) so the caller —
// typically subscription creation — still succeeds with zero associations.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_ExemptCustomerSubscriptionIsSkipped() {
	tests := []struct {
		name     string
		currency string
		behavior *types.TaxBehavior
	}{
		{name: "explicit inclusive", currency: "usd", behavior: lo.ToPtr(types.TaxBehaviorInclusive)},
		{name: "explicit exclusive", currency: "usd", behavior: lo.ToPtr(types.TaxBehaviorExclusive)},
		{name: "currency default resolving to exclusive", currency: "usd"},
		{name: "currency default resolving to inclusive", currency: "inr"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentExempt)
			sub := s.newSubscription(cust.ID, tt.currency)
			rate := s.persistedRate("exempt_skip", 10)

			resp, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
				TaxRateCode: rate.Code,
				EntityType:  types.TaxRateEntityTypeSubscription,
				EntityID:    sub.ID,
				Currency:    tt.currency,
				AutoApply:   true,
				TaxBehavior: tt.behavior,
			})
			s.Require().NoError(err)
			s.Nil(resp)

			resolved, err := s.svc.PrepareTaxRatesForInvoice(s.GetContext(), dto.CreateInvoiceRequest{
				CustomerID:     cust.ID,
				Currency:       tt.currency,
				SubscriptionID: &sub.ID,
			})
			s.Require().NoError(err)
			s.Empty(resolved.GetRates(), "the skipped create must not have persisted an association")
		})
	}
}

// runs at subscription level only. A customer-level template for an exempt customer is
// still allowed — it is a template, not a live association, and the skip fires when it
// is copied down to a subscription.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_ExemptCustomerLevelTemplateIsAllowed() {
	cust := s.newCustomer(types.TaxTreatmentExempt)
	rate := s.persistedRate("exempt_template", 10)

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    cust.ID,
		AutoApply:   true,
	})

	s.Require().NoError(err)
}

// an inclusive rate above 100% would mean the tax exceeds the tax-free price it is
// derived from. The extraction still computes, so this is rejected as a configuration error
// rather than because the math fails. Checked against the behavior that will actually be
// stored, so a rate that resolves to inclusive from the currency default is caught too.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_InclusiveRateAboveHundredPercentIsRejected() {
	tests := []struct {
		name       string
		currency   string
		behavior   *types.TaxBehavior
		percentage int64
		wantErr    bool
		why        string
	}{
		{
			name: "explicitly inclusive at 150%", currency: "usd",
			behavior: lo.ToPtr(types.TaxBehaviorInclusive), percentage: 150, wantErr: true,
		},
		{
			name: "inclusive via the currency default at 150%", currency: "inr",
			percentage: 150, wantErr: true,
			why: "INR resolves to inclusive with no explicit behavior — checking before the stamp would miss this",
		},
		{
			name: "explicitly exclusive at 150%", currency: "usd",
			behavior: lo.ToPtr(types.TaxBehaviorExclusive), percentage: 150, wantErr: false,
			why: "an exclusive rate above 100% is merely expensive, not contradictory",
		},
		{
			name: "inclusive at exactly 100%", currency: "usd",
			behavior: lo.ToPtr(types.TaxBehaviorInclusive), percentage: 100, wantErr: false,
			why: "the boundary is allowed — 100/(100+100) still leaves half the price behind",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cust := s.newCustomer(types.TaxTreatmentTaxable)
			sub := s.newSubscription(cust.ID, tt.currency)
			// Written straight to the repo: the create-rate DTO caps percentage_value at 100,
			// so this guard is defense in depth against a rate that arrived another way.
			rate := s.persistedRate("over_limit", tt.percentage)

			_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
				TaxRateCode: rate.Code,
				EntityType:  types.TaxRateEntityTypeSubscription,
				EntityID:    sub.ID,
				Currency:    tt.currency,
				AutoApply:   true,
				TaxBehavior: tt.behavior,
			})

			if tt.wantErr {
				s.Require().Error(err, "%s", tt.why)
				return
			}
			s.Require().NoError(err, "%s", tt.why)
		})
	}
}

// the same >100% guard applies to a tenant/customer-level row carrying an explicit
// inclusive behavior, which has no subscription and is never stamped.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_InclusiveOverHundredRejectedAtCustomerLevelToo() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	rate := s.persistedRate("template_over_limit", 150)

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    cust.ID,
		AutoApply:   true,
		TaxBehavior: lo.ToPtr(types.TaxBehaviorInclusive),
	})

	s.Require().Error(err, "the guard must not be skipped just because there is no subscription to stamp against")
}

// An archived rate must not be linkable — an association pointing at one would resolve to a
// rate the tenant has already retired.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_InactiveRateIsRejected() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")
	rate := s.persistedRateWithStatus("archived", 10, types.TaxRateStatusInactive)

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    sub.ID,
		Currency:    "usd",
		AutoApply:   true,
	})

	s.Require().Error(err)
}

func (s *TaxCalculationSuite) TestCreateTaxAssociation_UnknownRateCodeIsRejected() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: "no_such_code",
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    sub.ID,
		Currency:    "usd",
		AutoApply:   true,
	})

	s.Require().Error(err)
}

func (s *TaxCalculationSuite) TestCreateTaxAssociation_UnknownSubscriptionIsRejected() {
	rate := s.persistedRate("orphan_sub", 10)

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    "subs_does_not_exist",
		Currency:    "usd",
		AutoApply:   true,
	})

	s.Require().Error(err)
}

// external_customer_id is a customer-level shorthand: it resolves the customer and rewrites
// the request to target them.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_ExternalCustomerIDResolvesToCustomerLevel() {
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	rate := s.persistedRate("by_external_id", 10)

	resp, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode:        rate.Code,
		ExternalCustomerID: cust.ExternalID,
		AutoApply:          true,
	})

	s.Require().NoError(err)
	s.Equal(types.TaxRateEntityTypeCustomer, resp.EntityType)
	s.Equal(cust.ID, resp.EntityID)
}

// Passing both, pointing at different customers, is a contradiction rather than a precedence
// question — it is rejected instead of silently picking one.
func (s *TaxCalculationSuite) TestCreateTaxAssociation_ExternalCustomerIDConflictingWithEntityIDIsRejected() {
	first := s.newCustomer(types.TaxTreatmentTaxable)
	second := s.newCustomer(types.TaxTreatmentTaxable)
	rate := s.persistedRate("conflicting_ids", 10)

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode:        rate.Code,
		ExternalCustomerID: first.ExternalID,
		EntityType:         types.TaxRateEntityTypeCustomer,
		EntityID:           second.ID,
		AutoApply:          true,
	})

	s.Require().Error(err)
}

func (s *TaxCalculationSuite) TestCreateTaxAssociation_UnknownExternalCustomerIDIsRejected() {
	rate := s.persistedRate("unknown_external", 10)

	_, err := s.svc.CreateTaxAssociation(s.GetContext(), &dto.CreateTaxAssociationRequest{
		TaxRateCode:        rate.Code,
		ExternalCustomerID: "ext_does_not_exist",
		AutoApply:          true,
	})

	s.Require().Error(err)
}

// =============================================================================
// / state changes after an invoice exists
// =============================================================================

// tax_behavior is updatable, but only new and recomputed invoices see the change. A
// TaxApplied row already written keeps the behavior that was true when it was charged: it is
// frozen at apply time, not live-linked to the association.
func (s *TaxCalculationSuite) TestUpdateTaxAssociationBehavior_DoesNotRewriteHistoricalRows() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")
	rate := s.persistedRate("behavior_change", 10)

	assoc, err := s.svc.CreateTaxAssociation(ctx, &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    sub.ID,
		Currency:    "usd",
		AutoApply:   true,
		TaxBehavior: lo.ToPtr(types.TaxBehaviorInclusive),
	})
	s.Require().NoError(err)

	firstInvoice := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	firstInvoice.CustomerID = cust.ID
	_, err = s.svc.ApplyTaxesOnInvoice(ctx, firstInvoice, &dto.InvoiceTaxRates{
		Rates: []*dto.TaxRateWithBehavior{{
			TaxRateResponse: &dto.TaxRateResponse{TaxRate: rate},
			TaxBehavior:     types.TaxBehaviorInclusive,
		}},
	})
	s.Require().NoError(err)

	_, err = s.svc.UpdateTaxAssociation(ctx, assoc.ID, &dto.TaxAssociationUpdateRequest{
		TaxBehavior: lo.ToPtr(types.TaxBehaviorExclusive),
	})
	s.Require().NoError(err)

	persisted := s.taxAppliedFor(firstInvoice.ID)
	s.Require().Len(persisted, 1)
	s.Equal(types.TaxBehaviorInclusive, persisted[0].TaxBehavior,
		"the already-charged row stays inclusive — updating the association does not rewrite history")
	s.True(decimal.RequireFromString("9.09").Equal(persisted[0].TaxAmount),
		"and its amount is untouched too, got %s", persisted[0].TaxAmount)
}

// a subsequent invoice does pick up the updated behavior, which is the other half of
// the same rule: recompute means "reflect current state".
func (s *TaxCalculationSuite) TestUpdateTaxAssociationBehavior_AppliesToTheNextInvoice() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "usd")
	rate := s.persistedRate("behavior_change_next", 10)

	assoc, err := s.svc.CreateTaxAssociation(ctx, &dto.CreateTaxAssociationRequest{
		TaxRateCode: rate.Code,
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    sub.ID,
		Currency:    "usd",
		AutoApply:   true,
		TaxBehavior: lo.ToPtr(types.TaxBehaviorInclusive),
	})
	s.Require().NoError(err)

	_, err = s.svc.UpdateTaxAssociation(ctx, assoc.ID, &dto.TaxAssociationUpdateRequest{
		TaxBehavior: lo.ToPtr(types.TaxBehaviorExclusive),
	})
	s.Require().NoError(err)

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "usd",
		SubscriptionID: &sub.ID,
	})
	s.Require().NoError(err)
	s.Require().Len(resolved.GetRates(), 1)
	s.Equal(types.TaxBehaviorExclusive, resolved.GetRates()[0].TaxBehavior)
}

// a tax treatment change takes effect from the next invoice and never alters one already
// issued. customer.tax_treatment is read fresh at every compute, so the change is picked up
// immediately going forward, and nothing already written is touched.
func (s *TaxCalculationSuite) TestTaxTreatmentChange_AppliesGoingForwardOnly() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	rate := s.persistedRate("retro_check", 10)
	rates := &dto.InvoiceTaxRates{Rates: []*dto.TaxRateWithBehavior{{
		TaxRateResponse: &dto.TaxRateResponse{TaxRate: rate},
		TaxBehavior:     types.TaxBehaviorExclusive,
	}}}

	firstInvoice := s.invoiceFor(types.TaxTreatmentTaxable, decimal.NewFromInt(100))
	firstInvoice.CustomerID = cust.ID
	charged, err := s.svc.ApplyTaxesOnInvoice(ctx, firstInvoice, rates)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(10).Equal(charged.TotalTaxAmount), "the customer was taxable when this was charged")

	cust.TaxTreatment = types.TaxTreatmentExempt
	s.Require().NoError(s.GetStores().CustomerRepo.Update(ctx, cust))

	// Already issued: untouched.
	persisted := s.taxAppliedFor(firstInvoice.ID)
	s.Require().Len(persisted, 1)
	s.True(decimal.NewFromInt(10).Equal(persisted[0].TaxAmount),
		"marking the customer exempt afterward must not retroactively zero an issued invoice, got %s", persisted[0].TaxAmount)

	// Next invoice: the new status is picked up without any further action.
	nextResolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID: cust.ID,
		Currency:   "usd",
		TaxRates:   []string{rate.ID},
	})
	s.Require().NoError(err)
	s.True(nextResolved.IsExempt(), "the very next invoice sees the change, with no migration or recompute")
}

// =============================================================================
// LinkTaxRatesToEntity
// =============================================================================

// The batch path resolves each override through the same creation rules, so a subscription's
// linked associations come out stamped exactly as a direct create would stamp them.
func (s *TaxCalculationSuite) TestLinkTaxRatesToEntity_StampsEachOverride() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentTaxable)
	sub := s.newSubscription(cust.ID, "inr")
	first := s.persistedRate("link_first", 10)
	second := s.persistedRate("link_second", 18)

	err := s.svc.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
		EntityType: types.TaxRateEntityTypeSubscription,
		EntityID:   sub.ID,
		TaxRateOverrides: []*dto.TaxRateOverride{
			{TaxRateCode: first.Code, Currency: "inr", AutoApply: true},
			{TaxRateCode: second.Code, Currency: "inr", AutoApply: true, TaxBehavior: lo.ToPtr(types.TaxBehaviorExclusive)},
		},
	})
	s.Require().NoError(err)

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "inr",
		SubscriptionID: &sub.ID,
	})
	s.Require().NoError(err)
	s.Require().Len(resolved.GetRates(), 2)

	behaviorByID := make(map[string]types.TaxBehavior)
	for _, r := range resolved.GetRates() {
		behaviorByID[r.ID] = r.TaxBehavior
	}
	s.Equal(types.TaxBehaviorInclusive, behaviorByID[first.ID], "no explicit behavior on an INR subscription defaults inclusive")
	s.Equal(types.TaxBehaviorExclusive, behaviorByID[second.ID], "the explicit behavior is kept")
}

// through the batch path: linking to an exempt customer's subscription is skipped,
// the link itself succeeds, and nothing is persisted.
func (s *TaxCalculationSuite) TestLinkTaxRatesToEntity_ExemptCustomerSubscriptionIsSkipped() {
	ctx := s.GetContext()
	cust := s.newCustomer(types.TaxTreatmentExempt)
	sub := s.newSubscription(cust.ID, "usd")
	rate := s.persistedRate("link_exempt", 10)

	err := s.svc.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
		EntityType: types.TaxRateEntityTypeSubscription,
		EntityID:   sub.ID,
		TaxRateOverrides: []*dto.TaxRateOverride{
			{TaxRateCode: rate.Code, Currency: "usd", AutoApply: true},
		},
	})

	s.Require().NoError(err)

	resolved, err := s.svc.PrepareTaxRatesForInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     cust.ID,
		Currency:       "usd",
		SubscriptionID: &sub.ID,
	})
	s.Require().NoError(err)
	s.Empty(resolved.GetRates(), "the skipped link must not have created anything")
}
