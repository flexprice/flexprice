package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// Pause and resume run the real invoice flow against the in-memory stores. Every subscription
// bills an advance fixed fee of 310, an arrear fixed fee of 62 and usage at 1 per unit, on a
// monthly period anchored at its start.
type SubscriptionPauseSuite struct {
	testutil.BaseServiceTestSuite
	params         ServiceParams
	service        SubscriptionService
	invoiceService InvoiceService

	cust   *customer.Customer
	wallet *wallet.Wallet
}

const (
	pauseAdvanceFee = 310
	pauseArrearFee  = 62
)

func TestSubscriptionPauseService(t *testing.T) {
	suite.Run(t, new(SubscriptionPauseSuite))
}

func (s *SubscriptionPauseSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *SubscriptionPauseSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.BaseServiceTestSuite.ClearStores()

	stores := s.GetStores()
	s.params = ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		RedisCache:                   s.GetRedisCache(),
		WalletRepo:                   stores.WalletRepo,
		SubRepo:                      stores.SubscriptionRepo,
		SubScheduleRepo:              stores.SubscriptionScheduleRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        stores.SubscriptionPhaseRepo,
		PlanRepo:                     stores.PlanRepo,
		PriceRepo:                    stores.PriceRepo,
		PriceUnitRepo:                stores.PriceUnitRepo,
		EventRepo:                    stores.EventRepo,
		MeterRepo:                    stores.MeterRepo,
		MeterUsageRepo:               stores.MeterUsageRepo,
		CustomerRepo:                 stores.CustomerRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		HostedInvoiceTokenRepo:       stores.HostedInvoiceTokenRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		EntitlementRepo:              stores.EntitlementRepo,
		EntitlementGrantRepo:         stores.EntitlementGrantRepo,
		EnvironmentRepo:              stores.EnvironmentRepo,
		FeatureRepo:                  stores.FeatureRepo,
		AddonRepo:                    stores.AddonRepo,
		AddonAssociationRepo:         stores.AddonAssociationRepo,
		TenantRepo:                   stores.TenantRepo,
		UserRepo:                     stores.UserRepo,
		AuthRepo:                     stores.AuthRepo,
		PaymentRepo:                  stores.PaymentRepo,
		RefundRepo:                   stores.RefundRepo,
		CheckoutSessionRepo:          stores.CheckoutSessionRepo,
		CreditNoteRepo:               stores.CreditNoteRepo,
		CreditNoteLineItemRepo:       stores.CreditNoteLineItemRepo,
		CreditGrantRepo:              stores.CreditGrantRepo,
		CreditGrantApplicationRepo:   stores.CreditGrantApplicationRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		SettingsRepo:                 stores.SettingsRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		ConnectionRepo:               stores.ConnectionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		PlanPriceSyncRepo:            stores.PlanPriceSyncRepo,
		AddonPriceSyncRepo:           stores.AddonPriceSyncRepo,
		CommitmentRepo:               stores.CommitmentRepo,
		BillingTermRepo:              stores.BillingTermRepo,
		BillingInstalmentRepo:        stores.BillingInstalmentRepo,
		FXRateRepo:                   stores.FXRateRepo,
		IntegrationFactory:           s.GetIntegrationFactory(),
		ProrationCalculator:          s.GetCalculator(),
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}
	s.service = NewSubscriptionService(s.params)
	s.invoiceService = NewInvoiceService(s.params)

	s.cust = &customer.Customer{
		ID:         "cust_pause",
		ExternalID: "ext_cust_pause",
		Name:       "Pause Customer",
		Email:      "pause@test.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(stores.CustomerRepo.Create(s.GetContext(), s.cust))

	s.wallet = &wallet.Wallet{
		ID:                  "wallet_pause",
		CustomerID:          s.cust.ID,
		Currency:            "usd",
		WalletType:          types.WalletTypePrePaid,
		Balance:             decimal.Zero,
		CreditBalance:       decimal.Zero,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(stores.WalletRepo.CreateWallet(s.GetContext(), s.wallet))
}

func (s *SubscriptionPauseSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.BaseServiceTestSuite.ClearStores()
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// openPeriod is a monthly period that started 10 days ago, anchored at its start and ending where billing ends it: a
// start on the 29th to 31st ends on the next month's last day, where AddDate would roll into the month after.
func (s *SubscriptionPauseSuite) openPeriod() (time.Time, time.Time) {
	start := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -10)
	end, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: start,
		BillingAnchor:      start,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
		Timezone:           "UTC",
	})
	s.Require().NoError(err)
	return start, end
}

// The charges a test subscription can carry.
const (
	chargeAdvance = "advance"
	chargeArrear  = "arrear"
	chargeUsage   = "usage"
)

// billedSubscription is an active standalone subscription with the advance, arrear and usage
// charges, its current period [periodStart, periodEnd) and its anchor at periodStart.
func (s *SubscriptionPauseSuite) billedSubscription(id string, periodStart, periodEnd time.Time) *subscription.Subscription {
	return s.subscriptionWithCharges(id, periodStart, periodEnd, chargeAdvance, chargeArrear, chargeUsage)
}

// subscriptionWithCharges is billedSubscription with only the listed charges.
func (s *SubscriptionPauseSuite) subscriptionWithCharges(id string, periodStart, periodEnd time.Time, charges ...string) *subscription.Subscription {
	ctx := s.GetContext()
	pl := &plan.Plan{ID: "plan_" + id, Name: "Plan " + id, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))
	m := &meter.Meter{
		ID:          "meter_" + id,
		Name:        "API calls " + id,
		EventName:   "api_calls_" + id,
		Aggregation: meter.Aggregation{Type: types.AggregationSum},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             pl.ID,
		CustomerID:         s.cust.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		PauseStatus:        types.PauseStatusNone,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		ProrationBehavior:  types.ProrationBehaviorNone,
		Timezone:           "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	fixed := func(suffix string, amount int64, cadence types.InvoiceCadence) *subscription.SubscriptionLineItem {
		p := &price.Price{
			ID:                 "price_" + suffix + "_" + id,
			Amount:             decimal.NewFromInt(amount),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     cadence,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		return &subscription.SubscriptionLineItem{
			ID:                 "subs_line_" + suffix + "_" + id,
			SubscriptionID:     sub.ID,
			CustomerID:         sub.CustomerID,
			EntityID:           pl.ID,
			EntityType:         types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:    pl.Name,
			PriceID:            p.ID,
			PriceType:          types.PRICE_TYPE_FIXED,
			DisplayName:        suffix + " fee",
			Quantity:           decimal.NewFromInt(1),
			Currency:           sub.Currency,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			InvoiceCadence:     cadence,
			StartDate:          periodStart,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}

	usagePrice := &price.Price{
		ID:                 "price_usage_" + id,
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            m.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePrice))
	usageLine := &subscription.SubscriptionLineItem{
		ID:                 "subs_line_usage_" + id,
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		EntityID:           pl.ID,
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    pl.Name,
		PriceID:            usagePrice.ID,
		PriceType:          types.PRICE_TYPE_USAGE,
		MeterID:            m.ID,
		MeterDisplayName:   m.Name,
		DisplayName:        "API calls",
		Quantity:           decimal.Zero,
		Currency:           sub.Currency,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          periodStart,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(charges))
	for _, charge := range charges {
		switch charge {
		case chargeAdvance:
			lineItems = append(lineItems, fixed(chargeAdvance, pauseAdvanceFee, types.InvoiceCadenceAdvance))
		case chargeArrear:
			lineItems = append(lineItems, fixed(chargeArrear, pauseArrearFee, types.InvoiceCadenceArrear))
		case chargeUsage:
			lineItems = append(lineItems, usageLine)
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	return sub
}

// pausedSubscription stores a subscription that has been paused since pauseStart, with the
// current period [periodStart, periodEnd) and its anchor at anchor.
func (s *SubscriptionPauseSuite) pausedSubscription(id string, anchor, pauseStart, periodStart, periodEnd time.Time) (*subscription.Subscription, *subscription.SubscriptionPause) {
	ctx := s.GetContext()
	sub := s.billedSubscription(id, periodStart, periodEnd)
	pause := &subscription.SubscriptionPause{
		ID:                  "pause_" + id,
		SubscriptionID:      sub.ID,
		PauseStatus:         types.PauseStatusActive,
		PauseMode:           types.PauseModeImmediate,
		PauseStart:          pauseStart,
		OriginalPeriodStart: anchor,
		OriginalPeriodEnd:   anchor.AddDate(0, 1, 0),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, pause))

	sub.BillingAnchor = anchor
	sub.StartDate = anchor
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	sub.PauseStatus = types.PauseStatusActive
	sub.ActivePauseID = lo.ToPtr(pause.ID)
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
	return s.subscription(id), pause
}

// usage records qty units of sub's meter at the given time.
func (s *SubscriptionPauseSuite) usage(sub *subscription.Subscription, at time.Time, qty int64) {
	ctx := s.GetContext()
	id := types.GenerateUUID()
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID:                 id,
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			EventName:          "api_calls_" + sub.ID,
			ExternalCustomerID: s.cust.ExternalID,
			CustomerID:         s.cust.ID,
			Timestamp:          at,
			IngestedAt:         at,
		},
		MeterID:    "meter_" + sub.ID,
		QtyTotal:   decimal.NewFromInt(qty),
		UniqueHash: id,
	}}))
}

