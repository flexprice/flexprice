package service

import (
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
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type WalletServiceSuite struct {
	testutil.BaseServiceTestSuite
	service     WalletService
	subsService SubscriptionService
	testData    struct {
		wallet   *wallet.Wallet
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

func TestWalletService(t *testing.T) {
	suite.Run(t, new(WalletServiceSuite))
}

func (s *WalletServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

// TearDownTest is called after each test
func (s *WalletServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *WalletServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewWalletService(
		stores.WalletRepo,
		s.GetLogger(),
		stores.SubscriptionRepo,
		stores.PlanRepo,
		stores.PriceRepo,
		stores.EventRepo,
		stores.MeterRepo,
		stores.CustomerRepo,
		stores.InvoiceRepo,
		stores.EntitlementRepo,
		stores.FeatureRepo,
		s.GetPublisher(),
		s.GetWebhookPublisher(),
		s.GetDB(),
		s.GetConfig(),
	)
	s.subsService = NewSubscriptionService(
		stores.SubscriptionRepo,
		stores.PlanRepo,
		stores.PriceRepo,
		stores.EventRepo,
		stores.MeterRepo,
		stores.CustomerRepo,
		stores.InvoiceRepo,
		stores.EntitlementRepo,
		stores.FeatureRepo,
		s.GetPublisher(),
		s.GetWebhookPublisher(),
		s.GetDB(),
		s.GetLogger(),
		s.GetConfig(),
	)
}

func (s *WalletServiceSuite) setupTestData() {
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
		Currency:           "usd",
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
		Currency:           "usd",
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
		Currency:           "usd",
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
		Currency:           "usd",
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

	// Setup subscriptions with different currencies
	subscriptions := []*subscription.Subscription{
		{
			ID:                 "sub_1",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			Currency:           "usd",
			SubscriptionStatus: types.SubscriptionStatusActive,
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:                 "sub_2",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			Currency:           "INR", // Same currency, different case
			SubscriptionStatus: types.SubscriptionStatusActive,
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:                 "sub_3",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			Currency:           "EUR", // Different currency
			SubscriptionStatus: types.SubscriptionStatusActive,
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	subscriptionLineItems := []*subscription.SubscriptionLineItem{
		{
			CustomerID:       s.testData.customer.ID,
			PlanID:           s.testData.plan.ID,
			PlanDisplayName:  s.testData.plan.Name,
			PriceID:          s.testData.prices.storage.ID,
			PriceType:        types.PRICE_TYPE_USAGE,
			MeterID:          s.testData.meters.storage.ID,
			MeterDisplayName: s.testData.meters.storage.Name,
			DisplayName:      s.testData.meters.storage.Name,
			Quantity:         decimal.NewFromInt(0),
			BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
			StartDate:        s.testData.now.Add(-24 * time.Hour),
			EndDate:          s.testData.now.Add(6 * 24 * time.Hour),
			Metadata:         map[string]string{},
			BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			CustomerID:       s.testData.customer.ID,
			PlanID:           s.testData.plan.ID,
			PlanDisplayName:  s.testData.plan.Name,
			PriceID:          s.testData.prices.storageArchive.ID,
			PriceType:        types.PRICE_TYPE_USAGE,
			MeterID:          s.testData.meters.storage.ID,
			MeterDisplayName: s.testData.meters.storage.Name,
			DisplayName:      s.testData.meters.storage.Name,
			Quantity:         decimal.NewFromInt(0),
			BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
			StartDate:        s.testData.now.Add(-24 * time.Hour),
			EndDate:          s.testData.now.Add(6 * 24 * time.Hour),
			Metadata:         map[string]string{},
			BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			CustomerID:       s.testData.customer.ID,
			PlanID:           s.testData.plan.ID,
			PlanDisplayName:  s.testData.plan.Name,
			PriceID:          s.testData.prices.apiCalls.ID,
			PriceType:        types.PRICE_TYPE_USAGE,
			MeterID:          s.testData.meters.apiCalls.ID,
			MeterDisplayName: s.testData.meters.apiCalls.Name,
			DisplayName:      s.testData.meters.apiCalls.Name,
			Quantity:         decimal.NewFromInt(0),
			BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
			StartDate:        s.testData.now.Add(-24 * time.Hour),
			EndDate:          s.testData.now.Add(6 * 24 * time.Hour),
			Metadata:         map[string]string{},
			BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	for _, sub := range subscriptions {
		for i, lineItem := range subscriptionLineItems {
			lineItem.ID = s.GetUUID()
			lineItem.SubscriptionID = sub.ID
			lineItem.Currency = sub.Currency
			subscriptionLineItems[i] = lineItem
		}
		err := s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, subscriptionLineItems)
		s.NoError(err)
	}

	// Setup test invoices
	invoices := []*invoice.Invoice{
		{
			ID:              "inv_1",
			CustomerID:      s.testData.customer.ID,
			Currency:        "usd",
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			AmountDue:       decimal.NewFromInt(100),
			AmountRemaining: decimal.NewFromInt(100),
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:              "inv_2",
			CustomerID:      s.testData.customer.ID,
			Currency:        "usd",
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			AmountDue:       decimal.NewFromInt(150),
			AmountRemaining: decimal.NewFromInt(150),
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:              "inv_3",
			CustomerID:      s.testData.customer.ID,
			Currency:        "EUR",
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			AmountDue:       decimal.NewFromInt(200),
			AmountRemaining: decimal.NewFromInt(200),
			BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	for _, inv := range invoices {
		err := s.GetStores().InvoiceRepo.Create(s.GetContext(), inv)
		s.NoError(err)
	}

	s.testData.wallet = &wallet.Wallet{
		ID:           "wallet-1",
		CustomerID:   s.testData.customer.ID,
		Currency:     "usd",
		Balance:      decimal.NewFromInt(1000),
		WalletStatus: types.WalletStatusActive,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
}

func (s *WalletServiceSuite) TestCreateWallet() {
	req := &dto.CreateWalletRequest{
		CustomerID: "customer-2",
		Currency:   "usd",
		Metadata:   types.Metadata{"key": "value"},
	}

	resp, err := s.service.CreateWallet(s.GetContext(), req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.CustomerID, resp.CustomerID)
	s.Equal(req.Currency, resp.Currency)
	s.Equal(decimal.Zero, resp.Balance)
}

func (s *WalletServiceSuite) TestGetWalletByID() {
	resp, err := s.service.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(s.testData.wallet.CustomerID, resp.CustomerID)
	s.Equal(s.testData.wallet.Currency, resp.Currency)
	s.Equal(s.testData.wallet.Balance, resp.Balance)
}

func (s *WalletServiceSuite) TestGetWalletsByCustomerID() {
	// Create another wallet for same customer
	wallet2 := &wallet.Wallet{
		ID:           "wallet-2",
		CustomerID:   s.testData.wallet.CustomerID,
		Currency:     "EUR",
		Balance:      decimal.NewFromInt(500),
		WalletStatus: types.WalletStatusActive,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), wallet2))

	resp, err := s.service.GetWalletsByCustomerID(s.GetContext(), s.testData.wallet.CustomerID)
	s.NoError(err)
	s.Len(resp, 2)
}

func (s *WalletServiceSuite) TestTopUpWallet() {
	topUpReq := &dto.TopUpWalletRequest{
		Amount: decimal.NewFromInt(500),
	}
	resp, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, topUpReq)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(decimal.NewFromInt(1500), resp.Balance)
}

func (s *WalletServiceSuite) TestTerminateWallet() {
	err := s.service.TerminateWallet(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)

	// Verify the wallet status
	updatedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.Equal(types.WalletStatusClosed, updatedWallet.WalletStatus)
	s.Equal(decimal.NewFromInt(0).Equal(updatedWallet.Balance), true)

	// Verify transaction creation
	filter := types.NewWalletTransactionFilter()
	filter.WalletID = &s.testData.wallet.ID
	filter.QueryFilter.Limit = lo.ToPtr(10)

	transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), filter)
	s.NoError(err)
	s.Len(transactions, 1)
	s.Equal(types.TransactionTypeDebit, transactions[0].Type)
	s.Equal(decimal.NewFromInt(1000).Equal(transactions[0].Amount), true)
}

func (s *WalletServiceSuite) TestGetWalletBalance() {
	// Test cases
	testCases := []struct {
		name                    string
		walletID                string
		expectedError           bool
		expectedRealTimeBalance decimal.Decimal
		expectedUnpaidAmount    decimal.Decimal
		expectedCurrentUsage    decimal.Decimal
	}{
		{
			name:                    "Success - Active wallet with matching currency",
			walletID:                s.testData.wallet.ID,
			expectedRealTimeBalance: decimal.NewFromInt(688).Add(decimal.NewFromFloat(0.5)), // 1000 - (100 + 150) - 61.5
			expectedUnpaidAmount:    decimal.NewFromInt(250),                                // 100 + 150 (USD invoices only)
			expectedCurrentUsage:    decimal.NewFromFloat(61.5),                             // API calls: 30 + Storage: 31.5
		},
		{
			name:          "Error - Invalid wallet ID",
			walletID:      "invalid_id",
			expectedError: true,
		},
		{
			name:          "Error - Inactive wallet",
			walletID:      "wallet_inactive",
			expectedError: true,
		},
	}

	// Create inactive wallet for testing
	inactiveWallet := &wallet.Wallet{
		ID:           "wallet_inactive",
		CustomerID:   s.testData.customer.ID,
		Currency:     "usd",
		Balance:      decimal.NewFromInt(1000),
		WalletStatus: types.WalletStatusClosed,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().WalletRepo.CreateWallet(s.GetContext(), inactiveWallet)
	s.NoError(err)

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.GetWalletBalance(s.GetContext(), tc.walletID)
			if tc.expectedError {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.True(tc.expectedRealTimeBalance.Equal(resp.RealTimeBalance),
				"RealTimeBalance mismatch: expected %s, got %s",
				tc.expectedRealTimeBalance, resp.RealTimeBalance)
			s.True(tc.expectedUnpaidAmount.Equal(resp.UnpaidInvoiceAmount),
				"UnpaidInvoiceAmount mismatch: expected %s, got %s",
				tc.expectedUnpaidAmount, resp.UnpaidInvoiceAmount)
			s.True(tc.expectedCurrentUsage.Equal(resp.CurrentPeriodUsage),
				"CurrentPeriodUsage mismatch: expected %s, got %s",
				tc.expectedCurrentUsage, resp.CurrentPeriodUsage)
			s.NotZero(resp.BalanceUpdatedAt)
			s.NotNil(resp.Wallet)
		})
	}
}
