package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestSetCreateSubscriptionTrialWindow_UniformRecurringFixedFromPlan(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{}
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
	}
	err := setCreateSubscriptionTrialWindow(req, sub, prices)
	require.NoError(t, err)
	assert.True(t, sub.TrialStart.Equal(start))
	assert.Equal(t, start.AddDate(0, 0, 14), *sub.TrialEnd)
}

func TestSetCreateSubscriptionTrialWindow_RecurringFixedTrialMismatch(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{}
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 7}},
	}
	err := setCreateSubscriptionTrialWindow(req, sub, prices)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestSetCreateSubscriptionTrialWindow_UsagePriceDoesNotInheritTrial(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{}
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_USAGE, TrialPeriodDays: 99}},
	}
	err := setCreateSubscriptionTrialWindow(req, sub, prices)
	require.NoError(t, err)
	assert.Nil(t, sub.TrialStart)
	assert.Nil(t, sub.TrialEnd)
}

func TestSetCreateSubscriptionTrialWindow_RequestOverrideDays(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	seven := 7
	req := &dto.CreateSubscriptionRequest{SubscriptionCreationConfig: dto.SubscriptionCreationConfig{TrialPeriodDays: &seven}}
	err := setCreateSubscriptionTrialWindow(req, sub, nil)
	require.NoError(t, err)
	assert.Equal(t, start, *sub.TrialStart)
	assert.Equal(t, start.AddDate(0, 0, 7), *sub.TrialEnd)

	zero := 0
	req2 := &dto.CreateSubscriptionRequest{SubscriptionCreationConfig: dto.SubscriptionCreationConfig{TrialPeriodDays: &zero}}
	sub2 := &subscription.Subscription{StartDate: start}
	err2 := setCreateSubscriptionTrialWindow(req2, sub2, []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
	})
	require.NoError(t, err2)
	assert.Nil(t, sub2.TrialStart)
	assert.Nil(t, sub2.TrialEnd)
}

func TestSetCreateSubscriptionTrialWindow_FromPlanDays(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{}
	inherit := 5
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: inherit}},
	}

	err := setCreateSubscriptionTrialWindow(req, sub, prices)
	require.NoError(t, err)
	require.NotNil(t, sub.TrialStart)
	require.NotNil(t, sub.TrialEnd)
	assert.True(t, sub.TrialStart.Equal(start))
	assert.Equal(t, start.AddDate(0, 0, 5), *sub.TrialEnd)
}

func TestSetCreateSubscriptionTrialWindow_InternalBounds(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start}
	req := &dto.CreateSubscriptionRequest{
		TrialStart: &start,
		TrialEnd:   &trialEnd,
	}

	err := setCreateSubscriptionTrialWindow(req, sub, nil)
	require.NoError(t, err)
	assert.Equal(t, &start, sub.TrialStart)
	assert.Equal(t, &trialEnd, sub.TrialEnd)
}

func TestSyncTrialingStateFromCreateRequest_AutoFromTrialWindow(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	te := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		TrialStart:         &ts,
		TrialEnd:           &te,
	}
	req := &dto.CreateSubscriptionRequest{}

	syncTrialingStateFromCreateRequest(req, sub)
	assert.Equal(t, types.SubscriptionStatusTrialing, sub.SubscriptionStatus)
	assert.True(t, sub.CurrentPeriodStart.Equal(ts))
	assert.True(t, sub.CurrentPeriodEnd.Equal(te))
}

func TestSyncTrialingStateFromCreateRequest_ExplicitActiveNoPromote(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	te := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		TrialStart:         &ts,
		TrialEnd:           &te,
	}
	req := &dto.CreateSubscriptionRequest{SubscriptionStatus: types.SubscriptionStatusActive}

	syncTrialingStateFromCreateRequest(req, sub)
	assert.Equal(t, types.SubscriptionStatusActive, sub.SubscriptionStatus)
	assert.True(t, sub.CurrentPeriodStart.Equal(periodStart))
	assert.True(t, sub.CurrentPeriodEnd.Equal(periodEnd))
}

func TestSyncTrialingStateFromCreateRequest_DraftSkipped(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	te := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		SubscriptionStatus: types.SubscriptionStatusDraft,
		TrialStart:         &ts,
		TrialEnd:           &te,
	}
	req := &dto.CreateSubscriptionRequest{SubscriptionStatus: types.SubscriptionStatusDraft}

	syncTrialingStateFromCreateRequest(req, sub)
	assert.Equal(t, types.SubscriptionStatusDraft, sub.SubscriptionStatus)
}

