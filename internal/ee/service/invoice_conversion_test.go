package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func convLine(id, currency, amount string) *invoice.InvoiceLineItem {
	return &invoice.InvoiceLineItem{
		ID:       id,
		Currency: currency,
		Amount:   dec(amount),
		Quantity: decimal.NewFromInt(1),
	}
}

// A rate too small for the billing currency's precision would turn a real charge into a zero
// invoice. That must fail rather than finalize a free invoice.
func TestConvertInvoiceAmounts_NonZeroNetToZeroRejected(t *testing.T) {
	inv := &invoice.Invoice{
		ID:        "inv_tiny",
		Currency:  "usd",
		Subtotal:  dec("1"),
		Total:     dec("1"),
		AmountDue: dec("1"),
		LineItems: []*invoice.InvoiceLineItem{convLine("il_1", "usd", "1")},
	}
	res := &FXRateResolution{Rate: dec("0.001"), To: "jpy", RateID: "fxr_x", Scope: types.FXRateScopeTenant}

	err := convertInvoiceAmounts(inv, res, time.Now().UTC())

	require.Error(t, err)
	require.Equal(t, "usd", inv.Currency, "a rejected conversion must leave the invoice untouched")
	require.Nil(t, inv.FxConversion)
}

// TestConvertInvoice covers conversion, rounding, residual placement and the recorded originals.
func TestConvertInvoiceAmounts(t *testing.T) {
	convertedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	type lineExp struct {
		id      string
		amount  string
		origCy  string
		origAmt string
	}

	cases := []struct {
		name          string
		charge        string
		billing       string
		rate          string
		subtotal      string
		totalDiscount string
		totalPrepaid  string
		lines         []*invoice.InvoiceLineItem
		wantSubtotal  string
		wantTotal     string
		wantAmountDue string
		wantLines     []lineExp
	}{
		{
			name:          "single line 2dp exact",
			charge:        "usd",
			billing:       "inr",
			rate:          "83.50",
			subtotal:      "100",
			totalDiscount: "0",
			totalPrepaid:  "0",
			lines:         []*invoice.InvoiceLineItem{convLine("il_1", "usd", "100")},
			wantSubtotal:  "8350",
			wantTotal:     "8350",
			wantAmountDue: "8350",
			wantLines: []lineExp{
				{id: "il_1", amount: "8350", origCy: "usd", origAmt: "100"},
			},
		},
		{
			name:          "jpy three lines residual to largest positive (ERD example)",
			charge:        "usd",
			billing:       "jpy",
			rate:          "149.37",
			subtotal:      "100",
			totalDiscount: "0",
			totalPrepaid:  "0",
			lines: []*invoice.InvoiceLineItem{
				convLine("il_a", "usd", "33.33"),
				convLine("il_b", "usd", "33.33"),
				convLine("il_c", "usd", "33.34"),
			},
			wantSubtotal:  "14937",
			wantTotal:     "14937",
			wantAmountDue: "14937",
			wantLines: []lineExp{
				{id: "il_a", amount: "4979", origCy: "usd", origAmt: "33.33"},
				{id: "il_b", amount: "4979", origCy: "usd", origAmt: "33.33"},
				{id: "il_c", amount: "4979", origCy: "usd", origAmt: "33.34"},
			},
		},
		{
			name:          "negative proration line never absorbs residual",
			charge:        "usd",
			billing:       "jpy",
			rate:          "149.37",
			subtotal:      "66.66", // 100.00 charge minus a 33.34 credit line
			totalDiscount: "0",
			totalPrepaid:  "0",
			lines: []*invoice.InvoiceLineItem{
				convLine("il_pos", "usd", "100"),
				convLine("il_neg", "usd", "-33.34"),
			},
			// 14937 + (-4980) = 9957 = net, so residual 0; it must never land on il_neg.
			wantSubtotal:  "9957",
			wantTotal:     "9957",
			wantAmountDue: "9957",
			wantLines: []lineExp{
				{id: "il_pos", amount: "14937", origCy: "usd", origAmt: "100"},
				{id: "il_neg", amount: "-4980", origCy: "usd", origAmt: "-33.34"},
			},
		},
		{
			name:          "discount and prepaid credits converted, net drives total",
			charge:        "usd",
			billing:       "inr",
			rate:          "80",
			subtotal:      "100",
			totalDiscount: "10",
			totalPrepaid:  "20",
			lines: []*invoice.InvoiceLineItem{
				func() *invoice.InvoiceLineItem {
					li := convLine("il_1", "usd", "100")
					li.LineItemDiscount = dec("10")
					li.PrepaidCreditsApplied = dec("20")
					return li
				}(),
			},
			// net = (100-10-20)=70 *80 = 5600
			wantSubtotal:  "8000",
			wantTotal:     "5600",
			wantAmountDue: "5600",
			wantLines: []lineExp{
				{id: "il_1", amount: "8000", origCy: "usd", origAmt: "100"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &invoice.Invoice{
				ID:                         "inv_1",
				Currency:                   c.charge,
				Subtotal:                   dec(c.subtotal),
				TotalDiscount:              dec(c.totalDiscount),
				TotalPrepaidCreditsApplied: dec(c.totalPrepaid),
				LineItems:                  c.lines,
			}
			res := &FXRateResolution{
				Rate:   dec(c.rate),
				RateID: "fxr_x",
				Scope:  types.FXRateScopeTenant,
				From:   c.charge,
				To:     c.billing,
			}

			require.NoError(t, convertInvoiceAmounts(inv, res, convertedAt))

			require.Equal(t, c.billing, inv.Currency, "invoice currency becomes billing")
			require.True(t, dec(c.wantSubtotal).Equal(inv.Subtotal), "subtotal: want %s got %s", c.wantSubtotal, inv.Subtotal)
			require.True(t, dec(c.wantTotal).Equal(inv.Total), "total: want %s got %s", c.wantTotal, inv.Total)
			require.True(t, dec(c.wantAmountDue).Equal(inv.AmountDue), "amount_due: want %s got %s", c.wantAmountDue, inv.AmountDue)

			require.NotNil(t, inv.FxConversion)
			fx := inv.FxConversion
			require.Equal(t, c.charge, fx.ChargeCurrency)
			require.Equal(t, c.billing, fx.BillingCurrency)
			require.True(t, dec(c.rate).Equal(fx.Rate))
			require.Equal(t, "fxr_x", fx.RateID)
			require.Equal(t, convertedAt, fx.ConvertedAt)

			// Source snapshot is the pre-conversion charge amounts.
			require.True(t, dec(c.subtotal).Equal(fx.Source.Subtotal))
			require.True(t, dec(c.totalDiscount).Equal(fx.Source.TotalDiscount))
			require.True(t, dec(c.totalPrepaid).Equal(fx.Source.TotalPrepaidCreditsApplied))
			require.True(t, dec(c.subtotal).Sub(dec(c.totalDiscount)).Sub(dec(c.totalPrepaid)).Equal(fx.Source.Net))

			// Lines add up to the net (the PDF invariant).
			netFromLines := decimal.Zero
			for _, li := range inv.LineItems {
				netFromLines = netFromLines.Add(li.Amount).
					Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount).Sub(li.PrepaidCreditsApplied)
			}
			require.True(t, dec(c.wantTotal).Equal(netFromLines), "lines must sum to net: want %s got %s", c.wantTotal, netFromLines)

			byID := lo.SliceToMap(inv.LineItems, func(li *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) {
				return li.ID, li
			})
			for _, wl := range c.wantLines {
				got := byID[wl.id]
				require.NotNil(t, got, "line %s", wl.id)
				require.True(t, dec(wl.amount).Equal(got.Amount), "line %s amount: want %s got %s", wl.id, wl.amount, got.Amount)
				require.Equal(t, c.billing, got.Currency, "line %s currency", wl.id)
				require.NotNil(t, got.FxConversion, "line %s fx_conversion", wl.id)
				require.Equal(t, wl.origCy, got.FxConversion.ChargeCurrency)
				require.True(t, dec(wl.origAmt).Equal(got.FxConversion.Source.Subtotal), "line %s original: want %s got %s", wl.id, wl.origAmt, got.FxConversion.Source.Subtotal)
				require.True(t, got.FxConversion.Rate.IsZero(), "line %s must not carry the rate", wl.id)
			}
		})
	}
}

