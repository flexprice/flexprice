package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *LineItemProrationServiceSuite) mixedQuote(effectiveDate time.Time) (*LineItemProrationSummary, *LineItemProrationRequest) {
	outgoing := s.secondLineItem()
	s.recordBilled(outgoing.ID, decimal.NewFromInt(20))

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "settle_mixed",
		Entries: []LineItemProrationEntry{
			{
				LineItem:    s.td.lineItem,
				Price:       s.td.fixedPrice,
				Action:      types.ProrationActionAddItem,
				NewQuantity: s.td.lineItem.Quantity,
			},
			{LineItem: outgoing, Price: s.td.fixedPrice, Action: types.ProrationActionRemoveItem},
		},
	}

	quote, err := s.svc.Compute(s.GetContext(), req)
	s.Require().NoError(err)
	return quote, &req
}

// secondLineItem gives the suite a second removable line so a single Compute can yield
// both a charge and a credit — the mixed quote the netting change turns into one document.
func (s *LineItemProrationServiceSuite) secondLineItem() *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	item := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.td.sub.ID,
		CustomerID:     s.td.sub.CustomerID,
		PriceID:        s.td.fixedPrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Outgoing Addon",
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      s.td.periodStart,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, item))
	return item
}

func (s *LineItemProrationServiceSuite) invoiceCount() int {
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.Require().NoError(err)
	return len(invoices)
}

func (s *LineItemProrationServiceSuite) walletBalance() decimal.Decimal {
	wallets, err := s.GetStores().WalletRepo.GetWalletsByFilter(s.GetContext(), &types.WalletFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.Require().NoError(err)

	total := decimal.Zero
	for _, w := range wallets {
		total = total.Add(w.Balance)
	}
	return total
}

func (s *LineItemProrationServiceSuite) applyViaSettle(
	req LineItemProrationRequest,
) ([]dto.ChangedInvoice, error) {
	if req.Behavior != types.ProrationBehaviorCreateProrations {
		return nil, nil
	}

	ctx := s.GetContext()
	quote, err := s.svc.Compute(ctx, req)
	if err != nil {
		return nil, err
	}

	key := prorationChargeInvoiceKey(req)
	if quote.NetAmount().IsNegative() {
		key = req.IdempotencyKey
	}

	settleReq := NewSettleProrationRequest(
		req.Subscription, quote, req.EffectiveDate, req.Subscription.CurrentPeriodEnd,
		"Subscription update", key, SettleModeIssue,
	)
	settleReq.Reason = req.Reason

	settled, err := s.svc.Settle(ctx, settleReq)
	if err != nil {
		return nil, err
	}

	attemptProrationPayments(ctx, s.params, settled.GetChanged())

	return settled.Changed, nil
}

func (s *LineItemProrationServiceSuite) settleReq(
	quote *LineItemProrationSummary,
	effectiveDate time.Time,
	mode SettleMode,
) *SettleProrationRequest {
	return &SettleProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		Quote:          quote,
		PeriodStart:    effectiveDate,
		PeriodEnd:      s.td.periodEnd,
		DisplayName:    "Subscription update",
		IdempotencyKey: "settle_test_key",
		Mode:           mode,
	}
}

func (s *LineItemProrationServiceSuite) TestSettle_NetCharge_IssuesOneNettedInvoice() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)
	quote.TotalChargeAmount = quote.TotalChargeAmount.Add(decimal.NewFromInt(10))
	quote.ChargeLineItems[0].Amount = quote.ChargeLineItems[0].Amount.Add(decimal.NewFromInt(10))

	invoicesBefore := s.invoiceCount()
	walletBefore := s.walletBalance()

	result, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModeIssue))
	s.Require().NoError(err)

	s.Require().Len(result.Changed, 1, "one document for the batch, never two")
	s.Equal(invoicesBefore+1, s.invoiceCount())
	s.Equal(walletBefore.String(), s.walletBalance().String(), "a net charge never touches the wallet")

	inv := result.Changed[0].Invoice
	s.Require().NotNil(inv)
	s.Equal("10.00", inv.AmountDue.StringFixed(2), "billed the net, not the gross charge")
	s.Len(inv.LineItems, 2, "charge AND credit lines ride the same document")
}

