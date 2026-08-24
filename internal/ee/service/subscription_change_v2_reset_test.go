package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// resetRequest restarts the term and credits the unused remainder of the outgoing
// plan. See forfeitResetRequest for the variant that keeps the remainder.
func (s *SubscriptionChangeV2Suite) resetRequest(targetPlanID string) dto.SubscriptionChangeV2Request {
	req := s.changeRequest(targetPlanID, types.ProrationBehaviorCreateProrations)
	req.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
	return req
}

// forfeitResetRequest restarts the term and settles nothing for the period it cut
// short: the customer keeps no credit for the days they paid for and will not use.
func (s *SubscriptionChangeV2Suite) forfeitResetRequest(targetPlanID string) dto.SubscriptionChangeV2Request {
	req := s.changeRequest(targetPlanID, types.ProrationBehaviorNone)
	req.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
	return req
}

// ─────────────────────────────────────────────────────────────────────────────
// 3.1 Validation matrix
// ─────────────────────────────────────────────────────────────────────────────

func (s *SubscriptionChangeV2Suite) TestExecute_BillingPeriodBehaviourValidationMatrix() {
	ctx := s.GetContext()

	tests := []struct {
		name    string
		mutate  func(*dto.SubscriptionChangeV2Request)
		wantErr string
	}{
		{
			name:   "omitted behaves as unchanged",
			mutate: func(_ *dto.SubscriptionChangeV2Request) {},
		},
		{
			name: "explicit unchanged is accepted",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodBehaviour = types.BillingPeriodBehaviourUnchanged
			},
		},
		{
			name: "anchor_at_effect on an immediate change is accepted",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
			},
		},
		{
			name: "anchor_at_effect with prorations is accepted — it asks for the credit",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
				r.ProrationBehavior = types.ProrationBehaviorCreateProrations
			},
		},
		{
			name: "anchor_at_config is rejected rather than inferred",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtConfig
			},
			wantErr: "'anchor_at_config' is not supported yet",
		},
		{
			name: "billing_period is reserved",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodConfig = &dto.BillingPeriodConfig{
					BillingPeriodCount: lo.ToPtr(3),
				}
			},
			wantErr: "billing_period_config is not supported yet",
		},
		{
			name: "billing_period is reserved even alongside a supported behaviour",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
				r.BillingPeriodConfig = &dto.BillingPeriodConfig{}
			},
			wantErr: "billing_period_config is not supported yet",
		},
		{
			name: "an unknown behaviour is rejected",
			mutate: func(r *dto.SubscriptionChangeV2Request) {
				r.BillingPeriodBehaviour = "reset_next_period"
			},
			wantErr: "invalid billing_period_behaviour",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()

			req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
			tt.mutate(&req)

			_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
			if tt.wantErr == "" {
				s.Require().NoError(err)
				return
			}

			s.Require().Error(err)
			s.True(ierr.IsValidation(err))
			s.Contains(err.Error(), tt.wantErr)
			s.Equal(s.td.starter.ID, s.currentSub().PlanID, "a rejected request changes nothing")
		})
	}
}

// Resetting to an arbitrary instant contradicts calendar alignment.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetRejectedOnCalendarBilling() {
	ctx := s.GetContext()

	sub := s.currentSub()
	sub.BillingCycle = types.BillingCycleCalendar
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Contains(err.Error(), "calendar")
	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 3.4 Re-anchor
// ─────────────────────────────────────────────────────────────────────────────

func (s *SubscriptionChangeV2Suite) TestExecute_ResetMovesTheAnchorToTheChangeInstant() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	at := resp.EffectiveAt
	sub := s.currentSub()

	s.True(sub.BillingAnchor.Equal(at), "the anchor moves to the change instant")
	s.True(sub.CurrentPeriodStart.Equal(at), "the term restarts")

	expectedEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: at,
		BillingAnchor:      at,
		Unit:               sub.BillingPeriodCount,
		Period:             sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	s.Require().NoError(err)
	s.True(sub.CurrentPeriodEnd.Equal(expectedEnd), "the new period is a full period, never a stub")
	s.False(sub.CurrentPeriodEnd.Equal(s.td.periodEnd), "the old boundary is gone")

	s.Equal(types.BillingPeriodBehaviourAnchorAtEffect, resp.BillingPeriod.Behaviour)
	s.True(resp.BillingPeriod.BillingAnchor.Equal(at))
	s.True(resp.BillingPeriod.CurrentPeriodStart.Equal(at))
	s.True(resp.BillingPeriod.CurrentPeriodEnd.Equal(expectedEnd))
}

