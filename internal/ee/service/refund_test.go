package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/metrics/metricstest"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type RefundServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  RefundService
	testData struct {
		customer *customer.Customer
		invoice  *invoice.Invoice
		now      time.Time
	}
}

func TestRefundService(t *testing.T) {
	suite.Run(t, new(RefundServiceSuite))
}

func (s *RefundServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewRefundService(s.params())
	s.setupTestData()
}

func (s *RefundServiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *RefundServiceSuite) params() ServiceParams {
	return ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		RefundRepo:               s.GetStores().RefundRepo,
		PaymentRepo:              s.GetStores().PaymentRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:      s.GetStores().InvoiceLineItemRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub: types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}
}

func (s *RefundServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:        "cust_refund_1",
		Name:      "Refund Customer",
		Email:     "refund@example.com",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.invoice = &invoice.Invoice{
		ID:            "inv_refund_1",
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusSucceeded,
		Currency:      "USD",
		AmountDue:     decimal.NewFromInt(100),
		AmountPaid:    decimal.NewFromInt(100),
		Total:         decimal.NewFromInt(100),
		InvoiceNumber: lo.ToPtr("INV-REFUND-1"),
		EnvironmentID: "env_test",
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoice))
}

func (s *RefundServiceSuite) createPayment(id string, amount decimal.Decimal, method types.PaymentMethodType, gateway *string, createdAt time.Time) *payment.Payment {
	base := types.GetDefaultBaseModel(s.GetContext())
	base.CreatedAt = createdAt

	p := &payment.Payment{
		ID:                id,
		IdempotencyKey:    id,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     s.testData.invoice.ID,
		PaymentMethodType: method,
		PaymentGateway:    gateway,
		Amount:            amount,
		Currency:          "USD",
		PaymentStatus:     types.PaymentStatusSucceeded,
		EnvironmentID:     "env_test",
		BaseModel:         base,
	}
	if gateway != nil {
		p.GatewayPaymentID = lo.ToPtr("gw_" + id)
	}
	s.NoError(s.GetStores().PaymentRepo.Create(s.GetContext(), p))
	return p
}

func backToSource() *types.RefundTarget {
	return lo.ToPtr(types.RefundTargetBackToSource)
}

func (s *RefundServiceSuite) creditNote(amount decimal.Decimal) *creditnote.CreditNote {
	return &creditnote.CreditNote{
		ID:               "cn_refund_1",
		CreditNoteNumber: "CN-1",
		InvoiceID:        s.testData.invoice.ID,
		CustomerID:       s.testData.customer.ID,
		CreditNoteType:   types.CreditNoteTypeRefund,
		CreditNoteStatus: types.CreditNoteStatusDraft,
		Reason:           types.CreditNoteReasonDuplicate,
		Currency:         "USD",
		TotalAmount:      amount,
		EnvironmentID:    "env_test",
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
}

func (s *RefundServiceSuite) TestPrepareRefundsForCreditNoteSingleCardPayment() {
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(40)), s.testData.invoice, backToSource())
	s.NoError(err)
	s.Len(rows, 1)

	r := rows[0]
	s.Equal(types.RefundDestinationGateway, r.RefundDestination)
	s.Equal("pay_card", lo.FromPtr(r.PaymentID))
	s.Nil(r.RefundDestinationID, "destination is only known once the refund lands")
	s.Equal(r.IdempotencyKey, lo.FromPtr(r.GatewayIdempotencyToken))
	s.Equal("cn_refund_1", lo.FromPtr(r.CreditNoteID))
	s.Equal(s.testData.invoice.ID, r.InvoiceID)
	s.Equal(types.RefundStatusPending, r.RefundStatus)
	s.Equal(types.RefundReasonDuplicate, r.RefundReason)
	s.True(r.Amount.Equal(decimal.NewFromInt(40)))
	s.Equal(1, r.Attempt)

	stored, err := s.GetStores().RefundRepo.ListByInvoice(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.Len(stored, 1)
}