func TestSetCreateSubscriptionTrialWindow_ZeroClears(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{StartDate: start, TrialStart: &start, TrialEnd: &start}
	z := 0
	req := &dto.CreateSubscriptionRequest{SubscriptionCreationConfig: dto.SubscriptionCreationConfig{TrialPeriodDays: &z}}
	prices := []*dto.PriceResponse{
		{Price: &price.Price{BillingCadence: types.BILLING_CADENCE_RECURRING, Type: types.PRICE_TYPE_FIXED, TrialPeriodDays: 14}},
	}

	err := setCreateSubscriptionTrialWindow(req, sub, prices)
	require.NoError(t, err)
	assert.Nil(t, sub.TrialStart)
	assert.Nil(t, sub.TrialEnd)
}

func TestSetCreateSubscriptionTrialWindow_LocalDaysAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	sub := &subscription.Subscription{StartDate: time.Date(2027, 3, 1, 0, 0, 0, 0, loc).UTC(), Timezone: "America/New_York"}
	fourteen := 14
	req := &dto.CreateSubscriptionRequest{SubscriptionCreationConfig: dto.SubscriptionCreationConfig{TrialPeriodDays: &fourteen}, Timezone: "America/New_York"}

	require.NoError(t, setCreateSubscriptionTrialWindow(req, sub, nil))
	require.NotNil(t, sub.TrialEnd)
	assert.True(t, sub.TrialEnd.Equal(time.Date(2027, 3, 15, 0, 0, 0, 0, loc)), "trial end %s", sub.TrialEnd.In(loc))
}

type SubscriptionTrialInvoicePaidSuite struct {
	testutil.BaseServiceTestSuite
	svc SubscriptionService
}

func TestSubscriptionTrialInvoicePaid(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialInvoicePaidSuite))
}

func (s *SubscriptionTrialInvoicePaidSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionTrialInvoicePaidSuite) TestTrialEndPaidInvoice_ActivatesAndKeepsPeriod() {
	ctx := s.GetContext()
	anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 1, 15, 12, 30, 0, 0, time.UTC)

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Trial Paid Customer",
		Email:     "trial-paid@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Paid Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	// processSubscriptionTrialEnd already advanced the period before creating the invoice.
	firstPeriodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: trialEnd,
		BillingAnchor:      anchor,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
	})
	s.Require().NoError(err)

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         cust.ID,
		PlanID:             pl.ID,
		SubscriptionStatus: types.SubscriptionStatusIncomplete,
		Currency:           "usd",
		BillingAnchor:      anchor,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          anchor,
		CurrentPeriodStart: trialEnd,
		CurrentPeriodEnd:   firstPeriodEnd,
		TrialStart:         &anchor,
		TrialEnd:           &trialEnd,
		PaymentBehavior:    string(types.PaymentBehaviorDefaultActive),
		CollectionMethod:   string(types.CollectionMethodChargeAutomatically),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	inv := &invoice.Invoice{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		SubscriptionID: &sub.ID,
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		PaidAt:         &paidAt,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	s.Require().NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))

	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	// Payment only flips status; period was already advanced at trial end.
	s.Equal(types.SubscriptionStatusActive, updated.SubscriptionStatus)
	s.True(updated.CurrentPeriodStart.Equal(trialEnd))
	s.True(updated.CurrentPeriodEnd.Equal(firstPeriodEnd))
	s.NotNil(updated.TrialStart)
	s.NotNil(updated.TrialEnd)
}

func (s *SubscriptionTrialInvoicePaidSuite) TestTrialEndPaidInvoice_IdempotentWhenAlreadyActive() {
	ctx := s.GetContext()
	anchor := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Trial Idem Customer",
		Email:     "trial-idem@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Idem Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	firstPeriodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: trialEnd,
		BillingAnchor:      anchor,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
	})
	s.Require().NoError(err)

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         cust.ID,
		PlanID:             pl.ID,
		SubscriptionStatus: types.SubscriptionStatusIncomplete,
		Currency:           "usd",
		BillingAnchor:      anchor,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          anchor,
		CurrentPeriodStart: trialEnd,
		CurrentPeriodEnd:   firstPeriodEnd,
		TrialStart:         &anchor,
		TrialEnd:           &trialEnd,
		PaymentBehavior:    string(types.PaymentBehaviorDefaultActive),
		CollectionMethod:   string(types.CollectionMethodChargeAutomatically),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	inv := &invoice.Invoice{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		SubscriptionID: &sub.ID,
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		PaidAt:         &paidAt,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	s.Require().NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))
	first, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)

	s.Require().NoError(s.svc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))
	second, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)

	s.Equal(first.CurrentPeriodStart, second.CurrentPeriodStart)
	s.Equal(first.CurrentPeriodEnd, second.CurrentPeriodEnd)
	s.Equal(types.SubscriptionStatusActive, second.SubscriptionStatus)
}

