package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Flows that end the period early: the open draft holding credits applied at expiry is billed
// as the early invoice, or voided, instead of being left behind.

// settledSubscription is the shared starting point: 30 free credits expired mid-period with 20 of
// usage before the expiry (20 applied to the draft, 10 expired), 10 more usage after it, and 100
// purchased credits.
func (s *CreditExpiryInvoiceRaceSuite) settledSubscription(id string) (*subscription.Subscription, *invoice.Invoice) {
	tx, periodStart, periodEnd := s.midPeriodGrant(30)
	s.purchasedCredits(100)
	sub := s.usageSubscription(id, periodStart, periodStart, periodEnd)
	s.usage(sub, lo.FromPtr(tx.ExpiryDate).Add(-2*time.Hour), 20)

	_, err := s.walletService.ExpireCredits(s.GetContext(), tx.ID)
	s.Require().NoError(err)
	draft := s.onlyDraft(sub.ID)
	s.Require().True(decimal.NewFromInt(20).Equal(draft.TotalPrepaidCreditsApplied))

	s.usage(sub, time.Now().UTC().Add(-time.Hour), 10)
	return sub, draft
}

func (s *CreditExpiryInvoiceRaceSuite) subscriptionService() *subscriptionService {
	return NewSubscriptionService(s.params).(*subscriptionService)
}

func (s *CreditExpiryInvoiceRaceSuite) invoice(id string) *invoice.Invoice {
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return inv
}

func (s *CreditExpiryInvoiceRaceSuite) subscriptionInvoices(subID string) []*invoice.Invoice {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subID
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return invoices
}

// Immediate cancel with an invoice: the draft becomes the cancel invoice, keeping the 20 applied at
// expiry, and only the 10 used after the expiry is taken from purchased credits.
func (s *CreditExpiryInvoiceRaceSuite) TestPeriodEnd_ImmediateCancelBillsTheDraft() {
	sub, draft := s.settledSubscription("subs_cancel_invoice")

	_, err := s.subscriptionService().CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
		ProrationBehavior:              types.ProrationBehaviorNone,
	})
	s.Require().NoError(err)

	invoices := s.subscriptionInvoices(sub.ID)
	s.Require().Len(invoices, 1, "no second invoice for the same usage")
	inv := s.invoice(draft.ID)
	s.Equal(string(types.InvoiceBillingReasonProration), inv.BillingReason)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.True(inv.PeriodEnd.Before(draft.PeriodEnd.Add(-24*time.Hour)), "period end moved to the cancel")
	s.True(decimal.NewFromInt(30).Equal(inv.TotalPrepaidCreditsApplied), "applied %s", inv.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(90).Equal(s.creditsAvailable("wtxn_purchased")), "only 10 from purchased, left %s", s.creditsAvailable("wtxn_purchased"))
}

// Immediate cancel without an invoice: the draft is voided and its applied credits refunded.
func (s *CreditExpiryInvoiceRaceSuite) TestPeriodEnd_ImmediateCancelWithoutInvoiceVoidsTheDraft() {
	sub, draft := s.settledSubscription("subs_cancel_skip")

	_, err := s.subscriptionService().CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicySkip,
		ProrationBehavior:              types.ProrationBehaviorNone,
	})
	s.Require().NoError(err)

	s.Equal(types.InvoiceStatusVoided, s.invoice(draft.ID).InvoiceStatus)
	refunds := s.walletTransactions(types.TransactionReasonInvoiceVoidRefund)
	s.Require().Len(refunds, 1)
	s.True(decimal.NewFromInt(20).Equal(refunds[0].CreditAmount), "refunded %s", refunds[0].CreditAmount)
}

// Scheduled cancel inside the period: the draft's end follows the period's, so the run at the cancel
// date reuses it; reverting the schedule moves it back.
func (s *CreditExpiryInvoiceRaceSuite) TestPeriodEnd_ScheduledCancelMovesTheDraftAndRevertRestoresIt() {
	sub, draft := s.settledSubscription("subs_cancel_scheduled")
	cancelAt := time.Now().UTC().Add(2 * 24 * time.Hour)

	_, err := s.subscriptionService().CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeScheduledDate,
		CancelAt:          &cancelAt,
		ProrationBehavior: types.ProrationBehaviorNone,
	})
	s.Require().NoError(err)

	moved := s.invoice(draft.ID)
	s.True(moved.PeriodEnd.Equal(cancelAt), "draft ends at the cancel, got %s", moved.PeriodEnd)
	s.Equal(types.InvoiceStatusDraft, moved.InvoiceStatus)
	s.True(decimal.NewFromInt(20).Equal(moved.TotalPrepaidCreditsApplied))

	shortened, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.Require().NoError(err)
	reused, _, err := s.invoiceService.GetOrComputeCurrentPeriodDraft(s.GetContext(), shortened)
	s.Require().NoError(err)
	s.Equal(draft.ID, reused.ID, "the period-end run finds the moved draft")

	schedules, err := s.GetStores().SubscriptionScheduleRepo.GetBySubscriptionID(s.GetContext(), sub.ID)
	s.Require().NoError(err)
	s.Require().NotEmpty(schedules)
	s.Require().NoError(NewSubscriptionScheduleService(s.params, nil).Cancel(s.GetContext(), schedules[0].ID))

	s.True(s.invoice(draft.ID).PeriodEnd.Equal(*draft.PeriodEnd), "draft end restored with the period")
}