func (s *RefundServiceSuite) TestPrepareRefundsForCreditNoteSplitsAcrossPayments() {
	s.createPayment("pay_card", decimal.NewFromInt(60), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)
	s.createPayment("pay_credits", decimal.NewFromInt(40), types.PaymentMethodTypeCredits, nil, s.testData.now.Add(time.Minute))

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(100)), s.testData.invoice, backToSource())
	s.NoError(err)
	s.Len(rows, 2)

	s.Equal(types.RefundDestinationGateway, rows[0].RefundDestination)
	s.True(rows[0].Amount.Equal(decimal.NewFromInt(60)))

	s.Equal(types.RefundDestinationWallet, rows[1].RefundDestination)
	s.Equal("pay_credits", lo.FromPtr(rows[1].PaymentID))
	s.True(rows[1].Amount.Equal(decimal.NewFromInt(40)))
	s.Nil(rows[1].PaymentGateway)
}

func (s *RefundServiceSuite) TestPrepareRefundsForCreditNoteWithoutPaymentRows() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(100)), s.testData.invoice, nil)
	s.NoError(err)
	s.Len(rows, 1)
	s.Nil(rows[0].PaymentID)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
	s.True(rows[0].Amount.Equal(decimal.NewFromInt(100)))
}

func (s *RefundServiceSuite) TestPrepareRefundsRespectsPerPaymentCapacity() {
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	first, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(70)), s.testData.invoice, backToSource())
	s.NoError(err)
	s.NoError(s.service.Settle(s.GetContext(), &dto.SettleRefundRequest{RefundID: first[0].ID, SettledAmount: first[0].Amount}))

	cn := s.creditNote(decimal.NewFromInt(50))
	cn.ID = "cn_refund_2"
	second, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), cn, s.testData.invoice, backToSource())
	s.NoError(err)
	s.Len(second, 2)

	// Only 30 of the payment can still be given back; the rest cannot go through it.
	s.Equal(types.RefundDestinationGateway, second[0].RefundDestination)
	s.True(second[0].Amount.Equal(decimal.NewFromInt(30)))
	s.Equal(types.RefundDestinationWallet, second[1].RefundDestination)
	s.Nil(second[1].PaymentID)
	s.True(second[1].Amount.Equal(decimal.NewFromInt(20)))
}

func (s *RefundServiceSuite) TestPrepareRefundsForVoidedInvoiceNeverTargetsGateway() {
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	rows, err := s.service.PrepareRefundsForVoidedInvoice(s.GetContext(), s.testData.invoice, decimal.NewFromInt(100))
	s.NoError(err)
	s.Len(rows, 1)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
	s.Equal("pay_card", lo.FromPtr(rows[0].PaymentID))
}

func (s *RefundServiceSuite) TestPrepareRefundsForZeroAmountCreatesNothing() {
	rows, err := s.service.PrepareRefundsForVoidedInvoice(s.GetContext(), s.testData.invoice, decimal.Zero)
	s.NoError(err)
	s.Empty(rows)
}

func (s *RefundServiceSuite) TestSettleIsIdempotent() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(25)), s.testData.invoice, nil)
	s.NoError(err)

	req := &dto.SettleRefundRequest{
		RefundID:      rows[0].ID,
		SettledAmount: decimal.NewFromInt(25),
		DestinationID: lo.ToPtr("rfnd_1"),
	}
	s.NoError(s.service.Settle(s.GetContext(), req))

	after, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.Equal(types.RefundStatusSucceeded, after.RefundStatus)
	s.True(after.SettledAmount.Equal(decimal.NewFromInt(25)))
	firstSucceededAt := after.SucceededAt

	// Replay: the transition guard rejects it and nothing moves.
	s.NoError(s.service.Settle(s.GetContext(), &dto.SettleRefundRequest{
		RefundID:      rows[0].ID,
		SettledAmount: decimal.NewFromInt(999),
	}))

	replayed, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.True(replayed.SettledAmount.Equal(decimal.NewFromInt(25)))
	s.Equal(firstSucceededAt, replayed.SucceededAt)
}

func (s *RefundServiceSuite) TestUnsetTargetKeepsMoneyInTheWallet() {
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(40)), s.testData.invoice, nil)
	s.NoError(err)
	s.Len(rows, 1)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
	s.Equal("pay_card", lo.FromPtr(rows[0].PaymentID))
	s.Nil(rows[0].PaymentGateway)
	s.Nil(rows[0].GatewayIdempotencyToken)
}

func (s *RefundServiceSuite) TestPrepaidWalletTargetMatchesTheDefault() {
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(40)), s.testData.invoice,
		lo.ToPtr(types.RefundTargetPrepaidWallet))
	s.NoError(err)
	s.Len(rows, 1)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
}

