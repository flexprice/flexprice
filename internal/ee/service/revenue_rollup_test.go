package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// RevenueRollupSuite drives RollupSubscription against the REAL billing preview
// engine (PrepareSubscriptionInvoiceRequest) â not a hand-rolled decomposition â
// over a worked example: a 30-day monthly subscription
// with a $30 advance fixed charge, $0.01/call usage (1200 calls/day, a
// same-meter Feature+Entitlement present but non-binding â see
// seedWorkedExample), and a $500 minimum commitment with a 2x overage factor
// and true-up enabled. Î£ net_amount must reconcile to $530, and a second
// RollupSubscription call must be idempotent.
type RevenueRollupSuite struct {
	testutil.BaseServiceTestSuite
	ctx   context.Context
	svc   RevenueService
	store *testutil.InMemoryRevenueFactStore

	sub         *subscription.Subscription
	periodStart time.Time
	// periodEnd is the query bound passed to ListBySubscriptionPeriod. It must
	// reach through the current period's exclusive end (see seedWorkedExample)
	// to also catch the next period's advance-billed fixed row.
	periodEnd time.Time
}

func TestRevenueRollup(t *testing.T) {
	suite.Run(t, new(RevenueRollupSuite))
}

func (s *RevenueRollupSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ctx = s.GetContext()
	s.store = s.GetStores().RevenueFactRepo.(*testutil.InMemoryRevenueFactStore)
	s.svc = NewRevenueService(s.serviceParams())
}

func (s *RevenueRollupSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *RevenueRollupSuite) serviceParams() ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      stores.SubscriptionRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        stores.SubscriptionPhaseRepo,
		SubScheduleRepo:              stores.SubscriptionScheduleRepo,
		PlanRepo:                     stores.PlanRepo,
		PriceRepo:                    stores.PriceRepo,
		PriceUnitRepo:                stores.PriceUnitRepo,
		EventRepo:                    stores.EventRepo,
		MeterRepo:                    stores.MeterRepo,
		MeterUsageRepo:               stores.MeterUsageRepo,
		CustomerRepo:                 stores.CustomerRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		EntitlementRepo:              stores.EntitlementRepo,
		EntitlementGrantRepo:         stores.EntitlementGrantRepo,
		EnvironmentRepo:              stores.EnvironmentRepo,
		FeatureRepo:                  stores.FeatureRepo,
		AddonAssociationRepo:         stores.AddonAssociationRepo,
		TenantRepo:                   stores.TenantRepo,
		UserRepo:                     stores.UserRepo,
		AuthRepo:                     stores.AuthRepo,
		WalletRepo:                   stores.WalletRepo,
		PaymentRepo:                  stores.PaymentRepo,
		CreditNoteRepo:               stores.CreditNoteRepo,
		CreditNoteLineItemRepo:       stores.CreditNoteLineItemRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		CreditGrantRepo:              stores.CreditGrantRepo,
		CreditGrantApplicationRepo:   stores.CreditGrantApplicationRepo,
		ConnectionRepo:               stores.ConnectionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		SettingsRepo:                 stores.SettingsRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		TaskRepo:                     stores.TaskRepo,
		SecretRepo:                   stores.SecretRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
		RevenueFactRepo:              stores.RevenueFactRepo,
	}
}

