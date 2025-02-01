package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
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

type InvoiceServiceSuite struct {
	testutil.BaseServiceTestSuite
	service     InvoiceService
	eventRepo   *testutil.InMemoryEventStore
	invoiceRepo *testutil.InMemoryInvoiceStore
	testData    struct {
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
		events       struct {
			apiCalls  *events.Event
			storage   *events.Event
			archived  *events.Event
			archived2 *events.Event
		}
	}
}

func TestInvoiceService(t *testing.T) {
	suite.Run(t, new(InvoiceServiceSuite))
}

func (s *InvoiceServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *InvoiceServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.eventRepo.Clear()
	s.invoiceRepo.Clear()
}

func (s *InvoiceServiceSuite) setupService() {
	s.eventRepo = testutil.NewInMemoryEventStore()
	s.invoiceRepo = testutil.NewInMemoryInvoiceStore()

	s.service = NewInvoiceService(
		s.GetStores().SubscriptionRepo,
		s.GetStores().PlanRepo,
		s.GetStores().PriceRepo,
		s.eventRepo,
		s.GetStores().MeterRepo,
		s.GetStores().CustomerRepo,
		s.invoiceRepo,
		s.GetPublisher(),
		s.GetWebhookPublisher(),
		s.GetDB(),
		s.GetLogger(),
		s.GetConfig(),
	)

}

func (s *InvoiceServiceSuite) setupTestData() {
	// Clear any existing data
	s.BaseServiceTestSuite.ClearStores()

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
	for i := 0; i < 500; i++ {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.subscription.TenantID,
			EventName:          s.testData.meters.apiCalls.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			Timestamp:          s.testData.now.Add(-1 * time.Hour),
			Properties:         map[string]interface{}{},
		}
		s.NoError(s.eventRepo.InsertEvent(s.GetContext(), event))
	}

	storageEvents := []struct {
		bytes float64
		tier  string
	}{
		{bytes: 30, tier: "standard"},
		{bytes: 20, tier: "standard"},
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
		s.NoError(s.eventRepo.InsertEvent(s.GetContext(), event))
	}
}