func (s *SubscriptionPauseSuite) subscription(id string) *subscription.Subscription {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return sub
}

func (s *SubscriptionPauseSuite) pauseRecord(id string) *subscription.SubscriptionPause {
	pause, err := s.GetStores().SubscriptionRepo.GetPause(s.GetContext(), id)
	s.Require().NoError(err)
	return pause
}

func (s *SubscriptionPauseSuite) invoices(subID string) []*invoice.Invoice {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subID
	// Listing hides SKIPPED invoices unless asked for.
	filter.InvoiceStatus = []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
		types.InvoiceStatusVoided,
		types.InvoiceStatusSkipped,
	}
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return invoices
}

func (s *SubscriptionPauseSuite) invoicesWithReason(subID string, reason types.InvoiceBillingReason) []*invoice.Invoice {
	return lo.Filter(s.invoices(subID), func(inv *invoice.Invoice, _ int) bool {
		return inv.BillingReason == string(reason)
	})
}

func (s *SubscriptionPauseSuite) onlyInvoiceWithReason(subID string, reason types.InvoiceBillingReason) *invoice.Invoice {
	invoices := s.invoicesWithReason(subID, reason)
	s.Require().Len(invoices, 1, "invoices with reason %s", reason)
	return invoices[0]
}

// lineAmount is the amount billed for priceID on the invoice, and whether a line exists for it.
func (s *SubscriptionPauseSuite) lineAmount(invoiceID, priceID string) (decimal.Decimal, bool) {
	lines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	total := decimal.Zero
	found := false
	for _, line := range lines {
		if lo.FromPtr(line.PriceID) == priceID {
			total = total.Add(line.Amount)
			found = true
		}
	}
	return total, found
}

func (s *SubscriptionPauseSuite) invoiceLines(invoiceID string) []*invoice.InvoiceLineItem {
	lines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	return lines
}

// prorationCredits are the wallet top-ups issued for unused advance charges.
func (s *SubscriptionPauseSuite) prorationCredits() []*wallet.Transaction {
	filter := types.NewNoLimitWalletTransactionFilter()
	filter.WalletID = &s.wallet.ID
	filter.TransactionReason = lo.ToPtr(types.TransactionReasonSubscriptionCredit)
	txs, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), filter)
	s.Require().NoError(err)
	return txs
}

// fraction is the share of [start, end) covered by [from, to).
func fraction(from, to, start, end time.Time) float64 {
	return to.Sub(from).Seconds() / end.Sub(start).Seconds()
}

// webhookCount is how many events named eventName were published for the subscription.
func (s *SubscriptionPauseSuite) webhookCount(subID string, eventName types.WebhookEventName) int {
	events := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events()
	return lo.CountBy(events, func(event *types.WebhookEvent) bool {
		return event.EventName == eventName && event.EntityID == subID
	})
}

func (s *SubscriptionPauseSuite) assertValidationError(err error, message string) {
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
	s.True(strings.Contains(err.Error(), message), "error %q does not mention %q", err.Error(), message)
}

func (s *SubscriptionPauseSuite) pause(subID string, behavior types.ProrationBehavior) *dto.PauseSubscriptionResponse {
	resp, err := s.service.PauseSubscription(s.GetContext(), subID, &dto.PauseSubscriptionRequest{
		ProrationBehavior: behavior,
		Reason:            "customer hold",
		Metadata:          map[string]string{"requested_by": "support"},
	})
	s.Require().NoError(err)
	return resp
}

// cancellationSchedule is the subscription's pending cancellation schedule, or nil.
func (s *SubscriptionPauseSuite) cancellationSchedule(subID string) *subscription.SubscriptionSchedule {
	schedule, err := s.GetStores().SubscriptionScheduleRepo.GetPendingBySubscriptionAndType(s.GetContext(), subID, types.SubscriptionScheduleChangeTypeCancellation)
	s.Require().NoError(err)
	return schedule
}

// backdatePause moves a stored pause's start to start.
func (s *SubscriptionPauseSuite) backdatePause(pauseID string, start time.Time) {
	pause := s.pauseRecord(pauseID)
	pause.PauseStart = start
	s.NoError(s.GetStores().SubscriptionRepo.UpdatePause(s.GetContext(), pause))
}

// closedPeriod is a monthly period that ended at least a day ago, anchored at its start.
func (s *SubscriptionPauseSuite) closedPeriod() (time.Time, time.Time) {
	start := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -32)
	end, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: start,
		BillingAnchor:      start,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
		Timezone:           "UTC",
	})
	s.Require().NoError(err)
	return start, end
}

// completedPause stores a pause of sub from from to to that was resumed at to.
func (s *SubscriptionPauseSuite) completedPause(sub *subscription.Subscription, id string, from, to time.Time, behavior types.ProrationBehavior) {
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, &subscription.SubscriptionPause{
		ID:                  id,
		SubscriptionID:      sub.ID,
		PauseStatus:         types.PauseStatusCompleted,
		PauseMode:           types.PauseModeImmediate,
		ResumeMode:          types.ResumeModeImmediate,
		ProrationBehavior:   behavior,
		PauseStart:          from,
		ResumedAt:           lo.ToPtr(to),
		OriginalPeriodStart: sub.CurrentPeriodStart,
		OriginalPeriodEnd:   sub.CurrentPeriodEnd,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}))
}

// billPeriod runs the period-end billing for [start, end): the cycle draft is created, computed and,
// unless SKIPPED, finalized.
func (s *SubscriptionPauseSuite) billPeriod(subID string, start, end time.Time) *invoice.Invoice {
	draft, err := s.service.CreateDraftInvoiceForSubscription(s.GetContext(), subID, dto.Period{Start: start, End: end})
	s.Require().NoError(err)
	_, skipped, err := s.invoiceService.ComputeInvoice(s.GetContext(), draft.ID, nil)
	s.Require().NoError(err)
	if !skipped {
		s.Require().NoError(s.invoiceService.FinalizeInvoice(s.GetContext(), draft.ID, dto.FinalizeInvoiceRequest{}))
	}
	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), draft.ID)
	s.Require().NoError(err)
	return inv
}

// pauseCredits are the wallet credits given for the prorated paused time of sub's period starting at periodStart.
func (s *SubscriptionPauseSuite) pauseCredits(subID string, periodStart time.Time) []*wallet.Transaction {
	key := idempotency.NewGenerator().GenerateKey(idempotency.ScopePauseProratedCredit, map[string]interface{}{
		"subscription_id": subID,
		"period_start":    periodStart.UTC().Format(time.RFC3339Nano),
	})
	return lo.Filter(s.prorationCredits(), func(tx *wallet.Transaction, _ int) bool {
		return tx.IdempotencyKey == key
	})
}

// ---------------------------------------------------------------------------
// Pause
// ---------------------------------------------------------------------------

// Without pause_grace_period_days in the request or the setting, the pause schedules the cancellation
// at the period end. No invoice is raised: the period keeps its one cycle draft, and nothing is credited.
func (s *SubscriptionPauseSuite) TestPause_SchedulesAutoCancelAtPeriodEnd() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_pause_period_end", periodStart, periodEnd)

	resp := s.pause(sub.ID, "")

	s.Require().NotNil(resp.Pause)
	s.Require().NotNil(resp.Subscription)

	paused := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusPaused, paused.SubscriptionStatus)
	s.Equal(types.PauseStatusActive, paused.PauseStatus)
	s.Equal(resp.Pause.ID, lo.FromPtr(paused.ActivePauseID))
	s.True(paused.CurrentPeriodStart.Equal(periodStart), "period start unchanged")
	s.True(paused.CurrentPeriodEnd.Equal(periodEnd), "period end unchanged")
	s.True(paused.BillingAnchor.Equal(periodStart), "anchor unchanged")
	s.Require().NotNil(paused.CancelAt)
	s.True(paused.CancelAt.Equal(periodEnd), "cancels at the period end")
	s.Require().NotNil(paused.EndDate)
	s.True(paused.EndDate.Equal(periodEnd))
	s.True(paused.CancelAtPeriodEnd)
	s.Equal(types.CancellationReasonPauseExpired, paused.Metadata["cancellation_reason"])
	s.Equal(types.ProrationBehaviorNone, paused.ProrationBehavior, "the pause request is not saved on the subscription")

	schedule := s.cancellationSchedule(sub.ID)
	s.Require().NotNil(schedule, "the auto-cancel is a pending cancellation schedule")
	s.True(schedule.ScheduledAt.Equal(periodEnd))

	pause := s.pauseRecord(resp.Pause.ID)
	s.Equal(types.PauseStatusActive, pause.PauseStatus)
	s.Equal(types.PauseModeImmediate, pause.PauseMode)
	s.Equal(types.ProrationBehaviorNone, pause.ProrationBehavior, "omitted proration_behavior is none")
	s.True(pause.OriginalPeriodStart.Equal(periodStart))
	s.True(pause.OriginalPeriodEnd.Equal(periodEnd))
	s.Equal("customer hold", pause.Reason)
	s.Equal("support", pause.Metadata["requested_by"])
	s.Nil(pause.ResumedAt)

	invoices := s.invoices(sub.ID)
	s.Require().Len(invoices, 1, "only the period's cycle draft")
	s.Equal(types.InvoiceStatusDraft, invoices[0].InvoiceStatus)
	s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), invoices[0].BillingReason)
	s.True(invoices[0].PeriodStart.Equal(periodStart))
	s.True(invoices[0].PeriodEnd.Equal(periodEnd))
	s.Empty(s.prorationCredits(), "nothing is credited before the period closes")
}

