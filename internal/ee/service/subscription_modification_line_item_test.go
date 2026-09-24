package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// createRepricableFixedPrice adds the billing period count that createFixedPrice omits;
// minting an override price validates the full CreatePriceRequest.
func (s *SubscriptionModificationServiceSuite) createRepricableFixedPrice(amount decimal.Decimal) *price.Price {
	p := s.createFixedPrice(amount, types.InvoiceCadenceAdvance)
	p.BillingPeriodCount = 1
	s.Require().NoError(s.GetStores().PriceRepo.Update(s.GetContext(), p, false))
	return p
}

func (s *SubscriptionModificationServiceSuite) lineItemChangeRequest(
	lineItemID string,
	quantity *decimal.Decimal,
	amount *decimal.Decimal,
	effectiveDate time.Time,
) dto.ExecuteSubscriptionModifyRequest {
	return dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeLineItemChange,
		LineItemChangeParams: &dto.SubModifyLineItemChangeRequest{
			LineItems: []dto.LineItemChange{
				{ID: lineItemID, Quantity: quantity, Amount: amount, EffectiveDate: &effectiveDate},
			},
		},
	}
}

// proratedDelta mirrors the second-based coefficient the calculator applies.
func proratedDelta(periodStart, periodEnd, effectiveDate time.Time, delta decimal.Decimal) decimal.Decimal {
	end := periodEnd.Add(-time.Second)
	remaining := end.Sub(effectiveDate).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	coeff := decimal.NewFromFloat(remaining / end.Sub(periodStart).Seconds())
	return delta.Mul(coeff)
}

func (s *SubscriptionModificationServiceSuite) TestExecuteLineItemChange_PriceChange() {
	type tc struct {
		name              string
		oldAmount         decimal.Decimal
		newAmount         decimal.Decimal
		quantity          decimal.Decimal
		wantInvoiceAction dto.ChangedInvoiceAction
	}
	cases := []tc{
		{
			name:              "price_increase_charges",
			oldAmount:         decimal.NewFromInt(10),
			newAmount:         decimal.NewFromInt(20),
			quantity:          decimal.NewFromInt(2),
			wantInvoiceAction: dto.ChangedInvoiceActionCreated,
		},
		{
			name:              "price_decrease_credits",
			oldAmount:         decimal.NewFromInt(20),
			newAmount:         decimal.NewFromInt(10),
			quantity:          decimal.NewFromInt(1),
			wantInvoiceAction: dto.ChangedInvoiceActionWalletCredit,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			periodStart := s.GetNow()
			periodEnd := periodStart.AddDate(0, 1, 0)
			effectiveDate := periodStart.AddDate(0, 0, 15)

			cust := s.createCustomer("lic-" + tc.name)
			sub := s.createActiveSub(cust.ID)
			oldPrice := s.createRepricableFixedPrice(tc.oldAmount)
			li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, tc.quantity, types.InvoiceCadenceAdvance, oldPrice.ID)

			resp, err := s.service.Execute(ctx, sub.ID,
				s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(tc.newAmount), effectiveDate))
			s.Require().NoError(err)
			s.Require().Len(resp.ChangedResources.LineItems, 2)

			ended, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
			s.Require().NoError(err)
			s.Equal(effectiveDate, ended.EndDate, "predecessor must end at effective_date")

			successorID := ended.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID]
			s.Require().NotEmpty(successorID, "predecessor must point at its successor")

			created, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, successorID)
			s.Require().NoError(err)
			s.Equal(li.ID, created.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID])
			s.Equal(effectiveDate, created.StartDate)
			s.Equal(tc.quantity, created.Quantity, "quantity is carried over when only the price changes")

			s.NotEqual(oldPrice.ID, created.PriceID, "successor must carry a new override price")
			newPrice, err := s.GetStores().PriceRepo.Get(ctx, created.PriceID)
			s.Require().NoError(err)
			s.Equal(tc.newAmount, newPrice.Amount)
			s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, newPrice.EntityType)
			s.Equal(sub.ID, newPrice.EntityID)

			s.Require().Len(resp.ChangedResources.Invoices, 1)
			got := resp.ChangedResources.Invoices[0]
			s.Equal(tc.wantInvoiceAction, got.Action)

			want := proratedDelta(periodStart, periodEnd, effectiveDate,
				tc.newAmount.Sub(tc.oldAmount).Mul(tc.quantity)).Abs()
			tolerance := decimal.NewFromFloat(0.01)

			if tc.wantInvoiceAction == dto.ChangedInvoiceActionCreated {
				inv, fetchErr := s.GetStores().InvoiceRepo.Get(ctx, got.ID)
				s.Require().NoError(fetchErr)
				s.True(inv.AmountDue.Sub(want).Abs().LessThanOrEqual(tolerance),
					"charge %s should be ≈ %s", inv.AmountDue, want)
				return
			}

			s.Require().NotNil(got.WalletTransaction)
			s.True(got.WalletTransaction.Amount.Sub(want).Abs().LessThanOrEqual(tolerance),
				"credit %s should be ≈ %s", got.WalletTransaction.Amount, want)
		})
	}
}

