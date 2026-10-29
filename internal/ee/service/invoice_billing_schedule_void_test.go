package service

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/metrics/metricstest"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type invoiceVoidTermsStub struct {
	billingterms.Service
	run func(context.Context, billingterms.InvoiceVoidRequest, func(context.Context) error) error
}

func (s *invoiceVoidTermsStub) WithInvoiceVoid(ctx context.Context, req billingterms.InvoiceVoidRequest, apply func(context.Context) error) error {
	return s.run(ctx, req, apply)
}

func (s *InvoiceVoidRecalculateSuite) attachVoidSchedule(inv *invoice.Invoice) *invoice.InvoiceLineItem {
	ctx := s.GetContext()
	line := invoice.NewInvoiceLineItemBuilder(&invoice.InvoiceLineItem{
		ID: "line_" + inv.ID, InvoiceID: inv.ID, CustomerID: inv.CustomerID, SubscriptionID: inv.SubscriptionID,
		Amount: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1), Currency: inv.Currency,
		PeriodStart: inv.PeriodStart, PeriodEnd: inv.PeriodEnd, CommitmentID: lo.ToPtr("commitment_void"),
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithBillingScheduleItemID(lo.ToPtr("schedule_" + inv.ID)).Build()
	s.Require().NoError(s.invoiceRepo.AddLineItems(ctx, inv.ID, []*invoice.InvoiceLineItem{line}))
	return line
}

func (s *InvoiceVoidRecalculateSuite) TestScheduledVoid_RequiresCoordinatorBeforeRefunds() {
	ctx := s.GetContext()
	inv := s.buildFinalizedInvoice("scheduled_missing_core", decimal.NewFromInt(40), decimal.Zero, types.PaymentStatusSucceeded)
	s.attachVoidSchedule(inv)
	_, err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.True(ierr.IsServiceUnavailable(err), "%+v", err)
	stored, err := s.invoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.True(stored.RefundedAmount.IsZero())
	refunds, err := s.GetStores().RefundRepo.List(ctx, types.NewNoLimitRefundFilter())
	s.Require().NoError(err)
	s.Empty(refunds)
}

func (s *InvoiceVoidRecalculateSuite) TestScheduledVoid_PublishesAfterCommitAndDoesNotRepeat() {
	ctx := types.WithPostCommitHooks(s.GetContext())
	inv := s.buildFinalizedInvoice("scheduled_commit", decimal.Zero, decimal.Zero, types.PaymentStatusPending)
	line := s.attachVoidSchedule(inv)
	pub := &refundPostCommitPublisher{}
	svc := s.service.(*invoiceService)
	svc.WebhookPublisher = pub
	callbacks, cancellations := 0, 0
	svc.BillingTerms = &invoiceVoidTermsStub{run: func(tx context.Context, req billingterms.InvoiceVoidRequest, apply func(context.Context) error) error {
		s.Equal(inv.ID, req.InvoiceID)
		s.Equal([]billingterms.InvoiceSource{{ItemID: *line.BillingScheduleItemID, InvoiceLineItemID: line.ID}}, req.Sources)
		current, err := s.invoiceRepo.Get(tx, inv.ID)
		if err != nil {
			return err
		}
		if current.InvoiceStatus == types.InvoiceStatusVoided {
			return nil
		}
		callbacks++
		if err := apply(tx); err != nil {
			return err
		}
		stored, err := s.invoiceRepo.Get(tx, inv.ID)
		if err != nil {
			return err
		}
		s.Equal(types.InvoiceStatusVoided, stored.InvoiceStatus, "the callback persists voiding before source cancellation")
		cancellations++
		return nil
	}}
	got, err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusVoided, got.InvoiceStatus)
	s.Empty(pub.events)
	types.RunPostCommitHooks(ctx)
	s.Require().Len(pub.events, 1)
	s.Equal(types.WebhookEventInvoiceUpdateVoided, pub.events[0].EventName)
	s.Nil(pub.contexts[0].Value(types.CtxDBTransaction))
	_, err = s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)
	s.Len(pub.events, 1)
	s.Equal(1, callbacks)
	s.Equal(1, cancellations)
}

func (s *InvoiceVoidRecalculateSuite) TestScheduledVoid_CancelFailurePublishesNothing() {
	ctx := types.WithPostCommitHooks(s.GetContext())
	inv := s.buildFinalizedInvoice("scheduled_failed_cancel", decimal.Zero, decimal.Zero, types.PaymentStatusPending)
	s.attachVoidSchedule(inv)
	pub := &refundPostCommitPublisher{}
	svc := s.service.(*invoiceService)
	svc.WebhookPublisher = pub
	failure := errors.New("source cancellation failed")
	svc.BillingTerms = &invoiceVoidTermsStub{run: func(tx context.Context, _ billingterms.InvoiceVoidRequest, apply func(context.Context) error) error {
		if err := apply(tx); err != nil {
			return err
		}
		return failure
	}}
	_, err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.ErrorIs(err, failure)
	types.DiscardPostCommitHooks(ctx)
	types.RunPostCommitHooks(ctx)
	s.Empty(pub.events, "a failed cancellation must not announce a void")
}

func (s *InvoiceVoidRecalculateSuite) TestVoid_PostCommitUsesCommittedSnapshot() {
	reader := metricstest.Install(s.T())
	ctx := types.WithPostCommitHooks(s.GetContext())
	inv := s.buildFinalizedInvoice("void_snapshot", decimal.Zero, decimal.Zero, types.PaymentStatusPending)
	count := func(status types.InvoiceStatus) int64 {
		return reader.Sum("invoice.transitions", map[string]string{"invoice_type": string(inv.InvoiceType), "billing_reason": inv.BillingReason, "status": string(status)})
	}
	beforeVoided, beforeDraft := count(types.InvoiceStatusVoided), count(types.InvoiceStatusDraft)
	got, err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)
	s.Equal(beforeVoided, count(types.InvoiceStatusVoided))
	got.InvoiceStatus = types.InvoiceStatusDraft
	got.Currency = "eur"
	types.RunPostCommitHooks(ctx)
	s.Equal(beforeVoided+1, count(types.InvoiceStatusVoided))
	s.Equal(beforeDraft, count(types.InvoiceStatusDraft), "caller mutation cannot change the committed transition")
}
