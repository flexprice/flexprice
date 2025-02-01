package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  SubscriptionService
	testData struct {
		customer *customer.Customer
		plan     *plan.Plan
		meters   struct {
			apiCalls *meter.Meter
			storage  *meter.Meter
		}
		prices struct {
			apiCalls       *price.Price
			storage        *price.Price
			storageArchive *price.Price
		}
		subscription *subscription.Subscription
		now          time.Time
	}
}

func TestSubscriptionService(t *testing.T) {
	suite.Run(t, new(SubscriptionServiceSuite))
}

func (s *SubscriptionServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

// TearDownTest is called after each test
func (s *SubscriptionServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewSubscriptionService(
		stores.SubscriptionRepo,
		stores.PlanRepo,
		stores.PriceRepo,
		stores.EventRepo,
		stores.MeterRepo,
		stores.CustomerRepo,
		stores.InvoiceRepo,
		s.GetPublisher(),
		s.GetWebhookPublisher(),
		s.GetDB(),
		s.GetLogger(),
		s.GetConfig(),
	)
}

func (s *SubscriptionServiceSuite) setupTestData() {
	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_123",
		ExternalID: "ext_cust_123",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test plan
	s.testData.plan = &plan.Plan{
		ID:             "plan_123",
		Name:           "Test Plan",
		Description:    "Test Plan Description",
		InvoiceCadence: types.InvoiceCadenceAdvance,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	// Create test meters
	s.testData.meters.apiCalls = &meter.Meter{
		ID:        "meter_api_calls",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meters.apiCalls))

	s.testData.meters.storage = &meter.Meter{
		ID:        "meter_storage",
		Name:      "Storage",
		EventName: "storage_usage",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "bytes_used",
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meters.storage))

	// Create test prices
	upTo1000 := uint64(1000)
	upTo5000 := uint64(5000)

	s.testData.prices.apiCalls = &price.Price{
		ID:                 "price_api_calls",
		Amount:             decimal.Zero,
		Currency:           "USD",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		TierMode:           types.BILLING_TIER_SLAB,
		MeterID:            s.testData.meters.apiCalls.ID,
		Tiers: []price.PriceTier{
			{UpTo: &upTo1000, UnitAmount: decimal.NewFromFloat(0.02)},
			{UpTo: &upTo5000, UnitAmount: decimal.NewFromFloat(0.005)},
			{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.01)},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.apiCalls))

	s.testData.prices.storage = &price.Price{
		ID:                 "price_storage",
		Amount:             decimal.NewFromFloat(0.1),
		Currency:           "USD",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		MeterID:            s.testData.meters.storage.ID,
		FilterValues:       map[string][]string{"region": {"us-east-1"}, "tier": {"standard"}},
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.storage))

	s.testData.prices.storageArchive = &price.Price{
		ID:                 "price_storage_archive",
		Amount:             decimal.NewFromFloat(0.03),
		Currency:           "USD",
		PlanID:             s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		MeterID:            s.testData.meters.storage.ID,
		FilterValues:       map[string][]string{"region": {"us-east-1"}, "tier": {"archive"}},
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.prices.storageArchive))

	s.testData.now = time.Now().UTC()
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_123",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), s.testData.subscription))

	// Create test events
	for i := 0; i < 1500; i++ {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subscription.TenantID,
			EventName:          s.testData.meters.apiCalls.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			Timestamp:          s.testData.now.Add(-1 * time.Hour),
			Properties:         map[string]interface{}{},
		}
		s.NoError(s.GetStores().EventRepo.InsertEvent(s.GetContext(), event))
	}

	storageEvents := []struct {
		bytes float64
		tier  string
	}{
		{bytes: 100, tier: "standard"},
		{bytes: 200, tier: "standard"},
		{bytes: 300, tier: "archive"},
	}

	for _, se := range storageEvents {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subscription.TenantID,
			EventName:          s.testData.meters.storage.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			Timestamp:          s.testData.now.Add(-30 * time.Minute),
			Properties: map[string]interface{}{
				"bytes_used": se.bytes,
				"region":     "us-east-1",
				"tier":       se.tier,
			},
		}
		s.NoError(s.GetStores().EventRepo.InsertEvent(s.GetContext(), event))
	}
}

