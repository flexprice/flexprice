package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PaymentCrudSuite covers PaymentService CRUD paths, wallet selection for credits
// payments, Moyasar payment-link short-circuiting, and checkout payment creation.
type PaymentCrudSuite struct {
	testutil.BaseServiceTestSuite
	service  PaymentService
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
		now      time.Time
	}
}

func TestPaymentCrudSuite(t *testing.T) {
	suite.Run(t, new(PaymentCrudSuite))
}

func (s *PaymentCrudSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPaymentService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_pay_crud",
		ExternalID: "ext_cust_pay_crud",
		Name:       "Payment CRUD Customer",
		Email:      "pay_crud@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.invoice = s.createInvoice("inv_pay_crud", s.testData.customer.ID, decimal.NewFromInt(100), nil)
}

func (s *PaymentCrudSuite) createInvoice(id, customerID string, amountDue decimal.Decimal, metadata types.Metadata) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      customerID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       amountDue,
		AmountPaid:      decimal.Zero,
		AmountRemaining: amountDue,
		InvoiceNumber:   lo.ToPtr("INV-" + id),
		Metadata:        metadata,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

// createWallet creates a wallet with a backing credit transaction so the
// balance is actually consumable by debit operations.
func (s *PaymentCrudSuite) createWallet(id, customerID string, walletType types.WalletType, balance decimal.Decimal) *wallet.Wallet {
	w := &wallet.Wallet{
		ID:                  id,
		CustomerID:          customerID,
		Name:                "Wallet " + id,
		Currency:            "usd",
		Balance:             balance,
		CreditBalance:       balance,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          walletType,
		AlertState:          types.AlertStateOk,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))
	return w
}

func (s *PaymentCrudSuite) createStoredPayment(id string, mutate func(*payment.Payment)) *payment.Payment {
	p := &payment.Payment{
		ID:                id,
		IdempotencyKey:    "idem_" + id,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            decimal.NewFromInt(50),
		Currency:          "usd",
		PaymentStatus:     types.PaymentStatusPending,
		EnvironmentID:     types.GetEnvironmentID(s.GetContext()),
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	if mutate != nil {
		mutate(p)
	}
	s.NoError(s.GetStores().PaymentRepo.Create(s.GetContext(), p))
	return p
}

func (s *PaymentCrudSuite) paymentCount() int {
	count, err := s.GetStores().PaymentRepo.Count(s.GetContext(), &types.PaymentFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	return count
}

func (s *PaymentCrudSuite) TestCreatePaymentValidation() {
	paidInvoice := s.createInvoice("inv_paid", s.testData.customer.ID, decimal.NewFromInt(10), nil)
	paidInvoice.PaymentStatus = types.PaymentStatusSucceeded
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), paidInvoice))

	voidedInvoice := s.createInvoice("inv_voided", s.testData.customer.ID, decimal.NewFromInt(10), nil)
	voidedInvoice.InvoiceStatus = types.InvoiceStatusVoided
	s.NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), voidedInvoice))

	testCases := []struct {
		name string
		req  *dto.CreatePaymentRequest
	}{
		{
			name: "unsupported_destination_type_returns_validation_error",
			req: &dto.CreatePaymentRequest{
				DestinationType:   types.PaymentDestinationType("SUBSCRIPTION"),
				DestinationID:     "subs_123",
				PaymentMethodType: types.PaymentMethodTypeOffline,
				Amount:            decimal.NewFromInt(10),
				Currency:          "usd",
			},
		},
		{
			name: "invoice_not_found_returns_error",
			req: &dto.CreatePaymentRequest{
				DestinationType:   types.PaymentDestinationTypeInvoice,
				DestinationID:     "inv_does_not_exist",
				PaymentMethodType: types.PaymentMethodTypeOffline,
				Amount:            decimal.NewFromInt(10),
				Currency:          "usd",
			},
		},
		{
			name: "already_paid_invoice_returns_error",
			req: &dto.CreatePaymentRequest{
				DestinationType:   types.PaymentDestinationTypeInvoice,
				DestinationID:     paidInvoice.ID,
				PaymentMethodType: types.PaymentMethodTypeOffline,
				Amount:            decimal.NewFromInt(10),
				Currency:          "usd",
			},
		},
		{
			name: "voided_invoice_returns_error",
			req: &dto.CreatePaymentRequest{
				DestinationType:   types.PaymentDestinationTypeInvoice,
				DestinationID:     voidedInvoice.ID,
				PaymentMethodType: types.PaymentMethodTypeOffline,
				Amount:            decimal.NewFromInt(10),
				Currency:          "usd",
			},
		},
		{
			name: "currency_mismatch_returns_error",
			req: &dto.CreatePaymentRequest{
				DestinationType:   types.PaymentDestinationTypeInvoice,
				DestinationID:     s.testData.invoice.ID,
				PaymentMethodType: types.PaymentMethodTypeOffline,
				Amount:            decimal.NewFromInt(10),
				Currency:          "eur",
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			before := s.paymentCount()
			resp, err := s.service.CreatePayment(s.GetContext(), tc.req)
			s.Error(err)
			s.True(ierr.IsValidation(err), "expected validation error, got: %v", err)
			s.Nil(resp)
			s.Equal(before, s.paymentCount(), "failed create must not persist a payment")
		})
	}
}