func (s *InvoiceServiceSuite) TestCreateSubscriptionInvoice() {
	tests := []struct {
		name            string
		setupFunc       func()
		wantErr         bool
		expectedAmount  decimal.Decimal
		expectedCharges int
	}{
		{
			name: "successful invoice creation with usage",
			setupFunc: func() {
				s.invoiceRepo.Clear()
			},
			expectedAmount:  decimal.NewFromFloat(15),
			expectedCharges: 2,
		},
		{
			name: "no usage data available",
			setupFunc: func() {
				s.invoiceRepo.Clear()
				s.eventRepo.Clear()
			},
			expectedAmount:  decimal.Zero,
			expectedCharges: 0,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setupFunc != nil {
				tt.setupFunc()
			}

			// Create subscription invoice
			got, err := s.service.CreateSubscriptionInvoice(
				s.GetContext(),
				s.testData.subscription,
				s.testData.subscription.CurrentPeriodStart,
				s.testData.subscription.CurrentPeriodEnd,
			)

			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotEmpty(got.ID)
			s.Equal(s.testData.customer.ID, got.CustomerID)
			if got.SubscriptionID != nil {
				s.Equal(s.testData.subscription.ID, *got.SubscriptionID)
			}
			s.Equal(types.InvoiceTypeSubscription, got.InvoiceType)
			s.Equal(types.InvoiceStatusDraft, got.InvoiceStatus)
			s.Equal(types.InvoicePaymentStatusPending, got.PaymentStatus)
			s.Equal("USD", got.Currency)
			s.True(tt.expectedAmount.Equal(got.AmountDue), "amount due mismatch")
			s.True(decimal.Zero.Equal(got.AmountPaid), "amount paid mismatch")
			s.True(tt.expectedAmount.Equal(got.AmountRemaining), "amount remaining mismatch")
			s.Equal(fmt.Sprintf("Invoice for subscription %s", s.testData.subscription.ID), got.Description)
			s.Equal(s.testData.subscription.CurrentPeriodStart.Unix(), got.PeriodStart.Unix())
			s.Equal(s.testData.subscription.CurrentPeriodEnd.Unix(), got.PeriodEnd.Unix())
			s.Equal(types.StatusPublished, types.Status(got.Status))

			// Verify invoice and line items in DB
			invoice, err := s.invoiceRepo.Get(s.GetContext(), got.ID)
			s.NoError(err)
			s.Len(invoice.LineItems, tt.expectedCharges)

			if tt.expectedCharges > 0 {
				// Verify line item (Storage)
				item := invoice.LineItems[0]
				s.Equal(got.ID, item.InvoiceID)
				s.Equal(got.CustomerID, item.CustomerID)
				if got.SubscriptionID != nil && item.SubscriptionID != nil {
					s.Equal(*got.SubscriptionID, *item.SubscriptionID)
				}
				s.Equal(s.testData.prices.storage.ID, item.PriceID)
				s.Equal(s.testData.prices.storage.MeterID, *item.MeterID)
				s.True(decimal.NewFromFloat(5).Equal(item.Amount)) // 50 storage * $0.1
				s.True(decimal.NewFromFloat(50).Equal(item.Quantity))
				s.Equal(got.Currency, item.Currency)
				s.Equal(got.PeriodStart.Unix(), item.PeriodStart.Unix())
				s.Equal(got.PeriodEnd.Unix(), item.PeriodEnd.Unix())
				s.Equal(types.StatusPublished, types.Status(item.Status))
				s.Equal(got.TenantID, item.TenantID)

				// Verify line item (API Calls)
				item = invoice.LineItems[1]
				s.Equal(got.ID, item.InvoiceID)
				s.Equal(got.CustomerID, item.CustomerID)
				s.Equal(s.testData.prices.apiCalls.ID, item.PriceID)
				s.Equal(s.testData.prices.apiCalls.MeterID, *item.MeterID)
				s.True(decimal.NewFromFloat(10).Equal(item.Amount))
				s.True(decimal.NewFromFloat(500).Equal(item.Quantity))
				s.Equal(got.Currency, item.Currency)
				s.Equal(got.PeriodStart.Unix(), item.PeriodStart.Unix())
				s.Equal(got.PeriodEnd.Unix(), item.PeriodEnd.Unix())
				s.Equal(types.StatusPublished, types.Status(item.Status))
				s.Equal(got.TenantID, item.TenantID)
			}
		})
	}
}

func (s *InvoiceServiceSuite) TestFinalizeInvoice() {
	// Create a draft invoice first with line items
	draftInvoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  &s.testData.subscription.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.InvoicePaymentStatusPending,
		Currency:        "USD",
		AmountDue:       decimal.NewFromFloat(15),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(15),
		Description:     "Test Invoice",
		PeriodStart:     &s.testData.subscription.CurrentPeriodStart,
		PeriodEnd:       &s.testData.subscription.CurrentPeriodEnd,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
				CustomerID:     s.testData.customer.ID,
				SubscriptionID: &s.testData.subscription.ID,
				PriceID:        s.testData.prices.apiCalls.ID,
				MeterID:        &s.testData.meters.apiCalls.ID,
				Amount:         decimal.NewFromFloat(10),
				Quantity:       decimal.NewFromFloat(100),
				Currency:       "USD",
				PeriodStart:    &s.testData.subscription.CurrentPeriodStart,
				PeriodEnd:      &s.testData.subscription.CurrentPeriodEnd,
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
				CustomerID:     s.testData.customer.ID,
				SubscriptionID: &s.testData.subscription.ID,
				PriceID:        s.testData.prices.storage.ID,
				MeterID:        &s.testData.meters.storage.ID,
				Amount:         decimal.NewFromFloat(5),
				Quantity:       decimal.NewFromFloat(50),
				Currency:       "USD",
				PeriodStart:    &s.testData.subscription.CurrentPeriodStart,
				PeriodEnd:      &s.testData.subscription.CurrentPeriodEnd,
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			},
		},
	}
	s.NoError(s.invoiceRepo.CreateWithLineItems(s.GetContext(), draftInvoice))

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name: "successful finalization",
			id:   draftInvoice.ID,
		},
		{
			name:    "error when invoice not found",
			id:      "invalid_id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.FinalizeInvoice(s.GetContext(), tt.id)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			// Verify invoice is finalized
			inv, err := s.invoiceRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)

			// Verify line items are still present and published
			invoice, err := s.invoiceRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Len(invoice.LineItems, 2)
			for _, item := range invoice.LineItems {
				s.Equal(types.StatusPublished, types.Status(item.Status))
			}
		})
	}
}

