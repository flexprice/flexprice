package service

import (
	"sync"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PaymentServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  PaymentService
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
	}
}

func TestPaymentService(t *testing.T) {
	suite.Run(t, new(PaymentServiceSuite))
}

func (s *PaymentServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *PaymentServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *PaymentServiceSuite) setupService() {
	// Create the PaymentService
	s.service = NewPaymentService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		SubRepo:          s.GetStores().SubscriptionRepo,
		PlanRepo:         s.GetStores().PlanRepo,
		PriceRepo:        s.GetStores().PriceRepo,
		EventRepo:        s.GetStores().EventRepo,
		MeterRepo:        s.GetStores().MeterRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		EntitlementRepo:  s.GetStores().EntitlementRepo,
		EnvironmentRepo:  s.GetStores().EnvironmentRepo,
		FeatureRepo:      s.GetStores().FeatureRepo,
		TenantRepo:       s.GetStores().TenantRepo,
		UserRepo:         s.GetStores().UserRepo,
		AuthRepo:         s.GetStores().AuthRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		PaymentRepo:      s.GetStores().PaymentRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

func (s *PaymentServiceSuite) setupTestData() {
	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_test_payment",
		ExternalID: "ext_cust_test_payment",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test invoice
	s.testData.invoice = &invoice.Invoice{
		ID:              "inv_test_payment",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromFloat(100),
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.NewFromFloat(100),
		Description:     "Test Invoice for Payment Links",
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), s.testData.invoice))
}

func (s *PaymentServiceSuite) TestCreatePaymentWithPaymentLink() {
	// Add environment ID to context
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	// Test creating a payment with PAYMENT_LINK method type
	req := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            decimal.NewFromFloat(100),
		Currency:          "usd",
		ProcessPayment:    false, // Don't process immediately
	}

	resp, err := s.service.CreatePayment(ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(types.PaymentMethodTypePaymentLink, resp.PaymentMethodType)
	s.Equal("stripe", *resp.PaymentGateway)
	s.Equal(types.PaymentStatusInitiated, resp.PaymentStatus)
}

func (s *PaymentServiceSuite) TestCreatePaymentLink_InitiatedStatus() {
	// Add environment ID to context
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	// Create payment link request
	req := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            decimal.NewFromInt(1000),
		Currency:          "usd",
		ProcessPayment:    false, // Don't process immediately to avoid Stripe calls
	}

	// Test that payment is created with INITIATED status
	paymentResp, err := s.service.CreatePayment(ctx, req)
	s.NoError(err)
	s.NotNil(paymentResp)
	s.Equal(types.PaymentStatusInitiated, paymentResp.PaymentStatus)

	// Verify payment was created in database
	payment, err := s.GetStores().PaymentRepo.Get(ctx, paymentResp.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
	s.Equal(types.PaymentMethodTypePaymentLink, payment.PaymentMethodType)
	s.Equal(string(types.PaymentGatewayTypeStripe), *payment.PaymentGateway)
}

func (s *PaymentServiceSuite) TestCreatePaymentLink_TaxIDCollection() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	baseReq := func() *dto.CreatePaymentRequest {
		return &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     s.testData.invoice.ID,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
			Amount:            decimal.NewFromInt(1000),
			Currency:          "usd",
			ProcessPayment:    false, // Don't process immediately to avoid Stripe calls
		}
	}

	s.Run("enabled_on_stripe_payment_link_persists_to_gateway_metadata", func() {
		req := baseReq()
		req.GatewayOptions = &dto.PaymentGatewayOptions{
			Stripe: &dto.StripePaymentGatewayOptions{TaxIDCollectionEnabled: lo.ToPtr(true)},
		}

		resp, err := s.service.CreatePayment(ctx, req)
		s.Require().NoError(err)

		stored, err := s.GetStores().PaymentRepo.Get(ctx, resp.ID)
		s.Require().NoError(err)
		s.Equal("true", stored.GatewayMetadata["tax_id_collection_enabled"])
	})

	s.Run("rejected_when_not_payment_link", func() {
		req := baseReq()
		req.PaymentMethodType = types.PaymentMethodTypeCard
		req.PaymentMethodID = "pm_test"
		req.GatewayOptions = &dto.PaymentGatewayOptions{
			Stripe: &dto.StripePaymentGatewayOptions{TaxIDCollectionEnabled: lo.ToPtr(true)},
		}

		_, err := s.service.CreatePayment(ctx, req)
		s.Require().Error(err)
		s.True(ierr.IsValidation(err), "expected validation-class error, got: %v", err)
	})

	s.Run("rejected_when_gateway_not_stripe", func() {
		req := baseReq()
		req.PaymentGateway = lo.ToPtr(types.PaymentGatewayTypeRazorpay)
		req.GatewayOptions = &dto.PaymentGatewayOptions{
			Stripe: &dto.StripePaymentGatewayOptions{TaxIDCollectionEnabled: lo.ToPtr(true)},
		}

		_, err := s.service.CreatePayment(ctx, req)
		s.Require().Error(err)
		s.True(ierr.IsValidation(err), "expected validation-class error, got: %v", err)
	})
}