// TestConvertInvoiceAmounts_TieBreakLowestID: two positive lines of equal amount split a residual to the
// lowest line id, for determinism.
func TestConvertInvoiceAmounts_TieBreakLowestID(t *testing.T) {
	inv := &invoice.Invoice{
		ID:                         "inv_tie",
		Currency:                   "usd",
		Subtotal:                   dec("2"),
		TotalDiscount:              decimal.Zero,
		TotalPrepaidCreditsApplied: decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			convLine("il_b", "usd", "1"),
			convLine("il_a", "usd", "1"),
		},
	}
	res := &FXRateResolution{Rate: dec("149.37"), RateID: "r", Scope: "tenant", From: "usd", To: "jpy"}
	require.NoError(t, convertInvoiceAmounts(inv, res, time.Now()))

	// net = 2 * 149.37 = 298.74 -> 299; each line 1*149.37=149.37->149; sum 298; residual +1 to lowest id (il_a)
	byID := lo.SliceToMap(inv.LineItems, func(li *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) { return li.ID, li })
	require.True(t, dec("150").Equal(byID["il_a"].Amount))
	require.True(t, dec("149").Equal(byID["il_b"].Amount))
}

// A residual larger than the preferred charge line is spread over further charge lines instead of
// turning that line negative.
func TestConvertInvoiceAmounts_ResidualNeverFlipsChargeLine(t *testing.T) {
	lines := make([]*invoice.InvoiceLineItem, 0, 11)
	for i := 0; i < 10; i++ {
		lines = append(lines, convLine(fmt.Sprintf("il_%02d", i), "usd", "0.01"))
	}
	lines = append(lines, convLine("il_proration", "usd", "-0.09"))
	inv := &invoice.Invoice{
		ID:                         "inv_spread",
		Currency:                   "usd",
		Subtotal:                   dec("0.01"),
		TotalDiscount:              decimal.Zero,
		TotalPrepaidCreditsApplied: decimal.Zero,
		LineItems:                  lines,
	}
	res := &FXRateResolution{Rate: dec("51"), RateID: "r", Scope: "tenant", From: "usd", To: "jpy"}
	require.NoError(t, convertInvoiceAmounts(inv, res, time.Now()))

	// Lines round to 10 x 1 and -5 (sum 5); net 0.51 rounds to 1, so -4 goes to il_00..il_03.
	byID := lo.SliceToMap(inv.LineItems, func(li *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) { return li.ID, li })
	for i := 0; i < 10; i++ {
		want := "1"
		if i < 4 {
			want = "0"
		}
		id := fmt.Sprintf("il_%02d", i)
		require.True(t, dec(want).Equal(byID[id].Amount), "%s: want %s got %s", id, want, byID[id].Amount)
	}
	require.True(t, dec("-5").Equal(byID["il_proration"].Amount))
	require.True(t, dec("1").Equal(inv.Total))
	require.True(t, dec("1").Equal(inv.Subtotal))
}

