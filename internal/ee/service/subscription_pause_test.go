package service

import (
	"context"
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
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
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

// openPeriod is a monthly period that started 10 days ago, anchored at its start.
func openPeriod() (time.Time, time.Time) {
	start := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, -10)
	return start, start.AddDate(0, 1, 0)
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
	// Listing hides SKIPPED invoices unless asked for, and paused periods are SKIPPED.
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

// ---------------------------------------------------------------------------
// Pause
// ---------------------------------------------------------------------------

// With none, the closing invoice bills usage and the full arrear fee, no advance fee, and no
// credit is given. The subscription is paused from the pause time to the original period end.
func (s *SubscriptionPauseSuite) TestPause_NoneBillsFullArrearAndGivesNoCredit() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_pause_none", periodStart, periodEnd)
	s.usage(sub, time.Now().UTC().Add(-24*time.Hour), 5)

	resp := s.pause(sub.ID, "")

	s.Require().NotNil(resp.Pause)
	s.Require().NotNil(resp.Subscription)
	pausedAt := resp.Pause.PauseStart

	closing := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonProration)
	s.Equal(types.InvoiceStatusFinalized, closing.InvoiceStatus)
	s.True(closing.PeriodStart.Equal(periodStart), "closing invoice starts at the period start")
	s.True(closing.PeriodEnd.Equal(pausedAt), "closing invoice ends at the pause")

	arrear, ok := s.lineAmount(closing.ID, "price_arrear_"+sub.ID)
	s.Require().True(ok, "arrear fee is billed")
	s.True(decimal.NewFromInt(pauseArrearFee).Equal(arrear), "arrear %s", arrear)
	_, ok = s.lineAmount(closing.ID, "price_advance_"+sub.ID)
	s.False(ok, "advance fee is not billed on the closing invoice")
	usage, ok := s.lineAmount(closing.ID, "price_usage_"+sub.ID)
	s.Require().True(ok, "usage up to the pause is billed")
	s.True(decimal.NewFromInt(5).Equal(usage), "usage %s", usage)

	s.Empty(s.prorationCredits(), "no credit under none")

	paused := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusPaused, paused.SubscriptionStatus)
	s.Equal(types.PauseStatusActive, paused.PauseStatus)
	s.Equal(resp.Pause.ID, lo.FromPtr(paused.ActivePauseID))
	s.True(paused.CurrentPeriodStart.Equal(pausedAt), "paused period starts at the pause")
	s.True(paused.CurrentPeriodEnd.Equal(periodEnd), "paused period ends at the original period end")
	s.True(paused.BillingAnchor.Equal(periodStart), "anchor unchanged")
	s.Equal(types.ProrationBehaviorNone, paused.ProrationBehavior, "the pause request is not saved on the subscription")

	pause := s.pauseRecord(resp.Pause.ID)
	s.Equal(types.PauseStatusActive, pause.PauseStatus)
	s.Equal(types.PauseModeImmediate, pause.PauseMode)
	s.True(pause.OriginalPeriodStart.Equal(periodStart))
	s.True(pause.OriginalPeriodEnd.Equal(periodEnd))
	s.Equal("customer hold", pause.Reason)
	s.Equal("support", pause.Metadata["requested_by"])
	s.Nil(pause.ResumedAt)
}

// With create_prorations, the closing invoice bills only the used part of the arrear fee and the
// unused part of the advance fee is credited to the wallet. The subscription's own setting is untouched.
func (s *SubscriptionPauseSuite) TestPause_CreateProrationsProratesArrearAndCreditsAdvance() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_pause_prorate", periodStart, periodEnd)

	resp := s.pause(sub.ID, types.ProrationBehaviorCreateProrations)
	pausedAt := resp.Pause.PauseStart

	closing := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonProration)
	arrear, ok := s.lineAmount(closing.ID, "price_arrear_"+sub.ID)
	s.Require().True(ok)
	used := fraction(periodStart, pausedAt, periodStart, periodEnd)
	s.InDelta(pauseArrearFee*used, arrear.InexactFloat64(), 0.02, "arrear billed for the used part")

	credits := s.prorationCredits()
	s.Require().Len(credits, 1, "one credit for the unused advance fee")
	unused := fraction(pausedAt, periodEnd, periodStart, periodEnd)
	s.InDelta(pauseAdvanceFee*unused, credits[0].CreditAmount.InexactFloat64(), 0.02, "credit %s", credits[0].CreditAmount)

	s.Equal(types.ProrationBehaviorNone, s.subscription(sub.ID).ProrationBehavior, "the pause request is not saved on the subscription")
}