func (s *SubscriptionChangeV2Suite) TestExecute_UnchangedLeavesTheAnchorAlone() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().NoError(err)

	sub := s.currentSub()
	s.True(sub.BillingAnchor.Equal(s.td.periodStart))
	s.True(sub.CurrentPeriodStart.Equal(s.td.periodStart))
	s.True(sub.CurrentPeriodEnd.Equal(s.td.periodEnd))
	s.Equal(types.BillingPeriodBehaviourUnchanged, resp.BillingPeriod.Behaviour)
}

// ─────────────────────────────────────────────────────────────────────────────
// 3.2 The deferred path is a genuine no-op
// ─────────────────────────────────────────────────────────────────────────────

func (s *SubscriptionChangeV2Suite) TestExecute_DeferredResetIsANoOp() {
	ctx := s.GetContext()

	req := s.deferredRequest(s.td.pro.ID)
	req.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
	s.Require().NoError(err)
	s.True(resp.IsScheduled)

	sub := s.currentSub()
	s.True(sub.BillingAnchor.Equal(s.td.periodStart), "scheduling moves nothing")
	s.True(sub.CurrentPeriodEnd.Equal(s.td.periodEnd))

	sched := s.pendingPlanChange()
	s.Require().NotNil(sched)
	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err)
	s.Equal(types.BillingPeriodBehaviourAnchorAtEffect, config.BillingPeriodBehaviour,
		"the behaviour round-trips through the schedule blob")
}

// A month-end anchor must not drift when a deferred reset executes: assigning
// anchor = effective_at at a rolled Feb-28 boundary would move a Jan-31 chain to the 28th.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_DeferredResetKeepsMonthEndAnchor() {
	ctx := s.GetContext()

	jan31 := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	feb28 := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	sub := s.currentSub()
	sub.StartDate = jan31
	sub.BillingAnchor = jan31
	sub.CurrentPeriodStart = jan31
	sub.CurrentPeriodEnd = feb28
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	sched, config := s.createV2Schedule(s.td.pro.ID, feb28)
	config.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect
	s.rollPeriodTo(feb28, time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))

	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()))

	after := s.currentSub()
	s.True(after.BillingAnchor.Equal(jan31),
		"a deferred reset must leave the month-end anchor exactly where it was, got %s", after.BillingAnchor)
	s.True(after.CurrentPeriodEnd.Equal(time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)),
		"the chain stays on the 31st rather than drifting to the 28th")
	s.Equal(s.td.pro.ID, after.PlanID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 3.5 Settlement
// ─────────────────────────────────────────────────────────────────────────────

func (s *SubscriptionChangeV2Suite) resetInvoice(resp *dto.SubscriptionChangeV2Response) *dto.InvoiceResponse {
	for _, changed := range resp.ChangedResources.Invoices {
		if changed.Invoice != nil {
			return changed.Invoice
		}
	}
	return nil
}

func (s *SubscriptionChangeV2Suite) TestExecute_ResetNetsTheOldCreditAgainstTheNewFullPeriod() {
	ctx := s.GetContext()

	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	inv := s.resetInvoice(resp)
	s.Require().NotNil(inv, "an upgrade nets to a charge, so the settlement is an invoice")

	var charges, credits decimal.Decimal
	var sawProCharge, sawStarterCredit bool
	for _, li := range inv.LineItems {
		if li.Amount.IsNegative() {
			credits = credits.Add(li.Amount.Abs())
			if lo.FromPtr(li.PriceID) == s.td.starterBase.ID {
				sawStarterCredit = true
			}
			continue
		}
		charges = charges.Add(li.Amount)
		if lo.FromPtr(li.PriceID) == s.td.proBase.ID {
			sawProCharge = true
		}
	}

	s.True(sawProCharge, "the new plan is billed for its first full period")
	s.True(sawStarterCredit, "the unused portion of the outgoing plan is credited")

	s.Equal(s.td.proBase.Amount.String(), charges.String(),
		"the new plan is charged a full period, and the outgoing plan is never re-charged")
	s.True(credits.LessThan(charges), "the credit is netted, not stacked")
	s.Equal(charges.Sub(credits).String(), inv.AmountDue.String())
}

// The old plan's fixed fee must never be both credited and re-charged.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetDoesNotRebillTheOutgoingPlan() {
	ctx := s.GetContext()

	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	inv := s.resetInvoice(resp)
	s.Require().NotNil(inv)

	for _, li := range inv.LineItems {
		if lo.FromPtr(li.PriceID) == s.td.starterBase.ID {
			s.True(li.Amount.IsNegative(),
				"the outgoing plan may only appear as a credit, never as a charge")
		}
	}
}