func (s *LineItemProrationServiceSuite) TestSettle_NetCredit_PaysWalletAndRaisesNoInvoice() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)
	quote.TotalCreditAmount = quote.TotalCreditAmount.Add(decimal.NewFromInt(10))

	invoicesBefore := s.invoiceCount()

	result, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModeIssue))
	s.Require().NoError(err)

	s.Require().Len(result.Changed, 1)
	s.Equal(invoicesBefore, s.invoiceCount(), "a net credit is never invoiced")
	s.Equal("10.00", s.walletBalance().StringFixed(2), "the wallet receives the net, not the gross credit")
	s.Equal(dto.ChangedInvoiceStatusWalletIssued, result.Changed[0].Status)
}

func (s *LineItemProrationServiceSuite) TestSettle_ZeroNet_WritesNothing() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)
	s.Require().Equal("0.00", quote.NetAmount().StringFixed(2))

	invoicesBefore := s.invoiceCount()

	result, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModeIssue))
	s.Require().NoError(err)

	s.Empty(result.Changed, "a swap that nets to zero settles nothing")
	s.Nil(result.Draft)
	s.Equal(invoicesBefore, s.invoiceCount())
	s.Equal("0.00", s.walletBalance().StringFixed(2))
}

func (s *LineItemProrationServiceSuite) TestSettle_Preview_QuotesWithoutWriting() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)
	quote.TotalChargeAmount = quote.TotalChargeAmount.Add(decimal.NewFromInt(10))
	quote.ChargeLineItems[0].Amount = quote.ChargeLineItems[0].Amount.Add(decimal.NewFromInt(10))

	invoicesBefore := s.invoiceCount()

	result, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModePreview))
	s.Require().NoError(err)

	s.Require().Len(result.Changed, 1)
	s.Equal(dto.ChangedInvoiceStatusPreview, result.Changed[0].Status)
	s.Equal(invoicesBefore, s.invoiceCount(), "preview persists no invoice")
	s.Equal("0.00", s.walletBalance().StringFixed(2))

	s.Require().NotNil(result.Changed[0].Invoice)
	s.Equal("10.00", result.Changed[0].Invoice.AmountDue.StringFixed(2),
		"the quoted net is what execute would bill")
}

func (s *LineItemProrationServiceSuite) TestSettle_PreviewNetCredit_QuotesWalletWithoutPaying() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)
	quote.TotalCreditAmount = quote.TotalCreditAmount.Add(decimal.NewFromInt(10))

	result, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModePreview))
	s.Require().NoError(err)

	s.Require().Len(result.Changed, 1)
	s.Equal(dto.ChangedInvoiceStatusPreview, result.Changed[0].Status)
	s.Equal("0.00", s.walletBalance().StringFixed(2), "preview pays nothing out")
}

func (s *LineItemProrationServiceSuite) TestSettle_Draft_LocksTheNetForCheckout() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)
	quote.TotalChargeAmount = quote.TotalChargeAmount.Add(decimal.NewFromInt(10))
	quote.ChargeLineItems[0].Amount = quote.ChargeLineItems[0].Amount.Add(decimal.NewFromInt(10))

	result, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModeDraft))
	s.Require().NoError(err)

	s.Empty(result.Changed)
	s.Require().NotNil(result.Draft)
	s.Equal(types.InvoiceStatusDraft, result.Draft.InvoiceStatus)
	s.Equal("10.00", result.Draft.AmountDue.StringFixed(2),
		"the customer is asked for exactly what pay-later would have billed")
}