// The current period's open draft becomes the closing invoice instead of being billed again at
// the original period end.
func (s *SubscriptionPauseSuite) TestPause_OpenDraftBecomesTheClosingInvoice() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_pause_draft", periodStart, periodEnd)
	s.usage(sub, time.Now().UTC().Add(-24*time.Hour), 7)

	draft, _, err := s.invoiceService.GetOrComputeCurrentPeriodDraft(s.GetContext(), sub)
	s.Require().NoError(err)
	s.Require().NotNil(draft)
	s.Equal(string(types.InvoiceBillingReasonSubscriptionCycle), draft.BillingReason)

	resp := s.pause(sub.ID, types.ProrationBehaviorNone)

	s.Empty(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionCycle), "no cycle draft is left behind")
	closing := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonProration)
	s.Equal(draft.ID, closing.ID, "the open draft is reused as the closing invoice")
	s.True(closing.PeriodEnd.Equal(resp.Pause.PauseStart), "draft end moved to the pause")
	s.Equal(types.InvoiceStatusFinalized, closing.InvoiceStatus)
	usage, ok := s.lineAmount(closing.ID, "price_usage_"+sub.ID)
	s.Require().True(ok)
	s.True(decimal.NewFromInt(7).Equal(usage), "usage billed once, %s", usage)
}

// A parent's pause is mirrored on its inherited children.
func (s *SubscriptionPauseSuite) TestPause_ParentCascadesToInheritedChildren() {
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()

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

// ---------------------------------------------------------------------------
// While paused
// ---------------------------------------------------------------------------

// The billing run records a paused period as a $0 SKIPPED invoice with no line items, once.
func (s *SubscriptionPauseSuite) TestPausedPeriod_RecordedAsSkippedInvoice() {
	anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -2, 0)
	periodStart := anchor.AddDate(0, 1, 0)
	periodEnd := anchor.AddDate(0, 2, 0)
	sub, _ := s.pausedSubscription("subs_paused_period", anchor, anchor.AddDate(0, 0, 10), periodStart, periodEnd)
	s.usage(sub, periodStart.Add(24*time.Hour), 40)

	first, err := s.service.CreateDraftInvoiceForSubscription(s.GetContext(), sub.ID, dto.Period{Start: periodStart, End: periodEnd})
	s.Require().NoError(err)
	again, err := s.service.CreateDraftInvoiceForSubscription(s.GetContext(), sub.ID, dto.Period{Start: periodStart, End: periodEnd})
	s.Require().NoError(err)

	s.Equal(first.ID, again.ID, "one invoice per paused period")
	s.Equal(string(types.InvoiceBillingReasonSubscriptionPaused), first.BillingReason)
	s.Equal(types.InvoiceStatusSkipped, first.InvoiceStatus)
	s.True(first.Total.IsZero())
	s.True(first.AmountDue.IsZero())
	s.True(first.PeriodStart.Equal(periodStart))
	s.True(first.PeriodEnd.Equal(periodEnd))
	s.Empty(s.invoiceLines(first.ID), "usage during the pause is not billed")
}

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

// A billing draft cannot be opened for a paused subscription.
func (s *SubscriptionPauseSuite) TestPausedPeriod_BillingDraftRefused() {
	anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -1, 0)
	sub, _ := s.pausedSubscription("subs_paused_draft", anchor, anchor.AddDate(0, 0, 10), anchor.AddDate(0, 0, 10), anchor.AddDate(0, 1, 0))

	_, err := s.invoiceService.CreateDraftInvoiceForSubscription(s.GetContext(), dto.CreateSubscriptionDraftInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    sub.CurrentPeriodStart,
		PeriodEnd:      sub.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.assertValidationError(err, "cannot create a billing draft for a paused subscription")
	s.Empty(s.invoices(sub.ID))
}