func (s *PaymentServiceSuite) TestPaymentProcessor_PaymentLinkFlow() {
	// Add environment ID to context
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	// Create payment link request without processing
	req := &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            decimal.NewFromInt(1000),
		Currency:          "usd",
		ProcessPayment:    false, // Don't process immediately
	}

	// Create payment with INITIATED status
	paymentResp, err := s.service.CreatePayment(ctx, req)
	s.NoError(err)
	s.NotNil(paymentResp)
	s.Equal(types.PaymentStatusInitiated, paymentResp.PaymentStatus)

	// Verify payment was created in database with INITIATED status
	payment, err := s.GetStores().PaymentRepo.Get(ctx, paymentResp.ID)
	s.NoError(err)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)

	// Test that payment processor accepts INITIATED status for payment links
	// We'll just verify that the payment object has the correct status
	// without actually calling the processor (which would require Stripe setup)
	s.Equal(types.PaymentStatusInitiated, payment.PaymentStatus)
	s.Equal(types.PaymentMethodTypePaymentLink, payment.PaymentMethodType)
	s.Equal(string(types.PaymentGatewayTypeStripe), *payment.PaymentGateway)

	// Verify that the payment is in a state that would be accepted by the processor
	s.True(payment.PaymentStatus == types.PaymentStatusInitiated || payment.PaymentStatus == types.PaymentStatusPending)
}

func (s *PaymentServiceSuite) TestSyncPaymentStatusFromGateway_GuardConditions() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	tests := []struct {
		name   string
		status types.PaymentStatus
		gwID   *string
		gw     *string
	}{
		{
			name:   "terminal FAILED — no sync",
			status: types.PaymentStatusFailed,
			gwID:   lo.ToPtr("pay_xyz"),
			gw:     lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
		},
		{
			name:   "INITIATED — no sync",
			status: types.PaymentStatusInitiated,
			gwID:   lo.ToPtr("pay_xyz"),
			gw:     lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
		},
		{
			name:   "PENDING nil gateway_payment_id — no sync",
			status: types.PaymentStatusPending,
			gwID:   nil,
			gw:     lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
		},
		{
			name:   "PENDING nil gateway — no sync",
			status: types.PaymentStatusPending,
			gwID:   lo.ToPtr("pay_xyz"),
			gw:     nil,
		},
		{
			name:   "PENDING unsupported gateway nomod — no sync",
			status: types.PaymentStatusPending,
			gwID:   lo.ToPtr("nomod_ref"),
			gw:     lo.ToPtr(string(types.PaymentGatewayTypeNomod)),
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			p := &payment.Payment{
				ID:                "pay_guard_" + tt.name,
				DestinationType:   types.PaymentDestinationTypeInvoice,
				DestinationID:     s.testData.invoice.ID,
				PaymentMethodType: types.PaymentMethodTypePaymentLink,
				PaymentStatus:     tt.status,
				Amount:            decimal.NewFromFloat(100),
				Currency:          "usd",
				GatewayPaymentID:  tt.gwID,
				PaymentGateway:    tt.gw,
				BaseModel:         types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().PaymentRepo.Create(ctx, p))

			svc := s.service.(*paymentService)
			result, err := svc.syncPaymentStatusFromGateway(ctx, p)
			s.NoError(err)
			s.Equal(p.ID, result.ID)
			s.Equal(tt.status, result.PaymentStatus)
		})
	}
}