// TestConvertInvoiceAmounts_NoLineItems converts invoice-level totals directly with no rounding line.
func TestConvertInvoiceAmounts_NoLineItems(t *testing.T) {
	inv := &invoice.Invoice{
		ID:                         "inv_nolines",
		Currency:                   "usd",
		Subtotal:                   dec("100"),
		TotalDiscount:              dec("0"),
		TotalPrepaidCreditsApplied: dec("0"),
	}
	res := &FXRateResolution{Rate: dec("83.50"), RateID: "r", Scope: "tenant", From: "usd", To: "inr"}
	require.NoError(t, convertInvoiceAmounts(inv, res, time.Now()))
	require.True(t, dec("8350").Equal(inv.Subtotal))
	require.True(t, dec("8350").Equal(inv.Total))
}

// TestConvertInvoiceAmounts_IdentityNoOp: a same-currency resolution leaves the invoice untouched.
func TestConvertInvoiceAmounts_IdentityNoOp(t *testing.T) {
	inv := &invoice.Invoice{
		ID:        "inv_id",
		Currency:  "usd",
		Subtotal:  dec("100"),
		Total:     dec("100"),
		AmountDue: dec("100"),
		LineItems: []*invoice.InvoiceLineItem{convLine("il_1", "usd", "100")},
	}
	res := &FXRateResolution{Rate: decimal.NewFromInt(1), Scope: "identity", From: "usd", To: "usd"}
	require.NoError(t, convertInvoiceAmounts(inv, res, time.Now()))
	require.Nil(t, inv.FxConversion, "identity must not write fx_conversion")
	require.True(t, dec("100").Equal(inv.Subtotal))
	require.Nil(t, inv.LineItems[0].FxConversion)
}

type InvoiceConversionSuite struct {
	testutil.BaseServiceTestSuite
	svc *invoiceService
}

func TestInvoiceConversion(t *testing.T) {
	suite.Run(t, new(InvoiceConversionSuite))
}

