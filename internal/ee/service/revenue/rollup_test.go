package revenue

import (
	"context"
	"fmt"
	"github.com/flexprice/flexprice/internal/ee/service"
	"sort"
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
	"github.com/stretchr/testify/assert"
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
	svc   Service
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
	s.WithEnvironment("env_rollup")
	s.ctx = s.GetContext()
	s.store = s.GetStores().RevenueFactRepo.(*testutil.InMemoryRevenueFactStore)
	s.svc = New(s.serviceParams())
}

func (s *RevenueRollupSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *RevenueRollupSuite) serviceParams() service.ServiceParams {
	stores := s.GetStores()
	return service.ServiceParams{
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

	// Seed meter_usage: 1200 calls/day for 30 days, one bulk row per day,
	// alternating event source per day (source-grouped analytics rely on it).
	records := make([]*events.MeterUsage, 0, 30)
	for d := 0; d < 30; d++ {
		ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
		id := s.GetUUID()
		source := "api"
		if d%2 == 1 {
			source = "sdk"
		}
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
				Source:             source,
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
	s.enableRevenueAnalytics(ctx)
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

	// Idempotent, and now also inert: a recompute that produces the same values
	// writes nothing at all. Re-upserting identical rows every night was the
	// bulk of the write volume on subscriptions with many line items.
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows2, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Equal(len(rows), len(rows2), "idempotent recompute must not duplicate rows")

	versionByID := make(map[string]int64, len(rows))
	for _, r := range rows {
		versionByID[r.ID] = r.Version
	}
	for _, r := range rows2 {
		s.Equal(versionByID[r.ID], r.Version, "an unchanged recompute must not rewrite the row")
	}

	// A real change still lands: more usage on the last day moves that row and
	// bumps its version, while the untouched days stay put.
	lastDay := s.periodStart.AddDate(0, 0, 29).Add(6 * time.Hour)
	id := s.GetUUID()
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			EventName: "call_rollup_wk", ExternalCustomerID: "ext_rollup_wk",
			Timestamp: lastDay, IngestedAt: lastDay,
		},
		MeterID: "meter_rollup_wk", QtyTotal: decimal.NewFromInt(5000), UniqueHash: "wk_extra:" + id,
	}}))

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows3, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	bumped := 0
	for _, r := range rows3 {
		if prior, ok := versionByID[r.ID]; ok && r.Version > prior {
			bumped++
		}
	}
	s.NotZero(bumped, "a day whose usage changed must be rewritten")
	s.Less(bumped, len(rows), "days whose usage did not change must be left alone")
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

// TestRollupSubscription_OverageSplitsPerDay: when usage exceeds the
// commitment, the engine pairs each usage line with an overage line. Both
// halves now split per day around the commitment boundary: days until the
// within-commitment amount is reached bill as usage, later days as overage,
// and each half sums exactly to its engine line.
func (s *RevenueRollupSuite) TestRollupSubscription_OverageSplitsPerDay() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	s.seedWorkedExample(ctx)

	// seedWorkedExample seeds 1200 calls/day ($360 gross, under the $500
	// commitment). Add 1000/day so gross usage ($660) exceeds it: the engine
	// bills $500 within commitment and 16000 calls x $0.01 x 2 = $320 overage.
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

	core, observedLogs := observer.New(zapcore.InfoLevel)
	params := s.serviceParams()
	params.Logger = logger.NewFromSugared(zap.New(core).Sugar())
	svc := New(params)

	s.NoError(svc.RollupSubscription(ctx, s.sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	usageTotal, overageTotal := decimal.Zero, decimal.Zero
	usageDays, overageDays, billedOverageDays := 0, 0, 0
	for _, r := range rows {
		switch r.RevenueSource {
		case types.RevenueSourceUsage:
			s.Equal(types.Marginal, r.DecompositionMode, "usage half must split per day")
			usageTotal = usageTotal.Add(r.NetAmount)
			usageDays++
			if residual, identityOK := reconcileRow(r); !identityOK {
				s.Failf("row identity broken", "day %s residual %s", r.Day, residual.String())
			}
		case types.RevenueSourceOverage:
			s.Equal(types.Marginal, r.DecompositionMode, "overage half must split per day")
			overageTotal = overageTotal.Add(r.NetAmount)
			overageDays++
			if r.NetAmount.IsPositive() {
				billedOverageDays++
			}
		}
	}
	// Rows exist for the days that carry something, not for every day of the
	// period: a day whose half of the split is zero records nothing, and
	// writing one per line item per day is what made the table 99.99% empty.
	// The overage half therefore starts at the crossing rather than opening
	// with a run of zero days.
	s.Positive(usageDays, "the usage half must still split per day")
	s.LessOrEqual(usageDays, 30)
	s.Positive(overageDays, "the overage half must still split per day")
	s.Less(overageDays, usageDays, "overage begins only once the commitment is crossed")
	s.Equal("500", usageTotal.String(), "usage half must sum to the within-commitment amount")
	s.Equal("320", overageTotal.String(), "overage half must sum to the engine's overage line")
	s.Equal(overageDays, billedOverageDays, "every overage row written must carry overage")

	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
	}
	s.True(total.Equal(invReq.Subtotal), "all rows must reconcile to the preview subtotal")

	for _, entry := range observedLogs.All() {
		s.NotEqual("revenue_reconciliation_mismatch", entry.Message, "unexpected mismatch: %+v", entry.ContextMap())
	}
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
	s.enableRevenueAnalytics(ctx)
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
	svc := New(params)

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
	billingSvc := service.NewBillingService(s.serviceParams())
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

	res, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{Since: since})
	rolled, skipped := res.Rolled, res.Skipped
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
				ID:                     "li_final_1",
				InvoiceID:              "inv_final_1",
				CustomerID:             "cust_final_1",
				SubscriptionID:         lo.ToPtr(fx.subscriptionID),
				SubscriptionLineItemID: lo.ToPtr("sli_final_1"),
				PriceID:                lo.ToPtr(fx.priceID),
				Amount:                 decimal.NewFromInt(30),
				Quantity:               decimal.NewFromInt(1),
				Currency:               "usd",
				PeriodStart:            lo.ToPtr(fx.periodStart),
				PeriodEnd:              lo.ToPtr(periodEndExclusive),
				BaseModel:              types.GetDefaultBaseModel(ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	fx.invoice = inv
	return fx
}