func (s *LineItemProrationServiceSuite) TestSettle_Draft_RejectsNonPositiveNet() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	quote, _ := s.mixedQuote(effectiveDate)

	_, err := s.svc.Settle(ctx, s.settleReq(quote, effectiveDate, SettleModeDraft))
	s.Require().Error(err)
}

func (s *LineItemProrationServiceSuite) TestSettle_RejectsInvalidRequest() {
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	quote, _ := s.mixedQuote(effectiveDate)

	cases := map[string]func(*SettleProrationRequest){
		"missing display name": func(r *SettleProrationRequest) { r.DisplayName = "" },
		"missing subscription": func(r *SettleProrationRequest) { r.Subscription = nil },
		"missing quote":        func(r *SettleProrationRequest) { r.Quote = nil },
		"unknown mode":         func(r *SettleProrationRequest) { r.Mode = SettleMode(99) },
	}

	for name, mangle := range cases {
		s.Run(name, func() {
			invoicesBefore := s.invoiceCount()

			req := s.settleReq(quote, effectiveDate, SettleModeIssue)
			mangle(req)

			_, err := s.svc.Settle(s.GetContext(), req)
			s.Require().Error(err)
			s.Equal(invoicesBefore, s.invoiceCount(), "a rejected request writes nothing")
		})
	}

	s.Run("nil request", func() {
		_, err := s.svc.Settle(s.GetContext(), nil)
		s.Require().Error(err)
	})
}

func (s *LineItemProrationServiceSuite) TestMerge_SumsBucketsAndLines() {
	a := &LineItemProrationSummary{
		ChargeLineItems:   []dto.CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(10)}},
		TotalChargeAmount: decimal.NewFromInt(10),
		TotalCreditAmount: decimal.Zero,
	}
	b := &LineItemProrationSummary{
		CreditLineItems:   []dto.CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(-4)}},
		TotalChargeAmount: decimal.Zero,
		TotalCreditAmount: decimal.NewFromInt(4),
	}

	merged := emptyProrationSummary(s.td.sub).Merge(a, nil, b)

	s.Len(merged.ChargeLineItems, 1)
	s.Len(merged.CreditLineItems, 1)
	s.Equal("10", merged.TotalChargeAmount.String())
	s.Equal("4", merged.TotalCreditAmount.String())
	s.Equal("6", merged.NetAmount().String())
}

// =============================================================================
// Characterisation: today's settlement shapes and golden idempotency keys
// =============================================================================

// Characterisation baseline for settlement (plan Group A / PR A3).
//
// These tests pin what settlement does TODAY across every path that turns a
// LineItemProrationSummary into money: Compute, Apply, both preview twins and the
// pay-first draft, plus a golden key for every idempotency-key generator. They exist so
// the settlement refactor can be proven no-op rather than argued, and so the two
// deliberate divergences (netting a mixed quote; unifying the key generators) show up as
// exactly the assertions that had to change.

// -----------------------------------------------------------------------------
// golden idempotency keys
// -----------------------------------------------------------------------------