func (s *InvoiceConversionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewInvoiceService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		SubRepo:             s.GetStores().SubscriptionRepo,
		CustomerRepo:        s.GetStores().CustomerRepo,
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
		FXRateRepo:          s.GetStores().FXRateRepo,
		TaxRateRepo:         s.GetStores().TaxRateRepo,
		TaxAppliedRepo:      s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo:  s.GetStores().TaxAssociationRepo,
		SettingsRepo:        s.GetStores().SettingsRepo,
		WalletRepo:          s.GetStores().WalletRepo,
		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
	}).(*invoiceService)
}

func (s *InvoiceConversionSuite) ctx() context.Context {
	return types.SetEnvironmentID(s.GetContext(), "env_test")
}

func (s *InvoiceConversionSuite) seedCustomer(id string, billing *string) {
	s.NoError(s.GetStores().CustomerRepo.Create(s.ctx(), &domainCustomer.Customer{
		ID:              id,
		ExternalID:      id,
		Name:            "C " + id,
		BillingCurrency: billing,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}))
}

func (s *InvoiceConversionSuite) seedTenantRate(from, to, rate string) {
	s.NoError(s.GetStores().FXRateRepo.Create(s.ctx(), &fxrate.FXRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FX_RATE),
		Scope:         types.FXRateScopeTenant,
		ScopeID:       types.FXRateScopeIDTenant,
		FromCurrency:  from,
		ToCurrency:    to,
		Rate:          decimal.RequireFromString(rate),
		EnvironmentID: types.GetEnvironmentID(s.ctx()),
		BaseModel:     types.GetDefaultBaseModel(s.ctx()),
	}))
}

func (s *InvoiceConversionSuite) seedDraftInvoice(id, customerID, currency string, invType types.InvoiceType, subID *string, lines []*invoice.InvoiceLineItem) *invoice.Invoice {
	subtotal := decimal.Zero
	for _, li := range lines {
		li.InvoiceID = id
		li.CustomerID = customerID
		li.Currency = currency
		li.EnvironmentID = types.GetEnvironmentID(s.ctx())
		li.BaseModel = types.GetDefaultBaseModel(s.ctx())
		subtotal = subtotal.Add(li.Amount)
	}
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      customerID,
		SubscriptionID:  subID,
		InvoiceType:     invType,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        currency,
		Subtotal:        subtotal,
		Total:           subtotal,
		AmountDue:       subtotal,
		AmountRemaining: subtotal,
		LineItems:       lines,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.ctx(), inv))
	return inv
}

func line(id, amount string) *invoice.InvoiceLineItem {
	return &invoice.InvoiceLineItem{
		ID:       id,
		Amount:   decimal.RequireFromString(amount),
		Quantity: decimal.NewFromInt(1),
	}
}

func (s *InvoiceConversionSuite) TestOneOffConverts() {
	s.seedCustomer("cust_1", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_oneoff", "cust_1", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_1", "60"), line("il_2", "40")})

	s.NoError(s.svc.convertToBillingCurrency(s.ctx(), inv))

	s.Equal("inr", inv.Currency)
	s.True(decimal.RequireFromString("8300").Equal(inv.Subtotal), "subtotal got %s", inv.Subtotal)
	s.True(decimal.RequireFromString("8300").Equal(inv.AmountDue), "amount_due got %s", inv.AmountDue)
	s.True(inv.TotalTax.IsZero(), "no tax configured, total_tax got %s", inv.TotalTax)
	s.Require().NotNil(inv.FxConversion)
	s.Equal("usd", inv.FxConversion.ChargeCurrency)
	s.Equal("inr", inv.FxConversion.BillingCurrency)
	s.True(decimal.RequireFromString("83").Equal(inv.FxConversion.Rate))
	s.Equal(types.FXRateScopeTenant, inv.FxConversion.Scope)

	// Line originals persisted.
	stored, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.ctx(), "inv_oneoff")
	s.NoError(err)
	s.Len(stored, 2)
	for _, li := range stored {
		s.Equal("inr", li.Currency)
		s.Require().NotNil(li.FxConversion)
		s.Equal("usd", li.FxConversion.ChargeCurrency)
	}
}

func (s *InvoiceConversionSuite) TestMissingRateStaysDraft() {
	s.seedCustomer("cust_5", lo.ToPtr("inr"))
	// no tenant rate
	inv := s.seedDraftInvoice("inv_norate", "cust_5", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_r1", "100")})

	err := s.svc.convertToBillingCurrency(s.ctx(), inv)
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err), "missing rate must be an invalid-operation error, got %v", err)
	// The resolver's not-found must not leak through: a double-marked error makes the HTTP status
	// non-deterministic (ResolveError picks whichever sentinel it meets first).
	s.False(ierr.IsNotFound(err), "missing rate must not also match not-found, got %v", err)
	s.Equal("usd", inv.Currency, "invoice must stay in the charge currency")
	s.Nil(inv.FxConversion)
}