func (s *SubscriptionModificationServiceSuite) TestExecuteLineItemChange_QuantityAndPriceTogether() {
	ctx := s.GetContext()
	periodStart := s.GetNow()
	periodEnd := periodStart.AddDate(0, 1, 0)
	effectiveDate := periodStart.AddDate(0, 0, 10)

	cust := s.createCustomer("lic-combined")
	sub := s.createActiveSub(cust.ID)
	oldPrice := s.createRepricableFixedPrice(decimal.NewFromInt(10))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(2), types.InvoiceCadenceAdvance, oldPrice.ID)

	newQty, newAmount := decimal.NewFromInt(5), decimal.NewFromInt(20)
	resp, err := s.service.Execute(ctx, sub.ID,
		s.lineItemChangeRequest(li.ID, lo.ToPtr(newQty), lo.ToPtr(newAmount), effectiveDate))
	s.Require().NoError(err)

	successorID := resp.ChangedResources.LineItems[1].ID
	created, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, successorID)
	s.Require().NoError(err)
	s.Equal(newQty, created.Quantity)

	newPrice, err := s.GetStores().PriceRepo.Get(ctx, created.PriceID)
	s.Require().NoError(err)
	s.Equal(newAmount, newPrice.Amount)

	// Both sides move: credit 2 × $10, charge 5 × $20.
	want := proratedDelta(periodStart, periodEnd, effectiveDate,
		newAmount.Mul(newQty).Sub(decimal.NewFromInt(10).Mul(decimal.NewFromInt(2))))
	s.Require().Len(resp.ChangedResources.Invoices, 1)
	inv, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ChangedResources.Invoices[0].ID)
	s.Require().NoError(err)
	s.True(inv.AmountDue.Sub(want).Abs().LessThanOrEqual(decimal.NewFromFloat(0.01)),
		"charge %s should be ≈ %s", inv.AmountDue, want)
}

// A preview must not mint the override price or touch the line items.
func (s *SubscriptionModificationServiceSuite) TestPreviewLineItemChange_WritesNothing() {
	ctx := s.GetContext()
	periodStart := s.GetNow()
	effectiveDate := periodStart.AddDate(0, 0, 15)

	cust := s.createCustomer("lic-preview")
	sub := s.createActiveSub(cust.ID)
	oldPrice := s.createRepricableFixedPrice(decimal.NewFromInt(20))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, oldPrice.ID)

	pricesBefore, err := s.GetStores().PriceRepo.List(ctx, types.NewNoLimitPriceFilter())
	s.Require().NoError(err)

	resp, err := s.service.Preview(ctx, sub.ID,
		s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(decimal.NewFromInt(40)), effectiveDate))
	s.Require().NoError(err)
	s.Require().Len(resp.ChangedResources.LineItems, 2)
	s.Require().Len(resp.ChangedResources.Invoices, 1)
	s.Equal(dto.ChangedInvoiceStatusPreview, resp.ChangedResources.Invoices[0].Status)

	pricesAfter, err := s.GetStores().PriceRepo.List(ctx, types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Len(pricesAfter, len(pricesBefore), "preview must not persist an override price")

	unchanged, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.True(unchanged.EndDate.IsZero(), "preview must not end the line item")
	s.Equal(oldPrice.ID, unchanged.PriceID)
}