// seedWorkedExample builds a worked-example fixture â
// plan + fixed/usage prices + meter + feature + entitlement + subscription
// (with a $500/2x/true-up subscription-level commitment) â and seeds
// meter_usage at 1200 calls/day for 30 days, the real inputs the billing
// preview engine reads.
//
// The entitlement's UsageLimit is 0 rather than the design doc's illustrative
// 20 000: the real engine's subscription-level commitment/true-up is computed
// from the GROSS pre-entitlement usage amount (buildMeterUsageResponse splits
// commitment/overage before CalculateMeterUsageCharges applies any per-item
// entitlement deduction), so usage+trueup nets to exactly commitmentAmount
// minus whatever the entitlement deducted â not the "billable-after-entitlementLimit
// tops up to the commitment" shape the hand-rolled decomposition tests use.
// A non-zero entitlementLimit here would pull the $530 total off by exactly the
// deducted amount (verified empirically). UsageLimit=0 still exercises the
// real entitlement-resolution path (FeatureRepo/EntitlementRepo/
// GetAggregatedSubscriptionEntitlements) that resolveEntitlementLimit depends on,
// without perturbing the commitment math.
func (s *RevenueRollupSuite) seedWorkedExample(ctx context.Context) {
	s.periodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := s.periodStart.AddDate(0, 0, 30)
	// ReferencePointRevenueFacts returns this period's ARREAR usage/true-up (dated
	// within [periodStart, periodEndExclusive)) together with NEXT period's
	// ADVANCE fixed charge (dated at periodEndExclusive, the next period's own
	// start) â one RollupSubscription call spans both, so the query window
	// below must reach through periodEndExclusive to catch the fixed row too.
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID:         "cust_rollup_wk",
		ExternalID: "ext_rollup_wk",
		Name:       "Rollup Worked Example",
		Email:      "rollup@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        "plan_rollup_wk",
		Name:      "Rollup Worked Example Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	mtr := &meter.Meter{
		ID:        "meter_rollup_wk",
		Name:      "API Calls Rollup",
		EventName: "api_call_rollup_wk",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))

	feat := &feature.Feature{
		ID:        "feat_rollup_wk",
		Name:      "API Calls Feature Rollup",
		Type:      types.FeatureTypeMetered,
		MeterID:   mtr.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, feat))

	ent := &entitlement.Entitlement{
		ID:               "ent_rollup_wk",
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         pl.ID,
		FeatureID:        feat.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       lo.ToPtr(int64(0)),
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		IsSoftLimit:      false,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, ent)
	s.NoError(err)

	fixedPrice := &price.Price{
		ID:                 "price_rollup_fixed",
		Amount:             decimal.NewFromInt(30),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, fixedPrice))

	usagePrice := &price.Price{
		ID:                 "price_rollup_usage",
		Amount:             decimal.RequireFromString("0.01"),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            mtr.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePrice))

	commitmentAmount := decimal.NewFromInt(500)
	overageFactor := decimal.NewFromInt(2)
	s.sub = &subscription.Subscription{
		ID:                 "sub_rollup_wk",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          s.periodStart,
		BillingAnchor:      periodEndExclusive,
		CurrentPeriodStart: s.periodStart,
		CurrentPeriodEnd:   periodEndExclusive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CommitmentAmount:   &commitmentAmount,
		OverageFactor:      &overageFactor,
		EnableTrueUp:       true,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	lineItems := []*subscription.SubscriptionLineItem{
		{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.sub.ID,
			CustomerID:      s.sub.CustomerID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         fixedPrice.ID,
			PriceType:       fixedPrice.Type,
			DisplayName:     "Fixed",
			Quantity:        decimal.NewFromInt(1),
			Currency:        s.sub.Currency,
			BillingPeriod:   s.sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       s.sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		},
		{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:   s.sub.ID,
			CustomerID:       s.sub.CustomerID,
			EntityID:         pl.ID,
			EntityType:       types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName:  pl.Name,
			PriceID:          usagePrice.ID,
			PriceType:        usagePrice.Type,
			MeterID:          mtr.ID,
			MeterDisplayName: mtr.Name,
			DisplayName:      "API Calls",
			Quantity:         decimal.Zero,
			Currency:         s.sub.Currency,
			BillingPeriod:    s.sub.BillingPeriod,
			InvoiceCadence:   types.InvoiceCadenceArrear,
			StartDate:        s.sub.StartDate,
			BaseModel:        types.GetDefaultBaseModel(ctx),
		},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems

	// Seed meter_usage: 1200 calls/day for 30 days, one bulk row per day.
	records := make([]*events.MeterUsage, 0, 30)
	for d := 0; d < 30; d++ {
		ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
		id := s.GetUUID()
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				EventName:          mtr.EventName,
				ExternalCustomerID: cust.ExternalID,
				CustomerID:         cust.ID,
				Timestamp:          ts,
				IngestedAt:         ts,
			},
			MeterID:    mtr.ID,
			QtyTotal:   decimal.NewFromInt(1200),
			UniqueHash: fmt.Sprintf("%s:%s", mtr.EventName, id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
}

func (s *RevenueRollupSuite) TestRollupSubscription_WorkedExampleReconciles() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows, "worked example must produce rows")

	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
	}
	s.Equal("530", total.String(), "Î£ net_amount must reconcile to $530")

	// Idempotent: a second rollup of the same period bumps versions in place,
	// never duplicating rows.
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows2, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Equal(len(rows), len(rows2), "idempotent recompute must not duplicate rows")

	versionByID := make(map[string]int64, len(rows))
	for _, r := range rows {
		versionByID[r.ID] = r.Version
	}
	for _, r := range rows2 {
		s.Equal(versionByID[r.ID]+1, r.Version, "recompute must bump version in place, not insert a duplicate")
	}
}