// A one-off invoice is taxed at compute in the charge currency. Re-tax after conversion must
// rewrite the existing tax_applied row in the billing currency, not leave INR amounts labelled usd.
func (s *InvoiceConversionSuite) TestOneOffRetaxRewritesTaxAppliedInBillingCurrency() {
	s.seedCustomer("cust_tax", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_tax", "cust_tax", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_t1", "100")})

	tr := s.seedPercentRate("gst", "18")
	s.seedTaxRow(inv.ID, tr, "100", "18", "usd")

	s.Require().NoError(s.svc.convertToBillingCurrency(s.ctx(), inv))

	s.Equal("inr", inv.Currency)
	s.True(decimal.RequireFromString("1494").Equal(inv.TotalTax), "18%% of 8300, got %s", inv.TotalTax)

	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	rows, err := s.GetStores().TaxAppliedRepo.List(s.ctx(), filter)
	s.Require().NoError(err)
	s.Require().Len(rows, 1, "re-tax must update the existing row, not add a second one")
	s.Equal("inr", rows[0].Currency, "tax_applied must be rewritten in the billing currency")
	s.True(decimal.RequireFromString("1494").Equal(rows[0].TaxAmount), "tax_amount got %s", rows[0].TaxAmount)
	s.True(decimal.RequireFromString("8300").Equal(rows[0].TaxableAmount), "taxable_amount got %s", rows[0].TaxableAmount)
}

// seedPercentRate creates a published percentage tax rate.
func (s *InvoiceConversionSuite) seedPercentRate(name, pct string) *taxrate.TaxRate {
	p := decimal.RequireFromString(pct)
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            name,
		Code:            name + "_" + types.GenerateUUIDWithPrefix("code"),
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &p,
		EnvironmentID:   types.GetEnvironmentID(s.ctx()),
		BaseModel:       types.GetDefaultBaseModel(s.ctx()),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(s.ctx(), tr))
	return tr
}

// seedTaxRow records the published tax_applied row a compute leaves on a one-off draft.
func (s *InvoiceConversionSuite) seedTaxRow(invID string, tr *taxrate.TaxRate, taxable, tax, currency string) {
	s.Require().NoError(s.GetStores().TaxAppliedRepo.Create(s.ctx(), &taxapplied.TaxApplied{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_APPLIED),
		TaxRateID:     lo.ToPtr(tr.ID),
		EntityType:    types.TaxRateEntityTypeInvoice,
		EntityID:      invID,
		TaxableAmount: decimal.RequireFromString(taxable),
		TaxAmount:     decimal.RequireFromString(tax),
		TaxBehavior:   types.TaxBehaviorExclusive,
		Currency:      currency,
		AppliedAt:     time.Now().UTC(),
		EnvironmentID: types.GetEnvironmentID(s.ctx()),
		BaseModel:     types.GetDefaultBaseModel(s.ctx()),
	}))
}

// A rate archived between compute and finalize still applies: the draft was taxed with it, and
// conversion changes the currency, not the taxes. Two rates also prove each row is re-taxed.
func (s *InvoiceConversionSuite) TestOneOffRetaxKeepsArchivedRate() {
	s.seedCustomer("cust_arch", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_arch", "cust_arch", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_a1", "100")})

	cgst := s.seedPercentRate("cgst", "9")
	sgst := s.seedPercentRate("sgst", "9")
	s.seedTaxRow(inv.ID, cgst, "100", "9", "usd")
	s.seedTaxRow(inv.ID, sgst, "100", "9", "usd")

	// Archive SGST the way DeleteTaxRate does (status archived, row kept).
	sgst.Status = types.StatusArchived
	s.Require().NoError(s.GetStores().TaxRateRepo.Update(s.ctx(), sgst))

	s.Require().NoError(s.svc.convertToBillingCurrency(s.ctx(), inv))

	s.Equal("inr", inv.Currency)
	s.True(decimal.RequireFromString("1494").Equal(inv.TotalTax), "9%% + 9%% of 8300, got %s", inv.TotalTax)
	s.True(decimal.RequireFromString("9794").Equal(inv.Total), "total got %s", inv.Total)

	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	rows, err := s.GetStores().TaxAppliedRepo.List(s.ctx(), filter)
	s.Require().NoError(err)
	s.Require().Len(rows, 2)
	for _, r := range rows {
		s.Equal("inr", r.Currency, "row for rate %s", r.TaxRateID)
		s.True(decimal.RequireFromString("747").Equal(r.TaxAmount), "rate %s tax_amount got %s", r.TaxRateID, r.TaxAmount)
		s.True(decimal.RequireFromString("8300").Equal(r.TaxableAmount), "rate %s taxable got %s", r.TaxRateID, r.TaxableAmount)
	}
}