// SubscriptionTrialProrationSuite covers the first paid period after a trial (D10) through create
// and processSubscriptionTrialEnd.
type SubscriptionTrialProrationSuite struct {
	testutil.BaseServiceTestSuite
	params ServiceParams
	subSvc SubscriptionService
}

func TestSubscriptionTrialProration(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialProrationSuite))
}

func (s *SubscriptionTrialProrationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	s.params = ServiceParams{
		Logger: s.GetLogger(), Config: s.GetConfig(), DB: s.GetDB(),
		TaxAssociationRepo: st.TaxAssociationRepo, TaxRateRepo: st.TaxRateRepo, TaxAppliedRepo: st.TaxAppliedRepo,
		SubRepo: st.SubscriptionRepo, SubscriptionLineItemRepo: st.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo: st.SubscriptionPhaseRepo, SubScheduleRepo: st.SubscriptionScheduleRepo,
		PlanRepo: st.PlanRepo, PriceRepo: st.PriceRepo, PriceUnitRepo: st.PriceUnitRepo, EventRepo: st.EventRepo,
		MeterRepo: st.MeterRepo, CustomerRepo: st.CustomerRepo, InvoiceRepo: st.InvoiceRepo,
		InvoiceLineItemRepo: st.InvoiceLineItemRepo, EntitlementRepo: st.EntitlementRepo,
		EntitlementGrantRepo: st.EntitlementGrantRepo, EnvironmentRepo: st.EnvironmentRepo, FeatureRepo: st.FeatureRepo,
		TenantRepo: st.TenantRepo, UserRepo: st.UserRepo, AuthRepo: st.AuthRepo, WalletRepo: st.WalletRepo,
		PaymentRepo: st.PaymentRepo, CreditGrantRepo: st.CreditGrantRepo, CreditGrantApplicationRepo: st.CreditGrantApplicationRepo,
		CouponRepo: st.CouponRepo, CouponAssociationRepo: st.CouponAssociationRepo, CouponApplicationRepo: st.CouponApplicationRepo,
		AlertLogsRepo: st.AlertLogsRepo, WalletBalanceAlertPubSub: types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
		AddonRepo: st.AddonRepo, AddonAssociationRepo: st.AddonAssociationRepo, CheckoutSessionRepo: st.CheckoutSessionRepo,
		ConnectionRepo: st.ConnectionRepo, SettingsRepo: st.SettingsRepo, EventPublisher: s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(), ProrationCalculator: s.GetCalculator(), MeterUsageRepo: st.MeterUsageRepo,
		IntegrationFactory: s.GetIntegrationFactory(), PlanPriceSyncRepo: st.PlanPriceSyncRepo,
		EntityIntegrationMappingRepo: st.EntityIntegrationMappingRepo,
	}
	s.subSvc = NewSubscriptionService(s.params)
}