// A downgrade can credit more than the new plan costs; the excess must reach the wallet.
// proration_behavior selects what happens to the period the reset cut short.
// none forfeits it: the new plan is billed in full and nothing is credited back, which
// is the only way to ask for a clean restart. Before this, none was the *required*
// value and the credit was issued anyway, so no caller could express either intent.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetWithoutProrationsForfeitsTheRemainder() {
	ctx := s.GetContext()

	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	forfeited, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.forfeitResetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	invoice := s.resetInvoice(forfeited)
	s.Require().NotNil(invoice, "the new plan is still billed for its full period")
	s.Equal(s.td.proBase.Amount.String(), invoice.AmountDue.String(),
		"the full price of the new plan, with nothing credited back")

	for _, li := range invoice.LineItems {
		s.False(li.Amount.IsNegative(), "a forfeited remainder must not appear as a credit line")
	}
}

// The same change asking for the credit bills strictly less, and the difference is the
// unused remainder of the outgoing plan.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetWithProrationsCreditsTheRemainder() {
	ctx := s.GetContext()

	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	credited, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	invoice := s.resetInvoice(credited)
	s.Require().NotNil(invoice)
	s.True(invoice.AmountDue.LessThan(s.td.proBase.Amount),
		"asking for prorations must bill less than the full price, got %s", invoice.AmountDue)
	s.True(lo.SomeBy(invoice.LineItems, func(li *dto.InvoiceLineItemResponse) bool {
		return li.Amount.IsNegative()
	}), "the credit for the outgoing plan is a line on the settlement")
}

func (s *SubscriptionChangeV2Suite) TestExecute_ResetRoutesExcessCreditToTheWallet() {
	ctx := s.GetContext()

	cheap := s.createPlan("Cheap", "cheap")
	s.createFixedPrice(cheap.ID, "base_fee", 1)
	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(cheap.ID), time.Now().UTC())
	s.Require().NoError(err)

	var walletCredit *dto.ChangedInvoice
	for i := range resp.ChangedResources.Invoices {
		if resp.ChangedResources.Invoices[i].Action == dto.ChangedInvoiceActionWalletCredit {
			walletCredit = &resp.ChangedResources.Invoices[i]
		}
	}

	s.Require().NotNil(walletCredit, "credit beyond the new plan's charge must not be discarded")
	s.Require().NotNil(walletCredit.WalletTransaction)
	s.True(walletCredit.WalletTransaction.Amount.IsPositive())
}