// pauseGracePeriodSetting stores subscription_config with pause_grace_period_days set to days, or removes it when nil.
func (s *SubscriptionPauseSuite) pauseGracePeriodSetting(days *int) {
	ctx := s.GetContext()
	_ = s.GetStores().SettingsRepo.DeleteByKey(ctx, types.SettingKeySubscriptionConfig)
	if days == nil {
		return
	}
	value, err := utils.ToMap(types.SubscriptionConfig{GracePeriodDays: 3, PauseGracePeriodDays: days})
	s.Require().NoError(err)
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeySubscriptionConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

// The auto-cancel falls pause_grace_period_days after the pause: from the request, else from the
// subscription_config setting capped at the period end, else at the period end. Before the period end,
// the period and its open draft end at the cancel.
func (s *SubscriptionPauseSuite) TestPause_GracePeriodSetsTheCancel() {
	periodStart, periodEnd := s.openPeriod()
	tests := []struct {
		name        string
		requestDays *int
		settingDays *int
		// wantDays is the grace period in days after the pause; nil cancels at the period end.
		wantDays *int
	}{
		{name: "request", requestDays: lo.ToPtr(5), wantDays: lo.ToPtr(5)},
		{name: "setting", settingDays: lo.ToPtr(7), wantDays: lo.ToPtr(7)},
		{name: "request over setting", requestDays: lo.ToPtr(5), settingDays: lo.ToPtr(7), wantDays: lo.ToPtr(5)},
		{name: "setting past the period end", settingDays: lo.ToPtr(60)},
		{name: "neither"},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			s.pauseGracePeriodSetting(tt.settingDays)
			defer s.pauseGracePeriodSetting(nil)
			sub := s.billedSubscription("subs_pause_grace_"+string(rune('a'+i)), periodStart, periodEnd)
			draft, _, err := s.invoiceService.GetOrComputeCurrentPeriodDraft(s.GetContext(), sub)
			s.Require().NoError(err)
			s.Require().NotNil(draft)

			resp, err := s.service.PauseSubscription(s.GetContext(), sub.ID, &dto.PauseSubscriptionRequest{
				ProrationBehavior:    types.ProrationBehaviorCreateProrations,
				PauseGracePeriodDays: tt.requestDays,
			})
			s.Require().NoError(err)
			cancelAt := periodEnd
			if tt.wantDays != nil {
				cancelAt = resp.Pause.PauseStart.AddDate(0, 0, *tt.wantDays)
			}

			paused := s.subscription(sub.ID)
			s.Require().NotNil(paused.CancelAt)
			s.True(paused.CancelAt.Equal(cancelAt), "cancel at %s, want %s", paused.CancelAt, cancelAt)
			s.True(paused.EndDate.Equal(cancelAt))
			s.True(paused.CurrentPeriodEnd.Equal(cancelAt), "period ends at the cancel")
			s.True(paused.BillingAnchor.Equal(periodStart), "anchor unchanged")
			s.True(s.cancellationSchedule(sub.ID).ScheduledAt.Equal(cancelAt))

			pause := s.pauseRecord(resp.Pause.ID)
			s.Equal(types.ProrationBehaviorCreateProrations, pause.ProrationBehavior)
			s.True(pause.OriginalPeriodEnd.Equal(periodEnd), "the pause keeps the period's original end")

			invoices := s.invoices(sub.ID)
			s.Require().Len(invoices, 1, "the open draft is kept, not replaced")
			s.Equal(draft.ID, invoices[0].ID)
			s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), invoices[0].BillingReason)
			s.True(invoices[0].PeriodEnd.Equal(cancelAt), "draft ends at the cancel")
			s.Empty(s.pauseCredits(sub.ID, periodStart), "nothing is credited before the period closes")
		})
	}
}

// A parent's pause is mirrored on its inherited children.
func (s *SubscriptionPauseSuite) TestPause_ParentCascadesToInheritedChildren() {
	periodStart, periodEnd := s.openPeriod()
	parent := s.billedSubscription("subs_pause_parent", periodStart, periodEnd)
	parent.SubscriptionType = types.SubscriptionTypeParent
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), parent))
	child := s.inheritedChild("subs_pause_child", parent)

	s.pause(parent.ID, types.ProrationBehaviorNone)

	pausedChild := s.subscription(child.ID)
	s.Equal(types.SubscriptionStatusPaused, pausedChild.SubscriptionStatus)
	s.Equal(types.PauseStatusActive, pausedChild.PauseStatus)
}

func (s *SubscriptionPauseSuite) inheritedChild(id string, parent *subscription.Subscription) *subscription.Subscription {
	child := &subscription.Subscription{
		ID:                   id,
		PlanID:               parent.PlanID,
		CustomerID:           s.cust.ID,
		ParentSubscriptionID: lo.ToPtr(parent.ID),
		SubscriptionStatus:   parent.SubscriptionStatus,
		SubscriptionType:     types.SubscriptionTypeInherited,
		PauseStatus:          parent.PauseStatus,
		Currency:             parent.Currency,
		BillingPeriod:        parent.BillingPeriod,
		BillingPeriodCount:   parent.BillingPeriodCount,
		StartDate:            parent.StartDate,
		BillingAnchor:        parent.BillingAnchor,
		CurrentPeriodStart:   parent.CurrentPeriodStart,
		CurrentPeriodEnd:     parent.CurrentPeriodEnd,
		ProrationBehavior:    types.ProrationBehaviorNone,
		Timezone:             "UTC",
		BaseModel:            types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), child))
	return child
}

// Every refused pause returns a validation error and leaves the subscription and its invoices untouched.
func (s *SubscriptionPauseSuite) TestPause_Refused() {
	now := time.Now().UTC().Truncate(time.Second)
	periodStart, periodEnd := s.openPeriod()

	tests := []struct {
		name    string
		prepare func(sub *subscription.Subscription)
		req     *dto.PauseSubscriptionRequest
		message string
	}{
		{
			name:    "trialing",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionStatus = types.SubscriptionStatusTrialing },
			message: "only an active subscription can be paused",
		},
		{
			name:    "draft",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionStatus = types.SubscriptionStatusDraft },
			message: "only an active subscription can be paused",
		},
		{
			name:    "incomplete",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionStatus = types.SubscriptionStatusIncomplete },
			message: "only an active subscription can be paused",
		},
		{
			name:    "already paused",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionStatus = types.SubscriptionStatusPaused },
			message: "only an active subscription can be paused",
		},
		{
			name:    "cancelled",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionStatus = types.SubscriptionStatusCancelled },
			message: "only an active subscription can be paused",
		},
		{
			name:    "inherited child",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionType = types.SubscriptionTypeInherited },
			message: "a child subscription cannot be paused on its own",
		},
		{
			name:    "grouped invoicing child",
			prepare: func(sub *subscription.Subscription) { sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing },
			message: "a child subscription cannot be paused on its own",
		},
		{
			name:    "scheduled cancellation",
			prepare: func(sub *subscription.Subscription) { sub.CancelAt = lo.ToPtr(periodEnd) },
			message: "subscription is scheduled to cancel",
		},
		{
			name:    "cancellation at period end",
			prepare: func(sub *subscription.Subscription) { sub.CancelAtPeriodEnd = true },
			message: "subscription is scheduled to cancel",
		},
		{
			name: "period not started",
			prepare: func(sub *subscription.Subscription) {
				sub.CurrentPeriodStart = now.Add(24 * time.Hour)
				sub.CurrentPeriodEnd = now.Add(24*time.Hour).AddDate(0, 1, 0)
			},
			message: "subscription is outside its current billing period",
		},
		{
			name: "period ended and not rolled",
			prepare: func(sub *subscription.Subscription) {
				sub.CurrentPeriodStart = now.AddDate(0, -1, -1)
				sub.CurrentPeriodEnd = now.Add(-24 * time.Hour)
			},
			message: "subscription is outside its current billing period",
		},
		{
			name: "stripe managed",
			prepare: func(sub *subscription.Subscription) {
				s.NoError(s.GetStores().EntityIntegrationMappingRepo.Create(s.GetContext(), &entityintegrationmapping.EntityIntegrationMapping{
					ID:               "eim_" + sub.ID,
					EntityID:         sub.ID,
					EntityType:       types.IntegrationEntityTypeSubscription,
					ProviderType:     string(types.SecretProviderStripe),
					ProviderEntityID: "sub_stripe_" + sub.ID,
					EnvironmentID:    types.GetEnvironmentID(s.GetContext()),
					BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
				}))
			},
			message: "a Stripe-managed subscription cannot be paused in Flexprice",
		},
		{
			name:    "grace period past the period end",
			req:     &dto.PauseSubscriptionRequest{PauseGracePeriodDays: lo.ToPtr(40)},
			message: "pause_grace_period_days goes past the current period end",
		},
		{
			name:    "grace period below one day",
			req:     &dto.PauseSubscriptionRequest{PauseGracePeriodDays: lo.ToPtr(0)},
			message: "",
		},
		{
			name:    "invalid proration behavior",
			req:     &dto.PauseSubscriptionRequest{ProrationBehavior: "always"},
			message: "",
		},
		{
			name:    "reason too long",
			req:     &dto.PauseSubscriptionRequest{Reason: strings.Repeat("a", 256)},
			message: "",
		},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			id := "subs_pause_refused_" + string(rune('a'+i))
			sub := s.billedSubscription(id, periodStart, periodEnd)
			before := s.subscription(id)
			if tt.prepare != nil {
				tt.prepare(before)
				s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), before))
			}
			req := tt.req
			if req == nil {
				req = &dto.PauseSubscriptionRequest{}
			}

			_, err := s.service.PauseSubscription(s.GetContext(), sub.ID, req)

			s.assertValidationError(err, tt.message)
			after := s.subscription(id)
			s.Equal(before.SubscriptionStatus, after.SubscriptionStatus, "status unchanged")
			s.Nil(after.ActivePauseID, "no pause recorded")
			s.Equal(before.CurrentPeriodEnd, after.CurrentPeriodEnd, "period end unchanged")
			s.Nil(s.cancellationSchedule(id), "no auto-cancel scheduled")
			s.Empty(s.invoices(id), "nothing billed")
			s.Empty(s.prorationCredits(), "nothing credited")
		})
	}
}