// Four generators produce keys for one economic event today. Every value below is a
// golden: a key that moves silently re-bills a customer whose retry no longer dedupes
// against the original. The unification PR must leave each of these untouched, except
// the pay-first draft, which is a deliberate format change pinned separately in
// TestCharacteriseSettlement_PayFirstDraft_UsesRawAssociationKey.
func TestCharacteriseSettlement_GoldenIdempotencyKeys(t *testing.T) {
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{ID: "sub_golden"}

	t.Run("proration_charge_explicit_key", func(t *testing.T) {
		got := prorationChargeInvoiceKey(LineItemProrationRequest{
			Subscription:   sub,
			EffectiveDate:  effectiveDate,
			IdempotencyKey: "addon_add_assoc_golden_1775865600",
		})
		require.Equal(t, "proration_charge-ca934ee629be6195", got)
	})

	// With no caller key the source is the entries' "<lineItemID>,<action>" set, sorted.
	t.Run("proration_charge_derived_from_entries", func(t *testing.T) {
		got := prorationChargeInvoiceKey(LineItemProrationRequest{
			Subscription:  sub,
			EffectiveDate: effectiveDate,
			Entries: []LineItemProrationEntry{
				{LineItem: &subscription.SubscriptionLineItem{ID: "sli_b"}, Action: types.ProrationActionRemoveItem},
				{LineItem: &subscription.SubscriptionLineItem{ID: "sli_a"}, Action: types.ProrationActionAddItem},
			},
		})
		require.Equal(t, "proration_charge-682193b2fbafa47c", got)
	})

	// Entry order must not move the key, or a reordered batch re-bills on retry.
	t.Run("proration_charge_derived_is_order_independent", func(t *testing.T) {
		forward := prorationChargeInvoiceKey(LineItemProrationRequest{
			Subscription:  sub,
			EffectiveDate: effectiveDate,
			Entries: []LineItemProrationEntry{
				{LineItem: &subscription.SubscriptionLineItem{ID: "sli_a"}, Action: types.ProrationActionAddItem},
				{LineItem: &subscription.SubscriptionLineItem{ID: "sli_b"}, Action: types.ProrationActionRemoveItem},
			},
		})
		require.Equal(t, "proration_charge-682193b2fbafa47c", forward)
	})

	// Quantity change's generator: the shape the plan keeps.
	t.Run("quantity_change_parts", func(t *testing.T) {
		got := prorationChargeIdempotencyKey("sub_golden", []prorationChargeKeyPart{
			{lineItemID: "sli_b", effectiveDate: effectiveDate},
			{lineItemID: "sli_a", effectiveDate: effectiveDate},
		})
		require.Equal(t, "proration_charge-2bccf17c27fe7841", got)
	})

	t.Run("plan_change", func(t *testing.T) {
		r := &planChangeRequest{
			currentSub: &subscription.Subscription{
				ID:        "sub_golden",
				Version:   7,
				BaseModel: types.BaseModel{UpdatedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)},
			},
			toPlan: &plan.Plan{ID: "plan_target"},
		}

		// Without a client key the subscription's version + updated_at carry uniqueness.
		require.Equal(t, "plan_change-7d8fe165d3e97036", planChangeIdempotencyKey(r, ""))
		require.Equal(t, "plan_change-6dba346931339c23", planChangeIdempotencyKey(r, "outgoing_usage"))

		// A client key replaces both, so a caller retry is stable across a version bump.
		r.idempotencyKey = "client_key_golden"
		require.Equal(t, "plan_change-f56f34d66d8fafd0", planChangeIdempotencyKey(r, ""))
		require.Equal(t, "plan_change-14190dbda0ee27b5", planChangeIdempotencyKey(r, "outgoing_usage"))
	})

	// The addon params' keys are raw strings, not hashes — pinned verbatim because the
	// pay-first draft passes one straight to CreateComputedDraftInvoice while pay-later
	// hashes the same string through prorationChargeInvoiceKey.
	t.Run("addon_params_raw_keys", func(t *testing.T) {
		association := &addonassociation.AddonAssociation{ID: "addonassoc_golden", EntityID: "sub_golden"}

		attach := &addonAttachParams{association: association, effectiveDate: effectiveDate}
		require.Equal(t, "addon_add_addonassoc_golden_1775865600", attach.prorationIdempotencyKey())

		detach := &addonDetachParams{association: association, effectiveDate: effectiveDate}
		require.Equal(t, "addon_remove_sub_golden_addonassoc_golden_1775865600", detach.prorationIdempotencyKey())
	})

	// Nil-safe: a params struct with no association must not panic on the key path.
	t.Run("empty_association_yields_empty_key", func(t *testing.T) {
		require.Empty(t, (&addonAttachParams{}).prorationIdempotencyKey())
		require.Empty(t, (&addonDetachParams{}).prorationIdempotencyKey())
	})
}

// -----------------------------------------------------------------------------
// the settlement matrix: Compute + Apply
// -----------------------------------------------------------------------------