func (s *RevenueRollupSuite) TestFinalizeSubscriptionPeriod_FlipsAndStamps() {
	s.enableRevenueAnalytics(s.ctx)
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
	s.enableRevenueAnalytics(s.ctx)
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

func (f *failingRevenueFactRepo) FlipToFinal(ctx context.Context, subscriptionID, priceID, subLineItemID string, periodStart, periodEnd time.Time, invoiceID, invoiceLineItemID string) (int, error) {
	if f.Called != nil {
		select {
		case f.Called <- struct{}{}:
		default:
		}
	}
	return 0, ierr.NewError("forced flip failure").Mark(ierr.ErrDatabase)
}

func (s *RevenueRollupSuite) TestFinalizeSubscriptionPeriod_FlipErrorIsReturnedToCaller() {
	s.enableRevenueAnalytics(s.ctx)
	fx := s.seedFinalFlipFixture(s.ctx)

	params := s.serviceParams()
	params.RevenueFactRepo = &failingRevenueFactRepo{InMemoryRevenueFactStore: s.store}
	failingSvc := New(params)

	s.Error(failingSvc.FinalizeSubscriptionPeriod(s.ctx, fx.invoice.ID))
}

// TestRevertInvoiceFacts covers the voided-invoice guardrail: FINAL rows are
// immutable, so a void posts contra rows (negated amounts, is_revert=true)
// stamped with the same invoice â and the whole period then nets to zero.
// A second call must be a no-op (the async void hook can retry).
func (s *RevenueRollupSuite) TestRevertInvoiceFacts() {
	s.enableRevenueAnalytics(s.ctx)
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
	s.enableRevenueAnalytics(ctx)
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

	res, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{Since: time.Now().UTC().Add(-time.Hour)})
	rolled, skipped := res.Rolled, res.Skipped
	s.NoError(err)
	s.Zero(rolled)
	s.Zero(skipped)

	rows, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Empty(rows, "an un-opted-in tenant must produce no rows")
}