func (s *SubscriptionPauseSuite) TestPause_UnknownSubscriptionIsNotFound() {
	_, err := s.service.PauseSubscription(s.GetContext(), "subs_missing", &dto.PauseSubscriptionRequest{})
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "expected not found, got %v", err)
}

// A pause with nothing to bill so far still pauses, and nothing is finalized.
func (s *SubscriptionPauseSuite) TestPause_NothingToBillStillPauses() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.subscriptionWithCharges("subs_pause_nothing", periodStart, periodEnd, chargeAdvance)

	s.pause(sub.ID, types.ProrationBehaviorNone)

	s.Empty(lo.Filter(s.invoices(sub.ID), func(inv *invoice.Invoice, _ int) bool {
		return inv.InvoiceStatus == types.InvoiceStatusFinalized
	}), "nothing is billed")
	s.Equal(types.SubscriptionStatusPaused, s.subscription(sub.ID).SubscriptionStatus)
}

// ---------------------------------------------------------------------------
// While paused
// ---------------------------------------------------------------------------

// Recomputing a paused-period invoice keeps it SKIPPED with nothing on it.
func (s *SubscriptionPauseSuite) TestPausedPeriod_RecomputeKeepsItSkipped() {
	anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -2, 0)
	periodStart := anchor.AddDate(0, 1, 0)
	periodEnd := anchor.AddDate(0, 2, 0)
	sub, _ := s.pausedSubscription("subs_paused_recompute", anchor, anchor.AddDate(0, 0, 10), periodStart, periodEnd)
	s.usage(sub, periodStart.Add(24*time.Hour), 40)

	inv, err := s.invoiceService.CreatePausedPeriodInvoice(s.GetContext(), sub, periodStart, periodEnd)
	s.Require().NoError(err)

	recomputed, skipped, err := s.invoiceService.ComputeInvoice(s.GetContext(), inv.ID, nil)
	s.Require().NoError(err)
	s.True(skipped)
	s.Equal(types.InvoiceStatusSkipped, recomputed.InvoiceStatus)
	s.True(recomputed.Subtotal.IsZero())
	s.Empty(s.invoiceLines(inv.ID))
}

// A paused subscription's billing draft is the period's ordinary cycle draft.
func (s *SubscriptionPauseSuite) TestPausedPeriod_BillingDraftIsTheCycleDraft() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_paused_draft", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)
	paused := s.subscription(sub.ID)

	draft, err := s.invoiceService.CreateDraftInvoiceForSubscription(s.GetContext(), dto.CreateSubscriptionDraftInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    paused.CurrentPeriodStart,
		PeriodEnd:      paused.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.Require().NoError(err)
	s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), draft.BillingReason)
	s.Len(s.invoices(sub.ID), 1, "the pause's draft is reused, not duplicated")
}

// ---------------------------------------------------------------------------
// Resume
// ---------------------------------------------------------------------------

// Resume reverts the pause's auto-cancel: cancel_at, end_date, the period end and the draft's end
// come back, and no invoice is raised.
func (s *SubscriptionPauseSuite) TestResume_RevertsTheAutoCancelAndRaisesNoInvoice() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_resume_revert", periodStart, periodEnd)
	paused, err := s.service.PauseSubscription(s.GetContext(), sub.ID, &dto.PauseSubscriptionRequest{
		PauseGracePeriodDays: lo.ToPtr(5),
		Metadata:             map[string]string{"requested_by": "support"},
	})
	s.Require().NoError(err)
	schedule := s.cancellationSchedule(sub.ID)
	s.Require().NotNil(schedule)

	resp, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{
		Metadata: map[string]string{"resumed_by": "support"},
	})
	s.Require().NoError(err)
	resumedAt := lo.FromPtr(resp.Pause.ResumedAt)

	resumed := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusActive, resumed.SubscriptionStatus)
	s.Equal(types.PauseStatusNone, resumed.PauseStatus)
	s.Nil(resumed.ActivePauseID)
	s.Nil(resumed.CancelAt, "auto-cancel removed")
	s.Nil(resumed.EndDate)
	s.False(resumed.CancelAtPeriodEnd)
	s.True(resumed.CurrentPeriodStart.Equal(periodStart), "period start unchanged")
	s.True(resumed.CurrentPeriodEnd.Equal(periodEnd), "period end restored")
	s.True(resumed.BillingAnchor.Equal(periodStart), "anchor unchanged")
	s.Equal(types.ProrationBehaviorNone, resumed.ProrationBehavior)
	for _, key := range []string{"cancellation_type", "cancellation_reason", "effective_date"} {
		s.NotContains(resumed.Metadata, key, "the auto-cancel's metadata is removed")
	}

	s.Nil(s.cancellationSchedule(sub.ID), "no pending cancellation")
	cancelledSchedule, err := s.GetStores().SubscriptionScheduleRepo.Get(s.GetContext(), schedule.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusCancelled, cancelledSchedule.Status)

	invoices := s.invoices(sub.ID)
	s.Require().Len(invoices, 1, "only the period's cycle draft")
	s.Equal(types.InvoiceStatusDraft, invoices[0].InvoiceStatus)
	s.True(invoices[0].PeriodEnd.Equal(periodEnd), "draft end restored")
	s.Empty(s.prorationCredits())

	pause := s.pauseRecord(paused.Pause.ID)
	s.Equal(types.PauseStatusCompleted, pause.PauseStatus)
	s.Equal(types.ResumeModeImmediate, pause.ResumeMode)
	s.Require().NotNil(pause.ResumedAt)
	s.True(pause.ResumedAt.Equal(resumedAt))
	s.Equal("support", pause.Metadata["requested_by"], "pause metadata kept")
	s.Equal("support", pause.Metadata["resumed_by"], "resume metadata merged")
}

// cachedSubscriptionRepo serves Get from copies taken before a write, like the Redis cache does
// until the transaction commits. GetForUpdate still reads the row.
type cachedSubscriptionRepo struct {
	subscription.Repository
	cached map[string]*subscription.Subscription
}

func (r *cachedSubscriptionRepo) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	if cached, ok := r.cached[id]; ok {
		copied := *cached
		return &copied, nil
	}
	return r.Repository.Get(ctx, id)
}

// A paused subscription read before the resume stays cached while the resume runs; the resume must
// not write that copy back over the resumed fields.
func (s *SubscriptionPauseSuite) TestResume_StaleCachedSubscriptionDoesNotUndoTheResume() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_resume_cached", periodStart, periodEnd)
	paused := s.pause(sub.ID, types.ProrationBehaviorNone)

	params := s.params
	params.SubRepo = &cachedSubscriptionRepo{
		Repository: s.params.SubRepo,
		cached:     map[string]*subscription.Subscription{sub.ID: s.subscription(sub.ID)},
	}
	resp, err := NewSubscriptionService(params).ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	for name, got := range map[string]*subscription.Subscription{
		"stored":   s.subscription(sub.ID),
		"response": resp.Subscription.Subscription,
	} {
		s.Equal(types.SubscriptionStatusActive, got.SubscriptionStatus, name)
		s.Equal(types.PauseStatusNone, got.PauseStatus, name)
		s.Nil(got.ActivePauseID, name)
		s.Nil(got.CancelAt, name)
		s.True(got.CurrentPeriodStart.Equal(periodStart), name)
		s.True(got.CurrentPeriodEnd.Equal(periodEnd), name)
	}
	s.Equal(types.PauseStatusCompleted, s.pauseRecord(paused.Pause.ID).PauseStatus)
}