// Gates the Phase 5 deletion: a quantity-only change must net the same through both pipelines.
func (s *SubscriptionModificationServiceSuite) TestLineItemChange_QuantityParityWithQuantityChange() {
	ctx := s.GetContext()
	periodStart := s.GetNow()
	effectiveDate := periodStart.AddDate(0, 0, 12)
	amount := decimal.NewFromInt(30)
	oldQty, newQty := decimal.NewFromInt(2), decimal.NewFromInt(5)

	run := func(name string, req func(lineItemID string) dto.ExecuteSubscriptionModifyRequest) decimal.Decimal {
		cust := s.createCustomer("lic-parity-" + name)
		sub := s.createActiveSub(cust.ID)
		p := s.createFixedPrice(amount, types.InvoiceCadenceAdvance)
		li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, oldQty, types.InvoiceCadenceAdvance, p.ID)

		resp, err := s.service.Execute(ctx, sub.ID, req(li.ID))
		s.Require().NoError(err)
		s.Require().Len(resp.ChangedResources.Invoices, 1)

		inv, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ChangedResources.Invoices[0].ID)
		s.Require().NoError(err)
		return inv.AmountDue
	}

	old := run("old", func(lineItemID string) dto.ExecuteSubscriptionModifyRequest {
		return dto.ExecuteSubscriptionModifyRequest{
			Type: dto.SubscriptionModifyTypeQuantityChange,
			QuantityChangeParams: &dto.SubModifyQuantityChangeRequest{
				LineItems: []dto.LineItemQuantityChange{
					{ID: lineItemID, Quantity: newQty, EffectiveDate: &effectiveDate},
				},
			},
		}
	})

	fresh := run("new", func(lineItemID string) dto.ExecuteSubscriptionModifyRequest {
		return s.lineItemChangeRequest(lineItemID, lo.ToPtr(newQty), nil, effectiveDate)
	})

	s.True(old.Sub(fresh).Abs().LessThanOrEqual(decimal.NewFromFloat(0.01)),
		"quantity_change charged %s but line_item_change charged %s", old, fresh)
}

func (s *SubscriptionModificationServiceSuite) TestExecuteLineItemChange_RejectsNonFlatFeeReprice() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 5)

	cust := s.createCustomer("lic-tiered")
	sub := s.createActiveSub(cust.ID)

	p := s.createFixedPrice(decimal.NewFromInt(10), types.InvoiceCadenceAdvance)
	p.BillingModel = types.BILLING_MODEL_TIERED
	s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))

	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, p.ID)

	_, err := s.service.Execute(ctx, sub.ID,
		s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(decimal.NewFromInt(20)), effectiveDate))
	s.Require().Error(err)
	s.Contains(err.Error(), "billing model")

	// A quantity-only change on the same line item is still allowed.
	_, err = s.service.Execute(ctx, sub.ID,
		s.lineItemChangeRequest(li.ID, lo.ToPtr(decimal.NewFromInt(3)), nil, effectiveDate))
	s.Require().NoError(err)
}

// The checkout session must carry enough to rebuild the change on payment success,
// price included. Asserted on the persisted shape: opening a real session needs a provider.
func (s *SubscriptionModificationServiceSuite) TestLineItemChange_CheckoutParamsCarryThePrice() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	cust := s.createCustomer("lic-checkout-params")
	sub := s.createActiveSub(cust.ID)
	oldPrice := s.createRepricableFixedPrice(decimal.NewFromInt(10))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, oldPrice.ID)

	impl := s.service.(*subscriptionModificationService)
	request, err := impl.buildLineItemChangeRequest(ctx, sub.ID, &dto.SubModifyLineItemChangeRequest{
		LineItems: []dto.LineItemChange{
			{ID: li.ID, Amount: lo.ToPtr(decimal.NewFromInt(40)), EffectiveDate: &effectiveDate},
		},
	})
	s.Require().NoError(err)

	params := request.toModifySubscriptionParams()
	s.Require().NoError(params.Validate())
	s.Equal(types.ModifySubscriptionTypeLineItemChange, params.ModifyType)
	s.Equal(sub.ID, params.SubscriptionID)
	s.Require().Len(params.LineItemModifications, 1)

	mod := params.LineItemModifications[0]
	s.Equal(li.ID, mod.LineItemID)
	s.Require().NotNil(mod.Amount)
	s.Equal(decimal.NewFromInt(40), *mod.Amount)
	s.Nil(mod.Quantity, "a price-only change leaves quantity unset")
	s.Require().NotNil(mod.EffectiveDate)
	s.Equal(effectiveDate, *mod.EffectiveDate)
}