// TestRollupDirty_SkipsBlankEnvironmentSetting: an enabled row with no
// environment id must not scan every environment. An empty context
// environment makes the subscription list match all of them.
func (s *RevenueRollupSuite) TestRollupDirty_SkipsBlankEnvironmentSetting() {
	ctx := s.ctx
	sub := s.seedFixedOnlySubscription(ctx, "blankenv", nil, "price_dirty_blankenv", true)

	otherCtx := types.SetEnvironmentID(ctx, "env_other")
	other := s.seedFixedOnlySubscription(otherCtx, "blankother", nil, "price_dirty_blankother", true)

	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyRevenueAnalyticsConfig,
		Value:         map[string]interface{}{"enabled": true},
		EnvironmentID: "",
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(ctx, setting))

	res, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{Since: time.Now().UTC().Add(-time.Hour)})
	rolled, skipped := res.Rolled, res.Skipped
	s.NoError(err)
	s.Zero(rolled)
	s.Zero(skipped)

	for _, candidate := range []*subscription.Subscription{sub, other} {
		rows, listErr := s.store.ListBySubscriptionPeriod(ctx, candidate.ID, candidate.CurrentPeriodStart, candidate.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
		s.NoError(listErr)
		s.Empty(rows)
		rows, listErr = s.store.ListBySubscriptionPeriod(otherCtx, candidate.ID, candidate.CurrentPeriodStart, candidate.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
		s.NoError(listErr)
		s.Empty(rows)
	}
}

// TestInvoiceHooks_StayInOptedInEnvironment: finalizing an invoice in an
// environment that has not opted in must not write revenue_facts, even when
// another environment of the same tenant has the setting on.
func (s *RevenueRollupSuite) TestInvoiceHooks_StayInOptedInEnvironment() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)

	otherCtx := types.SetEnvironmentID(ctx, "env_other")
	other := s.seedFixedOnlySubscription(otherCtx, "hookother", nil, "price_dirty_hookother", true)
	inv := &invoice.Invoice{
		ID:              "inv_hook_other",
		CustomerID:      other.CustomerID,
		SubscriptionID:  lo.ToPtr(other.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(30),
		Subtotal:        decimal.NewFromInt(30),
		TotalDiscount:   decimal.Zero,
		AmountRemaining: decimal.NewFromInt(30),
		PeriodStart:     lo.ToPtr(other.CurrentPeriodStart),
		PeriodEnd:       lo.ToPtr(other.CurrentPeriodEnd),
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		BaseModel:       types.GetDefaultBaseModel(otherCtx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:             "li_hook_other",
				InvoiceID:      "inv_hook_other",
				CustomerID:     other.CustomerID,
				SubscriptionID: lo.ToPtr(other.ID),
				PriceType:      lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				PriceID:        lo.ToPtr("price_dirty_hookother"),
				Amount:         decimal.NewFromInt(30),
				Quantity:       decimal.NewFromInt(1),
				Currency:       "usd",
				PeriodStart:    lo.ToPtr(other.CurrentPeriodStart),
				PeriodEnd:      lo.ToPtr(other.CurrentPeriodEnd),
				BaseModel:      types.GetDefaultBaseModel(otherCtx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(otherCtx, inv))

	s.NoError(s.svc.FinalizeSubscriptionPeriod(otherCtx, inv.ID))
	s.NoError(s.svc.RevertInvoiceFacts(otherCtx, inv.ID))

	rows, err := s.store.ListBySubscriptionPeriod(otherCtx, other.ID, other.CurrentPeriodStart, other.CurrentPeriodEnd, types.FactFinal)
	s.NoError(err)
	s.Empty(rows)
	provisional, err := s.store.ListBySubscriptionPeriod(otherCtx, other.ID, other.CurrentPeriodStart, other.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Empty(provisional)
}

// TestRollupSubscription_GrantEntitledUsageSplitsPerDay: an uncrossed
// feature-scoped grant zeroes the bill, and the rollup still shows the
// entitled usage day by day — marginal rows with zero net and the full daily
// quantity recorded as entitled.
func (s *RevenueRollupSuite) TestRollupSubscription_GrantEntitledUsageSplitsPerDay() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
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
	s.enableRevenueAnalytics(ctx)
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
	s.enableRevenueAnalytics(ctx)
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
	svc := New(params)

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

	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
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

// TestGetRevenueAnalytics_EventSourceGrouping: group_by "source" splits usage
// facts by the event source recorded in meter_usage (even days fire from
// "api", odd days from "sdk" — see seedWorkedExample); fixed and true-up
// amounts have no events behind them and land under the unattributed source.
func (s *RevenueRollupSuite) TestGetRevenueAnalytics_EventSourceGrouping() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)
	s.enableRevenueAnalytics(ctx)
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	req := &dto.RevenueAnalyticsRequest{
		StartTime:   s.periodStart,
		EndTime:     s.periodEnd,
		Granularity: types.RevenueGranularityTotal,
		GroupBy:     []string{"source"},
		Status:      types.FactProvisional,
	}
	_, err := s.svc.GetRevenueAnalytics(ctx, req)
	s.Error(err, "source grouping requires a customer or subscription filter")

	req.SubscriptionIDs = []string{s.sub.ID}
	res, err := s.svc.GetRevenueAnalytics(ctx, req)
	s.NoError(err)

	bySource := map[string]decimal.Decimal{}
	total := decimal.Zero
	for _, r := range res.Rows {
		bySource[r.Group["source"]] = bySource[r.Group["source"]].Add(r.NetAmount)
		total = total.Add(r.NetAmount)
	}
	s.True(total.Equal(decimal.NewFromInt(530)), "grouping never changes the total, got %s", total)
	s.True(bySource["api"].Equal(decimal.NewFromInt(180)), "15 api days x $12, got %s", bySource["api"])
	s.True(bySource["sdk"].Equal(decimal.NewFromInt(180)), "15 sdk days x $12, got %s", bySource["sdk"])
	s.True(bySource[""].Equal(decimal.NewFromInt(170)), "fixed $30 + true-up $140 unattributed, got %s", bySource[""])
}

// seedLineCommitmentSubscription builds a subscription exercising LINE-LEVEL
// commitments (no subscription-level commitment): three $0.01/call usage
// lines — A plain with $60 of usage; B with a $50 amount commitment,
// true-up enabled and NO events at all (the engine bills the full commitment
// as a folded-in true-up); C with a $30 commitment, 2x overage factor and
// $60 of usage (bills $30 within + $60 factored overage on the same line).
func (s *RevenueRollupSuite) seedLineCommitmentSubscription(ctx context.Context) {
	s.periodStart = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := s.periodStart.AddDate(0, 0, 30)
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID:         "cust_licom",
		ExternalID: "ext_licom",
		Name:       "Line Commitment Example",
		Email:      "licom@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        "plan_licom",
		Name:      "Line Commitment Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	mkMeterAndPrice := func(key string) *price.Price {
		mtr := &meter.Meter{
			ID:        "meter_licom_" + key,
			Name:      "Calls " + key,
			EventName: "call_licom_" + key,
			Aggregation: meter.Aggregation{
				Type: types.AggregationSum,
			},
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))
		p := &price.Price{
			ID:                 "price_licom_" + key,
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
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		return p
	}
	priceA := mkMeterAndPrice("a")
	priceB := mkMeterAndPrice("b")
	priceC := mkMeterAndPrice("c")

	s.sub = &subscription.Subscription{
		ID:                 "sub_licom",
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
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	mkLineItem := func(p *price.Price, commitment *decimal.Decimal, overageFactor *decimal.Decimal) *subscription.SubscriptionLineItem {
		li := &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.sub.ID,
			CustomerID:      s.sub.CustomerID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			MeterID:         p.MeterID,
			DisplayName:     "Line " + p.ID,
			Quantity:        decimal.Zero,
			Currency:        s.sub.Currency,
			BillingPeriod:   s.sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceArrear,
			StartDate:       s.sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		}
		if commitment != nil {
			li.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
			li.CommitmentAmount = commitment
			li.CommitmentOverageFactor = overageFactor
			li.CommitmentTrueUpEnabled = true
		}
		return li
	}
	lineItems := []*subscription.SubscriptionLineItem{
		mkLineItem(priceA, nil, nil),
		mkLineItem(priceB, lo.ToPtr(decimal.NewFromInt(50)), lo.ToPtr(decimal.NewFromInt(1))),
		mkLineItem(priceC, lo.ToPtr(decimal.NewFromInt(30)), lo.ToPtr(decimal.NewFromInt(2))),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems

	// 200 calls/day for 30 days on meters A and C ($60 gross each); B stays
	// silent so its commitment true-up covers the whole line.
	var records []*events.MeterUsage
	for _, key := range []string{"a", "c"} {
		for d := 0; d < 30; d++ {
			ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
			id := s.GetUUID()
			records = append(records, &events.MeterUsage{
				Event: events.Event{
					ID:                 id,
					TenantID:           types.GetTenantID(ctx),
					EnvironmentID:      types.GetEnvironmentID(ctx),
					EventName:          "call_licom_" + key,
					ExternalCustomerID: cust.ExternalID,
					CustomerID:         cust.ID,
					Timestamp:          ts,
					IngestedAt:         ts,
				},
				MeterID:    "meter_licom_" + key,
				QtyTotal:   decimal.NewFromInt(200),
				UniqueHash: fmt.Sprintf("licom_%s:%s", key, id),
			})
		}
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
}

// TestE2E_LineItemCommitment: line-level commitments fold true-up/overage into
// the usage line's own amount — the rollup must split the parts back out and
// the whole lifecycle (rollup → finalize → flip) must reconcile.
func (s *RevenueRollupSuite) TestE2E_LineItemCommitment() {
	ctx := s.ctx
	s.seedLineCommitmentSubscription(ctx)
	s.enableRevenueAnalytics(ctx)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows)

	bySource := map[types.RevenueSource]decimal.Decimal{}
	total := decimal.Zero
	var trueupRows, overageRows []*revenuefact.RevenueFact
	for _, r := range rows {
		bySource[r.RevenueSource] = bySource[r.RevenueSource].Add(r.NetAmount)
		total = total.Add(r.NetAmount)
		switch r.RevenueSource {
		case types.RevenueSourceCommitmentTrueup:
			trueupRows = append(trueupRows, r)
		case types.RevenueSourceOverage:
			overageRows = append(overageRows, r)
		}
	}

	// A $60 usage + C $30 within-commitment = $90 usage;
	// B $50 true-up (no events); C ($60-$30)×2 = $60 overage. Total $200.
	s.True(bySource[types.RevenueSourceUsage].Equal(decimal.NewFromInt(90)), "usage = A $60 + C's $30 within commitment, got %s", bySource[types.RevenueSourceUsage])
	s.True(bySource[types.RevenueSourceCommitmentTrueup].Equal(decimal.NewFromInt(50)), "B bills its full commitment as true-up, got %s", bySource[types.RevenueSourceCommitmentTrueup])
	s.True(bySource[types.RevenueSourceOverage].Equal(decimal.NewFromInt(60)), "C's factored overage, got %s", bySource[types.RevenueSourceOverage])
	s.True(total.Equal(decimal.NewFromInt(200)))

	// The true-up row keeps B's REAL price (no synthetic id — the engine
	// folded it into the line), booked whole on period end with no usage row.
	s.Len(trueupRows, 1)
	s.Equal("price_licom_b", lo.FromPtr(trueupRows[0].PriceID))
	s.Equal(types.PeriodOnly, trueupRows[0].DecompositionMode)
	for _, r := range rows {
		if lo.FromPtr(r.PriceID) == "price_licom_b" {
			s.Equal(types.RevenueSourceCommitmentTrueup, r.RevenueSource, "a silent meter books no usage rows")
		}
	}

	// C's overage splits per day at the commitment boundary on its real price.
	s.Greater(len(overageRows), 1, "overage must split per day, not book whole-period")
	for _, r := range overageRows {
		s.Equal("price_licom_c", lo.FromPtr(r.PriceID))
		s.Equal(types.Marginal, r.DecompositionMode)
	}

	// The engine preview agrees with the decomposition.
	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	s.True(total.Equal(invReq.Subtotal), "rows must reconcile to the engine preview")

	// Finalize: the flip must consume every provisional row — the parts share
	// their line's real price, so one FlipToFinal per line covers all sources.
	inv := s.finalizeCurrentPreview(ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))
	remaining, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Empty(remaining, "the flip must consume every provisional row")
	booked, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	bookedTotal := decimal.Zero
	for _, r := range booked {
		bookedTotal = bookedTotal.Add(r.NetAmount)
	}
	s.True(bookedTotal.Equal(inv.Subtotal.Sub(inv.TotalDiscount)), "FINAL rows must sum to the invoice")
}

