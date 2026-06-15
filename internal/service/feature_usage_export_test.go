package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// FeatureUsageExportSuite drives the feature-usage analytics path the way the
// usage-analytics export job does (registration.go wires featureUsageTrackingService
// as the export's getter): no window_size, GroupBy=["source","feature_id"].
type FeatureUsageExportSuite struct {
	testutil.BaseServiceTestSuite
	svc *featureUsageTrackingService
}

func TestFeatureUsageExportSuite(t *testing.T) {
	suite.Run(t, new(FeatureUsageExportSuite))
}

func (s *FeatureUsageExportSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	params := ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  st.SubscriptionRepo,
		SubscriptionLineItemRepo: st.SubscriptionLineItemRepo,
		PlanRepo:                 st.PlanRepo,
		PriceRepo:                st.PriceRepo,
		MeterRepo:                st.MeterRepo,
		CustomerRepo:             st.CustomerRepo,
		FeatureRepo:              st.FeatureRepo,
		SettingsRepo:             st.SettingsRepo,
		MeterUsageRepo:           st.MeterUsageRepo,
		FeatureUsageRepo:         st.FeatureUsageRepo,
		EnvironmentRepo:          st.EnvironmentRepo,
		TenantRepo:               st.TenantRepo,
		EventRepo:                st.EventRepo,
		EntitlementRepo:          st.EntitlementRepo,
		InvoiceRepo:              st.InvoiceRepo,
		WalletRepo:               st.WalletRepo,
		UserRepo:                 st.UserRepo,
		AuthRepo:                 st.AuthRepo,
	}
	// Build the struct directly — the public constructor initialises Kafka pubsubs
	// (and Fatals without a broker), which the read-only analytics path never uses.
	s.svc = &featureUsageTrackingService{
		ServiceParams:       params,
		eventRepo:           st.EventRepo,
		featureUsageRepo:    st.FeatureUsageRepo,
		expressionEvaluator: expression.NewCELEvaluator(),
	}
}

// runZeroUsageExport seeds a customer with a windowed per-bucket commitment
// (bucket [11:00,11:30) over a MINUTE meter, $3 amount commitment) and NO usage,
// then runs the export's exact call (no window_size, GroupBy=[source,feature_id]).
// Returns the analytics row for the feature. trueUp toggles the bucket's true-up.
func (s *FeatureUsageExportSuite) runZeroUsageExport(trueUp bool) *dto.UsageAnalyticItem {
	ctx := s.GetContext()
	periodStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	exportStart := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	exportEnd := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)

	cust := &customer.Customer{
		ID: "cust_exp", ExternalID: "cust-da", Name: "da",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	// Minute-bucketed SUM meter (a sub-hour bucket only aligns to a 1-min window).
	m := &meter.Meter{
		ID: "meter_exp", Name: "dabucket", EventName: "dabucket",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeMinute},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	feat := &feature.Feature{
		ID: "feat_exp", Name: "dabucket", Type: types.FeatureTypeMetered, MeterID: m.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, feat))

	linePrice := &price.Price{
		ID: "price_exp_line", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_exp",
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE, MeterID: m.ID,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, linePrice))

	sub := &subscription.Subscription{
		ID: "sub_exp", CustomerID: cust.ID, PlanID: "plan_exp", Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd, BillingAnchor: periodStart,
		StartDate: periodStart, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	bucketPrice := &price.Price{
		ID: "price_exp_bkt", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: sub.ID,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE, MeterID: m.ID,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, bucketPrice))

	overage := decimal.NewFromInt(2)
	li := &subscription.SubscriptionLineItem{
		ID: "li_exp", SubscriptionID: sub.ID, CustomerID: cust.ID,
		PriceID: linePrice.ID, PriceType: types.PRICE_TYPE_USAGE, MeterID: m.ID,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: periodStart, EndDate: periodEnd, Quantity: decimal.NewFromInt(1),
		CommitmentWindowed: true, // bucket-only commitment, no top-level commitment
		CommitmentTimeBuckets: types.TimeOfDayBuckets{{
			ID: "bkt_exp", Start: types.Bucket{Hour: 11, Minute: 0}, End: types.Bucket{Hour: 11, Minute: 30},
			PriceID: bucketPrice.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(3), OverageFactor: &overage, TrueUpEnabled: trueUp,
		}},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	// In-memory List(WithLineItems) returns the batch created here (mirrors the DB join).
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))

	// NO usage events — mirrors the zero-usage export row.

	resp, err := s.svc.GetDetailedUsageAnalytics(ctx, &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: cust.ExternalID,
		StartTime:          exportStart,
		EndTime:            exportEnd,
		GroupBy:            []string{"source", "feature_id"},
	})
	s.NoError(err)

	for i := range resp.Items {
		if resp.Items[i].FeatureID == feat.ID {
			return &resp.Items[i]
		}
	}
	return nil
}

// TestExport_ZeroUsageBucketTrueUp_BillsCommitment: with bucket true-up ON, the
// export must bill the committed minimum on zero usage. 30 empty minute windows in
// [11:00,11:30) × $3 = $90.
func (s *FeatureUsageExportSuite) TestExport_ZeroUsageBucketTrueUp_BillsCommitment() {
	item := s.runZeroUsageExport(true)
	s.Require().NotNil(item, "expected an analytics row for the committed feature")
	s.True(item.TotalCost.Equal(decimal.NewFromInt(90)),
		"export must bill the bucket true-up minimum on zero usage; got total_cost=%s", item.TotalCost)
}

// TestExport_ZeroUsageBucketNoTrueUp_BillsZero documents the contrast: a bucket
// commitment WITHOUT true-up does not bill unused commitment, so zero usage is
// $0 — a row still appears (this is exactly the customer's total_cost=0 export
// when the bucket has no true_up_enabled).
func (s *FeatureUsageExportSuite) TestExport_ZeroUsageBucketNoTrueUp_BillsZero() {
	item := s.runZeroUsageExport(false)
	s.Require().NotNil(item, "a zero-usage committed line item still produces a row")
	s.True(item.TotalCost.IsZero(),
		"a bucket commitment without true-up bills $0 on zero usage; got total_cost=%s", item.TotalCost)
}