// A rate that no longer exists at all fails the conversion instead of under-taxing.
func (s *InvoiceConversionSuite) TestOneOffRetaxMissingRateFails() {
	s.seedCustomer("cust_gone", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_gone", "cust_gone", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_g1", "100")})

	ghost := &taxrate.TaxRate{ID: "taxrate_ghost"} // never stored
	s.seedTaxRow(inv.ID, ghost, "100", "18", "usd")

	err := s.svc.convertToBillingCurrency(s.ctx(), inv)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "want not-found, got %v", err)
}

func (s *InvoiceConversionSuite) TestConvertOnceRetrySkips() {
	s.seedCustomer("cust_6", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_once", "cust_6", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_o1", "100")})

	s.NoError(s.svc.convertToBillingCurrency(s.ctx(), inv))
	s.Require().NotNil(inv.FxConversion)
	firstRate := inv.FxConversion.Rate

	// A second pass must be a no-op even if a different rate now exists.
	s.seedTenantRate("inr", "usd", "0.5") // unrelated pair; the invoice is already inr
	s.NoError(s.svc.convertToBillingCurrency(s.ctx(), inv))
	s.Equal("inr", inv.Currency)
	s.True(firstRate.Equal(inv.FxConversion.Rate), "fx_conversion must not change on a second pass")
}

func (s *InvoiceConversionSuite) TestAmountPaidNonZeroNoOp() {
	s.seedCustomer("cust_7", lo.ToPtr("inr"))
	s.seedTenantRate("usd", "inr", "83")
	inv := s.seedDraftInvoice("inv_paid", "cust_7", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_p1", "100")})
	inv.AmountPaid = decimal.RequireFromString("100")

	s.NoError(s.svc.convertToBillingCurrency(s.ctx(), inv))
	s.Equal("usd", inv.Currency, "a draft that already carries a payment is not converted at finalize; pay-first converts at checkout instead")
	s.Nil(inv.FxConversion)
}

// taxAppliedSpy records the currency of every tax_applied write so a test can tell one tax pass from two.
type taxAppliedSpy struct {
	taxapplied.Repository
	created []string
	updated []string
}

func (r *taxAppliedSpy) Create(ctx context.Context, ta *taxapplied.TaxApplied) error {
	r.created = append(r.created, ta.Currency)
	return r.Repository.Create(ctx, ta)
}

func (r *taxAppliedSpy) Update(ctx context.Context, ta *taxapplied.TaxApplied) error {
	r.updated = append(r.updated, ta.Currency)
	return r.Repository.Update(ctx, ta)
}

func (s *InvoiceConversionSuite) TestFinalizeTaxesSubscriptionInvoiceOnce() {
	cases := []struct {
		name          string
		billing       *string
		wantCurrency  string
		wantTax       string
		wantTotal     string
		wantConverted bool
	}{
		{name: "converted: taxed once, in the billing currency", billing: lo.ToPtr("inr"), wantCurrency: "inr", wantTax: "1494", wantTotal: "9794", wantConverted: true},
		{name: "not converted: taxed once, in the charge currency", billing: nil, wantCurrency: "usd", wantTax: "18", wantTotal: "118"},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			spy := &taxAppliedSpy{Repository: s.GetStores().TaxAppliedRepo}
			params := s.svc.ServiceParams
			params.TaxAppliedRepo = spy
			svc := NewInvoiceService(params).(*invoiceService)

			s.seedCustomer("cust_once", tc.billing)
			s.seedTenantRate("usd", "inr", "83")
			s.seedDraftInvoice("inv_once_tax", "cust_once", "usd", types.InvoiceTypeSubscription, lo.ToPtr("sub_once"),
				[]*invoice.InvoiceLineItem{line("il_once", "100")})

			pct := decimal.RequireFromString("18")
			tr := &taxrate.TaxRate{
				ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
				Name:            "GST",
				Code:            "gst_" + types.GenerateUUIDWithPrefix("code"),
				TaxRateStatus:   types.TaxRateStatusActive,
				TaxRateType:     types.TaxRateTypePercentage,
				PercentageValue: &pct,
				EnvironmentID:   types.GetEnvironmentID(s.ctx()),
				BaseModel:       types.GetDefaultBaseModel(s.ctx()),
			}
			s.Require().NoError(s.GetStores().TaxRateRepo.Create(s.ctx(), tr))
			s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(s.ctx(), &taxassociation.TaxAssociation{
				ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
				TaxRateID:     tr.ID,
				EntityType:    types.TaxRateEntityTypeSubscription,
				EntityID:      "sub_once",
				StartDate:     time.Now().UTC().Add(-24 * time.Hour),
				Priority:      100,
				AutoApply:     true,
				Currency:      "usd",
				EnvironmentID: types.GetEnvironmentID(s.ctx()),
				BaseModel:     types.GetDefaultBaseModel(s.ctx()),
			}))

			s.Require().NoError(svc.FinalizeInvoice(s.ctx(), "inv_once_tax", dto.FinalizeInvoiceRequest{}))

			got, err := s.GetStores().InvoiceRepo.Get(s.ctx(), "inv_once_tax")
			s.Require().NoError(err)
			s.Equal(types.InvoiceStatusFinalized, got.InvoiceStatus)
			s.Equal(tc.wantCurrency, got.Currency)
			s.Equal(tc.wantConverted, got.FxConversion != nil)
			s.True(decimal.RequireFromString(tc.wantTax).Equal(got.TotalTax), "total_tax got %s", got.TotalTax)
			s.True(decimal.RequireFromString(tc.wantTotal).Equal(got.Total), "total got %s", got.Total)

			s.Equal([]string{tc.wantCurrency}, spy.created, "exactly one tax_applied row, written in the final currency")
			s.Empty(spy.updated, "no second tax pass rewriting the row")
		})
	}
}