// TestGetRevenueAnalytics_EventSourceGroupingSpansWholeDays: a request whose
// bounds fall inside a day must still attribute that day's usage. Facts are
// day-grained, so reading the source shares over the raw clock bounds left
// edge-day facts unattributed (seen in staging: events at 16:22 with a
// request ending 15:13 returned source "").
func (s *RevenueRollupSuite) TestGetRevenueAnalytics_EventSourceGroupingSpansWholeDays() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)
	s.enableRevenueAnalytics(ctx)
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))

	// seedWorkedExample fires every event at 12:00; bound the request at 09:00
	// on both edges so each edge day's events sit outside the raw window.
	req := &dto.RevenueAnalyticsRequest{
		StartTime:          s.periodStart.Add(9 * time.Hour),
		EndTime:            s.periodEnd.AddDate(0, 0, -1).Add(9 * time.Hour),
		Granularity:        types.RevenueGranularityTotal,
		GroupBy:            []string{"source"},
		Status:             types.FactProvisional,
		SubscriptionIDs:    []string{s.sub.ID},
		IncludeAdjustments: true,
	}
	res, err := s.svc.GetRevenueAnalytics(ctx, req)
	s.NoError(err)

	bySource := map[string]decimal.Decimal{}
	for _, r := range res.Rows {
		bySource[r.Group["source"]] = bySource[r.Group["source"]].Add(r.NetAmount)
	}
	s.True(bySource["api"].Equal(decimal.NewFromInt(180)), "api usage stays attributed, got %s", bySource["api"])
	s.True(bySource["sdk"].Equal(decimal.NewFromInt(180)), "sdk usage stays attributed, got %s", bySource["sdk"])

	// Only charges with no events behind them (the fixed fee, the true-up)
	// stay unattributed — never plain usage.
	for _, r := range res.Rows {
		if r.Group["source"] == "" {
			s.NotEqual(string(types.RevenueSourceUsage), r.Group["revenue_source"],
				"usage revenue must never land unattributed when events exist")
		}
	}

	// The response says what it covers, so a row set reads on its own.
	s.NotNil(res.Query)
	s.Equal([]string{s.sub.ID}, res.Query.SubscriptionIDs)

	// end_time is optional: it defaults to now instead of failing validation.
	open := &dto.RevenueAnalyticsRequest{StartTime: time.Now().UTC().AddDate(0, 0, -7)}
	s.NoError(open.Validate())
	s.False(open.EndTime.IsZero(), "end_time must default to now")
}

// TestWriteEntryPointsRequireOptIn walks every write method on the service
// interface against an environment that has not opted in. Each must be a
// silent no-op: an environment with the setting off is not merely unscanned
// by the batch jobs, it must never accumulate a row by any route.
func (s *RevenueRollupSuite) TestWriteEntryPointsRequireOptIn() {
	ctx := s.ctx
	sub := s.seedFixedOnlySubscription(ctx, "optin", nil, "price_dirty_optin", true)

	inv := &invoice.Invoice{
		ID:              "inv_optin",
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
		LineItems: []*invoice.InvoiceLineItem{{
			ID:             "li_optin",
			InvoiceID:      "inv_optin",
			CustomerID:     sub.CustomerID,
			SubscriptionID: lo.ToPtr(sub.ID),
			PriceType:      lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
			PriceID:        lo.ToPtr("price_dirty_optin"),
			Amount:         decimal.NewFromInt(30),
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			PeriodStart:    lo.ToPtr(sub.CurrentPeriodStart),
			PeriodEnd:      lo.ToPtr(sub.CurrentPeriodEnd),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))

	writes := map[string]func() error{
		"RollupSubscription": func() error { return s.svc.RollupSubscription(ctx, sub.ID) },
		"RollupDirty": func() error {
			_, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{Since: time.Now().UTC().Add(-time.Hour)})
			return err
		},
		"FinalizeSubscriptionPeriod": func() error { return s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID) },
		"RevertInvoiceFacts":         func() error { return s.svc.RevertInvoiceFacts(ctx, inv.ID) },
		"ReconcileBookedInvoices": func() error {
			_, _, _, err := s.svc.ReconcileBookedInvoices(ctx, time.Now().UTC().Add(-time.Hour))
			return err
		},
	}

	for name, write := range writes {
		s.Run(name, func() {
			s.NoError(write(), "an un-opted-in environment must be a no-op, not an error")

			for _, status := range []types.FactStatus{types.FactProvisional, types.FactFinal} {
				rows, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID,
					sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), status)
				s.NoError(err)
				s.Empty(rows, "%s wrote %s rows into an un-opted-in environment", name, status)
			}
		})
	}

	// Same calls, now opted in: proves the assertions above are about the
	// gate and not about a fixture that could never produce rows.
	s.enableRevenueAnalytics(ctx)
	s.NoError(s.svc.RollupSubscription(ctx, sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows, "the same call must write once the environment opts in")
}

