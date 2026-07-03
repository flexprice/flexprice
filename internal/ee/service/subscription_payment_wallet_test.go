package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionPaymentWalletSuite covers the wallet-amount capping and Stripe
// preflight helpers of subscriptionPaymentProcessor: calculateWalletPayableAmount,
// calculateWalletAllowedAmount, hasStripeConnection, getPaymentMethodID, and the
// gateway-less branches of processPaymentMethodCharge.
type SubscriptionPaymentWalletSuite struct {
	testutil.BaseServiceTestSuite
	proc     *subscriptionPaymentProcessor
	testData struct {
		customer *customer.Customer
		plan     *plan.Plan
		invoice  *invoice.Invoice
		now      time.Time
	}
}

func TestSubscriptionPaymentWalletSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionPaymentWalletSuite))
}

func (s *SubscriptionPaymentWalletSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	params := newTestServiceParams(&s.BaseServiceTestSuite)
	s.proc = &subscriptionPaymentProcessor{ServiceParams: &params}
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_sub_pay_wallet",
		ExternalID: "ext_cust_sub_pay_wallet",
		Name:       "Sub Payment Wallet Customer",
		Email:      "sub_pay_wallet@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        "plan_sub_pay_wallet",
		Name:      "Sub Payment Wallet Plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	s.testData.invoice = &invoice.Invoice{
		ID:              "inv_sub_pay_wallet",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromInt(100),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), s.testData.invoice))
}

func (s *SubscriptionPaymentWalletSuite) createSubscription(id string, mutate func(*subscription.Subscription)) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		Currency:           "usd",
		StartDate:          s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingAnchor:      s.testData.now.Add(-24 * time.Hour),
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	if mutate != nil {
		mutate(sub)
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, nil))
	return sub
}

func (s *SubscriptionPaymentWalletSuite) createWallet(id string, balance decimal.Decimal, priceTypes []types.WalletConfigPriceType, mutate func(*wallet.Wallet)) *wallet.Wallet {
	w := &wallet.Wallet{
		ID:                  id,
		CustomerID:          s.testData.customer.ID,
		Name:                "Wallet " + id,
		Currency:            "usd",
		Balance:             balance,
		CreditBalance:       balance,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          types.WalletTypePostPaid,
		AlertState:          types.AlertStateOk,
		Config:              types.WalletConfig{AllowedPriceTypes: priceTypes},
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	if mutate != nil {
		mutate(w)
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))
	return w
}

func (s *SubscriptionPaymentWalletSuite) createStripeConnection(encrypted bool) {
	conn := &connection.Connection{
		ID:            "conn_stripe_test",
		Name:          "Stripe Test Connection",
		ProviderType:  types.SecretProviderStripe,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	if encrypted {
		// Garbage (non-base64) ciphertext: decryption fails deterministically so
		// no live Stripe call can ever be attempted from tests.
		conn.EncryptedSecretData = types.ConnectionMetadata{
			Stripe: &types.StripeConnectionMetadata{
				SecretKey:      "!!!not-base64!!!",
				PublishableKey: "!!!not-base64!!!",
				WebhookSecret:  "!!!not-base64!!!",
			},
		}
	}
	s.NoError(s.GetStores().ConnectionRepo.Create(s.GetContext(), conn))
}

func (s *SubscriptionPaymentWalletSuite) TestCalculateWalletAllowedAmount() {
	priceTypeAmounts := map[string]decimal.Decimal{
		string(types.PRICE_TYPE_USAGE): decimal.RequireFromString("40.50"),
		string(types.PRICE_TYPE_FIXED): decimal.RequireFromString("59.50"),
	}

	testCases := []struct {
		name       string
		priceTypes []types.WalletConfigPriceType
		expected   decimal.Decimal
	}{
		{
			name:       "empty_config_allows_full_amount",
			priceTypes: nil,
			expected:   decimal.RequireFromString("100"),
		},
		{
			name:       "all_type_allows_full_amount",
			priceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			expected:   decimal.RequireFromString("100"),
		},
		{
			name:       "usage_only_allows_usage_portion",
			priceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			expected:   decimal.RequireFromString("40.50"),
		},
		{
			name:       "fixed_only_allows_fixed_portion",
			priceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			expected:   decimal.RequireFromString("59.50"),
		},
		{
			name:       "usage_and_fixed_allows_both_portions",
			priceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage, types.WalletConfigPriceTypeFixed},
			expected:   decimal.RequireFromString("100"),
		},
		{
			name:       "unknown_type_allows_nothing",
			priceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceType("SOMETHING_ELSE")},
			expected:   decimal.Zero,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			w := &wallet.Wallet{Config: types.WalletConfig{AllowedPriceTypes: tc.priceTypes}}
			got := s.proc.calculateWalletAllowedAmount(w, priceTypeAmounts)
			s.True(got.Equal(tc.expected), "expected %s, got %s", tc.expected, got)
		})
	}
}