// A carried line — same price on both plans — is paid for through the old period end,
// but the reset hands the customer a fresh full period starting now. The overlap must be
// credited back and re-charged, or they get it free.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetRebillsCarriedItemsForTheRestartedPeriod() {
	ctx := s.GetContext()

	lateral := s.createPlan("Lateral", "lateral")
	lateralBase := s.createFixedPrice(lateral.ID, "base_fee", 20)
	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(lateral.ID), time.Now().UTC())
	s.Require().NoError(err)

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.baseLine.ID, live[0].ID, "the line is still carried, not replaced")

	inv := s.resetInvoice(resp)
	s.Require().NotNil(inv, "a carried line still owes the overlap once the term restarts")

	var charges, credits decimal.Decimal
	for _, li := range inv.LineItems {
		if li.Amount.IsNegative() {
			credits = credits.Add(li.Amount.Abs())
			continue
		}
		charges = charges.Add(li.Amount)
	}

	s.Equal(lateralBase.Amount.String(), charges.String(), "a full period on the new term")
	s.True(credits.IsPositive(), "and the unused remainder of the old one credited back")
	s.True(charges.GreaterThan(credits),
		"the customer owes the overlap they had already paid for but no longer get")
}

// ─────────────────────────────────────────────────────────────────────────────
// 3.6 Preview parity
// ─────────────────────────────────────────────────────────────────────────────

func (s *SubscriptionChangeV2Suite) TestPreview_ResetQuotesTheCloseOutInvoice() {
	ctx := s.GetContext()

	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID))
	s.Require().NoError(err)

	quoted := s.resetInvoice(preview)
	s.Require().NotNil(quoted, "a reset preview must quote the close-out invoice, not the ordinary delta")

	var sawProCharge bool
	for _, li := range quoted.LineItems {
		if lo.FromPtr(li.PriceID) == s.td.proBase.ID && li.Amount.IsPositive() {
			sawProCharge = true
		}
	}
	s.True(sawProCharge, "the preview prices the target plan, not the outgoing one")

	s.Equal(s.td.starter.ID, s.currentSub().PlanID, "preview writes nothing")
	s.True(s.currentSub().BillingAnchor.Equal(s.td.periodStart), "preview does not re-anchor")

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	raised := s.resetInvoice(resp)
	s.Require().NotNil(raised)
	s.Equal(quoted.AmountDue.String(), raised.AmountDue.String(),
		"the quote and the invoice must agree")
}

// Preview reports where the anchor would land, not where it stands, so a caller can see
// the term restart before committing to it.
func (s *SubscriptionChangeV2Suite) TestPreview_ResetEchoesTheProjectedAnchor() {
	ctx := s.GetContext()

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID))
	s.Require().NoError(err)

	s.Equal(types.BillingPeriodBehaviourAnchorAtEffect, preview.BillingPeriod.Behaviour)
	s.True(preview.BillingPeriod.BillingAnchor.Equal(preview.EffectiveAt),
		"the projected anchor is the change instant")
	s.True(preview.BillingPeriod.CurrentPeriodStart.Equal(preview.EffectiveAt))
	s.False(preview.BillingPeriod.CurrentPeriodEnd.Equal(s.td.periodEnd),
		"and the projected period is the restarted one, not the interrupted one")

	s.True(s.currentSub().BillingAnchor.Equal(s.td.periodStart), "preview still writes nothing")

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)
	s.Equal(preview.BillingPeriod.Behaviour, resp.BillingPeriod.Behaviour)
}

// A deferred reset is a no-op, and preview must say so rather than projecting a move.
func (s *SubscriptionChangeV2Suite) TestPreview_DeferredResetEchoesAnUnmovedAnchor() {
	ctx := s.GetContext()

	req := s.deferredRequest(s.td.pro.ID)
	req.BillingPeriodBehaviour = types.BillingPeriodBehaviourAnchorAtEffect

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)

	s.True(preview.BillingPeriod.BillingAnchor.Equal(s.td.periodStart))
	s.True(preview.BillingPeriod.CurrentPeriodEnd.Equal(s.td.periodEnd))
}

// ─────────────────────────────────────────────────────────────────────────────
// The outgoing window's usage
// ─────────────────────────────────────────────────────────────────────────────

