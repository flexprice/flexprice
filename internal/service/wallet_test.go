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
		ID:             "wallet-1",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromInt(1000),
		CreditBalance:  decimal.NewFromInt(1000),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
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
		ID:             "wallet-2",
		CustomerID:     s.testData.wallet.CustomerID,
		Currency:       "EUR",
		Balance:        decimal.NewFromInt(500),
		CreditBalance:  decimal.NewFromInt(500),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
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
	s.True(decimal.NewFromInt(1500).Equal(resp.Balance),
		"Balance mismatch: expected %s, got %s",
		decimal.NewFromInt(1500), resp.Balance)
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
			name:                    "Inactive wallet",
			walletID:                "wallet_inactive",
			expectedRealTimeBalance: decimal.NewFromInt(0),
			expectedUnpaidAmount:    decimal.NewFromInt(0),
			expectedCurrentUsage:    decimal.NewFromInt(0),
			expectedError:           false,
		},
	}

	// Create inactive wallet for testing
	inactiveWallet := &wallet.Wallet{
		ID:             "wallet_inactive",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromInt(1000),
		CreditBalance:  decimal.NewFromInt(1000),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusClosed,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
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

func (s *WalletServiceSuite) TestWalletConversionRateHandling() {
	testCases := []struct {
		name           string
		conversionRate decimal.Decimal
		creditAmount   decimal.Decimal
		expectedAmount decimal.Decimal
	}{
		{
			name:           "Conversion rate 1:1",
			conversionRate: decimal.NewFromInt(1),
			creditAmount:   decimal.NewFromInt(100),
			expectedAmount: decimal.NewFromInt(100),
		},
		{
			name:           "Conversion rate 1:2 (1 credit = 2 currency units)",
			conversionRate: decimal.NewFromInt(2),
			creditAmount:   decimal.NewFromInt(100),
			expectedAmount: decimal.NewFromInt(200),
		},
		{
			name:           "Conversion rate 2:1 (2 credits = 1 currency unit)",
			conversionRate: decimal.NewFromFloat(0.5),
			creditAmount:   decimal.NewFromInt(100),
			expectedAmount: decimal.NewFromInt(50),
		},
		{
			name:           "High precision conversion rate",
			conversionRate: decimal.NewFromFloat(1.123456),
			creditAmount:   decimal.NewFromInt(100),
			expectedAmount: decimal.NewFromFloat(112.3456),
		},
		{
			name:           "Very small conversion rate",
			conversionRate: decimal.NewFromFloat(0.0001),
			creditAmount:   decimal.NewFromInt(10000),
			expectedAmount: decimal.NewFromInt(1),
		},
		{
			name:           "Very large conversion rate",
			conversionRate: decimal.NewFromInt(10000),
			creditAmount:   decimal.NewFromFloat(0.0001),
			expectedAmount: decimal.NewFromInt(1),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Create wallet with test conversion rate
			wallet := &wallet.Wallet{
				ID:             fmt.Sprintf("wallet-conv-%s", s.GetUUID()),
				CustomerID:     s.testData.customer.ID,
				Currency:       "usd",
				Balance:        decimal.Zero,
				CreditBalance:  decimal.Zero,
				ConversionRate: tc.conversionRate,
				WalletStatus:   types.WalletStatusActive,
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), wallet))

			// Top up wallet
			topUpReq := &dto.TopUpWalletRequest{
				Amount: tc.creditAmount,
			}
			resp, err := s.service.TopUpWallet(s.GetContext(), wallet.ID, topUpReq)
			s.NoError(err)
			s.NotNil(resp)

			// Verify balances
			s.True(tc.expectedAmount.Equal(resp.Balance),
				"Balance mismatch for %s: expected %s, got %s",
				tc.name, tc.expectedAmount, resp.Balance)
			s.True(tc.creditAmount.Equal(resp.CreditBalance),
				"Credit balance mismatch for %s: expected %s, got %s",
				tc.name, tc.creditAmount, resp.CreditBalance)

			// Verify conversion rate maintained
			s.True(tc.conversionRate.Equal(resp.ConversionRate),
				"Conversion rate changed: expected %s, got %s",
				tc.conversionRate, resp.ConversionRate)
		})
	}
}