// TestRejectPrepaidCrossCurrencyOneOff covers the create-time pre-paid guard.
func (s *InvoiceConversionSuite) TestRejectPrepaidCrossCurrencyOneOff() {
	s.seedCustomer("cust_pp_inr", lo.ToPtr("inr"))
	s.seedCustomer("cust_pp_usd", lo.ToPtr("usd"))
	s.seedCustomer("cust_pp_none", nil)

	paid := lo.ToPtr(types.PaymentStatusSucceeded)
	amt := lo.ToPtr(decimal.RequireFromString("100"))

	cases := []struct {
		name      string
		req       dto.CreateInvoiceRequest
		expectErr bool
	}{
		{
			name:      "prepaid + cross-currency rejected",
			req:       dto.CreateInvoiceRequest{CustomerID: "cust_pp_inr", Currency: "usd", PaymentStatus: paid},
			expectErr: true,
		},
		{
			name:      "amount_paid + cross-currency rejected",
			req:       dto.CreateInvoiceRequest{CustomerID: "cust_pp_inr", Currency: "usd", AmountPaid: amt},
			expectErr: true,
		},
		{
			name: "prepaid + same-currency allowed",
			req:  dto.CreateInvoiceRequest{CustomerID: "cust_pp_usd", Currency: "usd", PaymentStatus: paid},
		},
		{
			name: "prepaid + no billing currency allowed",
			req:  dto.CreateInvoiceRequest{CustomerID: "cust_pp_none", Currency: "usd", PaymentStatus: paid},
		},
		{
			name: "no payment + cross-currency allowed",
			req:  dto.CreateInvoiceRequest{CustomerID: "cust_pp_inr", Currency: "usd"},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			err := s.svc.validateInvoiceBillingCurrency(s.ctx(), tc.req)
			if tc.expectErr {
				s.Error(err)
				s.True(ierr.IsValidation(err))
			} else {
				s.NoError(err)
			}
		})
	}
}