func (s *SubscriptionServiceSuite) TestGetUsageBySubscription() {
	tests := []struct {
		name    string
		req     *dto.GetUsageBySubscriptionRequest
		want    *dto.GetUsageBySubscriptionResponse
		wantErr bool
	}{
		{
			name: "successful usage calculation with multiple meters and filters",
			req: &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: s.testData.subscription.ID,
				StartTime:      s.testData.now.Add(-48 * time.Hour),
				EndTime:        s.testData.now,
			},
			want: &dto.GetUsageBySubscriptionResponse{
				StartTime: s.testData.now.Add(-48 * time.Hour),
				EndTime:   s.testData.now,
				Amount:    61.5,
				Currency:  "USD",
				Charges: []*dto.SubscriptionUsageByMetersResponse{
					{
						MeterDisplayName: "Storage",
						Quantity:         decimal.NewFromInt(300).InexactFloat64(),
						Amount:           9, // archive: 300 * 0.03
					},
					{
						MeterDisplayName: "Storage",
						Quantity:         decimal.NewFromInt(300).InexactFloat64(),
						Amount:           30, // standard: 300 * 0.1
					},
					{
						MeterDisplayName: "API Calls",
						Quantity:         decimal.NewFromInt(1500).InexactFloat64(),
						Amount:           22.5, // tiers: (1000 *0.02=20) + (500*0.005=2.5)
					},
				},
			},
			wantErr: false,
		},
		{
			name: "zero usage period",
			req: &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: s.testData.subscription.ID,
				StartTime:      s.testData.now.Add(-100 * 24 * time.Hour),
				EndTime:        s.testData.now.Add(-50 * 24 * time.Hour),
			},
			want: &dto.GetUsageBySubscriptionResponse{
				StartTime: s.testData.now.Add(-100 * 24 * time.Hour),
				EndTime:   s.testData.now.Add(-50 * 24 * time.Hour),
				Amount:    0,
				Currency:  "USD",
				Charges:   []*dto.SubscriptionUsageByMetersResponse{},
			},
			wantErr: false,
		},
		{
			name: "default to current period when no times specified",
			req: &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: s.testData.subscription.ID,
			},
			want: &dto.GetUsageBySubscriptionResponse{
				StartTime: s.testData.subscription.CurrentPeriodStart,
				EndTime:   s.testData.subscription.CurrentPeriodEnd,
				Amount:    61.5, // same as first test since events fall in current period
				Currency:  "USD",
			},
			wantErr: false,
		},
		{
			name: "invalid subscription ID",
			req: &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: "invalid_id",
			},
			wantErr: true,
		},
		{
			name: "subscription not active",
			req: &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: "sub_inactive",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := s.service.GetUsageBySubscription(s.GetContext(), tt.req)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.Equal(tt.want.StartTime.Unix(), got.StartTime.Unix())
			s.Equal(tt.want.EndTime.Unix(), got.EndTime.Unix())
			s.Equal(tt.want.Amount, got.Amount)
			s.Equal(tt.want.Currency, got.Currency)

			if tt.want.Charges != nil {
				s.Len(got.Charges, len(tt.want.Charges))
				for i, wantCharge := range tt.want.Charges {
					if wantCharge == nil {
						continue
					}

					if i >= len(got.Charges) {
						err := fmt.Errorf("got %d charges, want %d", len(got.Charges), len(tt.want.Charges))
						s.Error(err)
						return
					}

					gotCharge := got.Charges[i]
					s.Equal(wantCharge.MeterDisplayName, gotCharge.MeterDisplayName)
					s.Equal(wantCharge.Quantity, gotCharge.Quantity)
					s.Equal(wantCharge.Amount, gotCharge.Amount)
				}
			}
		})
	}
}

func (s *SubscriptionServiceSuite) TestCreateSubscription() {
	testCases := []struct {
		name    string
		input   dto.CreateSubscriptionRequest
		want    *dto.SubscriptionResponse
		wantErr bool
	}{
		{
			name: "successful_subscription_creation",
			input: dto.CreateSubscriptionRequest{
				CustomerID:         s.testData.customer.ID,
				PlanID:             s.testData.plan.ID,
				StartDate:          s.testData.now,
				EndDate:            lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
				Currency:           "USD",
				InvoiceCadence:     types.InvoiceCadenceArrear,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
			},
			wantErr: false,
		},
		{
			name: "invalid_customer_id",
			input: dto.CreateSubscriptionRequest{
				CustomerID:         "invalid_customer",
				PlanID:             s.testData.plan.ID,
				StartDate:          s.testData.now,
				EndDate:            lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
				Currency:           "USD",
				InvoiceCadence:     types.InvoiceCadenceArrear,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
			},
			wantErr: true,
		},
		{
			name: "invalid_plan_id",
			input: dto.CreateSubscriptionRequest{
				CustomerID:         s.testData.customer.ID,
				PlanID:             "invalid_plan",
				StartDate:          s.testData.now,
				EndDate:            lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
				Currency:           "USD",
				InvoiceCadence:     types.InvoiceCadenceArrear,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
			},
			wantErr: true,
		},
		{
			name: "end_date_before_start_date",
			input: dto.CreateSubscriptionRequest{
				CustomerID:         s.testData.customer.ID,
				PlanID:             s.testData.plan.ID,
				StartDate:          s.testData.now,
				EndDate:            lo.ToPtr(s.testData.now.Add(-24 * time.Hour)),
				Currency:           "USD",
				InvoiceCadence:     types.InvoiceCadenceArrear,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreateSubscription(s.GetContext(), tc.input)
			if tc.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.NotEmpty(resp.ID)
			s.Equal(tc.input.CustomerID, resp.CustomerID)
			s.Equal(tc.input.PlanID, resp.PlanID)
			s.Equal(types.SubscriptionStatusActive, resp.SubscriptionStatus)
			s.Equal(tc.input.StartDate.Unix(), resp.StartDate.Unix())
			if tc.input.EndDate != nil {
				s.Equal(tc.input.EndDate.Unix(), resp.EndDate.Unix())
			}
		})
	}
}