func (s *SubscriptionPaymentWalletSuite) TestCalculateWalletPayableAmount() {
	priceTypeAmounts := map[string]decimal.Decimal{
		string(types.PRICE_TYPE_USAGE): decimal.NewFromInt(40),
		string(types.PRICE_TYPE_FIXED): decimal.NewFromInt(60),
	}

	s.Run("zero_available_credits_returns_zero", func() {
		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.Zero)
		s.True(got.IsZero())
	})

	s.Run("customer_without_wallets_returns_zero", func() {
		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.NewFromInt(100))
		s.True(got.IsZero())
	})

	s.Run("inactive_and_empty_wallets_are_excluded", func() {
		s.createWallet("wallet_frozen", decimal.NewFromInt(100), nil, func(w *wallet.Wallet) {
			w.WalletStatus = types.WalletStatusFrozen
		})
		s.createWallet("wallet_empty", decimal.Zero, nil, nil)

		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.NewFromInt(100))
		s.True(got.IsZero())

		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_frozen", types.WalletStatusClosed))
		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_empty", types.WalletStatusClosed))
	})

	s.Run("usage_restricted_wallet_is_capped_at_usage_amount", func() {
		s.createWallet("wallet_usage_cap", decimal.NewFromInt(100), []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage}, nil)

		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.NewFromInt(100))
		s.True(got.Equal(decimal.NewFromInt(40)), "usage-only wallet must pay at most the usage portion, got %s", got)

		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_usage_cap", types.WalletStatusClosed))
	})

	s.Run("payable_amount_is_capped_by_wallet_balance", func() {
		s.createWallet("wallet_low_balance", decimal.NewFromInt(25), nil, nil)

		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.NewFromInt(100))
		s.True(got.Equal(decimal.NewFromInt(25)), "payable amount must never exceed wallet balance, got %s", got)

		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_low_balance", types.WalletStatusClosed))
	})

	s.Run("payable_amount_is_capped_by_available_credits", func() {
		s.createWallet("wallet_credit_cap", decimal.NewFromInt(500), nil, nil)

		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.NewFromInt(10))
		s.True(got.Equal(decimal.NewFromInt(10)), "payable amount must never exceed available credits, got %s", got)

		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_credit_cap", types.WalletStatusClosed))
	})

	s.Run("multiple_wallets_accumulate_up_to_credits", func() {
		s.createWallet("wallet_multi_1", decimal.NewFromInt(30), []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage}, nil)
		s.createWallet("wallet_multi_2", decimal.NewFromInt(70), nil, nil)

		got := s.proc.calculateWalletPayableAmount(s.GetContext(), s.testData.customer.ID, priceTypeAmounts, decimal.NewFromInt(100))
		s.True(got.Equal(decimal.NewFromInt(100)), "expected 30 (usage-capped) + 70 (ALL) = 100, got %s", got)

		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_multi_1", types.WalletStatusClosed))
		s.NoError(s.GetStores().WalletRepo.UpdateWalletStatus(s.GetContext(), "wallet_multi_2", types.WalletStatusClosed))
	})
}

func (s *SubscriptionPaymentWalletSuite) TestHasStripeConnection() {
	s.Run("returns_false_without_connection", func() {
		s.False(s.proc.hasStripeConnection(s.GetContext()))
	})

	s.Run("returns_true_with_published_connection", func() {
		s.createStripeConnection(false)
		s.True(s.proc.hasStripeConnection(s.GetContext()))
	})
}