// ---------------------------------------------------------------------------
// Resume
// ---------------------------------------------------------------------------

// With none, resume closes the paused stretch, keeps the anchor and bills the full advance fee
// for the period that runs from the resume to the next anchor date.
func (s *SubscriptionPauseSuite) TestResume_KeepsAnchorAndBillsAdvanceForTheRestOfThePeriod() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_resume_none", periodStart, periodEnd)
	paused := s.pause(sub.ID, types.ProrationBehaviorNone)

	resp, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{
		Metadata: map[string]string{"resumed_by": "support"},
	})
	s.Require().NoError(err)
	resumedAt := lo.FromPtr(resp.Pause.ResumedAt)

	pausedStretch := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionPaused)
	s.Equal(types.InvoiceStatusSkipped, pausedStretch.InvoiceStatus)
	s.True(pausedStretch.PeriodStart.Equal(paused.Pause.PauseStart))
	s.True(pausedStretch.PeriodEnd.Equal(resumedAt))

	resumeInvoice := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	s.Equal(types.InvoiceStatusFinalized, resumeInvoice.InvoiceStatus)
	s.True(resumeInvoice.PeriodStart.Equal(resumedAt))
	s.True(resumeInvoice.PeriodEnd.Equal(periodEnd))
	advance, ok := s.lineAmount(resumeInvoice.ID, "price_advance_"+sub.ID)
	s.Require().True(ok, "advance fee billed at resume")
	s.True(decimal.NewFromInt(pauseAdvanceFee).Equal(advance), "full advance under none, got %s", advance)
	_, ok = s.lineAmount(resumeInvoice.ID, "price_arrear_"+sub.ID)
	s.False(ok, "arrear fee waits for the period end")
	_, ok = s.lineAmount(resumeInvoice.ID, "price_usage_"+sub.ID)
	s.False(ok, "usage waits for the period end")

	resumed := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusActive, resumed.SubscriptionStatus)
	s.Equal(types.PauseStatusNone, resumed.PauseStatus)
	s.Nil(resumed.ActivePauseID)
	s.True(resumed.BillingAnchor.Equal(periodStart), "anchor unchanged")
	s.True(resumed.CurrentPeriodStart.Equal(resumedAt))
	s.True(resumed.CurrentPeriodEnd.Equal(periodEnd), "period still ends on the next anchor date")
	s.Equal(types.ProrationBehaviorNone, resumed.ProrationBehavior)

	pause := s.pauseRecord(paused.Pause.ID)
	s.Equal(types.PauseStatusCompleted, pause.PauseStatus)
	s.Equal(types.ResumeModeImmediate, pause.ResumeMode)
	s.Require().NotNil(pause.ResumedAt)
	s.True(pause.ResumedAt.Equal(resumedAt))
	s.Equal("support", pause.Metadata["requested_by"], "pause metadata kept")
	s.Equal("support", pause.Metadata["resumed_by"], "resume metadata merged")
}