// TestJITRollupStampsSourceInvoice: rows derived from a finalized invoice
// carry that invoice's ids while still PROVISIONAL, so a row that never flips
// stays traceable to where it came from. The drift sweep must still treat them
// as unbooked — otherwise an invoice whose rows never flipped would look
// reconciled and the sweep would go blind to exactly that failure.
func (s *RevenueRollupSuite) TestJITRollupStampsSourceInvoice() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	sub := s.seedFixedOnlySubscription(ctx, "stamp", nil, "price_dirty_stamp", true)

	inv := &invoice.Invoice{
		ID:              "inv_stamp",
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
		LineItems: []*invoice.InvoiceLineItem{{
			ID:             "li_stamp",
			InvoiceID:      "inv_stamp",
			CustomerID:     sub.CustomerID,
			SubscriptionID: lo.ToPtr(sub.ID),
			PriceType:      lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
			PriceID:        lo.ToPtr("price_dirty_stamp"),
			Amount:         decimal.NewFromInt(30),
			Quantity:       decimal.NewFromInt(1),
			Currency:       "usd",
			PeriodStart:    lo.ToPtr(sub.CurrentPeriodStart),
			PeriodEnd:      lo.ToPtr(sub.CurrentPeriodEnd),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))

	s.NoError(s.svc.(*revenueService).rollupFromInvoice(ctx, inv))

	provisional, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(provisional)
	for _, r := range provisional {
		s.Equal(inv.ID, lo.FromPtr(r.InvoiceID), "a JIT row must name its source invoice")
		s.Equal("li_stamp", lo.FromPtr(r.InvoiceLineItemID))
	}

	// Stamped but not booked: the sweep's booked-row view must stay empty, or
	// it would score this invoice as reconciled while nothing has flipped.
	booked, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Empty(booked, "provisional rows must not count as booked revenue")

	// Once they flip they are booked, under the same ids.
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))
	booked, err = s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.NotEmpty(booked)
	for _, r := range booked {
		s.Equal(types.FactFinal, r.Status)
		s.Equal(inv.ID, lo.FromPtr(r.InvoiceID))
	}
}

// countingMeterUsageRepo records how many usage reads a rollup issues.
type countingMeterUsageRepo struct {
	events.MeterUsageRepository
	calls      int
	metersSeen []int
}

func (c *countingMeterUsageRepo) GetDailyUsageByMeter(ctx context.Context, params *events.DailyUsageParams) (map[string][]events.DailyUsagePoint, error) {
	c.calls++
	c.metersSeen = append(c.metersSeen, len(params.MeterIDs))
	return c.MeterUsageRepository.GetDailyUsageByMeter(ctx, params)
}

// TestRollupSubscription_ReadsUsageOncePerSubscription: the rollup must read
// usage once for the whole subscription, not once per line item. Production
// subscriptions carry hundreds of line items over a handful of meters, and the
// per-line-item read was ~1.23M serial ClickHouse round-trips per pass — the
// reason a full run took hours and never finished inside the activity timeout.
func (s *RevenueRollupSuite) TestRollupSubscription_ReadsUsageOncePerSubscription() {
	ctx := s.ctx
	s.seedLineCommitmentSubscription(ctx)
	s.enableRevenueAnalytics(ctx)

	counter := &countingMeterUsageRepo{MeterUsageRepository: s.GetStores().MeterUsageRepo}
	params := s.serviceParams()
	params.MeterUsageRepo = counter
	svc := New(params)

	s.NoError(svc.RollupSubscription(ctx, s.sub.ID))

	s.Equal(1, counter.calls, "one read for the whole subscription, not one per line item")
	s.Equal([]int{3}, counter.metersSeen, "all three meters must go out in that single read")

	// The batching must not change what gets written: same rows as the
	// per-line-item path produced.
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows)
	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
	}
	s.True(total.IsPositive(), "batched reads must still produce priced rows, got %s", total)
}

// TestBuildUsageCurve_AccumulatesFromItsOwnPeriodStart: the shared read returns
// per-day quantities, so a line item that starts mid-period must count only its
// own days. Accumulating in the repository instead pinned every caller to one
// window start, which is what forced a read per line item.
func (s *RevenueRollupSuite) TestBuildUsageCurve_AccumulatesFromItsOwnPeriodStart() {
	ctx := s.ctx
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	usage := []events.DailyUsagePoint{
		{Day: start, Qty: decimal.NewFromInt(100)},
		{Day: start.AddDate(0, 0, 1), Qty: decimal.NewFromInt(200)},
		{Day: start.AddDate(0, 0, 2), Qty: decimal.NewFromInt(300)},
	}

	svc := New(s.serviceParams()).(*revenueService)
	late, err := svc.buildUsageCurve(ctx, usageCurveInput{
		Price:       flatSum(s.T()),
		MeterID:     "meter_shared",
		PeriodStart: start.AddDate(0, 0, 1),
		PeriodEnd:   start.AddDate(0, 0, 3),
		Usage:       usage,
		AsOf:        start.AddDate(0, 0, 3),
	})
	s.NoError(err)
	s.Len(late, 2, "a line item starting on day 2 covers two days")
	s.True(late[len(late)-1].CumulativeGrossQty.Equal(decimal.NewFromInt(500)),
		"day 1's 100 belongs to an earlier window, got %s", late[len(late)-1].CumulativeGrossQty)

	full, err := svc.buildUsageCurve(ctx, usageCurveInput{
		Price:       flatSum(s.T()),
		MeterID:     "meter_shared",
		PeriodStart: start,
		PeriodEnd:   start.AddDate(0, 0, 3),
		Usage:       usage,
		AsOf:        start.AddDate(0, 0, 3),
	})
	s.NoError(err)
	s.True(full[len(full)-1].CumulativeGrossQty.Equal(decimal.NewFromInt(600)),
		"the same read serves the full window too, got %s", full[len(full)-1].CumulativeGrossQty)
}