// A charge-only quote settles as exactly one invoice and never touches the wallet.
// Netting is a no-op here, so this assertion must survive the refactor unchanged.
func (s *LineItemProrationServiceSuite) TestCharacteriseSettlement_ChargesOnly() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "characterise_charges_only",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	}

	quote, err := s.svc.Compute(ctx, req)
	s.Require().NoError(err)
	s.Equal("13.33", quote.TotalChargeAmount.StringFixed(2))
	s.True(quote.TotalCreditAmount.IsZero())
	s.Len(quote.ChargeLineItems, 1)
	s.Empty(quote.CreditLineItems)
	s.Equal("13.33", quote.NetAmount().StringFixed(2))

	settled, err := s.applyViaSettle(req)
	s.Require().NoError(err)
	s.Require().Len(settled, 1, "a charge-only quote settles as one document")
	s.Require().NotNil(settled[0].Invoice)
	s.Equal("13.33", settled[0].Invoice.AmountDue.StringFixed(2))
	s.Equal(types.InvoiceTypeOneOff, settled[0].Invoice.InvoiceType)
	s.True(s.walletBalance().IsZero(), "a charge must not credit the wallet")
}

// A credit-only quote settles as a wallet top-up and raises no invoice.
func (s *LineItemProrationServiceSuite) TestCharacteriseSettlement_CreditsOnly() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))
	invoicesBefore := s.invoiceCount()

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "characterise_credits_only",
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	quote, err := s.svc.Compute(ctx, req)
	s.Require().NoError(err)
	s.True(quote.TotalChargeAmount.IsZero())
	s.Equal("13.33", quote.TotalCreditAmount.StringFixed(2))
	s.Equal("-13.33", quote.NetAmount().StringFixed(2))

	settled, err := s.applyViaSettle(req)
	s.Require().NoError(err)
	s.Require().Len(settled, 1, "a credit-only quote settles as one wallet credit")
	s.Equal(invoicesBefore, s.invoiceCount(), "a credit must not raise an invoice")
	s.Equal("13.33", s.walletBalance().StringFixed(2))
}

// Divergence 1, as landed. This replaces the pre-netting baseline, which asserted that a
// mixed quote raised a full-charge invoice AND a full-credit wallet top-up — so a swap that
// netted to zero moved 13.33 in both directions. Settle nets, so it now moves nothing.
func (s *LineItemProrationServiceSuite) TestSettlement_Mixed_NetsToOneDocument() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	outgoing := s.secondLineItem()
	s.recordBilled(outgoing.ID, decimal.NewFromInt(20))
	invoicesBefore := s.invoiceCount()

	req := LineItemProrationRequest{
		Subscription:   s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "characterise_mixed",
		Entries: []LineItemProrationEntry{
			{
				LineItem:    s.td.lineItem,
				Price:       s.td.fixedPrice,
				Action:      types.ProrationActionAddItem,
				NewQuantity: s.td.lineItem.Quantity,
			},
			{
				LineItem: outgoing,
				Price:    s.td.fixedPrice,
				Action:   types.ProrationActionRemoveItem,
			},
		},
	}

	quote, err := s.svc.Compute(ctx, req)
	s.Require().NoError(err)
	s.Equal("13.33", quote.TotalChargeAmount.StringFixed(2))
	s.Equal("13.33", quote.TotalCreditAmount.StringFixed(2))
	s.Equal("0.00", quote.NetAmount().StringFixed(2), "the swap nets to nothing")
	s.Len(quote.ChargeLineItems, 1)
	s.Len(quote.CreditLineItems, 1, "Compute carries both sides and Settle nets them")

	settled, err := s.applyViaSettle(req)
	s.Require().NoError(err)

	s.Empty(settled, "a net-zero swap settles no document")
	s.Equal(invoicesBefore, s.invoiceCount(), "no invoice for a net-zero change")
	s.Equal("0.00", s.walletBalance().StringFixed(2), "and no wallet credit either")
}