func (s *SubscriptionPaymentWalletSuite) TestGetPaymentMethodID() {
	s.Run("subscription_gateway_payment_method_takes_precedence", func() {
		sub := s.createSubscription("subs_with_pm", func(sub *subscription.Subscription) {
			sub.GatewayPaymentMethodID = lo.ToPtr("pm_from_subscription")
		})

		got := s.proc.getPaymentMethodID(s.GetContext(), sub, s.testData.customer.ID)
		s.Equal("pm_from_subscription", got)
	})

	s.Run("returns_empty_when_default_payment_method_lookup_fails", func() {
		sub := s.createSubscription("subs_without_pm", nil)

		// No Stripe connection exists, so the default payment method lookup fails.
		got := s.proc.getPaymentMethodID(s.GetContext(), sub, s.testData.customer.ID)
		s.Empty(got)
	})
}

func (s *SubscriptionPaymentWalletSuite) TestProcessPaymentMethodCharge() {
	invResp := &dto.InvoiceResponse{
		Invoice: *s.testData.invoice,
	}

	s.Run("returns_zero_without_stripe_connection", func() {
		sub := s.createSubscription("subs_charge_noconn", nil)

		got := s.proc.processPaymentMethodCharge(s.GetContext(), sub, invResp, decimal.NewFromInt(100))
		s.True(got.IsZero())
	})

	s.Run("returns_zero_when_customer_has_no_stripe_mapping", func() {
		s.createStripeConnection(true)
		sub := s.createSubscription("subs_charge_nomap", nil)

		got := s.proc.processPaymentMethodCharge(s.GetContext(), sub, invResp, decimal.NewFromInt(100))
		s.True(got.IsZero())
	})

	s.Run("returns_zero_when_mapped_customer_has_no_payment_method", func() {
		// Connection exists (from previous subtest); mark the customer as synced
		// to Stripe but leave the subscription without a payment method — the
		// default payment method lookup fails on decryption, so no charge happens.
		cust, err := s.GetStores().CustomerRepo.Get(s.GetContext(), s.testData.customer.ID)
		s.NoError(err)
		cust.Metadata = map[string]string{"stripe_customer_id": "cus_test_123"}
		s.NoError(s.GetStores().CustomerRepo.Update(s.GetContext(), cust))

		sub := s.createSubscription("subs_charge_nopm", nil)

		got := s.proc.processPaymentMethodCharge(s.GetContext(), sub, invResp, decimal.NewFromInt(100))
		s.True(got.IsZero())
	})

	s.Run("failed_gateway_charge_returns_zero_and_records_failed_payment", func() {
		// Customer is mapped and the subscription carries a payment method, so the
		// processor creates a card payment; the gateway charge then fails (garbage
		// credentials) and the payment is recorded as FAILED.
		sub := s.createSubscription("subs_charge_fail", func(sub *subscription.Subscription) {
			sub.GatewayPaymentMethodID = lo.ToPtr("pm_test_card")
		})

		got := s.proc.processPaymentMethodCharge(s.GetContext(), sub, invResp, decimal.NewFromInt(100))
		s.True(got.IsZero())

		payments, err := s.GetStores().PaymentRepo.List(s.GetContext(), &types.PaymentFilter{
			QueryFilter:   types.NewNoLimitQueryFilter(),
			DestinationID: lo.ToPtr(s.testData.invoice.ID),
		})
		s.NoError(err)
		s.Len(payments, 1)
		s.Equal(types.PaymentMethodTypeCard, payments[0].PaymentMethodType)
		s.Equal(types.PaymentStatusFailed, payments[0].PaymentStatus)
		s.True(payments[0].Amount.Equal(decimal.NewFromInt(100)))
		s.Equal(sub.ID, payments[0].Metadata["subscription_id"])
	})
}