// TestCreatePayment_CustomerDestination_ValidatesCustomer covers the VAPT fix:
// CUSTOMER destination must now verify the customer exists in this tenant/env,
// so bogus/foreign customer_ids fail-fast at CreatePayment rather than producing
// an orphaned payment row (which would go on to fail later in ProcessPayment).
func (s *PaymentServiceSuite) TestCreatePayment_CustomerDestination_ValidatesCustomer() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	s.Run("missing_customer_rejected", func() {
		req := &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeCustomer,
			DestinationID:     "cust_does_not_exist",
			PaymentMethodType: types.PaymentMethodTypeCard,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			ProcessPayment:    false,
		}
		_, err := s.service.CreatePayment(ctx, req)
		s.Require().Error(err, "unknown customer_id must be rejected")
		s.True(ierr.IsValidation(err), "expected validation-class error (not 500), got: %v", err)
	})

	s.Run("existing_customer_ok", func() {
		req := &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeCustomer,
			DestinationID:     s.testData.customer.ID,
			PaymentMethodType: types.PaymentMethodTypeCard,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			ProcessPayment:    false,
		}
		resp, err := s.service.CreatePayment(ctx, req)
		s.Require().NoError(err, "existing customer must be accepted")
		s.Equal(types.PaymentDestinationTypeCustomer, resp.DestinationType)
		s.Equal(s.testData.customer.ID, resp.DestinationID)
	})
}

// The VAPT finding: CreatePayment previously auto-generated its idempotency key
// from time.Now() at second precision, so a client timeout + retry produced a
// different key, bypassed the unique constraint, and inserted a second payment
// row. With process_payment=true that meant a second real gateway charge against
// the customer's card.
//
// The key is now deterministic on the payment intent, and Create surfaces the
// unique-constraint violation as ErrAlreadyExists which CreatePayment catches and
// resolves by returning the existing payment.
func (s *PaymentServiceSuite) TestCreatePayment_RetryIsIdempotent() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	newReq := func() *dto.CreatePaymentRequest {
		return &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     s.testData.invoice.ID,
			PaymentMethodType: types.PaymentMethodTypeOffline,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			ProcessPayment:    false,
		}
	}

	first, err := s.service.CreatePayment(ctx, newReq())
	s.Require().NoError(err)
	s.Require().NotEmpty(first.IdempotencyKey, "auto-generated key must be set")

	// The retry: identical intent, no caller-supplied key.
	second, err := s.service.CreatePayment(ctx, newReq())
	s.Require().NoError(err, "retry must not error — it must return the existing payment")
	s.Equal(first.ID, second.ID, "retry created a duplicate payment row (the duplicate-charge bug)")
	s.Equal(first.IdempotencyKey, second.IdempotencyKey, "auto-generated key must be deterministic")
}

// Callers that genuinely want a second payment for the same intent (installments,
// partial re-attempts) opt out by supplying their own key. This is the escape
// hatch that makes the deterministic default safe to ship.
func (s *PaymentServiceSuite) TestCreatePayment_DistinctIdempotencyKeysCreateDistinctPayments() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	newReq := func(key string) *dto.CreatePaymentRequest {
		return &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     s.testData.invoice.ID,
			PaymentMethodType: types.PaymentMethodTypeOffline,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			ProcessPayment:    false,
			IdempotencyKey:    key,
		}
	}

	first, err := s.service.CreatePayment(ctx, newReq("installment-1"))
	s.Require().NoError(err)

	second, err := s.service.CreatePayment(ctx, newReq("installment-2"))
	s.Require().NoError(err)

	s.NotEqual(first.ID, second.ID, "distinct caller-supplied keys must create distinct payments")
}