// TestRollupDirty_ResumesFromCursor: a retried pass must continue after the
// last completed subscription, not restart at the head. Before this, all three
// activity attempts rewrote the same first slice of the list and the tail was
// never rolled at all.
func (s *RevenueRollupSuite) TestRollupDirty_ResumesFromCursor() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)

	// Ids are keyset-ordered, so "a" < "b" < "c".
	for _, key := range []string{"a", "b", "c"} {
		s.seedFixedOnlySubscription(ctx, "cursor_"+key, nil, "price_cursor_"+key, true)
	}

	var seen []types.RollupCursor
	res, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{
		Since:      time.Now().UTC().Add(-time.Hour),
		OnProgress: func(c types.RollupCursor) { seen = append(seen, c) },
	})
	s.NoError(err)
	s.Equal(3, res.Rolled)
	s.NotEmpty(seen, "progress must be reported so the activity can heartbeat")
	s.NotNil(res.Cursor)

	// Resuming from the final cursor has nothing left to do.
	done, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{
		Since:  time.Now().UTC().Add(-time.Hour),
		Cursor: res.Cursor,
	})
	s.NoError(err)
	s.Zero(done.Rolled, "a completed pass must not re-roll from the cursor")

	// Resuming from the first subscription rolls only what follows it.
	subs, err := s.GetStores().SubscriptionRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
	})
	s.NoError(err)
	ids := lo.Map(subs, func(sub *subscription.Subscription, _ int) string { return sub.ID })
	sort.Strings(ids)

	partial, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{
		Since: time.Now().UTC().Add(-time.Hour),
		Cursor: &types.RollupCursor{
			EnvironmentID:      types.GetEnvironmentID(ctx),
			LastSubscriptionID: ids[0],
		},
	})
	s.NoError(err)
	s.Equal(len(ids)-1, partial.Rolled, "resume must skip exactly the subscriptions at or before the cursor")
}

// TestSubscriptionsAfter covers the keyset itself: an offset would shift when
// rows change under a long pass and silently skip subscriptions, which is worse
// than the restart it replaces.
func TestSubscriptionsAfter(t *testing.T) {
	subs := []*subscription.Subscription{{ID: "sub_a"}, {ID: "sub_b"}, {ID: "sub_c"}}

	assert.Len(t, subscriptionsAfter(subs, ""), 3, "no cursor starts from the top")
	assert.Len(t, subscriptionsAfter(subs, "sub_a"), 2)
	assert.Equal(t, "sub_c", subscriptionsAfter(subs, "sub_b")[0].ID)
	assert.Empty(t, subscriptionsAfter(subs, "sub_c"), "past the end has nothing left")

	// A cursor naming a subscription that no longer exists still resumes in the
	// right place — the cursor is an ordering, not a row reference.
	assert.Equal(t, "sub_c", subscriptionsAfter(subs, "sub_bb")[0].ID)

	// Filtering, not slicing: an unordered page must still be handled.
	shuffled := []*subscription.Subscription{{ID: "sub_c"}, {ID: "sub_a"}, {ID: "sub_b"}}
	assert.Len(t, subscriptionsAfter(shuffled, "sub_a"), 2, "order of the page must not matter")
}

// TestScanScope_Triggers walks every reason a subscription must be rolled.
// Usage is only one of them: getting this set wrong does not fail loudly, it
// leaves facts silently stale, which is why each trigger is pinned here rather
// than tested through a single happy path.
func TestScanScope_Triggers(t *testing.T) {
	since := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	// Well past the period-open grace, so only the trigger under test can fire.
	before := since.Add(-30 * 24 * time.Hour)
	after := since.Add(time.Hour)

	// Quiet: no usage, untouched, period opened long ago.
	quiet := func() *subscription.Subscription {
		return &subscription.Subscription{
			ID: "sub_quiet", CustomerID: "cust_quiet",
			CurrentPeriodStart: before, CurrentPeriodEnd: after.AddDate(0, 1, 0),
			BaseModel: types.BaseModel{UpdatedAt: before},
		}
	}

	scope := scanScope{
		since:     since,
		now:       since,
		customers: map[string]struct{}{"cust_busy": {}},
	}

	assert.False(t, scope.includes(quiet()), "a subscription with nothing to recompute is skipped")

	busy := quiet()
	busy.CustomerID = "cust_busy"
	assert.True(t, scope.includes(busy), "new usage must roll the subscription")

	edited := quiet()
	edited.UpdatedAt = after
	assert.True(t, scope.includes(edited), "a plan, quantity or line-item change must roll it")

	// The case usage alone would miss: a period that just opened owes its
	// fixed charges and commitment true-ups before anything is metered.
	rolled := quiet()
	rolled.CurrentPeriodStart = after
	assert.True(t, scope.includes(rolled), "a newly opened period must be rolled with no usage at all")

	// And it stays in scope for a few runs, so a failed opening roll gets
	// another chance — by the next run the period start is already behind the
	// window and no other trigger fires.
	retry := quiet()
	retry.CurrentPeriodStart = since.Add(-36 * time.Hour)
	assert.True(t, scope.includes(retry), "a recently opened period must stay in scope")

	stale := quiet()
	stale.CurrentPeriodStart = since.Add(-periodOpenGrace - time.Hour)
	assert.False(t, stale.CurrentPeriodStart.After(since), "sanity")
	assert.False(t, scope.includes(stale), "an old period with nothing to do is skipped")

	assert.True(t, scanScope{full: true}.includes(quiet()), "a full pass rolls everything")
}

// TestScanScopeFor_FailsOpen: every answer the scope cannot trust must widen to
// a full pass. A subscription wrongly included costs a read that writes
// nothing; one wrongly excluded goes stale until the next rebuild.
func (s *RevenueRollupSuite) TestScanScopeFor_FailsOpen() {
	ctx := s.ctx
	svc := New(s.serviceParams()).(*revenueService)
	req := types.RollupDirtyRequest{Since: time.Now().UTC().Add(-time.Hour)}

	s.False(svc.scanScopeFor(ctx, req).full, "the scan narrows by default")

	s.True(svc.scanScopeFor(ctx, types.RollupDirtyRequest{Since: req.Since, ForceFull: true}).full,
		"the periodic rebuild overrides the narrowing")

	// Usage that cannot be attributed to a customer makes the answer
	// incomplete, so it must not be used to narrow anything.
	ts := time.Now().UTC()
	id := s.GetUUID()
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			EventName: "orphan", Timestamp: ts, IngestedAt: ts,
		},
		MeterID: "meter_orphan", QtyTotal: decimal.NewFromInt(1), UniqueHash: "orphan:" + id,
	}}))
	s.True(svc.scanScopeFor(ctx, req).full, "usage with no customer id must widen the scan")
}

