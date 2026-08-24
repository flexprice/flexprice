package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// Covers the NextPeriodAdvance boundary predicate: an item whose EndDate is exactly
// the next period start covers none of that period and must never be billed in advance
// for it. Every line item closed at a plan change lands on that exact boundary.
type BillingNextPeriodAdvanceSuite struct {
	testutil.BaseServiceTestSuite
	service BillingService

	jan1  time.Time
	jan15 time.Time
	feb1  time.Time
	feb15 time.Time
	mar1  time.Time

	sub *subscription.Subscription
}

func TestBillingNextPeriodAdvance(t *testing.T) {
	suite.Run(t, new(BillingNextPeriodAdvanceSuite))
}

func (s *BillingNextPeriodAdvanceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewBillingService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		TenantRepo:               s.GetStores().TenantRepo,
	})

	s.jan1 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.jan15 = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	s.feb1 = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	s.feb15 = time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	s.mar1 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	s.sub = &subscription.Subscription{
		ID:                 "sub_npa",
		PlanID:             "plan_npa",
		CustomerID:         "cust_npa",
		StartDate:          s.jan1,
		BillingAnchor:      s.feb1,
		CurrentPeriodStart: s.jan1,
		CurrentPeriodEnd:   s.feb1,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *BillingNextPeriodAdvanceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *BillingNextPeriodAdvanceSuite) lineItem(cadence types.InvoiceCadence, endDate time.Time) *subscription.SubscriptionLineItem {
	return &subscription.SubscriptionLineItem{
		ID:             "li_npa",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.sub.CustomerID,
		PriceID:        "price_npa",
		PriceType:      types.PRICE_TYPE_FIXED,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		StartDate:      s.jan1,
		EndDate:        endDate,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *BillingNextPeriodAdvanceSuite) TestClassify_AdvanceItemEndDateBoundary() {
	tests := []struct {
		name              string
		endDate           time.Time
		wantNextPeriod    bool
		wantCurrentPeriod bool
	}{
		{
			name:              "open ended item rolls into the next period",
			endDate:           time.Time{},
			wantNextPeriod:    true,
			wantCurrentPeriod: true,
		},
		{
			name:              "item ending exactly at the next period start is not billed in advance",
			endDate:           s.feb1,
			wantNextPeriod:    false,
			wantCurrentPeriod: true,
		},
		{
			name:              "item closed mid cycle is not billed in advance",
			endDate:           s.jan15,
			wantNextPeriod:    false,
			wantCurrentPeriod: true,
		},
		{
			name:              "item still active inside the next period is billed in advance",
			endDate:           s.feb15,
			wantNextPeriod:    true,
			wantCurrentPeriod: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.sub.LineItems = []*subscription.SubscriptionLineItem{s.lineItem(types.InvoiceCadenceAdvance, tt.endDate)}

			result := s.service.(*billingService).ClassifyLineItems(&dto.ClassifyLineItemsParams{
				Subscription:       s.sub,
				CurrentPeriodStart: s.jan1,
				CurrentPeriodEnd:   s.feb1,
				NextPeriodStart:    s.feb1,
				NextPeriodEnd:      s.mar1,
			})

			s.Len(result.NextPeriodAdvance, map[bool]int{true: 1, false: 0}[tt.wantNextPeriod])
			s.Len(result.CurrentPeriodAdvance, map[bool]int{true: 1, false: 0}[tt.wantCurrentPeriod])
			s.Empty(result.CurrentPeriodArrear)
		})
	}
}

// Arrear items never populate NextPeriodAdvance, so the predicate change cannot reach them.
func (s *BillingNextPeriodAdvanceSuite) TestClassify_ArrearItemUnaffectedByBoundary() {
	for _, endDate := range []time.Time{{}, s.feb1, s.feb15} {
		s.sub.LineItems = []*subscription.SubscriptionLineItem{s.lineItem(types.InvoiceCadenceArrear, endDate)}

		result := s.service.(*billingService).ClassifyLineItems(&dto.ClassifyLineItemsParams{
			Subscription:       s.sub,
			CurrentPeriodStart: s.jan1,
			CurrentPeriodEnd:   s.feb1,
			NextPeriodStart:    s.feb1,
			NextPeriodEnd:      s.mar1,
		})

		s.Len(result.CurrentPeriodArrear, 1)
		s.Empty(result.NextPeriodAdvance)
		s.Empty(result.CurrentPeriodAdvance)
	}
}