func (s *InvoiceServiceSuite) TestUpdatePaymentStatus() {
	// Create a finalized invoice first with line items
	finalizedInvoice := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  &s.testData.subscription.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.InvoicePaymentStatusPending,
		Currency:        "USD",
		AmountDue:       decimal.NewFromFloat(15),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(15),
		Description:     "Test Invoice",
		PeriodStart:     &s.testData.subscription.CurrentPeriodStart,
		PeriodEnd:       &s.testData.subscription.CurrentPeriodEnd,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
				CustomerID:     s.testData.customer.ID,
				SubscriptionID: &s.testData.subscription.ID,
				PriceID:        s.testData.prices.apiCalls.ID,
				MeterID:        &s.testData.meters.apiCalls.ID,
				Amount:         decimal.NewFromFloat(10),
				Quantity:       decimal.NewFromFloat(100),
				Currency:       "USD",
				PeriodStart:    &s.testData.subscription.CurrentPeriodStart,
				PeriodEnd:      &s.testData.subscription.CurrentPeriodEnd,
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
				CustomerID:     s.testData.customer.ID,
				SubscriptionID: &s.testData.subscription.ID,
				PriceID:        s.testData.prices.storage.ID,
				MeterID:        &s.testData.meters.storage.ID,
				Amount:         decimal.NewFromFloat(5),
				Quantity:       decimal.NewFromFloat(50),
				Currency:       "USD",
				PeriodStart:    &s.testData.subscription.CurrentPeriodStart,
				PeriodEnd:      &s.testData.subscription.CurrentPeriodEnd,
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			},
		},
	}
	s.NoError(s.invoiceRepo.CreateWithLineItems(s.GetContext(), finalizedInvoice))

	tests := []struct {
		name    string
		id      string
		status  types.InvoicePaymentStatus
		amount  *decimal.Decimal
		wantErr bool
	}{
		{
			name:   "successful payment status update to succeeded",
			id:     finalizedInvoice.ID,
			status: types.InvoicePaymentStatusSucceeded,
			amount: &decimal.Decimal{},
		},
		{
			name:    "error when invoice not found",
			id:      "invalid_id",
			status:  types.InvoicePaymentStatusSucceeded,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Set the amount to the full amount due for successful payment
			if tt.status == types.InvoicePaymentStatusSucceeded {
				amount := finalizedInvoice.AmountDue
				tt.amount = &amount
			}

			err := s.service.UpdatePaymentStatus(s.GetContext(), tt.id, tt.status, tt.amount)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			// Verify invoice payment status
			inv, err := s.invoiceRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(tt.status, inv.PaymentStatus)
			if tt.status == types.InvoicePaymentStatusSucceeded {
				s.True(inv.AmountDue.Equal(inv.AmountPaid), "amount paid should equal amount due")
				s.True(decimal.Zero.Equal(inv.AmountRemaining), "amount remaining should be zero")
			}

			// Verify line items are still present and published
			invoice, err := s.invoiceRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Len(invoice.LineItems, 2)
			for _, item := range invoice.LineItems {
				s.Equal(types.StatusPublished, types.Status(item.Status))
			}
		})
	}
}