func (s *RefundServiceSuite) TestBackToSourceFallsBackToWalletWhenNotRefundableAtTheGateway() {
	s.createPayment("pay_offline", decimal.NewFromInt(100), types.PaymentMethodTypeOffline, nil, s.testData.now)

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(40)), s.testData.invoice, backToSource())
	s.NoError(err)
	s.Len(rows, 1)
	s.Equal(types.RefundDestinationWallet, rows[0].RefundDestination)
}

func (s *RefundServiceSuite) TestUnknownTargetIsRejected() {
	_, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(40)), s.testData.invoice,
		lo.ToPtr(types.RefundTarget("GATEWAY")))
	s.Error(err)
}

func (s *RefundServiceSuite) TestSettleRejectsAmountAboveTheRefund() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(25)), s.testData.invoice, nil)
	s.NoError(err)

	err = s.service.Settle(s.GetContext(), &dto.SettleRefundRequest{
		RefundID:      rows[0].ID,
		SettledAmount: decimal.NewFromInt(26),
	})
	s.Error(err)

	after, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.Equal(types.RefundStatusPending, after.RefundStatus)
	s.True(after.SettledAmount.IsZero())
}

func (s *RefundServiceSuite) TestInFlightRefundsReducePaymentCapacity() {
	s.createPayment("pay_first", decimal.NewFromInt(150), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)
	s.createPayment("pay_second", decimal.NewFromInt(50), types.PaymentMethodTypeOffline, nil, s.testData.now.Add(time.Minute))

	first, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(100)), s.testData.invoice, nil)
	s.NoError(err)
	s.Len(first, 1)
	s.Equal("pay_first", lo.FromPtr(first[0].PaymentID))

	second := s.creditNote(decimal.NewFromInt(100))
	second.ID = "cn_refund_2"
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), second, s.testData.invoice, nil)
	s.NoError(err)

	// pay_first has 50 left once the in-flight 100 is reserved; the rest cannot come from a payment.
	s.Len(rows, 2)
	s.Equal("pay_first", lo.FromPtr(rows[0].PaymentID))
	s.True(rows[0].Amount.Equal(decimal.NewFromInt(50)))
	s.Equal("pay_second", lo.FromPtr(rows[1].PaymentID))
	s.True(rows[1].Amount.Equal(decimal.NewFromInt(50)))
}

func (s *RefundServiceSuite) TestFailFallsBackToWallet() {
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(100)), s.testData.invoice, backToSource())
	s.NoError(err)
	original := rows[0]

	s.NoError(s.service.Fail(s.GetContext(), original.ID, "gateway declined"))

	failed, err := s.GetStores().RefundRepo.Get(s.GetContext(), original.ID)
	s.NoError(err)
	s.Equal(types.RefundStatusFailed, failed.RefundStatus)
	s.Equal("gateway declined", lo.FromPtr(failed.FailureReason))

	fallback := s.fallbackOf(failed)
	s.Equal(2, fallback.Attempt)
	s.Equal(types.RefundDestinationWallet, fallback.RefundDestination)
	s.Equal(original.ID, fallback.Metadata["retry_of"])
	s.True(fallback.Amount.Equal(decimal.NewFromInt(100)))
	s.Nil(fallback.PaymentGateway)

	// Fail dispatches the fallback, which settles into the customer's wallet.
	s.Equal(types.RefundStatusSucceeded, fallback.RefundStatus)
	s.True(fallback.SettledAmount.Equal(decimal.NewFromInt(100)))

	s.NotEmpty(lo.FromPtr(fallback.RefundDestinationID), "wallet transaction id is the destination")
	s.True(s.customerWalletBalance().Equal(decimal.NewFromInt(100)))
}