// A parent's resume reaches its paused inherited children.
func (s *SubscriptionPauseSuite) TestResume_ParentCascadesToPausedInheritedChildren() {
	periodStart, periodEnd := s.openPeriod()
	parent := s.billedSubscription("subs_resume_parent", periodStart, periodEnd)
	parent.SubscriptionType = types.SubscriptionTypeParent
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), parent))
	child := s.inheritedChild("subs_resume_child", parent)
	s.pause(parent.ID, types.ProrationBehaviorNone)
	s.Require().Equal(types.SubscriptionStatusPaused, s.subscription(child.ID).SubscriptionStatus)

	_, err := s.service.ResumeSubscription(s.GetContext(), parent.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	resumedParent := s.subscription(parent.ID)
	resumedChild := s.subscription(child.ID)
	s.Equal(types.SubscriptionStatusActive, resumedChild.SubscriptionStatus)
	s.Equal(types.PauseStatusNone, resumedChild.PauseStatus)
	s.True(resumedChild.CurrentPeriodStart.Equal(resumedParent.CurrentPeriodStart))
	s.True(resumedChild.CurrentPeriodEnd.Equal(resumedParent.CurrentPeriodEnd))
}

func (s *SubscriptionPauseSuite) TestResume_Refused() {
	periodStart, periodEnd := s.openPeriod()

	s.Run("not paused", func() {
		sub := s.billedSubscription("subs_resume_active", periodStart, periodEnd)
		_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
		s.assertValidationError(err, "subscription is not paused")
		s.Empty(s.invoices(sub.ID))
	})

	s.Run("paused without a pause record", func() {
		sub := s.billedSubscription("subs_resume_stripe_paused", periodStart, periodEnd)
		sub.SubscriptionStatus = types.SubscriptionStatusPaused
		s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
		_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
		s.assertValidationError(err, "subscription is not paused")
	})

	s.Run("past its end date", func() {
		anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -1, 0)
		sub, pause := s.pausedSubscription("subs_resume_ended", anchor, anchor.AddDate(0, 0, 5), anchor.AddDate(0, 0, 5), anchor.AddDate(0, 1, 0))
		sub.EndDate = lo.ToPtr(time.Now().UTC().Add(-time.Hour))
		s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
		_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
		s.assertValidationError(err, "subscription has reached its end date")
		s.Equal(types.SubscriptionStatusPaused, s.subscription(sub.ID).SubscriptionStatus)
		s.Equal(types.PauseStatusActive, s.pauseRecord(pause.ID).PauseStatus)
		s.Empty(s.invoices(sub.ID))
	})
}

// A resume with nothing to bill still resumes, and nothing is finalized.
func (s *SubscriptionPauseSuite) TestResume_NothingToBillStillResumes() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.subscriptionWithCharges("subs_resume_nothing", periodStart, periodEnd, chargeArrear, chargeUsage)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	s.Empty(lo.Filter(s.invoices(sub.ID), func(inv *invoice.Invoice, _ int) bool {
		return inv.InvoiceStatus == types.InvoiceStatusFinalized
	}), "nothing is billed")
	s.Equal(types.SubscriptionStatusActive, s.subscription(sub.ID).SubscriptionStatus)
}

// A subscription can be paused again after a resume: each pause is its own record with its own auto-cancel.
func (s *SubscriptionPauseSuite) TestPause_AgainAfterResume() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_pause_twice", periodStart, periodEnd)
	first := s.pause(sub.ID, types.ProrationBehaviorNone)
	firstSchedule := s.cancellationSchedule(sub.ID)
	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	second := s.pause(sub.ID, types.ProrationBehaviorCreateProrations)

	s.NotEqual(first.Pause.ID, second.Pause.ID)
	s.Equal(types.PauseStatusCompleted, s.pauseRecord(first.Pause.ID).PauseStatus)
	s.Equal(types.PauseStatusActive, s.pauseRecord(second.Pause.ID).PauseStatus)
	s.Equal(types.ProrationBehaviorCreateProrations, s.pauseRecord(second.Pause.ID).ProrationBehavior)
	paused := s.subscription(sub.ID)
	s.Equal(second.Pause.ID, lo.FromPtr(paused.ActivePauseID))
	s.True(paused.BillingAnchor.Equal(periodStart), "anchor unchanged across both pauses")
	s.True(paused.CancelAt.Equal(periodEnd))

	secondSchedule := s.cancellationSchedule(sub.ID)
	s.Require().NotNil(secondSchedule)
	s.NotEqual(firstSchedule.ID, secondSchedule.ID, "the second pause has its own auto-cancel")
	s.Len(s.invoices(sub.ID), 1, "still one invoice for the period")
}

// Pause and resume each publish subscription.updated and their own event once.
func (s *SubscriptionPauseSuite) TestPauseAndResume_PublishWebhooks() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_pause_webhooks", periodStart, periodEnd)
	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()

	s.pause(sub.ID, types.ProrationBehaviorNone)
	s.Equal(1, s.webhookCount(sub.ID, types.WebhookEventSubscriptionPaused))
	s.Equal(0, s.webhookCount(sub.ID, types.WebhookEventSubscriptionResumed))

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)
	s.Equal(1, s.webhookCount(sub.ID, types.WebhookEventSubscriptionPaused))
	s.Equal(1, s.webhookCount(sub.ID, types.WebhookEventSubscriptionResumed))
	s.Equal(2, s.webhookCount(sub.ID, types.WebhookEventSubscriptionUpdated))
}

// A refused pause or resume publishes nothing.
func (s *SubscriptionPauseSuite) TestRefused_PublishesNoWebhooks() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_refused_webhooks", periodStart, periodEnd)
	sub.CancelAtPeriodEnd = true
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()

	_, err := s.service.PauseSubscription(s.GetContext(), sub.ID, &dto.PauseSubscriptionRequest{})
	s.Require().Error(err)
	_, err = s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().Error(err)

	s.Empty(s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

// The schedules API cannot remove a paused subscription's auto-cancel; resume does that.
func (s *SubscriptionPauseSuite) TestCancelScheduleWhilePaused_Refused() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_schedule_paused", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)
	schedule := s.cancellationSchedule(sub.ID)
	s.Require().NotNil(schedule)

	err := NewSubscriptionScheduleService(s.params, nil).Cancel(s.GetContext(), schedule.ID)

	s.assertValidationError(err, "the cancellation of a paused subscription cannot be cancelled")
	s.NotNil(s.cancellationSchedule(sub.ID), "the auto-cancel is still pending")
	stillPaused := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusPaused, stillPaused.SubscriptionStatus)
	s.True(stillPaused.CancelAt.Equal(periodEnd))
}

// ---------------------------------------------------------------------------
// Period close: one invoice per period
// ---------------------------------------------------------------------------

// The period-end run bills one invoice for the period. Usage is billed for active time only, the
// arrear fee drops the days of create_prorations pauses, and those days of the advance fee are
// credited once. Days of none pauses stay billed.
func (s *SubscriptionPauseSuite) TestClosedPeriod_OneInvoiceBillsActiveTimeOnly() {
	periodStart, periodEnd := s.closedPeriod()
	day := func(n int) time.Time { return periodStart.AddDate(0, 0, n) }
	period := periodEnd.Sub(periodStart)

	type pauseSpec struct {
		from, to int
		behavior types.ProrationBehavior
	}
	// Pause A covers the usage on day 6, pause B the usage on day 17.
	pauseA := func(behavior types.ProrationBehavior) pauseSpec { return pauseSpec{2, 12, behavior} }
	pauseB := func(behavior types.ProrationBehavior) pauseSpec { return pauseSpec{15, 20, behavior} }

	tests := []struct {
		name            string
		pauses          []pauseSpec
		wantUsage       int64
		proratedPausedD int
	}{
		{name: "no pause", wantUsage: 11111},
		{name: "one pause with none", pauses: []pauseSpec{pauseA(types.ProrationBehaviorNone)}, wantUsage: 11101},
		{name: "one pause with create_prorations", pauses: []pauseSpec{pauseA(types.ProrationBehaviorCreateProrations)}, wantUsage: 11101, proratedPausedD: 10},
		{
			name:            "two pauses with create_prorations",
			pauses:          []pauseSpec{pauseA(types.ProrationBehaviorCreateProrations), pauseB(types.ProrationBehaviorCreateProrations)},
			wantUsage:       10101,
			proratedPausedD: 15,
		},
		{
			name:            "create_prorations then none",
			pauses:          []pauseSpec{pauseA(types.ProrationBehaviorCreateProrations), pauseB(types.ProrationBehaviorNone)},
			wantUsage:       10101,
			proratedPausedD: 10,
		},
		{
			name:            "none then create_prorations",
			pauses:          []pauseSpec{pauseA(types.ProrationBehaviorNone), pauseB(types.ProrationBehaviorCreateProrations)},
			wantUsage:       10101,
			proratedPausedD: 5,
		},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			sub := s.billedSubscription("subs_closed_period_"+string(rune('a'+i)), periodStart, periodEnd)
			for j, p := range tt.pauses {
				s.completedPause(sub, sub.ID+"_pause_"+string(rune('a'+j)), day(p.from), day(p.to), p.behavior)
			}
			for n, qty := range map[int]int64{1: 1, 6: 10, 13: 100, 17: 1000, 25: 10000} {
				s.usage(sub, day(n), qty)
			}

			inv := s.billPeriod(sub.ID, periodStart, periodEnd)

			s.Require().Len(s.invoices(sub.ID), 1, "one invoice for the period")
			s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
			s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), inv.BillingReason)

			usage, ok := s.lineAmount(inv.ID, "price_usage_"+sub.ID)
			s.Require().True(ok)
			s.True(decimal.NewFromInt(tt.wantUsage).Equal(usage), "usage %s, want %d", usage, tt.wantUsage)

			proratedShare := (time.Duration(tt.proratedPausedD) * 24 * time.Hour).Seconds() / period.Seconds()
			arrear, ok := s.lineAmount(inv.ID, "price_arrear_"+sub.ID)
			s.Require().True(ok)
			s.InDelta(pauseArrearFee*(1-proratedShare), arrear.InexactFloat64(), 0.01, "arrear %s", arrear)

			advance, ok := s.lineAmount(inv.ID, "price_advance_"+sub.ID)
			s.Require().True(ok, "the next period's advance fee is billed")
			s.True(decimal.NewFromInt(pauseAdvanceFee).Equal(advance), "advance %s", advance)

			credits := s.pauseCredits(sub.ID, periodStart)
			if tt.proratedPausedD == 0 {
				s.Empty(credits, "no credit without create_prorations days")
				return
			}
			s.Require().Len(credits, 1, "one credit for the period")
			s.InDelta(pauseAdvanceFee*proratedShare, credits[0].CreditAmount.InexactFloat64(), 0.01, "credit %s", credits[0].CreditAmount)
		})
	}
}