// Threshold invoice: the draft becomes the threshold invoice and the period restarts after it.
func (s *CreditExpiryInvoiceRaceSuite) TestPeriodEnd_ThresholdBillsTheDraft() {
	sub, draft := s.settledSubscription("subs_threshold")
	limit := decimal.NewFromInt(5)
	sub.AutoInvoiceThreshold = &limit
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	now := time.Now().UTC()
	item := &dto.AutoInvoiceThresholdBillingResultItem{SubscriptionID: sub.ID}
	s.Require().NoError(s.subscriptionService().processAutoInvoiceThresholdSubscription(s.GetContext(), sub, now, item))

	s.True(item.Invoiced)
	s.Equal(draft.ID, item.InvoiceID, "the draft is the threshold invoice")
	s.Len(s.subscriptionInvoices(sub.ID), 1)
	inv := s.invoice(draft.ID)
	s.Equal(string(types.InvoiceBillingReasonAutoInvoiceThreshold), inv.BillingReason)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.True(decimal.NewFromInt(30).Equal(inv.TotalPrepaidCreditsApplied), "applied %s", inv.TotalPrepaidCreditsApplied)
	s.True(decimal.NewFromInt(90).Equal(s.creditsAvailable("wtxn_purchased")))
}

// A credit that expired since the period started and hasn't been processed yet defers the threshold
// invoice, which would otherwise finalize without it.
func (s *CreditExpiryInvoiceRaceSuite) TestPeriodEnd_ThresholdWaitsForPendingExpiry() {
	now := time.Now().UTC()
	periodStart, periodEnd := now.Add(-20*24*time.Hour), now.Add(10*24*time.Hour)
	s.seedGrant("wtxn_pending", decimal.NewFromInt(30), periodStart, now.Add(-time.Hour))
	sub := s.usageSubscription("subs_threshold_pending", periodStart, periodStart, periodEnd)
	limit := decimal.NewFromInt(5)
	sub.AutoInvoiceThreshold = &limit
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	s.usage(sub, now.Add(-2*time.Hour), 20)

	item := &dto.AutoInvoiceThresholdBillingResultItem{SubscriptionID: sub.ID}
	s.Require().NoError(s.subscriptionService().processAutoInvoiceThresholdSubscription(s.GetContext(), sub, now, item))

	s.False(item.Invoiced)
	s.Empty(s.subscriptionInvoices(sub.ID))
}

// Resume after a pause pushes the period end out by the pause; the draft's end follows, so the
// period-end run still finds it.
func (s *CreditExpiryInvoiceRaceSuite) TestPeriodEnd_ResumeMovesTheDraft() {
	sub, draft := s.settledSubscription("subs_resume")
	pause := &subscription.SubscriptionPause{
		ID:             "pause_subs_resume",
		SubscriptionID: sub.ID,
		PauseStatus:    types.PauseStatusActive,
		PauseMode:      types.PauseModeImmediate,
		PauseStart:     time.Now().UTC().Add(-48 * time.Hour),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(s.GetContext(), pause))
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	sub.PauseStatus = types.PauseStatusActive
	sub.ActivePauseID = &pause.ID
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	resumed, _, err := s.subscriptionService().executeResume(s.GetContext(), sub, pause, &dto.ResumeSubscriptionRequest{
		ResumeMode: types.ResumeModeImmediate,
	})
	s.Require().NoError(err)

	s.True(resumed.CurrentPeriodEnd.After(*draft.PeriodEnd), "period end moved out by the pause")
	s.True(s.invoice(draft.ID).PeriodEnd.Equal(resumed.CurrentPeriodEnd), "draft end follows the period")
	reused, _, err := s.invoiceService.GetOrComputeCurrentPeriodDraft(s.GetContext(), resumed)
	s.Require().NoError(err)
	s.Equal(draft.ID, reused.ID, "the period-end run finds the moved draft")
	s.True(decimal.NewFromInt(20).Equal(reused.TotalPrepaidCreditsApplied))
}