func (s *RevenueRollupSuite) TestRollupSubscription_MultiPeriodCommitmentSkipped() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	// Turn the subscription's commitment into a multi-period one â an ANNUAL
	// commitment on a MONTHLY subscription â which the rollup must skip
	// entirely rather than attribute the true-up to a single period.
	annual := types.BILLING_PERIOD_ANNUAL
	s.sub.CommitmentDuration = &annual
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, s.sub))

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Empty(rows, "multi-period commitment subscriptions must be skipped with zero rows")
}

// TestRollupSubscription_OverageStaysPeriodOnly: when usage exceeds a
// single-period commitment the engine emits a reduced usage line plus a
// separate is_overage line. The rollup keeps usage lines whole (period_only)
// and books the overage under its own revenue source, reconciling to the
// preview total.
func (s *RevenueRollupSuite) TestRollupSubscription_OverageStaysPeriodOnly() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	// seedWorkedExample already seeds 1200 calls/day (36000 calls = $360,
	// under the $500 commitment). Push another 1000 calls/day so gross usage
	// (66000 calls = $660) exceeds the commitment and the engine's per-period
	// commitment split emits an is_overage line.
	extra := make([]*events.MeterUsage, 0, 30)
	for d := 0; d < 30; d++ {
		ts := s.periodStart.AddDate(0, 0, d).Add(18 * time.Hour)
		id := s.GetUUID()
		extra = append(extra, &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				EventName:          "api_call_rollup_wk",
				ExternalCustomerID: "ext_rollup_wk",
				CustomerID:         "cust_rollup_wk",
				Timestamp:          ts,
				IngestedAt:         ts,
			},
			MeterID:    "meter_rollup_wk",
			QtyTotal:   decimal.NewFromInt(1000),
			UniqueHash: fmt.Sprintf("api_call_rollup_wk_extra:%s", id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, extra))

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows, "an overage subscription must now produce rows")

	total := decimal.Zero
	overageRows := 0
	for _, r := range rows {
		total = total.Add(r.NetAmount)
		if r.RevenueSource == types.RevenueSourceUsage {
			s.Equal(types.PeriodOnly, r.DecompositionMode, "commitment-reduced usage must stay whole, not split per day")
		}
		if r.RevenueSource == types.RevenueSourceOverage {
			overageRows++
		}
	}
	s.Equal(1, overageRows, "the engine's is_overage line must book under the overage source")

	invReq, err := NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	s.True(total.Equal(invReq.Subtotal), "Σ net_amount (%s) must reconcile to the preview subtotal (%s)", total.String(), invReq.Subtotal.String())
}

// TestRollupSubscription_BindingEntitlementLimitReconciles covers review Fix 2: with
// a REAL binding entitlement entitlementLimit (UsageLimit=20000, unlike
// seedWorkedExample's degenerate UsageLimit=0) alongside the $500 commitment
// alongside the $500 commitment, the total no longer equals $530 â the subscription-level
// commitment/true-up split runs on gross pre-entitlement usage, so the
// entitlementLimit genuinely perturbs the total.
// This asserts RECONCILIATION-BY-CONSTRUCTION instead of a hardcoded number:
// Î£ NetAmount(rows) must equal the same preview's Subtotal minus discounts,
// and zero revenue_reconciliation_mismatch logs must have fired.
func (s *RevenueRollupSuite) TestRollupSubscription_BindingEntitlementLimitReconciles() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	ent, err := s.GetStores().EntitlementRepo.Get(ctx, "ent_rollup_wk")
	s.NoError(err)
	ent.UsageLimit = lo.ToPtr(int64(20000))
	_, err = s.GetStores().EntitlementRepo.Update(ctx, ent)
	s.NoError(err)

	// Capture logs so we can assert zero revenue_reconciliation_mismatch
	// entries fired, instead of only inferring it from the row totals.
	core, observedLogs := observer.New(zapcore.InfoLevel)
	capturingLogger := logger.NewFromSugared(zap.New(core).Sugar())
	params := s.serviceParams()
	params.Logger = capturingLogger
	svc := NewRevenueService(params)

	s.NoError(svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows, "binding-entitlementLimit worked example must produce rows")

	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
	}

	// Recompute the same preview independently (RollupSubscription doesn't
	// expose the invReq it used) to get the engine's own Subtotal for this
	// exact fixture state â the reconciliation target, not a hardcoded number.
	billingSvc := NewBillingService(s.serviceParams())
	invReq, err := billingSvc.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	expected := invReq.Subtotal
	s.False(expected.Equal(decimal.NewFromInt(530)), "sanity: the binding entitlementLimit must actually perturb the total away from the zero-entitlementLimit $530 case")

	s.True(total.Equal(expected), "Î£ net_amount (%s) must reconcile to the preview's subtotal-minus-discount (%s)", total.String(), expected.String())

	for _, entry := range observedLogs.All() {
		s.NotEqual("revenue_reconciliation_mismatch", entry.Message, "unexpected reconciliation mismatch: %+v", entry.ContextMap())
	}
}

