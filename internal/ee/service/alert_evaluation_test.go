package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// MeterUsageTrackingEvaluationSuite covers alertService.EvaluateSpendBreachForEvent's short-circuit gates — unknown
// customer, no affected line items, nothing configured — which all return before any billing
// calculation runs. It uses real in-memory repos (unlike MeterUsageTrackingSuite in
// meter_usage_tracking_test.go) because EvaluateSpendAlertsForCustomer reads through
// CustomerRepo/SubscriptionLineItemRepo/AlertRepo.
//
// It does not cover an actual threshold-crossing → LogAlert → webhook run: that needs a full
// GetMeterUsageBySubscription/CalculateMeterUsageCharges fixture (customer, subscription, usage
// price, meter, and meter_usage rows), which no existing test in this codebase builds with
// in-memory stores yet. That's better proven via a live-DB integration test than invented here.
type MeterUsageTrackingEvaluationSuite struct {
	testutil.BaseServiceTestSuite
	svc      *meterUsageTrackingService
	customer *customer.Customer
}

func TestMeterUsageTrackingEvaluation(t *testing.T) {
	suite.Run(t, new(MeterUsageTrackingEvaluationSuite))
}

func (s *MeterUsageTrackingEvaluationSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.svc = &meterUsageTrackingService{
		ServiceParams: ServiceParams{
			Logger:                   s.GetLogger(),
			Config:                   s.GetConfig(),
			DB:                       s.GetDB(),
			SubRepo:                  s.GetStores().SubscriptionRepo,
			SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
			FeatureRepo:              s.GetStores().FeatureRepo,
			GroupRepo:                s.GetStores().GroupRepo,
			AlertRepo:                s.GetStores().AlertRepo,
			AlertLogsRepo:            s.GetStores().AlertLogsRepo,
			CustomerRepo:             s.GetStores().CustomerRepo,
			WalletRepo:               s.GetStores().WalletRepo,
			WebhookPublisher:         s.GetWebhookPublisher(),
		},
		meterUsageRepo:      s.GetStores().MeterUsageRepo,
		expressionEvaluator: expression.NewCELEvaluator(),
	}

	s.customer = &customer.Customer{
		ID:         "cust_eval_test",
		ExternalID: "ext_cust_eval_test",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.customer))
}

func (s *MeterUsageTrackingEvaluationSuite) countAlertLogs() int {
	logs, err := s.GetStores().AlertLogsRepo.List(s.GetContext(), types.NewNoLimitAlertLogFilter())
	s.Require().NoError(err)
	return len(logs)
}

func (s *MeterUsageTrackingEvaluationSuite) TestCheckSpendBreachForEvent_UnknownCustomer_NoOp() {
	event := &events.Event{
		ID:                 "event_unknown_customer",
		ExternalCustomerID: "does_not_exist",
	}

	s.NotPanics(func() {
		NewAlertService(s.svc.ServiceParams).EvaluateSpendBreachForEvent(s.GetContext(), event, s.customer, []string{"meter_eval_test"})
	})
	s.Equal(0, s.countAlertLogs())
}

func (s *MeterUsageTrackingEvaluationSuite) TestCheckSpendBreachForEvent_NoAffectedLineItems_NoOp() {
	event := &events.Event{
		ID:                 "event_no_line_items",
		ExternalCustomerID: s.customer.ExternalID,
	}

	s.NotPanics(func() {
		NewAlertService(s.svc.ServiceParams).EvaluateSpendBreachForEvent(s.GetContext(), event, s.customer, []string{"meter_does_not_exist"})
	})
	s.Equal(0, s.countAlertLogs())
}

func (s *MeterUsageTrackingEvaluationSuite) TestCheckSpendBreachForEvent_NoAlertSettingsConfigured_NoOp() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	sub := &subscription.Subscription{
		ID:                 "sub_eval_test",
		CustomerID:         s.customer.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingAnchor:      now,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItem := &subscription.SubscriptionLineItem{
		ID:             "subs_line_eval_test",
		SubscriptionID: sub.ID,
		CustomerID:     s.customer.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        "meter_eval_test",
		DisplayName:    "API Calls",
		Quantity:       decimal.Zero,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      sub.StartDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{lineItem}))

	event := &events.Event{
		ID:                 "event_no_alert_settings",
		ExternalCustomerID: s.customer.ExternalID,
	}

	// No alert_settings row exists for this subscription, so this must return before ever
	// attempting a billing calculation — which would otherwise fail/panic on this bare fixture
	// (no price/meter usage data behind it).
	s.NotPanics(func() {
		NewAlertService(s.svc.ServiceParams).EvaluateSpendBreachForEvent(ctx, event, s.customer, []string{"meter_eval_test"})
	})
	s.Equal(0, s.countAlertLogs())
}