// create_prorations on resume is saved on the subscription and prorates the advance fee on the
// resume invoice and the arrear fee on the next cycle invoice, which bills usage from the resume only.
func (s *SubscriptionPauseSuite) TestResume_CreateProrationsIsSavedAndProratesTheShortPeriod() {
	// Paused since the previous period; the resume falls 10 days into the current one.
	anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -1, -10)
	periodStart := anchor.AddDate(0, 1, 0)
	periodEnd := anchor.AddDate(0, 2, 0)
	sub, _ := s.pausedSubscription("subs_resume_prorate", anchor, anchor.AddDate(0, 0, 10), periodStart, periodEnd)
	s.usage(sub, periodStart.Add(24*time.Hour), 40)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{
		ProrationBehavior: lo.ToPtr(types.ProrationBehaviorCreateProrations),
	})
	s.Require().NoError(err)

	resumed := s.subscription(sub.ID)
	s.Equal(types.ProrationBehaviorCreateProrations, resumed.ProrationBehavior, "saved on the subscription")
	resumedAt := resumed.CurrentPeriodStart
	s.True(resumed.CurrentPeriodEnd.Equal(periodEnd), "period still ends on the next anchor date")
	remaining := fraction(resumedAt, periodEnd, periodStart, periodEnd)

	resumeInvoice := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	advance, ok := s.lineAmount(resumeInvoice.ID, "price_advance_"+sub.ID)
	s.Require().True(ok)
	s.InDelta(pauseAdvanceFee*remaining, advance.InexactFloat64(), 0.02, "advance prorated to the rest of the period")

	s.usage(sub, resumedAt.Add(time.Hour), 3)
	cycle, err := s.service.CreateDraftInvoiceForSubscription(s.GetContext(), sub.ID, dto.Period{Start: resumedAt, End: periodEnd})
	s.Require().NoError(err)
	computed, _, err := s.invoiceService.ComputeInvoice(s.GetContext(), cycle.ID, nil)
	s.Require().NoError(err)

	arrear, ok := s.lineAmount(computed.ID, "price_arrear_"+sub.ID)
	s.Require().True(ok)
	s.InDelta(pauseArrearFee*remaining, arrear.InexactFloat64(), 0.02, "arrear prorated on the next cycle invoice")
	usage, ok := s.lineAmount(computed.ID, "price_usage_"+sub.ID)
	s.Require().True(ok)
	s.True(decimal.NewFromInt(3).Equal(usage), "only usage after the resume is billed, got %s", usage)
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

// A paused subscription read before the resume stays cached while the resume runs; the payment
// step must not write that copy back over the resumed fields.
func (s *SubscriptionPauseSuite) TestResume_StaleCachedSubscriptionDoesNotUndoTheResume() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_resume_cached", periodStart, periodEnd)
	paused := s.pause(sub.ID, types.ProrationBehaviorNone)

	params := s.params
	params.SubRepo = &cachedSubscriptionRepo{
		Repository: s.params.SubRepo,
		cached:     map[string]*subscription.Subscription{sub.ID: s.subscription(sub.ID)},
	}
	resp, err := NewSubscriptionService(params).ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)
	resumedAt := lo.FromPtr(resp.Pause.ResumedAt)

	for name, got := range map[string]*subscription.Subscription{
		"stored":   s.subscription(sub.ID),
		"response": resp.Subscription.Subscription,
	} {
		s.Equal(types.SubscriptionStatusActive, got.SubscriptionStatus, name)
		s.Equal(types.PauseStatusNone, got.PauseStatus, name)
		s.Nil(got.ActivePauseID, name)
		s.True(got.CurrentPeriodStart.Equal(resumedAt), "%s: period starts at the resume", name)
		s.True(got.CurrentPeriodEnd.Equal(periodEnd), name)
	}
	s.Equal(types.PauseStatusCompleted, s.pauseRecord(paused.Pause.ID).PauseStatus)
}

// Without proration_behavior, resume keeps the subscription's own value.
func (s *SubscriptionPauseSuite) TestResume_OmittedProrationKeepsTheSubscriptionValue() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_resume_keep", periodStart, periodEnd)
	sub.ProrationBehavior = types.ProrationBehaviorCreateProrations
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	resumed := s.subscription(sub.ID)
	s.Equal(types.ProrationBehaviorCreateProrations, resumed.ProrationBehavior)
	resumeInvoice := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	advance, ok := s.lineAmount(resumeInvoice.ID, "price_advance_"+sub.ID)
	s.Require().True(ok)
	remaining := fraction(resumed.CurrentPeriodStart, periodEnd, periodStart, periodEnd)
	s.InDelta(pauseAdvanceFee*remaining, advance.InexactFloat64(), 0.02)
}