// A net credit has nothing to collect, so checkout is ignored and the change applies now.
func (s *SubscriptionModificationServiceSuite) TestExecuteLineItemChange_CheckoutIgnoredOnCredit() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	cust := s.createCustomer("lic-checkout-credit")
	sub := s.createActiveSub(cust.ID)
	oldPrice := s.createRepricableFixedPrice(decimal.NewFromInt(40))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, oldPrice.ID)

	req := s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(decimal.NewFromInt(10)), effectiveDate)
	req.Checkout = s.checkoutParamsRazorpay()

	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Nil(resp.CheckoutSession, "checkout must be ignored when the batch nets to a credit")
	s.Require().Len(resp.ChangedResources.Invoices, 1)
	s.Equal(dto.ChangedInvoiceActionWalletCredit, resp.ChangedResources.Invoices[0].Action)

	ended, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.False(ended.EndDate.IsZero(), "the pay-later path applies the change immediately")
}

// Pay-first must not touch line items or mint the override price before payment lands.
func (s *SubscriptionModificationServiceSuite) TestExecuteLineItemChange_PayFirstDefersApply() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	cust := s.createCustomer("lic-payfirst-defers")
	sub := s.createActiveSub(cust.ID)
	oldPrice := s.createRepricableFixedPrice(decimal.NewFromInt(10))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, oldPrice.ID)

	pricesBefore, err := s.GetStores().PriceRepo.List(ctx, types.NewNoLimitPriceFilter())
	s.Require().NoError(err)

	req := s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(decimal.NewFromInt(40)), effectiveDate)
	req.Checkout = s.checkoutParamsRazorpay()

	// The provider is unavailable in tests; what matters is that nothing was applied either way.
	_, _ = s.service.Execute(ctx, sub.ID, req)

	unchanged, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.True(unchanged.EndDate.IsZero(), "line items are applied on payment success, not at checkout")
	s.Equal(oldPrice.ID, unchanged.PriceID)

	pricesAfter, err := s.GetStores().PriceRepo.List(ctx, types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Len(pricesAfter, len(pricesBefore), "the override price must not be minted before payment")
}

// Replaying the persisted params applies the change, and a duplicate delivery is a no-op.
func (s *SubscriptionModificationServiceSuite) TestLineItemChange_CheckoutReplayIsIdempotent() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	cust := s.createCustomer("lic-replay")
	sub := s.createActiveSub(cust.ID)
	oldPrice := s.createRepricableFixedPrice(decimal.NewFromInt(10))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, oldPrice.ID)

	impl := s.service.(*subscriptionModificationService)
	params := &types.ModifySubscriptionParams{
		SubscriptionID: sub.ID,
		ModifyType:     types.ModifySubscriptionTypeLineItemChange,
		LineItemModifications: []types.ModifySubscriptionLineItem{
			{LineItemID: li.ID, Amount: lo.ToPtr(decimal.NewFromInt(40)), EffectiveDate: &effectiveDate},
		},
	}

	s.Require().NoError(impl.applyModifySubscriptionParams(ctx, params))

	ended, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.Equal(effectiveDate, ended.EndDate)
	successorID := ended.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID]
	s.Require().NotEmpty(successorID)

	s.Require().NoError(impl.applyModifySubscriptionParams(ctx, params), "duplicate delivery must not error")

	after, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.Equal(successorID, after.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID],
		"duplicate delivery must not create a second successor")
}

// Sessions written before line_item_change existed carry no modify type and must still replay.
func (s *SubscriptionModificationServiceSuite) TestLineItemChange_LegacyParamsReplayAsQuantityChange() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	cust := s.createCustomer("lic-legacy-replay")
	sub := s.createActiveSub(cust.ID)
	p := s.createFixedPrice(decimal.NewFromInt(10), types.InvoiceCadenceAdvance)
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, p.ID)

	impl := s.service.(*subscriptionModificationService)
	s.Require().NoError(impl.applyModifySubscriptionParams(ctx, &types.ModifySubscriptionParams{
		SubscriptionID: sub.ID,
		LineItemModifications: []types.ModifySubscriptionLineItem{
			{LineItemID: li.ID, Quantity: lo.ToPtr(decimal.NewFromInt(4)), EffectiveDate: &effectiveDate},
		},
	}))

	ended, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.Equal(effectiveDate, ended.EndDate, "an empty modify type must still replay the quantity change")
}