// A quote whose behaviour is None writes nothing at all — the preview path depends on it.
func (s *LineItemProrationServiceSuite) TestCharacteriseSettlement_BehaviourNone_WritesNothing() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)

	s.recordBilled(s.td.lineItem.ID, decimal.NewFromInt(20))
	invoicesBefore := s.invoiceCount()

	req := LineItemProrationRequest{
		Subscription:  s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd),
		EffectiveDate: effectiveDate,
		Behavior:      types.ProrationBehaviorNone,
		Entries: []LineItemProrationEntry{{
			LineItem: s.td.lineItem,
			Price:    s.td.fixedPrice,
			Action:   types.ProrationActionRemoveItem,
		}},
	}

	quote, err := s.svc.Compute(ctx, req)
	s.Require().NoError(err)
	s.True(quote.IsPreview, "behaviour None marks the quote as a preview")

	settled, err := s.applyViaSettle(req)
	s.Require().NoError(err)
	s.Empty(settled)
	s.Equal(invoicesBefore, s.invoiceCount())
	s.True(s.walletBalance().IsZero())
}

// The invoice request builder is shared by pay-later, preview and the pay-first draft, so
// what it puts on the document is a settlement invariant, not an invoice-service detail.
func (s *LineItemProrationServiceSuite) TestCharacteriseSettlement_ChargeInvoiceRequestShape() {
	ctx := s.GetContext()
	effectiveDate := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	sub := s.subCopyWithPeriod(s.td.periodStart, s.td.periodEnd)

	quote, err := s.svc.Compute(ctx, LineItemProrationRequest{
		Subscription:   sub,
		EffectiveDate:  effectiveDate,
		Behavior:       types.ProrationBehaviorCreateProrations,
		IdempotencyKey: "characterise_request_shape",
		Entries: []LineItemProrationEntry{{
			LineItem:    s.td.lineItem,
			Price:       s.td.fixedPrice,
			Action:      types.ProrationActionAddItem,
			NewQuantity: s.td.lineItem.Quantity,
		}},
	})
	s.Require().NoError(err)

	req := buildNettedProrationInvoiceRequest(NewSettleProrationRequest(
		sub, quote, effectiveDate, sub.CurrentPeriodEnd, "Subscription update", "key_shape", SettleModeIssue,
	))

	s.Equal(types.InvoiceTypeOneOff, req.InvoiceType)
	s.Equal(types.InvoiceBillingReasonSubscriptionUpdate, req.BillingReason)
	s.Equal(sub.ID, *req.SubscriptionID)
	s.Equal(sub.Currency, req.Currency)
	s.Equal("key_shape", *req.IdempotencyKey)

	// The window is passed in, not derived — a batch has no single effective date.
	s.True(req.PeriodStart.Equal(effectiveDate))
	s.True(req.PeriodEnd.Equal(sub.CurrentPeriodEnd))

	// All three totals carry the NET. For this charge-only quote net == the charge total,
	// so the amounts are unchanged from the pre-netting builder.
	s.Equal(quote.NetAmount(), req.AmountDue)
	s.Equal(quote.NetAmount(), req.Total)
	s.Equal(quote.NetAmount(), req.Subtotal)
	s.Equal(quote.TotalChargeAmount, req.AmountDue)
	s.Len(req.LineItems, len(quote.ChargeLineItems)+len(quote.CreditLineItems))

	// Line-item windows are per-entry and load-bearing for the billed-amounts credit cap.
	s.Require().NotEmpty(req.LineItems)
	s.True(req.LineItems[0].PeriodStart.Equal(effectiveDate))
	s.Equal(s.td.lineItem.ID, *req.LineItems[0].SubscriptionLineItemID)
}

// -----------------------------------------------------------------------------
// the preview twins and the pay-first draft
// -----------------------------------------------------------------------------

// setupMonthlyPeriod widens the shared fixture's 7-day window to a whole month, so a
// monthly addon price is quoted against a period it actually fits in.