// annual switches a stored subscription, its prices and line items to a yearly period [start, end).
func (s *SubscriptionPauseSuite) annual(sub *subscription.Subscription, start, end time.Time) *subscription.Subscription {
	ctx := s.GetContext()
	for _, item := range sub.LineItems {
		p, err := s.GetStores().PriceRepo.Get(ctx, item.PriceID)
		s.Require().NoError(err)
		p.BillingPeriod = types.BILLING_PERIOD_ANNUAL
		s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
		item.BillingPeriod = types.BILLING_PERIOD_ANNUAL
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, item))
	}
	sub.BillingPeriod = types.BILLING_PERIOD_ANNUAL
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = start, end
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
	return s.subscription(sub.ID)
}

// On a yearly plan billed in advance, the year's invoice drops the create_prorations paused days from
// the arrear fee and credits them from the year's advance fee, while none paused days stay billed.
func (s *SubscriptionPauseSuite) TestClosedPeriod_AnnualAdvanceFeeCreditsProratedPausedDays() {
	periodStart := time.Now().UTC().Truncate(time.Second).AddDate(-1, 0, -10)
	periodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: periodStart,
		BillingAnchor:      periodStart,
		Unit:               1,
		Period:             types.BILLING_PERIOD_ANNUAL,
		Timezone:           "UTC",
	})
	s.Require().NoError(err)
	sub := s.billedSubscription("subs_closed_annual", periodStart, periodEnd)
	sub = s.annual(sub, periodStart, periodEnd)
	s.completedPause(sub, "pause_annual_prorated", periodStart.AddDate(0, 2, 0), periodStart.AddDate(0, 2, 30), types.ProrationBehaviorCreateProrations)
	s.completedPause(sub, "pause_annual_none", periodStart.AddDate(0, 6, 0), periodStart.AddDate(0, 6, 20), types.ProrationBehaviorNone)
	s.usage(sub, periodStart.AddDate(0, 1, 0), 1)
	s.usage(sub, periodStart.AddDate(0, 2, 10), 10)
	s.usage(sub, periodStart.AddDate(0, 6, 10), 100)

	inv := s.billPeriod(sub.ID, periodStart, periodEnd)

	s.Require().Len(s.invoices(sub.ID), 1, "one invoice for the year")
	proratedShare := (30 * 24 * time.Hour).Seconds() / periodEnd.Sub(periodStart).Seconds()
	arrear, ok := s.lineAmount(inv.ID, "price_arrear_"+sub.ID)
	s.Require().True(ok)
	s.InDelta(pauseArrearFee*(1-proratedShare), arrear.InexactFloat64(), 0.01, "arrear %s", arrear)
	usage, ok := s.lineAmount(inv.ID, "price_usage_"+sub.ID)
	s.Require().True(ok)
	s.True(decimal.NewFromInt(1).Equal(usage), "usage only while active, got %s", usage)
	advance, ok := s.lineAmount(inv.ID, "price_advance_"+sub.ID)
	s.Require().True(ok)
	s.True(decimal.NewFromInt(pauseAdvanceFee).Equal(advance), "next year's advance fee in full, got %s", advance)

	credits := s.pauseCredits(sub.ID, periodStart)
	s.Require().Len(credits, 1)
	s.InDelta(pauseAdvanceFee*proratedShare, credits[0].CreditAmount.InexactFloat64(), 0.01, "credit %s", credits[0].CreditAmount)
}

// lineItemsTo switches the stored line items of sub and their prices to period.
func (s *SubscriptionPauseSuite) lineItemsTo(sub *subscription.Subscription, period types.BillingPeriod) {
	ctx := s.GetContext()
	for _, item := range s.subscription(sub.ID).LineItems {
		p, err := s.GetStores().PriceRepo.Get(ctx, item.PriceID)
		s.Require().NoError(err)
		p.BillingPeriod = period
		s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
		item.BillingPeriod = period
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, item))
	}
}

// A quarterly arrear fee on a monthly subscription is billed on the quarter's last month, without the
// create_prorations paused days of any month in the quarter. Days of none pauses stay billed.
func (s *SubscriptionPauseSuite) TestClosedPeriod_QuarterlyArrearOnMonthlyDropsProratedPausedDays() {
	quarterStart := time.Now().UTC().Truncate(time.Second).AddDate(0, -3, -5)
	quarterEnd := quarterStart.AddDate(0, 3, 0)
	lastMonthStart := quarterStart.AddDate(0, 2, 0)
	firstMonthEnd := quarterStart.AddDate(0, 1, 0)
	quarter := quarterEnd.Sub(quarterStart)

	tests := []struct {
		name     string
		behavior types.ProrationBehavior
		paused   bool
		want     float64
	}{
		{name: "no pause", want: pauseArrearFee},
		{name: "first-month pause with none", paused: true, behavior: types.ProrationBehaviorNone, want: pauseArrearFee},
		{
			name:     "first-month pause with create_prorations",
			paused:   true,
			behavior: types.ProrationBehaviorCreateProrations,
			want:     pauseArrearFee * (1 - (10*24*time.Hour).Seconds()/quarter.Seconds()),
		},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			sub := s.subscriptionWithCharges("subs_quarterly_arrear_"+string(rune('a'+i)), quarterStart, firstMonthEnd, chargeArrear)
			s.lineItemsTo(sub, types.BILLING_PERIOD_QUARTER)
			if tt.paused {
				s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, &subscription.SubscriptionPause{
					ID:                  sub.ID + "_pause",
					SubscriptionID:      sub.ID,
					PauseStatus:         types.PauseStatusCompleted,
					PauseMode:           types.PauseModeImmediate,
					ResumeMode:          types.ResumeModeImmediate,
					ProrationBehavior:   tt.behavior,
					PauseStart:          quarterStart.AddDate(0, 0, 5),
					ResumedAt:           lo.ToPtr(quarterStart.AddDate(0, 0, 15)),
					OriginalPeriodStart: quarterStart,
					OriginalPeriodEnd:   firstMonthEnd,
					EnvironmentID:       types.GetEnvironmentID(ctx),
					BaseModel:           types.GetDefaultBaseModel(ctx),
				}))
			}
			stored := s.subscription(sub.ID)
			stored.CurrentPeriodStart, stored.CurrentPeriodEnd = lastMonthStart, quarterEnd
			s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, stored))

			inv := s.billPeriod(sub.ID, lastMonthStart, quarterEnd)

			arrear, ok := s.lineAmount(inv.ID, "price_arrear_"+sub.ID)
			s.Require().True(ok, "the quarter's arrear fee is billed on its last month")
			s.InDelta(tt.want, arrear.InexactFloat64(), 0.01, "arrear %s", arrear)
		})
	}
}

// Monthly advance charges on a quarterly subscription refund the paused share of each month, not of
// the whole quarter: a pause over the second half of the first month refunds half of that month.
func (s *SubscriptionPauseSuite) TestClosedPeriod_AdvanceCreditUsesEachChargePeriod() {
	quarterStart := time.Now().UTC().Truncate(time.Second).AddDate(0, -3, -5)
	quarterEnd := quarterStart.AddDate(0, 3, 0)
	firstMonthEnd := quarterStart.AddDate(0, 1, 0)
	pausedFrom := quarterStart.AddDate(0, 0, 15)
	sub := s.subscriptionWithCharges("subs_quarterly_advance_credit", quarterStart, quarterEnd, chargeAdvance)
	sub.BillingPeriod = types.BILLING_PERIOD_QUARTER
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	s.completedPause(s.subscription(sub.ID), "pause_quarterly_advance_credit", pausedFrom, firstMonthEnd, types.ProrationBehaviorCreateProrations)

	s.billPeriod(sub.ID, quarterStart, quarterEnd)

	credits := s.pauseCredits(sub.ID, quarterStart)
	s.Require().Len(credits, 1)
	s.InDelta(pauseAdvanceFee*fraction(pausedFrom, firstMonthEnd, quarterStart, firstMonthEnd), credits[0].CreditAmount.InexactFloat64(), 0.01,
		"credit %s", credits[0].CreditAmount)
}

