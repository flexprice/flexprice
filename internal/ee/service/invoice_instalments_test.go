package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// januaryDraft subscribes from 1 Jan to a commitment billing $300 upfront and $70 a month in arrears, and computes the
// January draft the way the daily run does. It returns the draft and the commitment id.
func (s *CommitmentE2ESuite) januaryDraft(invoices InvoiceService, name string) (*invoice.Invoice, string) {
	ctx := s.GetContext()
	tokens := s.planWith(name, types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), upfrontPlusRemainder()...))

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	s.Require().True(sub.CurrentPeriodStart.Equal(s.jan), "current period starts %s", sub.CurrentPeriodStart)

	draft, skipped, err := invoices.GetOrComputeCurrentPeriodDraft(ctx, sub)
	s.Require().NoError(err)
	s.Require().False(skipped)
	s.Require().Len(s.storedInstalmentLines(draft.ID), 2)
	return draft, committed.ID
}

// billedInstalments returns the instalments the invoice's lines bill.
func (s *CommitmentE2ESuite) billedInstalments(invoiceID string) []*billingterm.Instalment {
	ids := lo.Map(s.storedInstalmentLines(invoiceID), func(line *invoice.InvoiceLineItem, _ int) string {
		return *line.BillingInstalmentID
	})
	s.Require().NotEmpty(ids)

	instalments, err := s.GetStores().BillingInstalmentRepo.List(s.GetContext(), &billingterm.InstalmentFilter{IDs: ids})
	s.Require().NoError(err)
	s.Require().Len(instalments, len(ids))
	return instalments
}

// instalmentEvents returns the ids of the instalments that published name events describe.
func (s *CommitmentE2ESuite) instalmentEvents(name types.WebhookEventName) []string {
	events := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events()
	return lo.FilterMap(events, func(event *types.WebhookEvent, _ int) (string, bool) {
		return event.EntityID, event.EventName == name
	})
}

func instalmentIDs(instalments []*billingterm.Instalment) []string {
	return lo.Map(instalments, func(inst *billingterm.Instalment, _ int) string { return inst.ID })
}

// Computing the draft leaves its instalments scheduled; finalizing it marks each one invoiced by it, with one event.
func (s *CommitmentE2ESuite) TestFinalize_ClaimsInstalments() {
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, _ := s.januaryDraft(invoices, "claim")
	for _, inst := range s.billedInstalments(draft.ID) {
		s.Equal(types.InstalmentStatusScheduled, inst.InstalmentStatus, "computing never claims")
		s.Nil(inst.InvoiceID)
	}

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	s.Require().NoError(invoices.FinalizeInvoice(s.GetContext(), draft.ID, dto.FinalizeInvoiceRequest{}))

	billed := s.billedInstalments(draft.ID)
	for _, inst := range billed {
		s.Equal(types.InstalmentStatusInvoiced, inst.InstalmentStatus)
		s.Equal(draft.ID, lo.FromPtr(inst.InvoiceID))
	}
	s.ElementsMatch(instalmentIDs(billed), s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))
}

// An edit after the draft was computed cancels the instalments it carries, so finalizing fails until a recompute
// replaces them with the new terms' instalments.
func (s *CommitmentE2ESuite) TestFinalize_StaleDraftFails() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, commitmentID := s.januaryDraft(invoices, "stale")
	stale := instalmentIDs(s.billedInstalments(draft.ID))

	_, err := s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	err = invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{})
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	s.Empty(s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)

	_, err = invoices.RecalculateInvoiceV2(ctx, draft.ID, false)
	s.Require().NoError(err)
	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))

	billed := s.billedInstalments(draft.ID)
	s.Len(billed, 2)
	for _, inst := range billed {
		s.NotContains(stale, inst.ID, "the cancelled instalment left the recomputed draft")
		s.Equal(types.InstalmentStatusInvoiced, inst.InstalmentStatus)
		s.Equal(draft.ID, lo.FromPtr(inst.InvoiceID))
	}
}

// Voiding puts the invoice's instalments back on schedule and recalculating bills them on the replacement. Instalments
// an edit cancelled stay cancelled when the invoice carrying them is voided.
func (s *CommitmentE2ESuite) TestVoid_ReschedulesInstalments() {
	ctx := s.GetContext()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, commitmentID := s.januaryDraft(invoices, "void")
	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))

	publisher.Reset()
	_, err := invoices.VoidInvoice(ctx, draft.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)

	rescheduled := s.billedInstalments(draft.ID)
	for _, inst := range rescheduled {
		s.Equal(types.InstalmentStatusScheduled, inst.InstalmentStatus)
		s.Nil(inst.InvoiceID)
	}
	s.ElementsMatch(instalmentIDs(rescheduled), s.instalmentEvents(types.WebhookEventBillingInstalmentUpdated))

	replacement, err := invoices.RecalculateInvoice(ctx, draft.ID)
	s.Require().NoError(err)
	s.Require().Equal(types.InvoiceStatusFinalized, replacement.InvoiceStatus)

	rebilled := s.billedInstalments(replacement.ID)
	s.ElementsMatch(instalmentIDs(rescheduled), instalmentIDs(rebilled))
	for _, inst := range rebilled {
		s.Equal(types.InstalmentStatusInvoiced, inst.InstalmentStatus)
		s.Equal(replacement.ID, lo.FromPtr(inst.InvoiceID))
	}

	_, err = s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	publisher.Reset()
	_, err = invoices.VoidInvoice(ctx, replacement.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)

	for _, inst := range s.billedInstalments(replacement.ID) {
		s.Equal(types.InstalmentStatusCancelled, inst.InstalmentStatus)
		s.Equal(replacement.ID, lo.FromPtr(inst.InvoiceID))
	}
	s.Empty(s.instalmentEvents(types.WebhookEventBillingInstalmentUpdated))
}

// A draft paid in full is finalized by the payment and claims its instalments.
func (s *CommitmentE2ESuite) TestPaymentFinalize_ClaimsInstalments() {
	ctx := s.GetContext()
	params := fullServiceParams(&s.BaseServiceTestSuite)
	computed, _ := s.januaryDraft(NewInvoiceService(params), "paid")

	draft, err := s.GetStores().InvoiceRepo.Get(ctx, computed.ID)
	s.Require().NoError(err)
	s.Require().True(draft.AmountDue.IsPositive())

	paid := &payment.Payment{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     draft.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            draft.AmountDue,
		Currency:          draft.Currency,
		PaymentStatus:     types.PaymentStatusSucceeded,
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PaymentRepo.Create(ctx, paid))

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	processor := &paymentProcessor{ServiceParams: params}
	s.Require().NoError(processor.handleInvoicePostProcessing(ctx, paid))

	finalized, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)

	billed := s.billedInstalments(draft.ID)
	for _, inst := range billed {
		s.Equal(types.InstalmentStatusInvoiced, inst.InstalmentStatus)
		s.Equal(draft.ID, lo.FromPtr(inst.InvoiceID))
	}
	s.ElementsMatch(instalmentIDs(billed), s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))
}