// Paused periods the billing run had not rolled yet are recorded at resume, one SKIPPED invoice
// each, and the period that holds the resume runs to the next anchor date.
func (s *SubscriptionPauseSuite) TestResume_RecordsPausedPeriodsTheBillingRunMissed() {
	// The stored period ended 10 days ago and was never rolled; the resume falls in the next one.
	anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -2, -10)
	staleStart := anchor.AddDate(0, 1, 0)
	staleEnd := anchor.AddDate(0, 2, 0)
	sub, _ := s.pausedSubscription("subs_resume_missed", anchor, anchor.AddDate(0, 0, 10), staleStart, staleEnd)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	resumed := s.subscription(sub.ID)
	resumedAt := resumed.CurrentPeriodStart
	nextAnchorDate, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: staleEnd,
		BillingAnchor:      anchor,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
		Timezone:           "UTC",
	})
	s.Require().NoError(err)
	s.True(resumed.BillingAnchor.Equal(anchor), "anchor unchanged")
	s.True(resumed.CurrentPeriodEnd.Equal(nextAnchorDate), "period runs to the next anchor date, got %s", resumed.CurrentPeriodEnd)

	pausedInvoices := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionPaused)
	s.Require().Len(pausedInvoices, 2, "the missed period and the stretch up to the resume")
	periods := lo.Map(pausedInvoices, func(inv *invoice.Invoice, _ int) dto.Period {
		s.Equal(types.InvoiceStatusSkipped, inv.InvoiceStatus)
		return dto.Period{Start: *inv.PeriodStart, End: *inv.PeriodEnd}
	})
	s.Contains(periods, dto.Period{Start: staleStart, End: staleEnd})
	s.Contains(periods, dto.Period{Start: staleEnd, End: resumedAt})
}

// A parent's resume reaches its paused inherited children with the new period.
func (s *SubscriptionPauseSuite) TestResume_ParentCascadesToPausedInheritedChildren() {
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()

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

	s.Run("invalid proration behavior", func() {
		anchor := time.Now().UTC().Truncate(time.Second).AddDate(0, -1, 0)
		sub, _ := s.pausedSubscription("subs_resume_invalid", anchor, anchor.AddDate(0, 0, 5), anchor.AddDate(0, 0, 5), anchor.AddDate(0, 1, 0))
		_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{
			ProrationBehavior: lo.ToPtr(types.ProrationBehavior("always")),
		})
		s.Require().Error(err)
		s.True(ierr.IsValidation(err))
		s.Equal(types.SubscriptionStatusPaused, s.subscription(sub.ID).SubscriptionStatus)
	})
}

// ---------------------------------------------------------------------------
// Proration across a pause and resume, and periods with nothing to bill
// ---------------------------------------------------------------------------

// Paused and resumed inside one period with create_prorations on both: the unused advance fee is
// credited at the pause and the advance fee for what is left is charged at the resume.
func (s *SubscriptionPauseSuite) TestPauseAndResumeInOnePeriod_CreateProrationsCreditsThenCharges() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_pause_resume_prorate", periodStart, periodEnd)

	paused := s.pause(sub.ID, types.ProrationBehaviorCreateProrations)
	resp, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{
		ProrationBehavior: lo.ToPtr(types.ProrationBehaviorCreateProrations),
	})
	s.Require().NoError(err)
	resumedAt := lo.FromPtr(resp.Pause.ResumedAt)

	credits := s.prorationCredits()
	s.Require().Len(credits, 1)
	unusedAtPause := fraction(paused.Pause.PauseStart, periodEnd, periodStart, periodEnd)
	s.InDelta(pauseAdvanceFee*unusedAtPause, credits[0].CreditAmount.InexactFloat64(), 0.02)

	resumeInvoice := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	advance, ok := s.lineAmount(resumeInvoice.ID, "price_advance_"+sub.ID)
	s.Require().True(ok)
	remainingAtResume := fraction(resumedAt, periodEnd, periodStart, periodEnd)
	s.InDelta(pauseAdvanceFee*remainingAtResume, advance.InexactFloat64(), 0.02)
}