// A quarterly advance fee on a monthly subscription credits, on each month's invoice, the paused share of
// the quarter that falls in that month, including months after the one the fee was billed in.
func (s *SubscriptionPauseSuite) TestClosedPeriod_QuarterlyAdvanceOnMonthlyCreditsEachMonth() {
	quarterStart := time.Now().UTC().Truncate(time.Second).AddDate(0, -3, -5)
	quarter := quarterStart.AddDate(0, 3, 0).Sub(quarterStart)

	for month := 0; month < 2; month++ {
		s.Run(fmt.Sprintf("pause in month %d", month+1), func() {
			ctx := s.GetContext()
			monthStart, monthEnd := quarterStart.AddDate(0, month, 0), quarterStart.AddDate(0, month+1, 0)
			sub := s.subscriptionWithCharges(fmt.Sprintf("subs_quarterly_advance_month_%d", month+1), quarterStart, quarterStart.AddDate(0, 1, 0), chargeAdvance)
			s.lineItemsTo(sub, types.BILLING_PERIOD_QUARTER)
			stored := s.subscription(sub.ID)
			stored.CurrentPeriodStart, stored.CurrentPeriodEnd = monthStart, monthEnd
			s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, stored))
			pausedFrom, pausedTo := monthStart.AddDate(0, 0, 5), monthStart.AddDate(0, 0, 15)
			s.completedPause(s.subscription(sub.ID), sub.ID+"_pause", pausedFrom, pausedTo, types.ProrationBehaviorCreateProrations)

			s.billPeriod(sub.ID, monthStart, monthEnd)

			credits := s.pauseCredits(sub.ID, monthStart)
			s.Require().Len(credits, 1, "the month's paused share of the quarterly fee is credited")
			want := pauseAdvanceFee * pausedTo.Sub(pausedFrom).Seconds() / quarter.Seconds()
			s.InDelta(want, credits[0].CreditAmount.InexactFloat64(), 0.01, "credit %s", credits[0].CreditAmount)
		})
	}
}

// A converted invoice is issued in the billing currency, so the paused advance credit is converted
// at the invoice's frozen rate and credited in that currency.
func (s *SubscriptionPauseSuite) TestClosedPeriod_ConvertedInvoiceCreditsInBillingCurrency() {
	ctx := s.GetContext()
	periodStart, periodEnd := s.closedPeriod()
	sub := s.subscriptionWithCharges("subs_closed_converted", periodStart, periodEnd, chargeAdvance)
	pausedFrom := periodStart.AddDate(0, 0, 10)
	s.completedPause(sub, "pause_closed_converted", pausedFrom, periodEnd, types.ProrationBehaviorCreateProrations)

	converted := &invoice.Invoice{
		ID:             "inv_closed_converted",
		CustomerID:     s.cust.ID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
		Currency:       "eur",
		PeriodStart:    lo.ToPtr(periodStart),
		PeriodEnd:      lo.ToPtr(periodEnd),
		FxConversion:   &types.FxConversion{ChargeCurrency: "usd", BillingCurrency: "eur", Rate: decimal.RequireFromString("0.9")},
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.invoiceService.(*invoiceService).creditPausedAdvanceCharges(ctx, converted))

	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(ctx, s.cust.ID)
	s.Require().NoError(err)
	eurWallet, ok := lo.Find(wallets, func(w *wallet.Wallet) bool { return types.IsMatchingCurrency(w.Currency, "eur") })
	s.Require().True(ok, "the credit goes to a wallet in the billing currency")
	filter := types.NewNoLimitWalletTransactionFilter()
	filter.WalletID = &eurWallet.ID
	txs, err := s.GetStores().WalletRepo.ListWalletTransactions(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(txs, 1)
	want := pauseAdvanceFee * 0.9 * fraction(pausedFrom, periodEnd, periodStart, periodEnd)
	s.InDelta(want, txs[0].CreditAmount.InexactFloat64(), 0.01, "credit %s", txs[0].CreditAmount)
}

// The credit is given once per subscription per period: finalizing again, or another invoice of the
// same period, credits nothing more.
func (s *SubscriptionPauseSuite) TestClosedPeriod_CreditGivenOncePerPeriod() {
	periodStart, periodEnd := s.closedPeriod()
	sub := s.billedSubscription("subs_closed_credit_once", periodStart, periodEnd)
	s.completedPause(sub, "pause_credit_once_a", periodStart.AddDate(0, 0, 2), periodStart.AddDate(0, 0, 12), types.ProrationBehaviorCreateProrations)
	s.completedPause(sub, "pause_credit_once_b", periodStart.AddDate(0, 0, 15), periodStart.AddDate(0, 0, 20), types.ProrationBehaviorCreateProrations)

	inv := s.billPeriod(sub.ID, periodStart, periodEnd)
	s.Require().Len(s.pauseCredits(sub.ID, periodStart), 1)
	credited := s.pauseCredits(sub.ID, periodStart)[0].CreditAmount

	invoiceService := s.invoiceService.(*invoiceService)
	s.Require().NoError(invoiceService.creditPausedAdvanceCharges(s.GetContext(), inv))
	otherInvoice := *inv
	otherInvoice.ID = "inv_same_period_other"
	otherInvoice.BillingReason = string(types.InvoiceBillingReasonProration)
	s.Require().NoError(invoiceService.creditPausedAdvanceCharges(s.GetContext(), &otherInvoice))

	credits := s.pauseCredits(sub.ID, periodStart)
	s.Require().Len(credits, 1, "still one credit for the period")
	s.True(credited.Equal(credits[0].CreditAmount))
	s.Len(s.prorationCredits(), 1, "no other credit in the wallet")
}

// A paused subscription cancelled at the period end has nothing left to bill, so its last invoice
// is SKIPPED, and the create_prorations paused part of the advance fee is still credited.
func (s *SubscriptionPauseSuite) TestClosedPeriod_AutoCancelledWithNothingToBillStillCredits() {
	periodStart, periodEnd := s.closedPeriod()
	sub := s.subscriptionWithCharges("subs_closed_auto_cancelled", periodStart, periodEnd, chargeAdvance)
	pausedFrom := periodStart.AddDate(0, 0, 20)
	ctx := s.GetContext()
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, &subscription.SubscriptionPause{
		ID:                  "pause_auto_cancelled",
		SubscriptionID:      sub.ID,
		PauseStatus:         types.PauseStatusCompleted,
		PauseMode:           types.PauseModeImmediate,
		ProrationBehavior:   types.ProrationBehaviorCreateProrations,
		PauseStart:          pausedFrom,
		OriginalPeriodStart: periodStart,
		OriginalPeriodEnd:   periodEnd,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}))
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	sub.CancelAt = lo.ToPtr(periodEnd)
	sub.EndDate = lo.ToPtr(periodEnd)
	sub.CancelAtPeriodEnd = true
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	inv := s.billPeriod(sub.ID, periodStart, periodEnd)

	s.Equal(types.InvoiceStatusSkipped, inv.InvoiceStatus)
	credits := s.pauseCredits(sub.ID, periodStart)
	s.Require().Len(credits, 1)
	s.InDelta(pauseAdvanceFee*fraction(pausedFrom, periodEnd, periodStart, periodEnd), credits[0].CreditAmount.InexactFloat64(), 0.01)
}

// ---------------------------------------------------------------------------
// Calendar billing
// ---------------------------------------------------------------------------

// monthStart is 00:00 UTC on the 1st of the month holding now, shifted by months.
func monthStart(months int) time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
}

// calendar switches a stored subscription to calendar billing.
func (s *SubscriptionPauseSuite) calendar(sub *subscription.Subscription) *subscription.Subscription {
	sub.BillingCycle = types.BillingCycleCalendar
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	return s.subscription(sub.ID)
}

// A calendar subscription paused mid-month is set to cancel on the next 1st, and a resume keeps
// the period on the 1st.
func (s *SubscriptionPauseSuite) TestCalendar_PauseAndResumeStayOnTheFirst() {
	periodStart, periodEnd := monthStart(0), monthStart(1)
	sub := s.calendar(s.billedSubscription("subs_calendar_pause", periodStart, periodEnd))

	s.pause(sub.ID, types.ProrationBehaviorCreateProrations)
	paused := s.subscription(sub.ID)
	s.True(paused.CancelAt.Equal(periodEnd), "auto-cancel on the next 1st, got %s", paused.CancelAt)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	resumed := s.subscription(sub.ID)
	s.Equal(types.BillingCycleCalendar, resumed.BillingCycle, "still calendar billed")
	s.True(resumed.BillingAnchor.Equal(periodStart), "anchor still the 1st")
	s.True(resumed.CurrentPeriodStart.Equal(periodStart))
	s.True(resumed.CurrentPeriodEnd.Equal(periodEnd), "period ends on the next 1st, got %s", resumed.CurrentPeriodEnd)
	s.Nil(resumed.CancelAt)
}

// ---------------------------------------------------------------------------
// Cancel and other changes while paused
// ---------------------------------------------------------------------------