func (s *PaymentCrudSuite) TestCreatePaymentOfflineHappyPath() {
	resp, err := s.service.CreatePayment(s.GetContext(), &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            decimal.RequireFromString("42.75"),
		Currency:          "USD",
		ProcessPayment:    false,
	})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(types.PaymentStatusPending, resp.PaymentStatus)

	stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), resp.ID)
	s.NoError(err)
	s.True(stored.Amount.Equal(decimal.RequireFromString("42.75")))
	s.Equal("usd", stored.Currency, "currency must be normalized to lowercase")
	s.NotEmpty(stored.IdempotencyKey, "idempotency key must be generated when not supplied")
	s.False(stored.TrackAttempts, "offline payments do not track attempts")
}

func (s *PaymentCrudSuite) TestCreatePaymentMoyasarPaymentLink() {
	s.Run("returns_existing_payment_url_without_creating_record", func() {
		inv := s.createInvoice("inv_moyasar_synced", s.testData.customer.ID, decimal.NewFromInt(100), types.Metadata{
			"moyasar_invoice_url": "https://moyasar.example/pay/abc",
		})
		before := s.paymentCount()

		resp, err := s.service.CreatePayment(s.GetContext(), &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     inv.ID,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeMoyasar),
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
		})
		s.NoError(err)
		s.NotNil(resp)
		s.NotNil(resp.PaymentURL)
		s.Equal("https://moyasar.example/pay/abc", *resp.PaymentURL)
		s.Equal(types.PaymentStatusPending, resp.PaymentStatus)
		s.Equal(before, s.paymentCount(), "existing payment link must not create a payment record")
	})

	s.Run("errors_when_invoice_not_synced_to_moyasar", func() {
		inv := s.createInvoice("inv_moyasar_unsynced", s.testData.customer.ID, decimal.NewFromInt(100), nil)

		resp, err := s.service.CreatePayment(s.GetContext(), &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     inv.ID,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeMoyasar),
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})

	s.Run("errors_when_stored_url_is_empty", func() {
		inv := s.createInvoice("inv_moyasar_empty_url", s.testData.customer.ID, decimal.NewFromInt(100), types.Metadata{
			"moyasar_invoice_url": "",
		})

		_, err := s.service.CreatePayment(s.GetContext(), &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     inv.ID,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeMoyasar),
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("external_payment_bypasses_sync_check_and_creates_record", func() {
		inv := s.createInvoice("inv_moyasar_external", s.testData.customer.ID, decimal.NewFromInt(100), nil)

		resp, err := s.service.CreatePayment(s.GetContext(), &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     inv.ID,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeMoyasar),
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			Metadata:          types.Metadata{"external_payment": "true"},
		})
		s.NoError(err)
		s.NotNil(resp)

		stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusInitiated, stored.PaymentStatus)
		s.Equal(string(types.PaymentGatewayTypeMoyasar), lo.FromPtr(stored.PaymentGateway))
	})
}