// The concurrency case CodeRabbit and CodeAnt both flagged. Lookup-then-insert is
// not atomic, so two in-flight requests can both miss and both attempt Create.
// The loser must receive the winner's payment rather than a raw constraint error.
func (s *PaymentServiceSuite) TestCreatePayment_ConcurrentRetriesReturnSamePayment() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	const concurrent = 8
	var wg sync.WaitGroup
	ids := make([]string, concurrent)
	errs := make([]error, concurrent)

	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func(i int) {
			defer wg.Done()
			resp, err := s.service.CreatePayment(ctx, &dto.CreatePaymentRequest{
				DestinationType:   types.PaymentDestinationTypeInvoice,
				DestinationID:     s.testData.invoice.ID,
				PaymentMethodType: types.PaymentMethodTypeOffline,
				Amount:            decimal.NewFromInt(100),
				Currency:          "usd",
				ProcessPayment:    false,
			})
			errs[i] = err
			if err == nil {
				ids[i] = resp.ID
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		s.Require().NoError(err, "concurrent request %d failed instead of returning the existing payment", i)
	}

	unique := lo.Uniq(ids)
	s.Len(unique, 1, "concurrent identical requests produced %d distinct payments (duplicate charges)", len(unique))
}

// A constraint violation that is NOT the idempotency-key index (a duplicate
// payment ID, say) is also marked ErrAlreadyExists by the repository. Treating
// every ErrAlreadyExists as an idempotency conflict made CreatePayment re-fetch
// by a key that was never inserted, turning a 409 into a misleading 404.
func (s *PaymentServiceSuite) TestCreatePayment_NonIdempotencyConstraintErrorIsNotMasked() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	err := s.GetStores().PaymentRepo.Create(ctx, &payment.Payment{
		ID:                "pay_duplicate_id_probe",
		IdempotencyKey:    "key-already-taken",
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            decimal.NewFromInt(100),
		Currency:          "usd",
		PaymentStatus:     types.PaymentStatusPending,
		BaseModel:         types.GetDefaultBaseModel(ctx),
	})
	s.Require().NoError(err)

	// Same ID, different idempotency key: a non-idempotency constraint failure.
	err = s.GetStores().PaymentRepo.Create(ctx, &payment.Payment{
		ID:                "pay_duplicate_id_probe",
		IdempotencyKey:    "a-different-key",
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            decimal.NewFromInt(100),
		Currency:          "usd",
		PaymentStatus:     types.PaymentStatusPending,
		BaseModel:         types.GetDefaultBaseModel(ctx),
	})
	s.Require().Error(err)
	s.False(payment.IsIdempotencyKeyConflict(err),
		"a duplicate-ID violation must not be tagged as an idempotency conflict")
	s.False(ierr.IsNotFound(err), "must not surface as ErrNotFound")
}

// Payments created without an explicit TenantID take it from context. The store
// must persist that resolved tenant, otherwise the row is kept with an empty
// TenantID, a later retry compares "" against the context tenant, the duplicate
// goes undetected, and the idempotency guarantee silently disappears.
func (s *PaymentServiceSuite) TestCreatePayment_RetryIsIdempotentWhenTenantComesFromContext() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	newPayment := func() *payment.Payment {
		return &payment.Payment{
			ID:                types.GenerateUUIDWithPrefix("pay"),
			IdempotencyKey:    "tenant-from-context-key",
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     s.testData.invoice.ID,
			PaymentMethodType: types.PaymentMethodTypeOffline,
			Amount:            decimal.NewFromInt(100),
			Currency:          "usd",
			PaymentStatus:     types.PaymentStatusPending,
			EnvironmentID:     "test-env-id",
			// TenantID deliberately left empty — resolved from ctx.
		}
	}

	s.Require().NoError(s.GetStores().PaymentRepo.Create(ctx, newPayment()))

	// The retry carries the tenant explicitly — as a caller that populated
	// BaseModel would. If the first insert was stored with an empty TenantID,
	// this comparison misses and the duplicate slips through.
	retry := newPayment()
	retry.TenantID = types.GetTenantID(ctx)
	s.Require().NotEmpty(retry.TenantID, "test context must carry a tenant for this to be meaningful")

	err := s.GetStores().PaymentRepo.Create(ctx, retry)
	s.Require().Error(err, "second insert with the same key must be rejected")
	s.True(payment.IsIdempotencyKeyConflict(err),
		"context-resolved tenant must still be matched by the uniqueness check")
}