// TestPaymentBeforeConversionRejected covers the no-payment-before-conversion guard.
func (s *InvoiceConversionSuite) TestPaymentBeforeConversionRejected() {
	paySvc := NewPaymentService(ServiceParams{
		Logger:              s.GetLogger(),
		Config:              s.GetConfig(),
		DB:                  s.GetDB(),
		CustomerRepo:        s.GetStores().CustomerRepo,
		InvoiceRepo:         s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo: s.GetStores().InvoiceLineItemRepo,
		SubRepo:             s.GetStores().SubscriptionRepo,
		CheckoutSessionRepo: s.GetStores().CheckoutSessionRepo,
		PaymentRepo:         s.GetStores().PaymentRepo,
		SettingsRepo:        s.GetStores().SettingsRepo,
		WalletRepo:          s.GetStores().WalletRepo,
		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
	}).(*paymentService)

	s.seedCustomer("cust_pay_inr", lo.ToPtr("inr"))

	// An unconverted DRAFT for a cross-currency customer cannot be paid.
	draft := s.seedDraftInvoice("inv_pay_draft", "cust_pay_inr", "usd", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_pay1", "100")})
	err := paySvc.validateInvoicePaymentEligibility(s.ctx(), draft, &dto.CreatePaymentRequest{
		DestinationID: draft.ID,
		Currency:      "usd",
		Amount:        decimal.RequireFromString("100"),
	})
	s.Error(err, "paying an unconverted cross-currency draft must be rejected")
	s.True(ierr.IsValidation(err))

	// Once converted (fx_conversion set, currency now inr), a matching payment is allowed by the guard.
	converted := s.seedDraftInvoice("inv_pay_converted", "cust_pay_inr", "inr", types.InvoiceTypeOneOff, nil,
		[]*invoice.InvoiceLineItem{line("il_pay2", "8300")})
	converted.FxConversion = &types.FxConversion{ChargeCurrency: "usd", BillingCurrency: "inr", Rate: decimal.RequireFromString("83")}
	err = paySvc.validateInvoicePaymentEligibility(s.ctx(), converted, &dto.CreatePaymentRequest{
		DestinationID: converted.ID,
		Currency:      "inr",
		Amount:        decimal.RequireFromString("8300"),
	})
	s.NoError(err, "a converted invoice can be paid in the billing currency")
}

// countingFXRateRepo counts fx_rates reads.
type countingFXRateRepo struct {
	fxrate.Repository
	reads int
}

func (c *countingFXRateRepo) Get(ctx context.Context, id string) (*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.Get(ctx, id)
}

func (c *countingFXRateRepo) List(ctx context.Context, f *types.FXRateFilter) ([]*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.List(ctx, f)
}

func (c *countingFXRateRepo) Count(ctx context.Context, f *types.FXRateFilter) (int, error) {
	c.reads++
	return c.Repository.Count(ctx, f)
}

func (c *countingFXRateRepo) GetTenantRate(ctx context.Context, from, to string) (*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.GetTenantRate(ctx, from, to)
}

func (c *countingFXRateRepo) FindOverlapping(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, vf, vt *time.Time, excludeID string) ([]*fxrate.FXRate, error) {
	c.reads++
	return c.Repository.FindOverlapping(ctx, scope, scopeID, from, to, vf, vt, excludeID)
}

func (s *InvoiceConversionSuite) newServiceWithFXSpy() (*invoiceService, *countingFXRateRepo) {
	spy := &countingFXRateRepo{Repository: s.GetStores().FXRateRepo}
	params := s.svc.ServiceParams
	params.FXRateRepo = spy
	return NewInvoiceService(params).(*invoiceService), spy
}

// Customers with no billing currency, or the charge currency, never read fx_rates and are unchanged.
func (s *InvoiceConversionSuite) TestExistingCustomersUnaffected() {
	cases := []struct {
		name    string
		billing *string
	}{
		{name: "no billing currency", billing: nil},
		{name: "billing currency equals charge currency", billing: lo.ToPtr("usd")},
	}

	for i, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.seedCustomer("cust_un", tc.billing)
			// A tenant rate exists, so the test proves it is deliberately NOT consulted.
			s.seedTenantRate("usd", "inr", "83")

			inv := s.seedDraftInvoice("inv_un", "cust_un", "usd", types.InvoiceTypeOneOff, nil,
				[]*invoice.InvoiceLineItem{line("il_u1", "60"), line("il_u2", "40")})

			svc, spy := s.newServiceWithFXSpy()
			s.NoError(svc.convertToBillingCurrency(s.ctx(), inv))

			s.Equal(0, spy.reads, "case %d: finalize must not query fx_rates", i)
			s.Equal("usd", inv.Currency)
			s.Nil(inv.FxConversion)
			s.True(decimal.RequireFromString("100").Equal(inv.Subtotal))
			for _, li := range inv.LineItems {
				s.Equal("usd", li.Currency)
				s.Nil(li.FxConversion, "case %d: no line fx_conversion should be stamped", i)
			}
		})
	}
}