// A pause with nothing to bill up to it still pauses: the closing invoice is SKIPPED, not finalized.
func (s *SubscriptionPauseSuite) TestPause_NothingToBillStillPauses() {
	periodStart, periodEnd := openPeriod()
	sub := s.subscriptionWithCharges("subs_pause_nothing", periodStart, periodEnd, chargeAdvance)

	s.pause(sub.ID, types.ProrationBehaviorNone)

	s.Empty(lo.Filter(s.invoices(sub.ID), func(inv *invoice.Invoice, _ int) bool {
		return inv.InvoiceStatus == types.InvoiceStatusFinalized
	}), "nothing is billed")
	s.Equal(types.SubscriptionStatusPaused, s.subscription(sub.ID).SubscriptionStatus)
}

// A resume with no advance charges still resumes: the resume invoice is SKIPPED, not finalized.
func (s *SubscriptionPauseSuite) TestResume_NothingToBillStillResumes() {
	periodStart, periodEnd := openPeriod()
	sub := s.subscriptionWithCharges("subs_resume_nothing", periodStart, periodEnd, chargeArrear, chargeUsage)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	resumeInvoices := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	for _, inv := range resumeInvoices {
		s.Equal(types.InvoiceStatusSkipped, inv.InvoiceStatus)
		s.True(inv.Total.IsZero())
	}
	s.Equal(types.SubscriptionStatusActive, s.subscription(sub.ID).SubscriptionStatus)
}

// A subscription can be paused again after a resume, and each pause is its own record.
func (s *SubscriptionPauseSuite) TestPause_AgainAfterResume() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_pause_twice", periodStart, periodEnd)
	first := s.pause(sub.ID, types.ProrationBehaviorNone)
	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	second := s.pause(sub.ID, types.ProrationBehaviorNone)

	s.NotEqual(first.Pause.ID, second.Pause.ID)
	s.Equal(types.PauseStatusCompleted, s.pauseRecord(first.Pause.ID).PauseStatus)
	s.Equal(types.PauseStatusActive, s.pauseRecord(second.Pause.ID).PauseStatus)
	paused := s.subscription(sub.ID)
	s.Equal(second.Pause.ID, lo.FromPtr(paused.ActivePauseID))
	s.True(paused.BillingAnchor.Equal(periodStart), "anchor unchanged across both pauses")
}

// Pause and resume each publish subscription.updated and their own event once.
func (s *SubscriptionPauseSuite) TestPauseAndResume_PublishWebhooks() {
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()
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

// A calendar subscription paused and resumed mid-month stays on the 1st: the closing invoice and
// the resume invoice are prorated against the calendar month, and the period after the resume ends
// on the next 1st.
func (s *SubscriptionPauseSuite) TestCalendar_PauseAndResumeStayOnTheFirst() {
	periodStart, periodEnd := monthStart(0), monthStart(1)
	sub := s.calendar(s.billedSubscription("subs_calendar_pause", periodStart, periodEnd))

	paused := s.pause(sub.ID, types.ProrationBehaviorCreateProrations)
	closing := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonProration)
	s.True(closing.PeriodStart.Equal(periodStart), "closing invoice starts on the 1st")
	arrear, ok := s.lineAmount(closing.ID, "price_arrear_"+sub.ID)
	s.Require().True(ok)
	s.InDelta(pauseArrearFee*fraction(periodStart, paused.Pause.PauseStart, periodStart, periodEnd), arrear.InexactFloat64(), 0.02)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{
		ProrationBehavior: lo.ToPtr(types.ProrationBehaviorCreateProrations),
	})
	s.Require().NoError(err)

	resumed := s.subscription(sub.ID)
	s.Equal(types.BillingCycleCalendar, resumed.BillingCycle, "still calendar billed")
	s.True(resumed.BillingAnchor.Equal(periodStart), "anchor still the 1st")
	s.True(resumed.CurrentPeriodEnd.Equal(periodEnd), "period ends on the next 1st, got %s", resumed.CurrentPeriodEnd)

	resumeInvoice := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	advance, ok := s.lineAmount(resumeInvoice.ID, "price_advance_"+sub.ID)
	s.Require().True(ok)
	s.InDelta(pauseAdvanceFee*fraction(resumed.CurrentPeriodStart, periodEnd, periodStart, periodEnd), advance.InexactFloat64(), 0.02)

	nextEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: resumed.CurrentPeriodEnd,
		BillingAnchor:      resumed.BillingAnchor,
		Unit:               resumed.BillingPeriodCount,
		Period:             resumed.BillingPeriod,
		Timezone:           resumed.Timezone,
	})
	s.Require().NoError(err)
	s.True(nextEnd.Equal(monthStart(2)), "the following period is a full calendar month, got %s", nextEnd)
}