// seedFixedOnlySubscription builds a minimal customer/plan/subscription with
// a single FIXED line item â deliberately simpler than seedWorkedExample
// (no usage/meter/entitlement machinery) since TestRollupDirty_* exercises
// RollupDirty's scan/tally/error-isolation logic, not decomposition itself.
// When commitmentDuration is non-nil the subscription also carries a $500/2x
// commitment with that duration, to drive the multi-period-commitment skip.
// When createPrice is false, priceID is left dangling (never created in
// PriceRepo) so PrepareSubscriptionInvoiceRequest fails hydrating it â a
// genuine per-subscription error, not a policy skip.
func (s *RevenueRollupSuite) seedFixedOnlySubscription(
	ctx context.Context, idSuffix string, commitmentDuration *types.BillingPeriod, priceID string, createPrice bool,
) *subscription.Subscription {
	cust := &customer.Customer{
		ID:         "cust_dirty_" + idSuffix,
		ExternalID: "ext_dirty_" + idSuffix,
		Name:       "Dirty " + idSuffix,
		Email:      "dirty_" + idSuffix + "@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{ID: "plan_dirty_" + idSuffix, Name: "Dirty Plan " + idSuffix, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	if createPrice {
		fixedPrice := &price.Price{
			ID:                 priceID,
			Amount:             decimal.NewFromInt(30),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, fixedPrice))
	}

	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 30)

	sub := &subscription.Subscription{
		ID:                 "sub_dirty_" + idSuffix,
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          periodStart,
		BillingAnchor:      periodEnd,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	if commitmentDuration != nil {
		commitmentAmount := decimal.NewFromInt(500)
		overageFactor := decimal.NewFromInt(2)
		sub.CommitmentAmount = &commitmentAmount
		sub.OverageFactor = &overageFactor
		sub.CommitmentDuration = commitmentDuration
		sub.EnableTrueUp = true
	}

	lineItems := []*subscription.SubscriptionLineItem{
		{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  sub.ID,
			CustomerID:      sub.CustomerID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         priceID,
			PriceType:       types.PRICE_TYPE_FIXED,
			DisplayName:     "Fixed",
			Quantity:        decimal.NewFromInt(1),
			Currency:        sub.Currency,
			BillingPeriod:   sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub
}

// enableRevenueAnalytics opts the test tenant/environment into the rollup â
// RollupDirty only scans (tenant, environment)s carrying an enabled
// revenue_analytics_config setting.
func (s *RevenueRollupSuite) enableRevenueAnalytics(ctx context.Context) {
	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyRevenueAnalyticsConfig,
		Value:         map[string]interface{}{"enabled": true},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(ctx, setting))
}

// TestRollupDirty_TallyAndErrorIsolation: RollupDirty
// must tally a rollable subscription, a policy-skipped one (multi-period
// commitment), and one whose per-subscription error must NOT abort the
// batch â all three get scanned in one call.
func (s *RevenueRollupSuite) TestRollupDirty_TallyAndErrorIsolation() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)

	rollable := s.seedFixedOnlySubscription(ctx, "ok", nil, "price_dirty_ok", true)
	annual := types.BILLING_PERIOD_ANNUAL
	multiPeriod := s.seedFixedOnlySubscription(ctx, "skip", &annual, "price_dirty_skip", true)
	failing := s.seedFixedOnlySubscription(ctx, "err", nil, "price_dirty_missing", false)

	since := time.Now().UTC().Add(-time.Hour)

	rolled, skipped, err := s.svc.RollupDirty(ctx, since)
	s.NoError(err, "a per-subscription error must never abort the batch")
	s.Equal(1, rolled, "only the plain fixed-charge subscription should roll")
	s.Equal(2, skipped, "multi-period-commitment skip + per-subscription error both tally as skipped")

	rows, err := s.store.ListBySubscriptionPeriod(ctx, rollable.ID, rollable.CurrentPeriodStart, rollable.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows, "the rollable subscription must have actually been rolled despite another subscription's error")

	skipRows, err := s.store.ListBySubscriptionPeriod(ctx, multiPeriod.ID, multiPeriod.CurrentPeriodStart, multiPeriod.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Empty(skipRows, "the multi-period-commitment subscription must not have been rolled")

	errRows, err := s.store.ListBySubscriptionPeriod(ctx, failing.ID, failing.CurrentPeriodStart, failing.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Empty(errRows, "the failing subscription must not have written any rows")
}

// --- FINAL flip / revert / JIT (formerly revenue_rollup_final_test.go) ---

// finalFlipFixture is one PROVISIONAL fixed-revenue fact plus the matching
// finalized invoice + line item, hand-built (not via the preview engine) so
// the flip/reconcile/revert behavior is isolated from decomposition.
type finalFlipFixture struct {
	subscriptionID string
	priceID        string
	periodStart    time.Time
	periodEnd      time.Time // inclusive last day, matching revenue_facts Day bounds
	invoice        *invoice.Invoice
}

func (s *RevenueRollupSuite) seedFinalFlipFixture(ctx context.Context) *finalFlipFixture {
	periodStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := periodStart.AddDate(0, 0, 30)
	fx := &finalFlipFixture{
		subscriptionID: "sub_final_1",
		priceID:        "price_final_1",
		periodStart:    periodStart,
		periodEnd:      periodEndExclusive.AddDate(0, 0, -1),
	}

	fact := &revenuefact.RevenueFact{
		ID:             "revfact_final_1",
		CustomerID:     "cust_final_1",
		SubscriptionID: fx.subscriptionID,
		SubLineItemID:  lo.ToPtr("sli_final_1"),
		PriceID:        lo.ToPtr(fx.priceID),
		RevenueSource:  types.RevenueSourceFixed,
		PeriodStart:    fx.periodStart,
		PeriodEnd:      fx.periodEnd,
		Day:            fx.periodStart,
		NetAmount:      decimal.NewFromInt(30),
		Currency:       "usd",
		Status:         types.FactProvisional,
	}
	s.NoError(s.store.UpsertProvisional(ctx, []*revenuefact.RevenueFact{fact}))

	inv := &invoice.Invoice{
		ID:              "inv_final_1",
		CustomerID:      "cust_final_1",
		SubscriptionID:  lo.ToPtr(fx.subscriptionID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(30),
		Subtotal:        decimal.NewFromInt(30),
		TotalDiscount:   decimal.Zero,
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromInt(30),
		Description:     "Final flip test invoice",
		PeriodStart:     lo.ToPtr(fx.periodStart),
		PeriodEnd:       lo.ToPtr(periodEndExclusive),
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		BaseModel:       types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:             "li_final_1",
				InvoiceID:      "inv_final_1",
				CustomerID:     "cust_final_1",
				SubscriptionID: lo.ToPtr(fx.subscriptionID),
				PriceID:        lo.ToPtr(fx.priceID),
				Amount:         decimal.NewFromInt(30),
				Quantity:       decimal.NewFromInt(1),
				Currency:       "usd",
				PeriodStart:    lo.ToPtr(fx.periodStart),
				PeriodEnd:      lo.ToPtr(periodEndExclusive),
				BaseModel:      types.GetDefaultBaseModel(ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	fx.invoice = inv
	return fx
}

func (s *RevenueRollupSuite) TestFinalizeSubscriptionPeriod_FlipsAndStamps() {
	fx := s.seedFinalFlipFixture(s.ctx)

	s.NoError(s.svc.FinalizeSubscriptionPeriod(s.ctx, fx.invoice.ID))

	rows, err := s.store.ListBySubscriptionPeriod(s.ctx, fx.subscriptionID, fx.periodStart, fx.periodEnd, types.FactFinal)
	s.NoError(err)
	s.NotEmpty(rows)
	s.Equal(fx.invoice.ID, lo.FromPtr(rows[0].InvoiceID))
	s.Equal("li_final_1", lo.FromPtr(rows[0].InvoiceLineItemID))
	s.Equal(types.FactFinal, rows[0].Status)

	// No more PROVISIONAL rows left for this grain â the flip moved them, it
	// did not duplicate them.
	provisional, err := s.store.ListBySubscriptionPeriod(s.ctx, fx.subscriptionID, fx.periodStart, fx.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Empty(provisional)

	// Sum reconciles against the invoice's Subtotal - TotalDiscount.
	sum := decimal.Zero
	for _, r := range rows {
		sum = sum.Add(r.NetAmount)
	}
	s.True(sum.Equal(fx.invoice.Subtotal.Sub(fx.invoice.TotalDiscount)))
}

// TestFinalizeSubscriptionPeriod_TrueupRowMatchesBySyntheticPriceID verifies a
// commitment-trueup provisional row written under the stable synthetic
// price_id ("trueup:"+subLineItemID) is still found and stamped when the
// finalized line item carries a completely different (fresh random) price_id,
// exactly as the real billing engine assigns â the flip re-derives the same
// synthetic id from metadata + sub_line_item_id.
func (s *RevenueRollupSuite) TestFinalizeSubscriptionPeriod_TrueupRowMatchesBySyntheticPriceID() {
	subscriptionID := "sub_final_trueup"
	periodStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := periodStart.AddDate(0, 0, 30)
	periodEnd := periodEndExclusive.AddDate(0, 0, -1)
	subLineItemID := "sli_final_trueup"
	syntheticPriceID := stableTrueupPriceID(subLineItemID, subscriptionID, false)

	fact := &revenuefact.RevenueFact{
		ID:             "revfact_final_trueup",
		CustomerID:     "cust_final_1",
		SubscriptionID: subscriptionID,
		SubLineItemID:  lo.ToPtr(subLineItemID),
		PriceID:        lo.ToPtr(syntheticPriceID),
		RevenueSource:  types.RevenueSourceCommitmentTrueup,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Day:            periodEnd,
		NetAmount:      decimal.NewFromInt(50),
		Currency:       "usd",
		Status:         types.FactProvisional,
	}
	s.NoError(s.store.UpsertProvisional(s.ctx, []*revenuefact.RevenueFact{fact}))

	inv := &invoice.Invoice{
		ID:              "inv_final_trueup",
		CustomerID:      "cust_final_1",
		SubscriptionID:  lo.ToPtr(subscriptionID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(50),
		Subtotal:        decimal.NewFromInt(50),
		TotalDiscount:   decimal.Zero,
		AmountRemaining: decimal.NewFromInt(50),
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		BaseModel:       types.GetDefaultBaseModel(s.ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:                     "li_final_trueup",
				InvoiceID:              "inv_final_trueup",
				CustomerID:             "cust_final_1",
				SubscriptionID:         lo.ToPtr(subscriptionID),
				SubscriptionLineItemID: lo.ToPtr(subLineItemID),
				// The billing engine assigns a fresh random price_id to
				// trueup/overage lines on every compute â deliberately NOT the
				// synthetic id the provisional row was written under.
				PriceID:     lo.ToPtr(types.GenerateUUIDWithPrefix("price")),
				Amount:      decimal.NewFromInt(50),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				PeriodStart: lo.ToPtr(periodStart),
				PeriodEnd:   lo.ToPtr(periodEndExclusive),
				Metadata:    types.Metadata{types.MetadataKeyIsCommitmentTrueup: types.MetadataValueTrue},
				BaseModel:   types.GetDefaultBaseModel(s.ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.ctx, inv))

	s.NoError(s.svc.FinalizeSubscriptionPeriod(s.ctx, inv.ID))

	rows, err := s.store.ListBySubscriptionPeriod(s.ctx, subscriptionID, periodStart, periodEnd, types.FactFinal)
	s.NoError(err)
	s.NotEmpty(rows)
	s.Equal(inv.ID, lo.FromPtr(rows[0].InvoiceID))
	s.Equal("li_final_trueup", lo.FromPtr(rows[0].InvoiceLineItemID))
}

// failingRevenueFactRepo wraps the in-memory store so FlipToFinal always
// errors â proving FinalizeSubscriptionPeriod propagates the error to its
// (async) caller instead of swallowing it.
type failingRevenueFactRepo struct {
	*testutil.InMemoryRevenueFactStore
	Called chan struct{}
}

func (f *failingRevenueFactRepo) FlipToFinal(ctx context.Context, subscriptionID, priceID string, periodStart, periodEnd time.Time, invoiceID, invoiceLineItemID string) (int, error) {
	if f.Called != nil {
		select {
		case f.Called <- struct{}{}:
		default:
		}
	}
	return 0, ierr.NewError("forced flip failure").Mark(ierr.ErrDatabase)
}

func (s *RevenueRollupSuite) TestFinalizeSubscriptionPeriod_FlipErrorIsReturnedToCaller() {
	fx := s.seedFinalFlipFixture(s.ctx)

	params := s.serviceParams()
	params.RevenueFactRepo = &failingRevenueFactRepo{InMemoryRevenueFactStore: s.store}
	failingSvc := NewRevenueService(params)

	s.Error(failingSvc.FinalizeSubscriptionPeriod(s.ctx, fx.invoice.ID))
}

// TestRevertInvoiceFacts covers the voided-invoice guardrail: FINAL rows are
// immutable, so a void posts contra rows (negated amounts, is_revert=true)
// stamped with the same invoice â and the whole period then nets to zero.
// A second call must be a no-op (the async void hook can retry).
func (s *RevenueRollupSuite) TestRevertInvoiceFacts() {
	fx := s.seedFinalFlipFixture(s.ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(s.ctx, fx.invoice.ID))

	s.NoError(s.svc.RevertInvoiceFacts(s.ctx, fx.invoice.ID))

	rows, err := s.store.ListBySubscriptionPeriod(s.ctx, fx.subscriptionID, fx.periodStart, fx.periodEnd, types.FactFinal)
	s.NoError(err)
	s.Len(rows, 2, "one original + one contra row")

	total := decimal.Zero
	revertSeen := false
	for _, r := range rows {
		total = total.Add(r.NetAmount)
		if r.IsRevert {
			revertSeen = true
			s.Equal(fx.invoice.ID, lo.FromPtr(r.InvoiceID), "contra row keeps the voided invoice stamp")
		}
	}
	s.True(revertSeen, "a contra row must exist")
	s.True(total.IsZero(), "original + revert must net to zero")

	// Idempotent: a retried revert adds nothing.
	s.NoError(s.svc.RevertInvoiceFacts(s.ctx, fx.invoice.ID))
	rows2, err := s.store.ListBySubscriptionPeriod(s.ctx, fx.subscriptionID, fx.periodStart, fx.periodEnd, types.FactFinal)
	s.NoError(err)
	s.Len(rows2, 2, "revert is idempotent")
}

// TestFinalizeSubscriptionPeriod_JITRollupWhenNoProvisionalRows covers the
// re-issued-invoice guardrail: an invoice finalized when NO provisional rows
// exist for its period (re-drafted after a void, or a period the schedule
// never covered) must trigger a just-in-time rollup of that period and then
// flip, so the new invoice still gets FINAL facts.
func (s *RevenueRollupSuite) TestFinalizeSubscriptionPeriod_JITRollupWhenNoProvisionalRows() {
	ctx := s.ctx
	sub := s.seedFixedOnlySubscription(ctx, "jit", nil, "price_dirty_jit", true)

	inv := &invoice.Invoice{
		ID:              "inv_jit_1",
		CustomerID:      sub.CustomerID,
		SubscriptionID:  lo.ToPtr(sub.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(30),
		Subtotal:        decimal.NewFromInt(30),
		TotalDiscount:   decimal.Zero,
		AmountRemaining: decimal.NewFromInt(30),
		PeriodStart:     lo.ToPtr(sub.CurrentPeriodStart),
		PeriodEnd:       lo.ToPtr(sub.CurrentPeriodEnd),
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		BaseModel:       types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:             "li_jit_1",
				InvoiceID:      "inv_jit_1",
				CustomerID:     sub.CustomerID,
				SubscriptionID: lo.ToPtr(sub.ID),
				PriceType:      lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				PriceID:        lo.ToPtr("price_dirty_jit"),
				Amount:         decimal.NewFromInt(30),
				Quantity:       decimal.NewFromInt(1),
				Currency:       "usd",
				PeriodStart:    lo.ToPtr(sub.CurrentPeriodStart),
				PeriodEnd:      lo.ToPtr(sub.CurrentPeriodEnd),
				BaseModel:      types.GetDefaultBaseModel(ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))

	// Precondition: no provisional rows exist for this period.
	pre, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, types.FactProvisional)
	s.NoError(err)
	s.Empty(pre)

	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, -1), types.FactFinal)
	s.NoError(err)
	s.NotEmpty(rows, "JIT rollup must have produced rows the flip then stamped")
	s.Equal(inv.ID, lo.FromPtr(rows[0].InvoiceID))
}

// TestRollupDirty_SkipsTenantsWithoutSetting proves the tenant gate: with no
// enabled revenue_analytics_config for the tenant/environment, RollupDirty
// scans nothing even though rollable subscriptions exist.
func (s *RevenueRollupSuite) TestRollupDirty_SkipsTenantsWithoutSetting() {
	ctx := s.ctx
	sub := s.seedFixedOnlySubscription(ctx, "ungated", nil, "price_dirty_ungated", true)

	rolled, skipped, err := s.svc.RollupDirty(ctx, time.Now().UTC().Add(-time.Hour))
	s.NoError(err)
	s.Zero(rolled)
	s.Zero(skipped)

	rows, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Empty(rows, "an un-opted-in tenant must produce no rows")
}

// TestRollupSubscription_GrantEntitledUsageSplitsPerDay: an uncrossed
// feature-scoped grant zeroes the bill, and the rollup still shows the
// entitled usage day by day — marginal rows with zero net and the full daily
// quantity recorded as entitled.
func (s *RevenueRollupSuite) TestRollupSubscription_GrantEntitledUsageSplitsPerDay() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	grant := &entitlementgrant.EntitlementGrant{
		ID:                  "eg_rollup_wk",
		EntitlementConfigID: "ec_rollup_wk",
		CustomerID:          "cust_rollup_wk",
		SubscriptionID:      s.sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_rollup_wk",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100000),
		ValidFrom:           s.periodStart,
		ValidTo:             s.periodEnd,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementGrantRepo.Create(ctx, grant)
	s.NoError(err)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	usageRows := 0
	for _, r := range rows {
		if r.RevenueSource != types.RevenueSourceUsage {
			continue
		}
		usageRows++
		s.Equal(types.Marginal, r.DecompositionMode, "grant-billed usage must split per day")
		s.True(r.NetAmount.IsZero(), "uncrossed grant bills nothing")
		s.Equal("1200", r.EntitlementQty.String(), "each day must record its entitled quantity")
		if residual, identityOK := reconcileRow(r); s.True(identityOK, "row identity must hold") {
			_ = residual
		}
	}
	s.Equal(30, usageRows, "one marginal row per day of the period")
}

// TestRollupSubscription_GrantOverageSplitsPerDay: a quota-crossed grant
// bills usage inside its overage window; days before the crossing stay
// entitled at zero net, days after carry the billed amount, and the rows sum
// exactly to the engine charge.
func (s *RevenueRollupSuite) TestRollupSubscription_GrantOverageSplitsPerDay() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	// Quota crossed at day 10: the remaining 20 days x 1200 calls fall in the
	// overage window. Snapshot usage matches (36000 total - 12000 quota).
	crossedAt := s.periodStart.AddDate(0, 0, 10)
	grant := &entitlementgrant.EntitlementGrant{
		ID:                  "eg_rollup_crossed",
		EntitlementConfigID: "ec_rollup_wk",
		CustomerID:          "cust_rollup_wk",
		SubscriptionID:      s.sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_rollup_wk",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(12000),
		Usage:               decimal.NewFromInt(36000),
		QuotaCrossedAt:      &crossedAt,
		ValidFrom:           s.periodStart,
		ValidTo:             s.periodEnd,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementGrantRepo.Create(ctx, grant)
	s.NoError(err)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	usageTotal := decimal.Zero
	entitledDays, billedDays := 0, 0
	for _, r := range rows {
		if r.RevenueSource != types.RevenueSourceUsage {
			continue
		}
		s.Equal(types.Marginal, r.DecompositionMode)
		usageTotal = usageTotal.Add(r.NetAmount)
		if r.NetAmount.IsZero() {
			entitledDays++
		} else {
			billedDays++
			s.Equal("12", r.NetAmount.String(), "billed days charge 1200 calls at $0.01")
		}
		if residual, identityOK := reconcileRow(r); !identityOK {
			s.Failf("row identity broken", "day %s residual %s", r.Day, residual.String())
		}
	}
	s.Equal(10, entitledDays, "days before the quota crossing stay entitled")
	s.Equal(20, billedDays, "days after the crossing are billed")
	s.Equal("240", usageTotal.String(), "usage rows must sum to the engine's overage charge (24000 x $0.01)")
}

// TestRollupSubscription_CouponDiscountReconciles: a 10% subscription-level
// coupon no longer skips the rollup — discounts are dry-run, spread onto rows
// (line_discount/invoice_discount), and Σ net_amount reconciles to
// Subtotal - discount with zero mismatch logs.
func (s *RevenueRollupSuite) TestRollupSubscription_CouponDiscountReconciles() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	c := &coupon.Coupon{
		ID:            "coupon_rollup_wk",
		Name:          "10% off",
		Type:          types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromInt(10)),
		Cadence:       types.CouponCadenceForever,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, &coupon_association.CouponAssociation{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:       c.ID,
		SubscriptionID: s.sub.ID,
		StartDate:      s.periodStart,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		Coupon:         c,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}))

	core, observedLogs := observer.New(zapcore.InfoLevel)
	params := s.serviceParams()
	params.Logger = logger.NewFromSugared(zap.New(core).Sugar())
	svc := NewRevenueService(params)

	s.NoError(svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows, "a discounted subscription must now produce rows")

	total := decimal.Zero
	discountTotal := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
		discountTotal = discountTotal.Add(r.LineDiscount).Add(r.InvoiceDiscount)
	}
	s.True(discountTotal.IsPositive(), "the coupon discount must land on the rows")

	invReq, err := NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	expected := invReq.Subtotal.Sub(discountTotal)
	s.True(total.Equal(expected), "Σ net_amount (%s) must equal subtotal (%s) minus discounts (%s)",
		total.String(), invReq.Subtotal.String(), discountTotal.String())

	for _, entry := range observedLogs.All() {
		s.NotEqual("revenue_reconciliation_mismatch", entry.Message, "unexpected mismatch: %+v", entry.ContextMap())
	}
}