// TestRollupDirty_SkipsQuietSubscriptions: the point of the whole exercise — a
// subscription with no usage and no changes must cost nothing. Its
// already-written facts must survive untouched.
func (s *RevenueRollupSuite) TestRollupDirty_SkipsQuietSubscriptions() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	s.seedWorkedExample(ctx)

	params := s.serviceParams()
	counter := &countingMeterUsageRepo{MeterUsageRepository: s.GetStores().MeterUsageRepo}
	params.MeterUsageRepo = counter
	svc := New(params)

	// A first pass with a window that predates the seeded usage rolls it.
	res, err := svc.RollupDirty(ctx, types.RollupDirtyRequest{Since: s.periodStart.Add(-time.Hour)})
	s.NoError(err)
	s.Equal(1, res.Rolled)
	before, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(before)

	// A second pass whose window starts after all usage was ingested has
	// nothing to do: no rollup, and no usage read for that subscription.
	readsBefore := counter.calls
	quiet, err := svc.RollupDirty(ctx, types.RollupDirtyRequest{Since: time.Now().UTC().Add(time.Hour)})
	s.NoError(err)
	s.Zero(quiet.Rolled, "a quiet subscription must not be re-rolled")
	s.Equal(readsBefore, counter.calls, "a skipped subscription must cost no usage read at all")

	after, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Equal(len(before), len(after), "skipping must not disturb rows already written")
}

// TestScanScopeFor_PriceEditWidensScan: a price edit changes the amount on
// every subscription using it but bumps nothing on the subscription, so neither
// usage nor updated_at sees it.
func (s *RevenueRollupSuite) TestScanScopeFor_PriceEditWidensScan() {
	ctx := s.ctx
	s.seedWorkedExample(ctx)

	svc := New(s.serviceParams()).(*revenueService)

	// A window opening after everything was created narrows the scan.
	future := time.Now().UTC().Add(time.Hour)
	s.False(svc.scanScopeFor(ctx, types.RollupDirtyRequest{Since: future}).full)

	// Editing a price must widen it again.
	p, err := s.GetStores().PriceRepo.Get(ctx, "price_rollup_usage")
	s.NoError(err)
	p.UpdatedAt = future.Add(time.Minute)
	s.NoError(s.GetStores().PriceRepo.Update(ctx, p, false))

	s.True(svc.scanScopeFor(ctx, types.RollupDirtyRequest{Since: future}).full,
		"a price edit must widen the scan; nothing else would notice it")
}

// TestIncrementalMatchesFullRebuild is the test that guards the whole scoping
// design: whatever the triggers do, an incremental pass and a full rebuild must
// leave the same rows. A missed trigger shows up here as a difference rather
// than as silently stale production data.
func (s *RevenueRollupSuite) TestIncrementalMatchesFullRebuild() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	s.seedWorkedExample(ctx)

	snapshot := func() map[string]string {
		rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
		s.NoError(err)
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			key := fmt.Sprintf("%s|%s|%s|%s",
				lo.FromPtr(r.PriceID), lo.FromPtr(r.SubLineItemID),
				r.Day.Format("2006-01-02"), r.RevenueSource)
			out[key] = r.NetAmount.String()
		}
		return out
	}

	incSvc := New(s.serviceParams())

	// Roll incrementally, then add usage and roll again — the second pass is
	// scoped, because only this subscription's customer saw activity.
	_, err := incSvc.RollupDirty(ctx, types.RollupDirtyRequest{Since: s.periodStart.Add(-time.Hour)})
	s.NoError(err)

	ts := s.periodStart.AddDate(0, 0, 5).Add(9 * time.Hour)
	id := s.GetUUID()
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			EventName: "call_rollup_wk", ExternalCustomerID: "ext_rollup_wk", CustomerID: "cust_rollup_wk",
			Timestamp: ts, IngestedAt: time.Now().UTC(),
		},
		MeterID: "meter_rollup_wk", QtyTotal: decimal.NewFromInt(3000), UniqueHash: "inc_extra:" + id,
	}}))

	_, err = incSvc.RollupDirty(ctx, types.RollupDirtyRequest{Since: time.Now().UTC().Add(-time.Minute)})
	s.NoError(err)
	incrementalState := snapshot()

	// Now force a full rebuild over the same data. It must agree exactly.
	_, err = incSvc.RollupDirty(ctx, types.RollupDirtyRequest{
		Since:     s.periodStart.Add(-time.Hour),
		ForceFull: true,
	})
	s.NoError(err)
	fullState := snapshot()

	s.Equal(fullState, incrementalState,
		"an incremental pass must leave exactly what a full rebuild would")
}

// TestScanScope_ChildUsageRollsTheParent: a parent subscription bills its
// inherited children's usage, but the usage rows carry the CHILD's customer id.
// Matching only the parent's own customer leaves it looking quiet while its
// facts go stale — the under-scope case the package invariant forbids.
func TestScanScope_ChildUsageRollsTheParent(t *testing.T) {
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	parent := &subscription.Subscription{
		ID: "sub_parent", CustomerID: "cust_parent",
		CurrentPeriodStart: since.Add(-30 * 24 * time.Hour), CurrentPeriodEnd: since.AddDate(0, 1, 0),
		BaseModel: types.BaseModel{UpdatedAt: since.Add(-30 * 24 * time.Hour)},
	}

	// Only the child's customer shows activity.
	quiet := scanScope{
		since:     since,
		now:       since,
		customers: map[string]struct{}{"cust_child": {}},
	}
	assert.False(t, quiet.includes(parent), "sanity: the parent's own customer is not active")

	withParents := quiet
	withParents.activeParents = map[string]struct{}{"sub_parent": {}}
	assert.True(t, withParents.includes(parent), "child usage must roll the parent")
}

// TestRollupDirty_StaleCursorDoesNotSkipEverything: a cursor naming an
// environment that is gone must be ignored, not obeyed. Skipping until a match
// that never arrives walks the whole list, rolls nothing, and reports success.
func (s *RevenueRollupSuite) TestRollupDirty_StaleCursorDoesNotSkipEverything() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	s.seedFixedOnlySubscription(ctx, "stalecursor", nil, "price_stalecursor", true)

	res, err := s.svc.RollupDirty(ctx, types.RollupDirtyRequest{
		Since:  time.Now().UTC().Add(-time.Hour),
		Cursor: &types.RollupCursor{EnvironmentID: "env_that_no_longer_exists", LastSubscriptionID: "sub_zzz"},
	})
	s.NoError(err)
	s.NotZero(res.Rolled, "a cursor for a vanished environment must not silence the whole run")
}