// seedOutgoingUsage puts a usage price on the outgoing plan and records consumption
// inside the period the reset will cut short.
func (s *SubscriptionChangeV2Suite) seedOutgoingUsage(units int64, rate int64) *price.Price {
	ctx := s.GetContext()

	m := &meter.Meter{
		ID:          "meter_reset_api_calls",
		Name:        "API Calls",
		EventName:   "api_call",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "units"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	usagePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(rate),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_USAGE,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.td.starter.ID,
		MeterID:            m.ID,
		LookupKey:          "api_calls",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, usagePrice))

	item := &subscription.SubscriptionLineItem{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:     s.td.sub.ID,
		CustomerID:         s.td.sub.CustomerID,
		EntityID:           s.td.starter.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    s.td.starter.Name,
		PriceID:            usagePrice.ID,
		PriceType:          types.PRICE_TYPE_USAGE,
		MeterID:            m.ID,
		DisplayName:        "API Calls",
		Quantity:           decimal.Zero,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          s.td.periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, item))

	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			ExternalCustomerID: s.td.customer.ExternalID,
			CustomerID:         s.td.customer.ID,
			EventName:          m.EventName,
			Timestamp:          s.td.periodStart.AddDate(0, 0, 1),
		},
		MeterID:  m.ID,
		QtyTotal: decimal.NewFromInt(units),
	}}))

	return usagePrice
}

// The outgoing period never reaches its boundary, and the next roll's window starts at
// the change instant — so if the reset does not bill this usage, nobody ever will.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetInvoicesTheOutgoingWindowsUsageSeparately() {
	ctx := s.GetContext()

	usagePrice := s.seedOutgoingUsage(40, 2)
	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	var usageInvoice, netInvoice *dto.InvoiceResponse
	for _, changed := range resp.ChangedResources.Invoices {
		if changed.Invoice == nil {
			continue
		}
		if lo.SomeBy(changed.Invoice.LineItems, func(li *dto.InvoiceLineItemResponse) bool {
			return lo.FromPtr(li.PriceID) == usagePrice.ID
		}) {
			usageInvoice = changed.Invoice
			continue
		}
		netInvoice = changed.Invoice
	}

	s.Require().NotNil(usageInvoice, "the outgoing window's usage must be invoiced at the change")
	s.Equal("80", usageInvoice.AmountDue.String(), "40 units at 2")

	for _, li := range usageInvoice.LineItems {
		s.Equal(usagePrice.ID, lo.FromPtr(li.PriceID),
			"the usage invoice carries usage only — arrear fixed charges misprice over a short window")
	}

	s.Require().NotNil(netInvoice, "the plan-change net is settled separately")
	s.False(lo.SomeBy(netInvoice.LineItems, func(li *dto.InvoiceLineItemResponse) bool {
		return lo.FromPtr(li.PriceID) == usagePrice.ID
	}), "usage must not be counted twice")
}

// With usage kept separate, a net-negative downgrade still leaves a revenue record for
// what was consumed instead of folding it into a wallet credit.
func (s *SubscriptionChangeV2Suite) TestExecute_ResetInvoicesUsageEvenWhenTheNetIsACredit() {
	ctx := s.GetContext()

	usagePrice := s.seedOutgoingUsage(1, 1)
	cheap := s.createPlan("Cheap", "cheap")
	s.createFixedPrice(cheap.ID, "base_fee", 1)
	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(cheap.ID), time.Now().UTC())
	s.Require().NoError(err)

	var sawUsageInvoice, sawWalletCredit bool
	for _, changed := range resp.ChangedResources.Invoices {
		if changed.Action == dto.ChangedInvoiceActionWalletCredit {
			sawWalletCredit = true
			continue
		}
		if changed.Invoice != nil && lo.SomeBy(changed.Invoice.LineItems, func(li *dto.InvoiceLineItemResponse) bool {
			return lo.FromPtr(li.PriceID) == usagePrice.ID
		}) {
			sawUsageInvoice = true
		}
	}

	s.True(sawUsageInvoice, "consumed usage is invoiced regardless of which way the net falls")
	s.True(sawWalletCredit, "and the net credit still reaches the wallet")
}