func (s *PaymentCrudSuite) TestCreatePaymentCreditsWalletSelection() {
	newCustomerWithInvoice := func(id string) (*customer.Customer, *invoice.Invoice) {
		cust := &customer.Customer{
			ID:         "cust_" + id,
			ExternalID: "ext_cust_" + id,
			Name:       "Credits Customer " + id,
			Email:      id + "@example.com",
			BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))
		inv := s.createInvoice("inv_"+id, cust.ID, decimal.NewFromInt(100), nil)
		return cust, inv
	}

	creditsReq := func(invoiceID, paymentMethodID string) *dto.CreatePaymentRequest {
		return &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     invoiceID,
			PaymentMethodType: types.PaymentMethodTypeCredits,
			PaymentMethodID:   paymentMethodID,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			ProcessPayment:    false,
		}
	}

	s.Run("errors_when_customer_has_no_wallets", func() {
		_, inv := newCustomerWithInvoice("credits_nowallet")
		_, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, ""))
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("errors_when_only_prepaid_wallets_exist", func() {
		cust, inv := newCustomerWithInvoice("credits_prepaid_only")
		s.createWallet("wallet_prepaid_only", cust.ID, types.WalletTypePrePaid, decimal.NewFromInt(500))

		_, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, ""))
		s.Error(err)
		s.True(ierr.IsNotFound(err), "prepaid wallets are not eligible for invoice payments")
	})

	s.Run("selects_postpaid_wallet_with_sufficient_balance", func() {
		cust, inv := newCustomerWithInvoice("credits_happy")
		w := s.createWallet("wallet_credits_happy", cust.ID, types.WalletTypePostPaid, decimal.NewFromInt(200))

		resp, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, ""))
		s.NoError(err)
		s.Equal(w.ID, resp.PaymentMethodID)

		stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Equal(w.ID, stored.PaymentMethodID)
		s.Equal(w.ID, stored.Metadata["wallet_id"])
		s.Equal(string(types.WalletTypePostPaid), stored.Metadata["wallet_type"])
		s.Equal(types.PaymentStatusPending, stored.PaymentStatus)
	})

	s.Run("errors_when_wallet_balance_insufficient", func() {
		cust, inv := newCustomerWithInvoice("credits_lowbal")
		s.createWallet("wallet_credits_lowbal", cust.ID, types.WalletTypePostPaid, decimal.NewFromInt(40))

		_, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, ""))
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err), "wallet payment amount must be capped by wallet balance")
	})

	s.Run("errors_when_multiple_postpaid_wallets_exist", func() {
		cust, inv := newCustomerWithInvoice("credits_multi")
		s.createWallet("wallet_credits_multi_1", cust.ID, types.WalletTypePostPaid, decimal.NewFromInt(200))
		s.createWallet("wallet_credits_multi_2", cust.ID, types.WalletTypePostPaid, decimal.NewFromInt(200))

		_, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, ""))
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("explicit_prepaid_wallet_is_rejected", func() {
		cust, inv := newCustomerWithInvoice("credits_explicit_pre")
		w := s.createWallet("wallet_explicit_pre", cust.ID, types.WalletTypePrePaid, decimal.NewFromInt(500))

		_, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, w.ID))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("explicit_postpaid_wallet_is_accepted", func() {
		cust, inv := newCustomerWithInvoice("credits_explicit_post")
		w := s.createWallet("wallet_explicit_post", cust.ID, types.WalletTypePostPaid, decimal.NewFromInt(500))

		resp, err := s.service.CreatePayment(s.GetContext(), creditsReq(inv.ID, w.ID))
		s.NoError(err)
		s.Equal(w.ID, resp.PaymentMethodID)
	})
}