// D10/I1–I4: calendar subs keep the calendar anchor after a trial and bill a prorated stub;
// anniversary subs re-anchor at trial end. Trial end is in local days; an early end can fall at any time.
// want is $31 x used/full in real seconds.
func (s *SubscriptionTrialProrationSuite) TestFirstPaidPeriodAfterTrial() {
	ny, err := time.LoadLocation("America/New_York")
	s.Require().NoError(err)
	utc := func(m time.Month, d, h, mi int) time.Time { return time.Date(2027, m, d, h, mi, 0, 0, time.UTC) }
	nyAt := func(m time.Month, d int) time.Time { return time.Date(2027, m, d, 0, 0, 0, 0, ny).UTC() }
	cases := []struct {
		name               string
		cycle              types.BillingCycle
		tz                 string
		start              time.Time
		trialEnd           time.Time
		endEarlyAt         *time.Time
		wantStart, wantEnd time.Time
		wantAmount         string
	}{
		{name: "I1 calendar stub Jan29-Feb1 (3/31)", cycle: types.BillingCycleCalendar, tz: "UTC", start: utc(1, 15, 0, 0),
			trialEnd: utc(1, 29, 0, 0), wantStart: utc(1, 29, 0, 0), wantEnd: utc(2, 1, 0, 0), wantAmount: "3.00"},
		{name: "I3 anniversary re-anchors Jan29-Feb28", cycle: types.BillingCycleAnniversary, tz: "UTC", start: utc(1, 15, 0, 0),
			trialEnd: utc(1, 29, 0, 0), wantStart: utc(1, 29, 0, 0), wantEnd: utc(2, 28, 0, 0), wantAmount: "31.00"},
		// DST starts Mar 14 2027: 14 local days end at Mar 15 00:00 EDT; 408h used of a 743h March.
		{name: "I4 calendar New York across DST", cycle: types.BillingCycleCalendar, tz: "America/New_York", start: nyAt(3, 1),
			trialEnd: nyAt(3, 15), wantStart: nyAt(3, 15), wantEnd: nyAt(4, 1), wantAmount: "17.02"},
		{name: "I4 anniversary New York across DST", cycle: types.BillingCycleAnniversary, tz: "America/New_York", start: nyAt(3, 1),
			trialEnd: nyAt(3, 15), wantStart: nyAt(3, 15), wantEnd: nyAt(4, 15), wantAmount: "31.00"},
		// Early end Jan 20 13:45: 274.25h used of a 744h January.
		{name: "early trial end calendar", cycle: types.BillingCycleCalendar, tz: "UTC", start: utc(1, 15, 0, 0),
			trialEnd: utc(1, 29, 0, 0), endEarlyAt: lo.ToPtr(utc(1, 20, 13, 45)), wantStart: utc(1, 20, 13, 45), wantEnd: utc(2, 1, 0, 0), wantAmount: "11.43"},
		{name: "early trial end anniversary", cycle: types.BillingCycleAnniversary, tz: "UTC", start: utc(1, 15, 0, 0),
			trialEnd: utc(1, 29, 0, 0), endEarlyAt: lo.ToPtr(utc(1, 20, 13, 45)), wantStart: utc(1, 20, 13, 45), wantEnd: utc(2, 20, 13, 45), wantAmount: "31.00"},
	}
	for i, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			st := s.GetStores()
			id := fmt.Sprintf("trial_%d", i)
			s.Require().NoError(st.CustomerRepo.Create(ctx, &customer.Customer{
				ID: "cust_" + id, ExternalID: "ext_" + id, Name: id, Timezone: tc.tz, BaseModel: types.GetDefaultBaseModel(ctx),
			}))
			s.Require().NoError(st.PlanRepo.Create(ctx, &plan.Plan{ID: "plan_" + id, Name: id, BaseModel: types.GetDefaultBaseModel(ctx)}))
			s.Require().NoError(st.PriceRepo.Create(ctx, &price.Price{
				ID: "price_" + id, Amount: decimal.NewFromInt(31), Currency: "usd",
				EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_" + id, Type: types.PRICE_TYPE_FIXED,
				BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE, InvoiceCadence: types.InvoiceCadenceAdvance,
				BaseModel: types.GetDefaultBaseModel(ctx),
			}))

			start := tc.start
			resp, err := s.subSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
				CustomerID: "cust_" + id, PlanID: "plan_" + id, Currency: "usd", StartDate: &start,
				BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1, BillingCycle: tc.cycle, ProrationBehavior: types.ProrationBehaviorCreateProrations,
				SubscriptionCreationConfig: dto.SubscriptionCreationConfig{TrialPeriodDays: lo.ToPtr(14)},
			})
			s.Require().NoError(err)
			sub, err := st.SubscriptionRepo.Get(ctx, resp.ID)
			s.Require().NoError(err)
			s.Require().NotNil(sub.TrialEnd)
			s.True(sub.TrialEnd.Equal(tc.trialEnd), "trial end %s, want %s", sub.TrialEnd, tc.trialEnd)

			now := tc.trialEnd
			if tc.endEarlyAt != nil {
				now = *tc.endEarlyAt
				sub.TrialEnd = lo.ToPtr(now)
			}
			inv, err := s.subSvc.(*subscriptionService).processSubscriptionTrialEnd(ctx, sub, NewInvoiceService(s.params), now)
			s.Require().NoError(err)

			after, err := st.SubscriptionRepo.Get(ctx, resp.ID)
			s.Require().NoError(err)
			s.True(after.CurrentPeriodStart.Equal(tc.wantStart), "period start %s, want %s", after.CurrentPeriodStart, tc.wantStart)
			s.True(after.CurrentPeriodEnd.Equal(tc.wantEnd), "period end %s, want %s", after.CurrentPeriodEnd, tc.wantEnd)
			s.Require().NotNil(inv, "a non-zero first paid period raises a trial-end invoice")
			want := decimal.RequireFromString(tc.wantAmount)
			s.True(inv.Subtotal.Sub(want).Abs().LessThanOrEqual(decimal.NewFromFloat(0.01)), "first paid period subtotal %s, want %s", inv.Subtotal, want)
		})
	}
}