func (s *SubscriptionServiceSuite) TestGetSubscription() {
	testCases := []struct {
		name    string
		id      string
		want    *dto.SubscriptionResponse
		wantErr bool
	}{
		{
			name:    "existing_subscription",
			id:      s.testData.subscription.ID,
			wantErr: false,
		},
		{
			name:    "non_existent_subscription",
			id:      "non_existent",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetSubscription(s.GetContext(), tc.id)
			if tc.wantErr {
				s.Error(err)
				s.Nil(resp)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tc.id, resp.ID)
		})
	}
}

func (s *SubscriptionServiceSuite) TestCancelSubscription() {
	// Create an active subscription for cancel tests
	activeSub := &subscription.Subscription{
		ID:                 "sub_to_cancel",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now,
		EndDate:            lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), activeSub))

	testCases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "cancel_active_subscription",
			id:      activeSub.ID,
			wantErr: false,
		},
		{
			name:    "cancel_non_existent_subscription",
			id:      "non_existent",
			wantErr: true,
		},
		{
			name:    "cancel_already_canceled_subscription",
			id:      activeSub.ID, // Will be canceled by first test case
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.service.CancelSubscription(s.GetContext(), tc.id, false)
			if tc.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)

			// Verify the subscription status
			sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), tc.id)
			s.NoError(err)
			s.NotNil(sub)
			s.Equal(types.SubscriptionStatusCancelled, sub.SubscriptionStatus)
		})
	}
}

func (s *SubscriptionServiceSuite) TestListSubscriptions() {
	// Create additional test subscriptions
	testSubs := []*subscription.Subscription{
		{
			ID:                 "sub_1",
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.plan.ID,
			SubscriptionStatus: types.SubscriptionStatusActive,
			StartDate:          s.testData.now,
			EndDate:            lo.ToPtr(s.testData.now.Add(30 * 24 * time.Hour)),
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:                 "sub_2",
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.plan.ID,
			SubscriptionStatus: types.SubscriptionStatusCancelled,
			StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
			EndDate:            lo.ToPtr(s.testData.now),
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	for _, sub := range testSubs {
		s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	}

	testCases := []struct {
		name      string
		input     *types.SubscriptionFilter
		wantCount int
		wantErr   bool
	}{
		{
			name:      "list_all_subscriptions",
			input:     &types.SubscriptionFilter{QueryFilter: types.NewDefaultQueryFilter()},
			wantCount: 3, // 2 new + 1 from setupTestData
			wantErr:   false,
		},
		{
			name: "filter_by_customer",
			input: &types.SubscriptionFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				CustomerID:  s.testData.customer.ID,
			},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "filter_by_status_active",
			input: &types.SubscriptionFilter{
				QueryFilter:        types.NewDefaultQueryFilter(),
				SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "filter_by_status_cancelled",
			input: &types.SubscriptionFilter{
				QueryFilter:        types.NewDefaultQueryFilter(),
				SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusCancelled},
			},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			subs, err := s.service.ListSubscriptions(s.GetContext(), tc.input)
			if tc.wantErr {
				s.Error(err)
				s.Nil(subs)
				return
			}

			s.NoError(err)
			s.NotNil(subs)
			s.Len(subs.Items, tc.wantCount)

			if tc.input.CustomerID != "" {
				for _, sub := range subs.Items {
					s.Equal(tc.input.CustomerID, sub.CustomerID)
				}
			}

			if tc.input.SubscriptionStatus != nil {
				for _, sub := range subs.Items {
					s.Contains(tc.input.SubscriptionStatus, sub.SubscriptionStatus)
				}
			}
		})
	}
}