func (s *PaymentCrudSuite) TestGetPayment() {
	s.Run("empty_id_returns_validation_error", func() {
		resp, err := s.service.GetPayment(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})

	s.Run("unknown_id_returns_not_found", func() {
		resp, err := s.service.GetPayment(s.GetContext(), "pay_does_not_exist")
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("invoice_destination_includes_invoice_number", func() {
		p := s.createStoredPayment("pay_get_happy", nil)

		resp, err := s.service.GetPayment(s.GetContext(), p.ID)
		s.NoError(err)
		s.Equal(p.ID, resp.ID)
		s.True(resp.Amount.Equal(decimal.NewFromInt(50)))
		s.Equal(s.testData.invoice.ID, resp.DestinationID)
		s.Equal(lo.FromPtr(s.testData.invoice.InvoiceNumber), lo.FromPtr(resp.InvoiceNumber))
	})
}

func (s *PaymentCrudSuite) TestUpdatePayment() {
	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.UpdatePayment(s.GetContext(), "", dto.UpdatePaymentRequest{})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_id_returns_not_found", func() {
		_, err := s.service.UpdatePayment(s.GetContext(), "pay_missing", dto.UpdatePaymentRequest{})
		s.Error(err)
	})

	s.Run("updates_all_provided_fields", func() {
		p := s.createStoredPayment("pay_upd_fields", nil)
		succeededAt := s.testData.now.Truncate(time.Second)

		resp, err := s.service.UpdatePayment(s.GetContext(), p.ID, dto.UpdatePaymentRequest{
			PaymentStatus:   lo.ToPtr(string(types.PaymentStatusSucceeded)),
			PaymentGateway:  lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
			PaymentMethodID: lo.ToPtr("pm_updated"),
			SucceededAt:     &succeededAt,
			ErrorMessage:    lo.ToPtr("was retried"),
			Metadata:        lo.ToPtr(types.Metadata{"source": "test"}),
		})
		s.NoError(err)
		s.Equal(types.PaymentStatusSucceeded, resp.PaymentStatus)

		stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
		s.Equal(string(types.PaymentGatewayTypeStripe), lo.FromPtr(stored.PaymentGateway))
		s.Equal("pm_updated", stored.PaymentMethodID)
		s.True(succeededAt.Equal(lo.FromPtr(stored.SucceededAt)))
		s.Equal("was retried", lo.FromPtr(stored.ErrorMessage))
		s.Equal("test", stored.Metadata["source"])
	})

	s.Run("gateway_payment_id_promotes_initiated_to_pending", func() {
		p := s.createStoredPayment("pay_upd_promote", func(p *payment.Payment) {
			p.PaymentStatus = types.PaymentStatusInitiated
		})

		_, err := s.service.UpdatePayment(s.GetContext(), p.ID, dto.UpdatePaymentRequest{
			GatewayPaymentID: lo.ToPtr("pi_123"),
		})
		s.NoError(err)

		stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusPending, stored.PaymentStatus)
		s.Equal("pi_123", lo.FromPtr(stored.GatewayPaymentID))
	})

	s.Run("gateway_payment_id_leaves_non_initiated_status_unchanged", func() {
		p := s.createStoredPayment("pay_upd_no_promote", func(p *payment.Payment) {
			p.PaymentStatus = types.PaymentStatusProcessing
		})

		_, err := s.service.UpdatePayment(s.GetContext(), p.ID, dto.UpdatePaymentRequest{
			GatewayPaymentID: lo.ToPtr("pi_456"),
		})
		s.NoError(err)

		stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusProcessing, stored.PaymentStatus)
	})
}