// Each refund row counts once per status it reaches; a replayed failure adds nothing.
func (s *RefundServiceSuite) TestRefundTransitionsCounted() {
	r := metricstest.Install(s.T())
	s.createPayment("pay_card", decimal.NewFromInt(100), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)
	count := func(provider string, destination types.RefundDestination, status types.RefundStatus) int64 {
		return r.Sum("refund.transitions", map[string]string{
			"provider":    provider,
			"destination": string(destination),
			"reason":      string(types.RefundReasonDuplicate),
			"status":      string(status),
		})
	}
	steps := []struct {
		provider    string
		destination types.RefundDestination
		status      types.RefundStatus
	}{
		{"razorpay", types.RefundDestinationGateway, types.RefundStatusPending},
		{"razorpay", types.RefundDestinationGateway, types.RefundStatusFailed},
		{"none", types.RefundDestinationWallet, types.RefundStatusPending},
		{"none", types.RefundDestinationWallet, types.RefundStatusSucceeded},
	}
	before := make([]int64, len(steps))
	for i, st := range steps {
		before[i] = count(st.provider, st.destination, st.status)
	}

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(100)), s.testData.invoice, backToSource())
	s.Require().NoError(err)
	s.Require().NoError(s.service.Fail(s.GetContext(), rows[0].ID, "gateway declined"))
	s.Require().NoError(s.service.Fail(s.GetContext(), rows[0].ID, "gateway declined again"))

	for i, st := range steps {
		s.Equal(before[i]+1, count(st.provider, st.destination, st.status), "%s %s", st.destination, st.status)
	}
}

func (s *RefundServiceSuite) TestFailOnSettledRowIsNoOp() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(30)), s.testData.invoice, nil)
	s.NoError(err)
	s.NoError(s.service.Settle(s.GetContext(), &dto.SettleRefundRequest{RefundID: rows[0].ID, SettledAmount: rows[0].Amount}))

	s.NoError(s.service.Fail(s.GetContext(), rows[0].ID, "late failure"))

	after, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.Equal(types.RefundStatusSucceeded, after.RefundStatus)

	all, err := s.GetStores().RefundRepo.ListByInvoice(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.Len(all, 1)
}

func (s *RefundServiceSuite) TestDispatchWalletRowSettlesOnce() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(80)), s.testData.invoice, nil)
	s.NoError(err)

	s.NoError(s.service.Dispatch(s.GetContext(), rows[0].ID))
	s.NoError(s.service.Dispatch(s.GetContext(), rows[0].ID))

	after, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.Equal(types.RefundStatusSucceeded, after.RefundStatus)

	s.NotEmpty(lo.FromPtr(after.RefundDestinationID))
	balance := s.customerWalletBalance()
	s.True(balance.Equal(decimal.NewFromInt(80)), "expected 80, got %s", balance)
}

func (s *RefundServiceSuite) TestRetryFailedRefundReusesFallback() {
	s.createPayment("pay_card", decimal.NewFromInt(50), types.PaymentMethodTypeCard, lo.ToPtr("razorpay"), s.testData.now)

	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(50)), s.testData.invoice, backToSource())
	s.NoError(err)
	s.NoError(s.service.Fail(s.GetContext(), rows[0].ID, "gateway declined"))

	failed, err := s.GetStores().RefundRepo.Get(s.GetContext(), rows[0].ID)
	s.NoError(err)
	fallbackID := failed.Metadata["fallback_refund_id"]

	retried, err := s.service.RetryRefund(s.GetContext(), rows[0].ID)
	s.NoError(err)
	s.Equal(fallbackID, retried.ID)

	all, err := s.GetStores().RefundRepo.ListByInvoice(s.GetContext(), s.testData.invoice.ID)
	s.NoError(err)
	s.Len(all, 2)
}

func (s *RefundServiceSuite) TestRetrySettledRefundRejected() {
	rows, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(10)), s.testData.invoice, nil)
	s.NoError(err)
	s.NoError(s.service.Settle(s.GetContext(), &dto.SettleRefundRequest{RefundID: rows[0].ID, SettledAmount: rows[0].Amount}))

	_, err = s.service.RetryRefund(s.GetContext(), rows[0].ID)
	s.Error(err)
}

func (s *RefundServiceSuite) TestListRefundsByInvoice() {
	_, err := s.service.PrepareRefundsForCreditNote(s.GetContext(), s.creditNote(decimal.NewFromInt(10)), s.testData.invoice, nil)
	s.NoError(err)

	resp, err := s.service.ListRefunds(s.GetContext(), &types.RefundFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
		InvoiceIDs:  []string{s.testData.invoice.ID},
	})
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.Equal(1, resp.Pagination.Total)
}

func (s *RefundServiceSuite) fallbackOf(failed *refund.Refund) *refund.Refund {
	id := failed.Metadata["fallback_refund_id"]
	s.NotEmpty(id)
	r, err := s.GetStores().RefundRepo.Get(s.GetContext(), id)
	s.NoError(err)
	return r
}

func (s *RefundServiceSuite) customerWalletBalance() decimal.Decimal {
	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.Len(wallets, 1)
	return wallets[0].Balance
}