// TestScanScope_CommitmentAccruesWithoutUsage: a windowed commitment's true-up
// fills empty windows, and the bucketed curve is clamped to today, so one more
// window becomes billable every day with no usage at all. Every other trigger
// reads that subscription as quiet — usage never arrives, nothing is edited,
// the period opened long ago, and its opening roll already wrote facts covering
// the current period. Without this trigger its accrual stops after day one.
func TestScanScope_CommitmentAccruesWithoutUsage(t *testing.T) {
	since := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	committed := &subscription.Subscription{
		ID: "sub_committed", CustomerID: "cust_committed",
		CurrentPeriodStart: since.Add(-240 * time.Hour),
		CurrentPeriodEnd:   since.AddDate(0, 1, 0),
		BaseModel:          types.BaseModel{UpdatedAt: since.Add(-240 * time.Hour)},
	}

	// Every other signal says "nothing to do".
	quiet := scanScope{
		since:     since,
		now:       since,
		customers: map[string]struct{}{},
	}
	assert.False(t, quiet.includes(committed), "sanity: no other trigger fires for this subscription")

	accruing := quiet
	accruing.accruing = map[string]struct{}{"sub_committed": {}}
	assert.True(t, accruing.includes(committed),
		"a commitment billable without usage must be rolled every pass")
}

// TestScanScopeFor_IncludesWindowedCommitments wires the probe end to end, and
// pins its precision: only a WINDOWED commitment accrues without usage. A
// non-windowed line commitment settles period_only — one row whose amount is
// fixed once usage is — so pulling it into every pass would be waste.
func (s *RevenueRollupSuite) TestScanScopeFor_IncludesWindowedCommitments() {
	ctx := s.ctx
	s.seedLineCommitmentSubscription(ctx)
	svc := New(s.serviceParams()).(*revenueService)
	req := types.RollupDirtyRequest{Since: time.Now().UTC().Add(time.Hour)}

	// The fixture's commitments are non-windowed.
	s.Empty(svc.scanScopeFor(ctx, req).accruing,
		"a non-windowed commitment does not accrue and must not be pulled in every pass")

	// Make one of them windowed.
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(ctx, s.sub)
	s.NoError(err)
	s.NotEmpty(items)
	items[0].CommitmentWindowed = true
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, items[0]))

	scope := svc.scanScopeFor(ctx, req)
	s.False(scope.full, "sanity: nothing else should widen this scan")
	s.Contains(scope.accruing, s.sub.ID, "a windowed commitment must mark its subscription as accruing")
	s.True(scope.includes(s.sub), "it must be rolled despite no new usage")
}

// TestRollupSubscription_GrantWindowsReadNothingExtra: grant-billed usage is
// shaped from the grants' quota-crossed windows. Reading each window separately
// was one round-trip per window per line item on top of the per-line-item read
// -- the same N+1 as the main curve, one level down. Every window is now sliced
// out of the subscription's single read.
func (s *RevenueRollupSuite) TestRollupSubscription_GrantWindowsReadNothingExtra() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	s.seedWorkedExample(ctx)

	grant := &entitlementgrant.EntitlementGrant{
		ID:                  "eg_grant_reads",
		EntitlementConfigID: "ec_grant_reads",
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

	counter := &countingMeterUsageRepo{MeterUsageRepository: s.GetStores().MeterUsageRepo}
	params := s.serviceParams()
	params.MeterUsageRepo = counter
	svc := New(params)

	s.NoError(svc.RollupSubscription(ctx, s.sub.ID))
	s.Equal(1, counter.calls,
		"grant windows must be sliced from the subscription's single read, not queried per window")
}

// TestRollupSubscription_ZeroRevenuePeriodStillRecords: the export contract
// says absence of rows means analytics is off or not yet computed, never that
// revenue was zero. A period whose every row carries nothing must therefore
// leave a record that it WAS computed, rather than vanishing and reading as
// uncomputed downstream.
func (s *RevenueRollupSuite) TestRollupSubscription_ZeroRevenuePeriodStillRecords() {
	ctx := s.ctx
	s.enableRevenueAnalytics(ctx)
	// A usage-only subscription with no usage at all: every row it produces
	// carries nothing.
	sub := s.seedZeroRevenueSubscription(ctx)

	s.NoError(s.svc.RollupSubscription(ctx, sub.ID))

	rows, err := s.store.ListBySubscriptionPeriod(ctx, sub.ID,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Len(rows, 1, "a zero-revenue period records exactly one row, not none and not one per day")
	s.True(rows[0].NetAmount.IsZero())

	// Re-rolling keeps it at one: the stored row makes the period computed, so
	// the remaining empty rows still have nothing to correct.
	s.NoError(s.svc.RollupSubscription(ctx, sub.ID))
	rows, err = s.store.ListBySubscriptionPeriod(ctx, sub.ID,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd.AddDate(0, 0, 1), types.FactProvisional)
	s.NoError(err)
	s.Len(rows, 1, "the marker must not multiply across runs")
}

// seedZeroRevenueSubscription builds a usage-only subscription with no usage at
// all: every row it can produce carries nothing.
func (s *RevenueRollupSuite) seedZeroRevenueSubscription(ctx context.Context) *subscription.Subscription {
	cust := &customer.Customer{
		ID: "cust_zero", ExternalID: "ext_zero", Name: "Zero",
		Email: "zero@example.com", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_zero", Name: "Zero Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	mtr := &meter.Meter{
		ID: "meter_zero", Name: "Zero", EventName: "zero.event",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "qty"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))

	p := &price.Price{
		ID: "price_zero", Amount: decimal.RequireFromString("0.01"), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: pl.ID, MeterID: mtr.ID,
		Type: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		ID: "sub_zero", PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: periodStart, BillingAnchor: periodStart.AddDate(0, 0, 30),
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodStart.AddDate(0, 0, 30),
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := []*subscription.SubscriptionLineItem{{
		ID: "sli_zero", SubscriptionID: sub.ID, CustomerID: cust.ID,
		EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: pl.Name, PriceID: p.ID, PriceType: p.Type, MeterID: mtr.ID,
		DisplayName: "Zero usage", Quantity: decimal.Zero, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: sub.StartDate, BaseModel: types.GetDefaultBaseModel(ctx),
	}}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	sub.LineItems = lineItems
	return sub
}