// Two entries for one line item have no defined composition. Before the guard the quote summed
// both deltas while apply silently skipped the second, billing for a change never made.
func (s *SubscriptionModificationServiceSuite) TestLineItemChange_RejectsDuplicateLineItemID() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 10)

	cust := s.createCustomer("lic-dupe")
	sub := s.createActiveSub(cust.ID)
	p := s.createRepricableFixedPrice(decimal.NewFromInt(10))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, p.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeLineItemChange,
		LineItemChangeParams: &dto.SubModifyLineItemChangeRequest{
			LineItems: []dto.LineItemChange{
				{ID: li.ID, Amount: lo.ToPtr(decimal.NewFromInt(12)), EffectiveDate: &effectiveDate},
				{ID: li.ID, Amount: lo.ToPtr(decimal.NewFromInt(15)), EffectiveDate: &effectiveDate},
			},
		},
	}

	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "duplicate line item id")

	// Nothing was billed and the line item is untouched.
	unchanged, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.True(unchanged.EndDate.IsZero())
	s.Equal(p.ID, unchanged.PriceID)

	_, err = s.service.Preview(ctx, sub.ID, req)
	s.Require().Error(err, "preview must reject the same shape execute does")
}

// A preview quotes a document it never writes; callers tell that apart by the placeholder ID.
func (s *SubscriptionModificationServiceSuite) TestPreviewLineItemChange_CarriesPlaceholderIDs() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	run := func(name string, oldAmount, newAmount decimal.Decimal) dto.ChangedInvoice {
		cust := s.createCustomer("lic-placeholder-" + name)
		sub := s.createActiveSub(cust.ID)
		p := s.createRepricableFixedPrice(oldAmount)
		li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, p.ID)

		resp, err := s.service.Preview(ctx, sub.ID,
			s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(newAmount), effectiveDate))
		s.Require().NoError(err)
		s.Require().Len(resp.ChangedResources.Invoices, 1)
		return resp.ChangedResources.Invoices[0]
	}

	charge := run("charge", decimal.NewFromInt(20), decimal.NewFromInt(40))
	s.Equal(dto.ChangedInvoiceActionCreated, charge.Action)
	s.Equal(previewInvoiceID, charge.ID)

	credit := run("credit", decimal.NewFromInt(40), decimal.NewFromInt(20))
	s.Equal(dto.ChangedInvoiceActionWalletCredit, credit.Action)
	s.Equal(previewWalletCreditID, credit.ID)
}

// A prorated adjustment is not a period charge; without the label and window the invoice reads
// as a full-price line, since the amount is a delta but the quantity is the priced quantity.
func (s *SubscriptionModificationServiceSuite) TestExecuteLineItemChange_InvoiceLineIsLabelledAsProration() {
	ctx := s.GetContext()
	effectiveDate := s.GetNow().AddDate(0, 0, 15)

	cust := s.createCustomer("lic-label")
	sub := s.createActiveSub(cust.ID)
	p := s.createRepricableFixedPrice(decimal.NewFromInt(20))
	li := s.createFixedLineItemWithPrice(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceAdvance, p.ID)

	resp, err := s.service.Execute(ctx, sub.ID,
		s.lineItemChangeRequest(li.ID, nil, lo.ToPtr(decimal.NewFromInt(40)), effectiveDate))
	s.Require().NoError(err)
	s.Require().Len(resp.ChangedResources.Invoices, 1)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ChangedResources.Invoices[0].ID)
	s.Require().NoError(err)
	s.Require().Len(inv.LineItems, 1)

	display := lo.FromPtr(inv.LineItems[0].DisplayName)
	s.Contains(display, "Proration charge", "invoice line must say it is a proration: %q", display)
	s.Contains(display, effectiveDate.Format("2 Jan 2006"), "invoice line must carry the window: %q", display)
}