func (s *WalletServiceSuite) TestWalletTransactionAmountHandling() {
	testCases := []struct {
		name                 string
		initialCreditBalance decimal.Decimal
		conversionRate       decimal.Decimal
		operation            struct {
			creditAmount decimal.Decimal
			txType       types.TransactionType
		}
		expectedBalances struct {
			credit  decimal.Decimal
			actual  decimal.Decimal
			usedAmt decimal.Decimal
		}
		shouldError bool
	}{
		{
			name:                 "Credit transaction with exact amounts",
			initialCreditBalance: decimal.NewFromInt(100),
			conversionRate:       decimal.NewFromInt(2),
			operation: struct {
				creditAmount decimal.Decimal
				txType       types.TransactionType
			}{
				creditAmount: decimal.NewFromInt(50),
				txType:       types.TransactionTypeCredit,
			},
			expectedBalances: struct {
				credit  decimal.Decimal
				actual  decimal.Decimal
				usedAmt decimal.Decimal
			}{
				credit:  decimal.NewFromInt(150),
				actual:  decimal.NewFromInt(300),
				usedAmt: decimal.Zero,
			},
		},
		{
			name:                 "Debit transaction with exact amounts",
			initialCreditBalance: decimal.NewFromInt(100),
			conversionRate:       decimal.NewFromInt(2),
			operation: struct {
				creditAmount decimal.Decimal
				txType       types.TransactionType
			}{
				creditAmount: decimal.NewFromInt(50),
				txType:       types.TransactionTypeDebit,
			},
			expectedBalances: struct {
				credit  decimal.Decimal
				actual  decimal.Decimal
				usedAmt decimal.Decimal
			}{
				credit:  decimal.NewFromInt(50),
				actual:  decimal.NewFromInt(100),
				usedAmt: decimal.NewFromInt(50),
			},
		},
		{
			name:                 "Debit more than available balance",
			initialCreditBalance: decimal.NewFromInt(100),
			conversionRate:       decimal.NewFromInt(2),
			operation: struct {
				creditAmount decimal.Decimal
				txType       types.TransactionType
			}{
				creditAmount: decimal.NewFromInt(150),
				txType:       types.TransactionTypeDebit,
			},
			shouldError: true,
		},
		{
			name:                 "Zero amount transaction",
			initialCreditBalance: decimal.NewFromInt(100),
			conversionRate:       decimal.NewFromInt(2),
			operation: struct {
				creditAmount decimal.Decimal
				txType       types.TransactionType
			}{
				creditAmount: decimal.Zero,
				txType:       types.TransactionTypeCredit,
			},
			shouldError: true,
		},
		{
			name:                 "High precision conversion with small amounts",
			initialCreditBalance: decimal.NewFromInt(100),
			conversionRate:       decimal.NewFromFloat(1.123456789),
			operation: struct {
				creditAmount decimal.Decimal
				txType       types.TransactionType
			}{
				creditAmount: decimal.NewFromFloat(0.000001),
				txType:       types.TransactionTypeCredit,
			},
			expectedBalances: struct {
				credit  decimal.Decimal
				actual  decimal.Decimal
				usedAmt decimal.Decimal
			}{
				credit:  decimal.NewFromInt(100).Add(decimal.NewFromFloat(0.000001)),
				actual:  decimal.NewFromInt(100).Add(decimal.NewFromFloat(0.000001)).Mul(decimal.NewFromFloat(1.123456789)),
				usedAmt: decimal.Zero,
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Create wallet with test parameters
			walletObj := &wallet.Wallet{
				ID:             fmt.Sprintf("wallet-tx-%s", s.GetUUID()),
				CustomerID:     s.testData.customer.ID,
				Currency:       "usd",
				Balance:        tc.initialCreditBalance.Mul(tc.conversionRate),
				CreditBalance:  tc.initialCreditBalance,
				ConversionRate: tc.conversionRate,
				WalletStatus:   types.WalletStatusActive,
				BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
			}
			s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), walletObj))

			// Perform operation
			var err error
			if tc.operation.txType == types.TransactionTypeCredit {
				_, err = s.service.TopUpWallet(s.GetContext(), walletObj.ID, &dto.TopUpWalletRequest{
					Amount: tc.operation.creditAmount,
				})
			} else {
				op := &wallet.WalletOperation{
					WalletID:     walletObj.ID,
					Type:         tc.operation.txType,
					CreditAmount: tc.operation.creditAmount,
				}
				err = s.GetStores().WalletRepo.DebitWallet(s.GetContext(), op)
			}

			if tc.shouldError {
				s.Error(err)
				return
			}
			s.NoError(err)

			// Verify final wallet state
			updatedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletObj.ID)
			s.NoError(err)
			s.True(tc.expectedBalances.credit.Equal(updatedWallet.CreditBalance),
				"Credit balance mismatch: expected %s, got %s",
				tc.expectedBalances.credit, updatedWallet.CreditBalance)
			s.True(tc.expectedBalances.actual.Equal(updatedWallet.Balance),
				"Actual balance mismatch: expected %s, got %s",
				tc.expectedBalances.actual, updatedWallet.Balance)

			// Verify transaction record
			filter := types.NewWalletTransactionFilter()
			filter.WalletID = &walletObj.ID
			transactions, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), filter)
			s.NoError(err)
			s.NotEmpty(transactions)
			lastTx := transactions[len(transactions)-1]

			// Verify transaction amounts
			// if tc.operation.txType == types.TransactionTypeDebit {
			// 	s.True(tc.expectedBalances.usedAmt.Equal(lastTx.AmountUsed),
			// 		"Amount used mismatch: expected %s, got %s",
			// 		tc.expectedBalances.usedAmt, lastTx.AmountUsed)
			// }
			s.True(tc.operation.creditAmount.Equal(lastTx.CreditAmount),
				"Transaction credit amount mismatch: expected %s, got %s",
				tc.operation.creditAmount, lastTx.CreditAmount)
			s.True(tc.operation.creditAmount.Mul(tc.conversionRate).Equal(lastTx.Amount),
				"Transaction amount mismatch: expected %s, got %s",
				tc.operation.creditAmount.Mul(tc.conversionRate), lastTx.Amount)
		})
	}
}