// An immediate cancel of a paused subscription finalizes the period's draft up to now: usage is
// billed for active time only and the arrear fee drops the paused time of a create_prorations pause,
// whose paused part of the advance fee is credited up to the period's original end. The pause is
// completed and its auto-cancel removed.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_ImmediateBillsActiveTimeUpToNow() {
	periodStart, periodEnd := s.openPeriod()
	pausedFrom := periodStart.AddDate(0, 0, 4)
	arrearByBehavior := map[types.ProrationBehavior]float64{}
	var cancelInvoice *invoice.Invoice

	for _, behavior := range []types.ProrationBehavior{types.ProrationBehaviorNone, types.ProrationBehaviorCreateProrations} {
		sub := s.billedSubscription("subs_cancel_paused_"+string(behavior), periodStart, periodEnd)
		s.usage(sub, periodStart.AddDate(0, 0, 2), 3)
		paused := s.pause(sub.ID, behavior)
		s.backdatePause(paused.Pause.ID, pausedFrom)
		s.usage(sub, periodStart.AddDate(0, 0, 6), 50)

		_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
			CancellationType:  types.CancellationTypeImmediate,
			ProrationBehavior: types.ProrationBehaviorCreateProrations,
		})
		s.Require().NoError(err)

		cancelled := s.subscription(sub.ID)
		s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)
		s.Equal(types.PauseStatusNone, cancelled.PauseStatus)
		s.Nil(cancelled.ActivePauseID)
		s.Equal(types.PauseStatusCompleted, s.pauseRecord(paused.Pause.ID).PauseStatus)
		s.Nil(s.cancellationSchedule(sub.ID), "the auto-cancel is replaced by this cancel")

		s.Require().Len(s.invoices(sub.ID), 1, "the period's one invoice")
		cancelInvoice = s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonProration)
		s.Equal(types.InvoiceStatusFinalized, cancelInvoice.InvoiceStatus)
		s.True(cancelInvoice.PeriodStart.Equal(periodStart))
		s.True(cancelInvoice.PeriodEnd.Equal(lo.FromPtr(cancelled.EndDate)), "billed up to the cancel")

		usage, ok := s.lineAmount(cancelInvoice.ID, "price_usage_"+sub.ID)
		s.Require().True(ok)
		s.True(decimal.NewFromInt(3).Equal(usage), "only usage before the pause, got %s", usage)
		arrear, ok := s.lineAmount(cancelInvoice.ID, "price_arrear_"+sub.ID)
		s.Require().True(ok)
		arrearByBehavior[behavior] = arrear.InexactFloat64()

		credits := s.pauseCredits(sub.ID, periodStart)
		if behavior == types.ProrationBehaviorNone {
			s.Empty(credits, "no credit under none, and the cancel adds no proration credit")
			continue
		}
		s.Require().Len(credits, 1)
		s.InDelta(pauseAdvanceFee*fraction(pausedFrom, periodEnd, periodStart, periodEnd), credits[0].CreditAmount.InexactFloat64(), 0.01)
	}

	activeShare := fraction(periodStart, pausedFrom, periodStart, *cancelInvoice.PeriodEnd)
	s.InDelta(arrearByBehavior[types.ProrationBehaviorNone]*activeShare, arrearByBehavior[types.ProrationBehaviorCreateProrations], 0.02,
		"create_prorations bills the arrear fee for the active time only")
	s.Len(s.prorationCredits(), 1, "only the create_prorations pause credited")
}

// An immediate cancel that runs after the pause's auto-cancel time, before the billing run cancelled
// the subscription, ends the subscription and bills the period up to the auto-cancel time.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_AfterAutoCancelTimeEndsAtIt() {
	ctx := s.GetContext()
	periodStart, periodEnd := s.openPeriod()
	cancelAt := periodStart.AddDate(0, 0, 5)
	sub := s.billedSubscription("subs_cancel_after_auto_cancel", periodStart, periodEnd)
	pause := &subscription.SubscriptionPause{
		ID:                  "pause_cancel_after_auto_cancel",
		SubscriptionID:      sub.ID,
		PauseStatus:         types.PauseStatusActive,
		PauseMode:           types.PauseModeImmediate,
		ProrationBehavior:   types.ProrationBehaviorNone,
		PauseStart:          periodStart.AddDate(0, 0, 2),
		OriginalPeriodStart: periodStart,
		OriginalPeriodEnd:   periodEnd,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, pause))
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	sub.PauseStatus = types.PauseStatusActive
	sub.ActivePauseID = lo.ToPtr(pause.ID)
	sub.CancelAt, sub.EndDate, sub.CancelAtPeriodEnd = lo.ToPtr(cancelAt), lo.ToPtr(cancelAt), true
	sub.CurrentPeriodEnd = cancelAt
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
	})
	s.Require().NoError(err)

	cancelled := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)
	s.True(cancelled.EndDate.Equal(cancelAt), "ends at the auto-cancel time, got %s", cancelled.EndDate)
	final := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonProration)
	s.True(final.PeriodEnd.Equal(cancelAt), "billed up to the auto-cancel time, got %s", final.PeriodEnd)
}

// Scheduled cancellations are refused while paused.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_ScheduledRefused() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_cancel_paused_scheduled", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	for _, cancellationType := range []types.CancellationType{types.CancellationTypeEndOfPeriod, types.CancellationTypeScheduledDate} {
		_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
			CancellationType:  cancellationType,
			CancelAt:          lo.ToPtr(periodEnd.Add(-time.Hour)),
			ProrationBehavior: types.ProrationBehaviorNone,
		})
		s.assertValidationError(err, "a paused subscription can only be cancelled immediately")
	}

	stillPaused := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusPaused, stillPaused.SubscriptionStatus)
	s.True(stillPaused.CancelAt.Equal(periodEnd), "the pause's auto-cancel is unchanged")
}

// Cancelling a paused parent cancels its paused inherited children.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_ParentCancelsPausedChildren() {
	periodStart, periodEnd := s.openPeriod()
	parent := s.billedSubscription("subs_cancel_paused_parent", periodStart, periodEnd)
	parent.SubscriptionType = types.SubscriptionTypeParent
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), parent))
	child := s.inheritedChild("subs_cancel_paused_child", parent)
	s.pause(parent.ID, types.ProrationBehaviorNone)

	_, err := s.service.CancelSubscription(s.GetContext(), parent.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorNone,
	})
	s.Require().NoError(err)

	cancelledChild := s.subscription(child.ID)
	s.Equal(types.SubscriptionStatusCancelled, cancelledChild.SubscriptionStatus)
	s.Equal(types.PauseStatusNone, cancelledChild.PauseStatus)
}

// The update API cannot move a subscription out of a pause; resume or cancel must be used.
func (s *SubscriptionPauseSuite) TestUpdateWhilePaused_StatusChangeRefused() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_update_paused", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{
		Status: types.SubscriptionStatusActive,
	})
	s.assertValidationError(err, "cannot change the status of a paused subscription")
	s.Equal(types.SubscriptionStatusPaused, s.subscription(sub.ID).SubscriptionStatus)

	_, err = s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{
		Status: types.SubscriptionStatusPaused,
	})
	s.NoError(err, "sending the current status is not a change")
}

// A subscription paused outside Flexprice, such as by Stripe's sync, has no pause record and its
// status still follows the update API.
func (s *SubscriptionPauseSuite) TestUpdateWhilePaused_ExternalPauseFollowsUpdates() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_update_external_pause", periodStart, periodEnd)
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err := s.service.UpdateSubscription(s.GetContext(), sub.ID, dto.UpdateSubscriptionRequest{
		Status: types.SubscriptionStatusActive,
	})
	s.NoError(err)
	s.Equal(types.SubscriptionStatusActive, s.subscription(sub.ID).SubscriptionStatus)
}

func (s *SubscriptionPauseSuite) TestDeleteLineItemWhilePaused_Refused() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_delete_line_paused", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)
	lineItemID := "subs_line_advance_" + sub.ID

	_, err := s.service.DeleteSubscriptionLineItem(s.GetContext(), lineItemID, dto.DeleteSubscriptionLineItemRequest{
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	})
	s.assertValidationError(err, "cannot delete a line item of a paused subscription")

	lineItem, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), lineItemID)
	s.Require().NoError(err)
	s.True(lineItem.EndDate.IsZero(), "line item still running")
}

func (s *SubscriptionPauseSuite) TestAddonChangeWhilePaused_Refused() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_addon_paused", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := NewAddonChangeService(s.params).Resolve(s.GetContext(), AddonChangeRequest{
		Subscription: s.subscription(sub.ID),
		Removes:      []*dto.RemoveAddonRequest{{AddonAssociationID: "addon_assoc_paused"}},
	})
	s.assertValidationError(err, "cannot change addons on a paused subscription")
}

// A paused subscription's upcoming credit grants can still be listed.
func (s *SubscriptionPauseSuite) TestUpcomingGrantsWhilePaused_NotFound() {
	periodStart, periodEnd := s.openPeriod()
	sub := s.billedSubscription("subs_grants_paused", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := s.service.GetUpcomingCreditGrantApplications(s.GetContext(), &dto.GetUpcomingCreditGrantApplicationsRequest{
		SubscriptionIDs: []string{sub.ID},
	})
	s.True(ierr.IsNotFound(err), "expected not found, got %v", err)
}