// A calendar subscription paused across months records each calendar month as its own SKIPPED
// invoice, and the period after the resume ends on the next 1st.
func (s *SubscriptionPauseSuite) TestCalendar_ResumeAfterPausedMonths() {
	anchor := monthStart(-2)
	sub, _ := s.pausedSubscription("subs_calendar_months", anchor, anchor.AddDate(0, 0, 10), monthStart(-2), monthStart(-1))
	s.calendar(sub)

	_, err := s.service.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	resumed := s.subscription(sub.ID)
	resumedAt := resumed.CurrentPeriodStart
	s.True(resumed.CurrentPeriodEnd.Equal(monthStart(1)), "period ends on the next 1st, got %s", resumed.CurrentPeriodEnd)
	s.True(resumed.BillingAnchor.Equal(anchor))

	periods := lo.Map(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionPaused), func(inv *invoice.Invoice, _ int) dto.Period {
		s.Equal(types.InvoiceStatusSkipped, inv.InvoiceStatus)
		return dto.Period{Start: *inv.PeriodStart, End: *inv.PeriodEnd}
	})
	s.ElementsMatch([]dto.Period{
		{Start: monthStart(-2), End: monthStart(-1)},
		{Start: monthStart(-1), End: monthStart(0)},
		{Start: monthStart(0), End: resumedAt},
	}, periods)

	resumeInvoice := s.onlyInvoiceWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	s.True(resumeInvoice.PeriodEnd.Equal(monthStart(1)))
	advance, ok := s.lineAmount(resumeInvoice.ID, "price_advance_"+sub.ID)
	s.Require().True(ok)
	s.True(decimal.NewFromInt(pauseAdvanceFee).Equal(advance), "full advance under none, got %s", advance)
}

// ---------------------------------------------------------------------------
// Cancel and other changes while paused
// ---------------------------------------------------------------------------

// An immediate cancel of a paused subscription bills and credits nothing, whatever the request
// asks for, and completes the pause.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_ImmediateBillsNothing() {
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_cancel_paused", periodStart, periodEnd)
	paused := s.pause(sub.ID, types.ProrationBehaviorCreateProrations)
	s.Require().Len(s.prorationCredits(), 1)
	invoicesBefore := len(s.invoices(sub.ID))

	_, err := s.service.CancelSubscription(s.GetContext(), sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
		ProrationBehavior:              types.ProrationBehaviorCreateProrations,
	})
	s.Require().NoError(err)

	s.Len(s.invoices(sub.ID), invoicesBefore, "no invoice raised")
	s.Len(s.prorationCredits(), 1, "no second credit")
	cancelled := s.subscription(sub.ID)
	s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)
	s.Equal(types.PauseStatusNone, cancelled.PauseStatus)
	s.Nil(cancelled.ActivePauseID)
	s.Equal(types.PauseStatusCompleted, s.pauseRecord(paused.Pause.ID).PauseStatus)
}

// Scheduled cancellations are refused while paused.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_ScheduledRefused() {
	periodStart, periodEnd := openPeriod()
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
	s.Nil(stillPaused.CancelAt)
}

// Cancelling a paused parent cancels its paused inherited children.
func (s *SubscriptionPauseSuite) TestCancelWhilePaused_ParentCancelsPausedChildren() {
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()
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
	periodStart, periodEnd := openPeriod()
	sub := s.billedSubscription("subs_grants_paused", periodStart, periodEnd)
	s.pause(sub.ID, types.ProrationBehaviorNone)

	_, err := s.service.GetUpcomingCreditGrantApplications(s.GetContext(), &dto.GetUpcomingCreditGrantApplicationsRequest{
		SubscriptionIDs: []string{sub.ID},
	})
	s.True(ierr.IsNotFound(err), "expected not found, got %v", err)
}