func (s *SubscriptionPaymentWalletSuite) TestShouldAllowPartialWalletPayment() {
	testCases := []struct {
		name     string
		behavior types.PaymentBehavior
		flowType types.InvoiceFlowType
		expected bool
	}{
		{
			name:     "subscription_creation_with_default_active_allows_partial",
			behavior: types.PaymentBehaviorDefaultActive,
			flowType: types.InvoiceFlowSubscriptionCreation,
			expected: true,
		},
		{
			name:     "subscription_creation_with_allow_incomplete_disallows_partial",
			behavior: types.PaymentBehaviorAllowIncomplete,
			flowType: types.InvoiceFlowSubscriptionCreation,
			expected: false,
		},
		{
			name:     "renewal_flow_always_allows_partial",
			behavior: types.PaymentBehaviorErrorIfIncomplete,
			flowType: types.InvoiceFlowRenewal,
			expected: true,
		},
		{
			name:     "manual_flow_always_allows_partial",
			behavior: types.PaymentBehaviorAllowIncomplete,
			flowType: types.InvoiceFlowManual,
			expected: true,
		},
		{
			name:     "cancel_flow_always_allows_partial",
			behavior: types.PaymentBehaviorAllowIncomplete,
			flowType: types.InvoiceFlowCancel,
			expected: true,
		},
		{
			name:     "unknown_flow_type_disallows_partial",
			behavior: types.PaymentBehaviorDefaultActive,
			flowType: types.InvoiceFlowType("mystery"),
			expected: false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.Equal(tc.expected, s.proc.shouldAllowPartialWalletPayment(tc.behavior, tc.flowType))
		})
	}
}

func (s *SubscriptionPaymentWalletSuite) TestHandlePaymentBehaviorUnsupportedCombinations() {
	invResp := &dto.InvoiceResponse{Invoice: *s.testData.invoice}

	testCases := []struct {
		name             string
		collectionMethod string
		behavior         types.PaymentBehavior
	}{
		{
			name:             "send_invoice_rejects_allow_incomplete",
			collectionMethod: string(types.CollectionMethodSendInvoice),
			behavior:         types.PaymentBehaviorAllowIncomplete,
		},
		{
			name:             "charge_automatically_rejects_default_incomplete",
			collectionMethod: string(types.CollectionMethodChargeAutomatically),
			behavior:         types.PaymentBehaviorDefaultIncomplete,
		},
		{
			name:             "unknown_collection_method_is_rejected",
			collectionMethod: "carrier_pigeon",
			behavior:         types.PaymentBehaviorDefaultActive,
		},
	}

	for i, tc := range testCases {
		s.Run(tc.name, func() {
			sub := s.createSubscription("subs_behavior_"+string(rune('a'+i)), func(sub *subscription.Subscription) {
				sub.CollectionMethod = tc.collectionMethod
			})
			originalStatus := sub.SubscriptionStatus

			err := s.proc.HandlePaymentBehavior(s.GetContext(), sub, invResp, tc.behavior, types.InvoiceFlowRenewal)
			s.Error(err)

			stored, gerr := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
			s.NoError(gerr)
			s.Equal(originalStatus, stored.SubscriptionStatus,
				"unsupported behavior must not change subscription status")
		})
	}
}

func (s *SubscriptionPaymentWalletSuite) TestHandlePaymentBehaviorSendInvoiceStatusTransitions() {
	invResp := &dto.InvoiceResponse{Invoice: *s.testData.invoice}

	s.Run("default_active_activates_subscription_without_payment", func() {
		sub := s.createSubscription("subs_send_inv_active", func(sub *subscription.Subscription) {
			sub.CollectionMethod = string(types.CollectionMethodSendInvoice)
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
		})

		s.NoError(s.proc.HandlePaymentBehavior(s.GetContext(), sub, invResp, types.PaymentBehaviorDefaultActive, types.InvoiceFlowRenewal))

		stored, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, stored.SubscriptionStatus)
	})

	s.Run("default_incomplete_marks_subscription_incomplete", func() {
		sub := s.createSubscription("subs_send_inv_incomplete", func(sub *subscription.Subscription) {
			sub.CollectionMethod = string(types.CollectionMethodSendInvoice)
			sub.SubscriptionStatus = types.SubscriptionStatusActive
		})

		s.NoError(s.proc.HandlePaymentBehavior(s.GetContext(), sub, invResp, types.PaymentBehaviorDefaultIncomplete, types.InvoiceFlowRenewal))

		stored, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
	})
}