func (s *InvoiceServiceSuite) TestGetCustomerInvoiceSummary() {
	// Setup test data
	customer := s.testData.customer
	now := s.testData.now

	// Create test invoices with different states and currencies
	invoices := []*invoice.Invoice{
		{
			ID:              "inv_1",
			CustomerID:      customer.ID,
			Currency:        "USD",
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.InvoicePaymentStatusPending,
			AmountDue:       decimal.NewFromInt(100),
			AmountRemaining: decimal.NewFromInt(100),
			DueDate:         lo.ToPtr(now.Add(-24 * time.Hour)), // Overdue
			LineItems: []*invoice.InvoiceLineItem{
				{
					ID:        "line_1",
					InvoiceID: "inv_1",
					Amount:    decimal.NewFromInt(60),
					PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
					Currency:  "USD",
				},
				{
					ID:        "line_2",
					InvoiceID: "inv_1",
					Amount:    decimal.NewFromInt(40),
					PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
					Currency:  "USD",
				},
			},
			BaseModel: types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:              "inv_2",
			CustomerID:      customer.ID,
			Currency:        "USD",
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.InvoicePaymentStatusSucceeded,
			AmountDue:       decimal.NewFromInt(200),
			AmountRemaining: decimal.Zero,
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:              "inv_3",
			CustomerID:      customer.ID,
			Currency:        "EUR", // Different currency
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.InvoicePaymentStatusPending,
			AmountDue:       decimal.NewFromInt(300),
			AmountRemaining: decimal.NewFromInt(300),
			LineItems: []*invoice.InvoiceLineItem{
				{
					ID:        "line_3",
					InvoiceID: "inv_3",
					Amount:    decimal.NewFromInt(300),
					PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
					Currency:  "EUR",
				},
			},
			BaseModel: types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:              "inv_4",
			CustomerID:      customer.ID,
			Currency:        "usd", // Same as USD but different case
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.InvoicePaymentStatusPending,
			AmountDue:       decimal.NewFromInt(150),
			AmountRemaining: decimal.NewFromInt(150),
			LineItems: []*invoice.InvoiceLineItem{
				{
					ID:        "line_4",
					InvoiceID: "inv_4",
					Amount:    decimal.NewFromInt(150),
					PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
					Currency:  "usd",
				},
			},
			BaseModel: types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	// Store test invoices
	for _, inv := range invoices {
		err := s.invoiceRepo.CreateWithLineItems(s.GetContext(), inv)
		s.NoError(err)
	}

	// Test cases
	testCases := []struct {
		name            string
		customerID      string
		currency        string
		expectedError   bool
		expectedSummary *dto.CustomerInvoiceSummary
	}{
		{
			name:       "Success - USD currency",
			customerID: customer.ID,
			currency:   "USD",
			expectedSummary: &dto.CustomerInvoiceSummary{
				CustomerID:          customer.ID,
				Currency:            "USD",
				TotalRevenueAmount:  decimal.NewFromInt(450), // 100 + 200 + 150
				TotalUnpaidAmount:   decimal.NewFromInt(250), // 100 + 150
				TotalOverdueAmount:  decimal.NewFromInt(100), // inv_1
				TotalInvoiceCount:   3,                       // USD invoices only
				UnpaidInvoiceCount:  2,                       // inv_1 and inv_4
				OverdueInvoiceCount: 1,                       // inv_1
				UnpaidUsageCharges:  decimal.NewFromInt(60),  // from inv_1
				UnpaidFixedCharges:  decimal.NewFromInt(190), // 40 from inv_1 + 150 from inv_4
			},
		},
		{
			name:       "Success - EUR currency",
			customerID: customer.ID,
			currency:   "EUR",
			expectedSummary: &dto.CustomerInvoiceSummary{
				CustomerID:          customer.ID,
				Currency:            "EUR",
				TotalRevenueAmount:  decimal.NewFromInt(300),
				TotalUnpaidAmount:   decimal.NewFromInt(300),
				TotalOverdueAmount:  decimal.Zero,
				TotalInvoiceCount:   1,
				UnpaidInvoiceCount:  1,
				OverdueInvoiceCount: 0,
				UnpaidUsageCharges:  decimal.NewFromInt(300),
				UnpaidFixedCharges:  decimal.Zero,
			},
		},
		{
			name:       "Success - No invoices found",
			customerID: customer.ID,
			currency:   "GBP",
			expectedSummary: &dto.CustomerInvoiceSummary{
				CustomerID:          customer.ID,
				Currency:            "GBP",
				TotalRevenueAmount:  decimal.Zero,
				TotalUnpaidAmount:   decimal.Zero,
				TotalOverdueAmount:  decimal.Zero,
				TotalInvoiceCount:   0,
				UnpaidInvoiceCount:  0,
				OverdueInvoiceCount: 0,
				UnpaidUsageCharges:  decimal.Zero,
				UnpaidFixedCharges:  decimal.Zero,
			},
		},
		{
			name:       "Success - Invalid customer ID",
			customerID: "invalid_id",
			currency:   "USD",
			expectedSummary: &dto.CustomerInvoiceSummary{
				CustomerID:          "invalid_id",
				Currency:            "USD",
				TotalRevenueAmount:  decimal.Zero,
				TotalUnpaidAmount:   decimal.Zero,
				TotalOverdueAmount:  decimal.Zero,
				TotalInvoiceCount:   0,
				UnpaidInvoiceCount:  0,
				OverdueInvoiceCount: 0,
				UnpaidUsageCharges:  decimal.Zero,
				UnpaidFixedCharges:  decimal.Zero,
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			summary, err := s.service.GetCustomerInvoiceSummary(s.GetContext(), tc.customerID, tc.currency)
			if tc.expectedError {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(summary)
			s.Equal(tc.expectedSummary.CustomerID, summary.CustomerID)
			s.Equal(tc.expectedSummary.Currency, summary.Currency)
			s.True(tc.expectedSummary.TotalRevenueAmount.Equal(summary.TotalRevenueAmount),
				"TotalRevenueAmount mismatch: expected %s, got %s",
				tc.expectedSummary.TotalRevenueAmount, summary.TotalRevenueAmount)
			s.True(tc.expectedSummary.TotalUnpaidAmount.Equal(summary.TotalUnpaidAmount),
				"TotalUnpaidAmount mismatch: expected %s, got %s",
				tc.expectedSummary.TotalUnpaidAmount, summary.TotalUnpaidAmount)
			s.True(tc.expectedSummary.TotalOverdueAmount.Equal(summary.TotalOverdueAmount),
				"TotalOverdueAmount mismatch: expected %s, got %s",
				tc.expectedSummary.TotalOverdueAmount, summary.TotalOverdueAmount)
			s.Equal(tc.expectedSummary.TotalInvoiceCount, summary.TotalInvoiceCount)
			s.Equal(tc.expectedSummary.UnpaidInvoiceCount, summary.UnpaidInvoiceCount)
			s.Equal(tc.expectedSummary.OverdueInvoiceCount, summary.OverdueInvoiceCount)
			s.True(tc.expectedSummary.UnpaidUsageCharges.Equal(summary.UnpaidUsageCharges),
				"UnpaidUsageCharges mismatch: expected %s, got %s",
				tc.expectedSummary.UnpaidUsageCharges, summary.UnpaidUsageCharges)
			s.True(tc.expectedSummary.UnpaidFixedCharges.Equal(summary.UnpaidFixedCharges),
				"UnpaidFixedCharges mismatch: expected %s, got %s",
				tc.expectedSummary.UnpaidFixedCharges, summary.UnpaidFixedCharges)
		})
	}
}