func (s *PaymentCrudSuite) TestDeletePayment() {
	s.Run("empty_id_returns_validation_error", func() {
		err := s.service.DeletePayment(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_id_returns_error", func() {
		err := s.service.DeletePayment(s.GetContext(), "pay_missing")
		s.Error(err)
	})

	s.Run("deleted_payment_is_no_longer_retrievable", func() {
		p := s.createStoredPayment("pay_delete_me", nil)
		s.NoError(s.service.DeletePayment(s.GetContext(), p.ID))

		_, err := s.GetStores().PaymentRepo.Get(s.GetContext(), p.ID)
		s.Error(err)
	})
}

func (s *PaymentCrudSuite) TestGetPaymentByGatewayTrackingID() {
	s.Run("returns_nil_when_no_payment_matches", func() {
		resp, err := s.service.GetPaymentByGatewayTrackingID(s.GetContext(), "cs_missing", "stripe")
		s.NoError(err)
		s.Nil(resp)
	})

	s.Run("returns_payment_when_tracking_id_matches", func() {
		p := s.createStoredPayment("pay_tracking", func(p *payment.Payment) {
			p.PaymentGateway = lo.ToPtr(string(types.PaymentGatewayTypeStripe))
			p.GatewayTrackingID = lo.ToPtr("cs_test_123")
		})

		resp, err := s.service.GetPaymentByGatewayTrackingID(s.GetContext(), "cs_test_123", "stripe")
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(p.ID, resp.ID)
		s.Equal("cs_test_123", lo.FromPtr(resp.GatewayTrackingID))
	})
}

func (s *PaymentCrudSuite) TestPaymentExistsByGatewayPaymentID() {
	s.Run("returns_false_when_no_payment_exists", func() {
		exists, err := s.service.PaymentExistsByGatewayPaymentID(s.GetContext(), "pi_missing")
		s.NoError(err)
		s.False(exists)
	})

	s.Run("returns_true_when_payment_exists", func() {
		s.createStoredPayment("pay_gw_exists", func(p *payment.Payment) {
			p.GatewayPaymentID = lo.ToPtr("pi_exists_123")
		})

		exists, err := s.service.PaymentExistsByGatewayPaymentID(s.GetContext(), "pi_exists_123")
		s.NoError(err)
		s.True(exists)
	})
}

func (s *PaymentCrudSuite) TestCreatePaymentForCheckout() {
	s.Run("nil_request_returns_validation_error", func() {
		_, err := s.service.CreatePaymentForCheckout(s.GetContext(), nil)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("nil_invoice_returns_validation_error", func() {
		_, err := s.service.CreatePaymentForCheckout(s.GetContext(), &dto.CreateCheckoutPaymentRequest{
			Gateway: types.PaymentGatewayTypeStripe,
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("creates_initiated_payment_for_invoice_amount_due", func() {
		resp, err := s.service.CreatePaymentForCheckout(s.GetContext(), &dto.CreateCheckoutPaymentRequest{
			Invoice: s.testData.invoice,
			Gateway: types.PaymentGatewayTypeStripe,
		})
		s.NoError(err)
		s.NotNil(resp)

		stored, err := s.GetStores().PaymentRepo.Get(s.GetContext(), resp.ID)
		s.NoError(err)
		s.Equal(types.PaymentStatusInitiated, stored.PaymentStatus)
		s.Equal(types.PaymentMethodTypePaymentLink, stored.PaymentMethodType)
		s.Equal(s.testData.invoice.ID, stored.DestinationID)
		s.True(stored.Amount.Equal(s.testData.invoice.AmountDue))
		s.Equal(string(types.PaymentGatewayTypeStripe), lo.FromPtr(stored.PaymentGateway))
		s.False(stored.TrackAttempts, "checkout payments are promoted via webhook, not attempt tracking")
		s.NotEmpty(stored.IdempotencyKey)
	})

	s.Run("idempotency_key_is_deterministic_per_invoice_and_gateway", func() {
		first, err := s.service.CreatePaymentForCheckout(s.GetContext(), &dto.CreateCheckoutPaymentRequest{
			Invoice: s.testData.invoice,
			Gateway: types.PaymentGatewayTypeStripe,
		})
		s.NoError(err)

		second, err := s.service.CreatePaymentForCheckout(s.GetContext(), &dto.CreateCheckoutPaymentRequest{
			Invoice: s.testData.invoice,
			Gateway: types.PaymentGatewayTypeStripe,
		})
		s.NoError(err)
		s.Equal(first.IdempotencyKey, second.IdempotencyKey,
			"same invoice+gateway must map to the same idempotency key so duplicates are detectable")
	})
}