// A status update is written only while the stored status still matches the
// one the transition was validated against, so a decision made from a stale
// read cannot overwrite a concurrent update.
func (s *PaymentServiceSuite) TestUpdatePaymentRejectsConcurrentStatusChange() {
	ctx := s.GetContext()
	repo := s.GetStores().PaymentRepo

	p := &payment.Payment{
		ID:                "pay_concurrent_status",
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentStatus:     types.PaymentStatusPending,
		Amount:            decimal.NewFromFloat(100),
		Currency:          "usd",
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.NoError(repo.Create(ctx, p))

	// Another writer settles the payment first.
	concurrent, err := repo.Get(ctx, p.ID)
	s.NoError(err)
	concurrent.PaymentStatus = types.PaymentStatusSucceeded
	s.NoError(repo.Update(ctx, concurrent))

	// This update was validated against PENDING and must not apply on top.
	_, err = s.service.UpdatePayment(ctx, p.ID, dto.UpdatePaymentRequest{
		PaymentStatus: lo.ToPtr(string(types.PaymentStatusFailed)),
	})
	s.Error(err)

	stored, getErr := repo.Get(ctx, p.ID)
	s.NoError(getErr)
	s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus,
		"the concurrent writer's status must survive")
}

// The guard must not reject an update when nothing else touched the payment.
func (s *PaymentServiceSuite) TestUpdatePaymentAppliesWhenStatusUnchanged() {
	ctx := s.GetContext()
	repo := s.GetStores().PaymentRepo

	p := &payment.Payment{
		ID:                "pay_uncontended_status",
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentStatus:     types.PaymentStatusPending,
		Amount:            decimal.NewFromFloat(100),
		Currency:          "usd",
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.NoError(repo.Create(ctx, p))

	_, err := s.service.UpdatePayment(ctx, p.ID, dto.UpdatePaymentRequest{
		PaymentStatus: lo.ToPtr(string(types.PaymentStatusSucceeded)),
	})
	s.NoError(err)

	stored, getErr := repo.Get(ctx, p.ID)
	s.NoError(getErr)
	s.Equal(types.PaymentStatusSucceeded, stored.PaymentStatus)
}

// The repository reports a conflict rather than silently writing nothing.
func (s *PaymentServiceSuite) TestUpdateWithExpectedStatusReportsConflict() {
	ctx := s.GetContext()
	repo := s.GetStores().PaymentRepo

	p := &payment.Payment{
		ID:                "pay_cas_conflict",
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentStatus:     types.PaymentStatusPending,
		Amount:            decimal.NewFromFloat(100),
		Currency:          "usd",
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.NoError(repo.Create(ctx, p))

	p.PaymentStatus = types.PaymentStatusSucceeded
	err := repo.UpdateWithExpectedStatus(ctx, p, types.PaymentStatusFailed)
	s.Error(err)
	s.True(ierr.IsVersionConflict(err), "expected a version conflict, got: %v", err)
}

// Deleting is conditioned on the status the deletability check was made
// against, so a payment that settles between the read and the write is not
// deleted on the strength of a stale check.
func (s *PaymentServiceSuite) TestDeletePaymentRejectsConcurrentStatusChange() {
	ctx := s.GetContext()
	repo := s.GetStores().PaymentRepo

	p := &payment.Payment{
		ID:                "pay_delete_raced",
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentStatus:     types.PaymentStatusInitiated,
		Amount:            decimal.NewFromFloat(100),
		Currency:          "usd",
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.NoError(repo.Create(ctx, p))

	// The payment settles after it was read as deletable.
	settled, err := repo.Get(ctx, p.ID)
	s.NoError(err)
	settled.PaymentStatus = types.PaymentStatusSucceeded
	s.NoError(repo.Update(ctx, settled))

	err = repo.DeleteWithExpectedStatus(ctx, p.ID, types.PaymentStatusInitiated)
	s.Error(err, "a settled payment must not be deleted on a stale deletability check")
	s.True(ierr.IsVersionConflict(err), "expected a version conflict, got: %v", err)
}
