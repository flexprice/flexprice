package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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
		s.Nil(inst.InvoicedAt)
	}

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	s.Require().NoError(invoices.FinalizeInvoice(s.GetContext(), draft.ID, dto.FinalizeInvoiceRequest{}))
	s.requireBilledBy(draft.ID, instalmentIDs(s.billedInstalments(draft.ID)))
	s.ElementsMatch(instalmentIDs(s.billedInstalments(draft.ID)), s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))
}

// requireBilledBy checks that the invoice is finalized and that its instalment lines bill exactly the instalments
// want, each invoiced by it when it was finalized.
func (s *CommitmentE2ESuite) requireBilledBy(invoiceID string, want []string) {
	stored, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.Require().NotNil(stored.FinalizedAt)

	billed := s.billedInstalments(invoiceID)
	s.ElementsMatch(want, instalmentIDs(billed))
	for _, inst := range billed {
		s.Equal(types.InstalmentStatusInvoiced, inst.InstalmentStatus)
		s.Equal(invoiceID, lo.FromPtr(inst.InvoiceID))
		s.True(stored.FinalizedAt.Equal(lo.FromPtr(inst.InvoicedAt)), "invoiced at %v, finalized at %v", inst.InvoicedAt, stored.FinalizedAt)
	}
}

// An edit after the draft was computed cancels the instalments it carries, so finalizing recomputes the draft with
// the new terms' instalments and finalizes it.
func (s *CommitmentE2ESuite) TestFinalize_EditedTermsRecomputeTheDraft() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, commitmentID := s.januaryDraft(invoices, "stale")
	stale := instalmentIDs(s.billedInstalments(draft.ID))

	_, err := s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))

	billed := s.billedInstalments(draft.ID)
	s.Require().Len(billed, 2)
	for _, inst := range billed {
		s.NotContains(stale, inst.ID, "the cancelled instalment left the recomputed draft")
	}
	s.requireBilledBy(draft.ID, instalmentIDs(billed))
	s.ElementsMatch(instalmentIDs(billed), s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))

	finalized, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.True(finalized.AmountDue.Equal(dec("550")), "500 up front and the first of ten 50 instalments, got %s", finalized.AmountDue)
}

// milestoneDraft subscribes from 1 Jan to a commitment billing a $300 milestone and $70 a month in arrears, computes
// the January draft, then triggers the milestone for 15 January, after the draft was computed. It returns the draft
// and the milestone's id.
func (s *CommitmentE2ESuite) milestoneDraft(invoices InvoiceService, name string, edit func(draftID string)) (*invoice.Invoice, string) {
	ctx := s.GetContext()
	tokens := s.planWith(name, types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	terms := upfrontPlusRemainder()
	terms[0].BillTrigger = lo.ToPtr(types.InstalmentBillTriggerManual)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), terms...))

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	draft, _, err := invoices.GetOrComputeCurrentPeriodDraft(ctx, sub)
	s.Require().NoError(err)
	s.Require().Len(s.storedInstalmentLines(draft.ID), 1, "the untriggered milestone waits at the end of the term")
	edit(draft.ID)

	listed, err := s.commitments.ListInstalments(ctx, committed.ID, nil)
	s.Require().NoError(err)
	milestone, ok := lo.Find(listed.Items, func(inst *billingterm.Instalment) bool {
		return inst.BillTrigger == types.InstalmentBillTriggerManual
	})
	s.Require().True(ok)

	_, err = s.commitments.TriggerInstalment(ctx, committed.ID, milestone.ID, dto.TriggerInstalmentRequest{
		BillOn: lo.ToPtr(s.jan.AddDate(0, 0, 14)),
	})
	s.Require().NoError(err)
	return draft, milestone.ID
}

func (s *CommitmentE2ESuite) TestFinalize_RecomputesADraftMissingADueInstalment() {
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, milestoneID := s.milestoneDraft(invoices, "missed", func(string) {})

	s.Require().NoError(invoices.FinalizeInvoice(s.GetContext(), draft.ID, dto.FinalizeInvoiceRequest{}))

	billed := instalmentIDs(s.billedInstalments(draft.ID))
	s.Len(billed, 2)
	s.Contains(billed, milestoneID, "the recomputed draft bills the triggered milestone")
	s.requireBilledBy(draft.ID, billed)
}

// A parent's draft bills its grouped children's instalments too, so a child's milestone triggered after the draft was
// computed is picked up when the draft is finalized.
func (s *CommitmentE2ESuite) TestFinalize_RecomputesAParentDraftMissingAChildInstalment() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	tokens := s.planWith("grouped_child", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	parent := s.groupSubscription(s.subscribe(tokens, s.jan).ID, types.SubscriptionTypeParent, nil)
	child := s.groupSubscription(s.subscribe(tokens, s.jan).ID, types.SubscriptionTypeGroupedInvoicing, lo.ToPtr(parent.ID))

	terms := upfrontPlusRemainder()
	terms[0].BillTrigger = lo.ToPtr(types.InstalmentBillTriggerManual)
	committed := s.commit(child.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), terms...))

	draft, _, err := invoices.GetOrComputeCurrentPeriodDraft(ctx, parent)
	s.Require().NoError(err)

	listed, err := s.commitments.ListInstalments(ctx, committed.ID, nil)
	s.Require().NoError(err)
	milestone, ok := lo.Find(listed.Items, func(inst *billingterm.Instalment) bool {
		return inst.BillTrigger == types.InstalmentBillTriggerManual
	})
	s.Require().True(ok)
	s.Require().NotContains(lo.Map(s.storedInstalmentLines(draft.ID), func(line *invoice.InvoiceLineItem, _ int) string {
		return lo.FromPtr(line.BillingInstalmentID)
	}), milestone.ID)

	_, err = s.commitments.TriggerInstalment(ctx, committed.ID, milestone.ID, dto.TriggerInstalmentRequest{
		BillOn: lo.ToPtr(s.jan.AddDate(0, 0, 14)),
	})
	s.Require().NoError(err)

	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))
	s.Contains(instalmentIDs(s.billedInstalments(draft.ID)), milestone.ID, "the parent's recomputed draft bills the child's milestone")
}

// groupSubscription makes the subscription a grouped-invoicing parent or child and returns it.
func (s *CommitmentE2ESuite) groupSubscription(id string, subType types.SubscriptionType, parentID *string) *subscription.Subscription {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)

	sub.SubscriptionType = subType
	sub.ParentSubscriptionID = parentID
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	return sub
}

// A manually edited draft is never recomputed: it finalizes without the milestone, which waits for the next invoice.
func (s *CommitmentE2ESuite) TestFinalize_ManuallyEditedDraftLeavesADueInstalmentScheduled() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, milestoneID := s.milestoneDraft(invoices, "missed_edited", func(draftID string) {
		_, err := invoices.AddBulkLineItem(ctx, draftID, dto.AddBulkLineItemRequest{Items: []dto.AddLineItemRequest{
			{DisplayName: "Setup fee", Amount: dec("50"), Quantity: dec("1")},
		}})
		s.Require().NoError(err)
	})

	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))

	s.NotContains(instalmentIDs(s.billedInstalments(draft.ID)), milestoneID)
	milestone, err := s.GetStores().BillingInstalmentRepo.Get(ctx, milestoneID)
	s.Require().NoError(err)
	s.Equal(types.InstalmentStatusScheduled, milestone.InstalmentStatus)
}

// draftPayment stores a payment for the whole draft in the given status.
func (s *CommitmentE2ESuite) draftPayment(draft *invoice.Invoice, status types.PaymentStatus) *payment.Payment {
	p := &payment.Payment{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     draft.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            draft.AmountDue,
		Currency:          draft.Currency,
		PaymentStatus:     status,
		BaseModel:         types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().PaymentRepo.Create(s.GetContext(), p))
	return p
}

// An edit waits while a draft billing the commitment's instalments is being paid, since the payment would finalize the
// draft with instalments the edit cancels. A payment that has not started or has failed does not hold it.
func (s *CommitmentE2ESuite) TestReplaceBillingTerms_WaitsForADraftPayment() {
	tests := []struct {
		status  types.PaymentStatus
		refused bool
	}{
		{types.PaymentStatusInitiated, false},
		{types.PaymentStatusPending, true},
		{types.PaymentStatusProcessing, true},
		{types.PaymentStatusSucceeded, true},
		{types.PaymentStatusFailed, false},
	}
	for _, tt := range tests {
		s.Run(string(tt.status), func() {
			ctx := s.GetContext()
			draft, commitmentID := s.januaryDraft(NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite)), "draft_payment_"+string(tt.status))
			s.draftPayment(draft, tt.status)

			_, err := s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
			if !tt.refused {
				s.Require().NoError(err)
				return
			}

			s.Require().Error(err)
			s.True(ierr.IsInvalidOperation(err), "got %v", err)
			for _, inst := range s.billedInstalments(draft.ID) {
				s.Equal(types.InstalmentStatusScheduled, inst.InstalmentStatus, "the refused edit cancels nothing")
			}
		})
	}
}

// An edit locks each draft billing the commitment's instalments, under the commitment lock, before it reads the
// draft's payments.
func (s *CommitmentE2ESuite) TestReplaceBillingTerms_LocksTheDrafts() {
	ctx := s.GetContext()
	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	draft, commitmentID := s.januaryDraft(NewInvoiceService(params), "edit_lock")

	events = events[:0]
	_, err := NewCommitmentService(params).ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)
	s.Equal([]string{"tx", "lock:commitment:" + commitmentID, "lock:invoice:" + draft.ID}, events[:3])
}

// Starting a charge locks the invoice and checks the draft is fresh in the transaction that moves the payment to
// processing, so an edit either changes the draft first and the charge is refused, or sees the payment and waits.
func (s *CommitmentE2ESuite) TestProcessPayment_LocksTheDraftBeforeCharging() {
	ctx := s.GetContext()
	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	params.PaymentRepo = &recordingPaymentRepo{Repository: params.PaymentRepo, events: &events}
	draft, _ := s.januaryDraft(NewInvoiceService(params), "charge_lock")
	pending := s.draftPayment(draft, types.PaymentStatusPending)

	events = events[:0]
	_, err := NewPaymentProcessorService(params).ProcessPayment(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal([]string{"tx", "lock:invoice:" + draft.ID, "update:" + string(types.PaymentStatusProcessing), "tx_end"}, events[:4])
}

// recordingPaymentRepo records each payment update with the status it writes.
type recordingPaymentRepo struct {
	payment.Repository
	events *[]string
}

func (r *recordingPaymentRepo) Update(ctx context.Context, p *payment.Payment) error {
	*r.events = append(*r.events, "update:"+string(p.PaymentStatus))
	return r.Repository.Update(ctx, p)
}

// A payment recorded without processing after an edit is charged later through ProcessPayment, which refuses the stale
// draft before the payment leaves pending.
func (s *CommitmentE2ESuite) TestProcessPayment_RefusesAStaleDraft() {
	ctx := s.GetContext()
	params := fullServiceParams(&s.BaseServiceTestSuite)
	draft, commitmentID := s.januaryDraft(NewInvoiceService(params), "process_stale")

	_, err := s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	pending := s.draftPayment(draft, types.PaymentStatusPending)
	_, err = NewPaymentProcessorService(params).ProcessPayment(ctx, pending.ID)
	s.Require().ErrorIs(err, errStaleDraft)

	stored, err := s.GetStores().PaymentRepo.Get(ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusPending, stored.PaymentStatus)

	unpaid, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, unpaid.InvoiceStatus)
}

// An instalment line keeps its amount and quantity, since finalizing records the instalment as billed at its planned
// amount. Its other fields stay editable.
func (s *CommitmentE2ESuite) TestUpdateLineItem_KeepsAnInstalmentLinesAmount() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, _ := s.januaryDraft(invoices, "line_edit")
	line := s.storedInstalmentLines(draft.ID)[0]

	for _, req := range []dto.UpdateLineItemRequest{{Amount: lo.ToPtr(dec("1"))}, {Quantity: lo.ToPtr(dec("2"))}} {
		_, err := invoices.UpdateLineItem(ctx, draft.ID, line.ID, req)
		s.Require().Error(err)
		s.True(ierr.IsValidation(err), "got %v", err)
	}

	_, err := invoices.UpdateLineItem(ctx, draft.ID, line.ID, dto.UpdateLineItemRequest{DisplayName: lo.ToPtr("Upfront")})
	s.Require().NoError(err)
}

// A manually edited draft is never recomputed, so a stale instalment still fails its finalization.
func (s *CommitmentE2ESuite) TestFinalize_ManuallyEditedStaleDraftFails() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, commitmentID := s.januaryDraft(invoices, "edited")
	stale := instalmentIDs(s.billedInstalments(draft.ID))

	_, err := invoices.AddBulkLineItem(ctx, draft.ID, dto.AddBulkLineItemRequest{Items: []dto.AddLineItemRequest{
		{DisplayName: "Setup fee", Amount: dec("50"), Quantity: dec("1")},
	}})
	s.Require().NoError(err)

	_, err = s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	err = invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{})
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	s.Empty(s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))

	stored, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)
	s.ElementsMatch(stale, instalmentIDs(s.billedInstalments(draft.ID)), "the edited draft keeps its lines")
}

// catchUpDrafts computes the January and February drafts of a commitment billed whole up front, as a catch-up over
// two periods does, and returns them with their subscription. Both carry the upfront instalment, and each also bills
// $50 of usage above the commitment.
func (s *CommitmentE2ESuite) catchUpDrafts(
	invoices InvoiceService,
	name string,
) (january, february *invoice.Invoice, sub *subscription.Subscription) {
	ctx := s.GetContext()
	feb, mar := s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0)
	tokens := s.planWith(name, types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), wholeValueUpfront(nil)))
	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "1500")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "1500")

	drafts := make([]*invoice.Invoice, 0, 2)
	for _, period := range []types.Period{{Start: s.jan, End: feb}, {Start: feb, End: mar}} {
		draft, err := invoices.CreateDraftInvoiceForSubscription(ctx, dto.CreateSubscriptionDraftInvoiceRequest{
			SubscriptionID: created.ID,
			PeriodStart:    period.Start,
			PeriodEnd:      period.End,
			ReferencePoint: types.ReferencePointPeriodEnd,
		})
		s.Require().NoError(err)

		computed, skipped, err := invoices.ComputeInvoice(ctx, draft.ID, nil)
		s.Require().NoError(err)
		s.Require().False(skipped)
		drafts = append(drafts, computed)
	}

	upfront := instalmentIDs(s.billedInstalments(drafts[0].ID))
	s.Require().Len(upfront, 1)
	s.Require().Equal(upfront, instalmentIDs(s.billedInstalments(drafts[1].ID)))

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	return drafts[0], drafts[1], sub
}

// requireOnlyUsage checks that the invoice was finalized with no instalment, billing only its usage above the
// commitment.
func (s *CommitmentE2ESuite) requireOnlyUsage(invoiceID string) {
	stored, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.Empty(s.storedInstalmentLines(invoiceID), "the recompute dropped the instalment the other draft billed")
	s.True(stored.AmountDue.Equal(dec("50")), "only the usage above the commitment, got %s", stored.AmountDue)
}

// Finalizing the January draft bills the shared instalment, so the February draft is recomputed without it and
// finalized with its usage.
func (s *CommitmentE2ESuite) TestFinalize_CatchUpDraftsInOrder() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	january, february, _ := s.catchUpDrafts(invoices, "in_order")
	upfront := instalmentIDs(s.billedInstalments(january.ID))

	s.Require().NoError(invoices.FinalizeInvoice(ctx, january.ID, dto.FinalizeInvoiceRequest{}))
	s.Require().NoError(invoices.FinalizeInvoice(ctx, february.ID, dto.FinalizeInvoiceRequest{}))

	s.requireBilledBy(january.ID, upfront)
	s.requireOnlyUsage(february.ID)
}

// Processing the February draft first bills the shared instalment, so the January draft is recomputed without it and
// finalized with its usage.
func (s *CommitmentE2ESuite) TestProcessDraft_CatchUpDraftsOutOfOrder() {
	ctx := s.GetContext()
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	january, february, sub := s.catchUpDrafts(invoices, "out_of_order")
	upfront := instalmentIDs(s.billedInstalments(february.ID))

	s.Require().NoError(invoices.ProcessDraftInvoice(ctx, february.ID, nil, sub, types.InvoiceFlowRenewal))
	s.Require().NoError(invoices.ProcessDraftInvoice(ctx, january.ID, nil, sub, types.InvoiceFlowRenewal))

	s.requireBilledBy(february.ID, upfront)
	s.requireOnlyUsage(january.ID)
}

// Voiding puts the invoice's instalments back on schedule and recalculating bills them on the replacement. Instalments
// an edit cancelled stay cancelled when the invoice carrying them is voided.
// Voiding takes the lock of each commitment whose instalments it reschedules, so it can't land halfway through an edit.
func (s *CommitmentE2ESuite) TestVoid_LocksTheCommitment() {
	ctx := s.GetContext()
	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	invoices := NewInvoiceService(params)
	draft, commitmentID := s.januaryDraft(invoices, "void_lock")
	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))

	events = events[:0]
	_, err := invoices.VoidInvoice(ctx, draft.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)
	s.Contains(events, "lock:commitment:"+commitmentID)
}

// A draft computed before a commitment was written, and not recomputed since, is recomputed when it is finalized.
func (s *CommitmentE2ESuite) TestFinalize_RecomputesADraftOlderThanACommitment() {
	ctx := s.GetContext()
	feb, mar := s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0)
	tokens := s.planWith("finalize_after_commitment", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	s.record(s.tokens, feb.AddDate(0, 0, 9), "100")

	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft := s.computedDraft(invoices, sub.ID, types.Period{Start: feb, End: mar})
	s.Require().True(draft.AmountDue.Equal(dec("10")), "only the usage, got %s", draft.AmountDue)

	// create, unlike Create, does not recompute the drafts, as when that recompute fails.
	req := floorRequest("Floor", tokens.ID, "100", s.jan, s.jan.AddDate(1, 0, 0))
	req.SubscriptionID = sub.ID
	_, _, err := s.commitments.(*commitmentService).create(ctx, req, commitmentCreateOptions{})
	s.Require().NoError(err)

	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))
	finalized, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)
	s.True(finalized.AmountDue.Equal(dec("100")), "the floor is billed, got %s", finalized.AmountDue)
}

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
		s.Nil(inst.InvoicedAt)
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

	// An edit keeps invoiced instalments, so voiding their invoice still reschedules them.
	_, err = s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	publisher.Reset()
	_, err = invoices.VoidInvoice(ctx, replacement.ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)

	for _, id := range instalmentIDs(rebilled) {
		inst, err := s.GetStores().BillingInstalmentRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(types.InstalmentStatusScheduled, inst.InstalmentStatus)
		s.Nil(inst.InvoiceID)
	}
	s.ElementsMatch(instalmentIDs(rebilled), s.instalmentEvents(types.WebhookEventBillingInstalmentUpdated))
}

// A draft paid in full is finalized by the payment and claims its instalments.
func (s *CommitmentE2ESuite) TestPaymentFinalize_ClaimsInstalments() {
	ctx := s.GetContext()
	params := fullServiceParams(&s.BaseServiceTestSuite)
	computed, _ := s.januaryDraft(NewInvoiceService(params), "paid")

	draft, err := s.GetStores().InvoiceRepo.Get(ctx, computed.ID)
	s.Require().NoError(err)
	s.Require().True(draft.AmountDue.IsPositive())
	s.Require().NoError(NewPaymentService(params).(*paymentService).validateInvoicePaymentEligibility(ctx, draft, &dto.CreatePaymentRequest{
		DestinationType: types.PaymentDestinationTypeInvoice,
		DestinationID:   draft.ID,
		Amount:          draft.AmountDue,
		Currency:        draft.Currency,
	}))

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

// After an edit cancelled a draft's instalments, a new payment on it is refused, while one a gateway already collected
// is recorded. Such a payment still finalizes the draft; the cancelled instalments stay cancelled and unclaimed.
func (s *CommitmentE2ESuite) TestPaymentFinalize_StaleDraftKeepsPayment() {
	ctx := s.GetContext()
	params := fullServiceParams(&s.BaseServiceTestSuite)
	computed, commitmentID := s.januaryDraft(NewInvoiceService(params), "paid_stale")
	stale := instalmentIDs(s.billedInstalments(computed.ID))

	_, err := s.commitments.ReplaceBillingTerms(ctx, commitmentID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	draft, err := s.GetStores().InvoiceRepo.Get(ctx, computed.ID)
	s.Require().NoError(err)

	payments := NewPaymentService(params).(*paymentService)
	err = payments.validateInvoicePaymentEligibility(ctx, draft, &dto.CreatePaymentRequest{
		DestinationType: types.PaymentDestinationTypeInvoice,
		DestinationID:   draft.ID,
		Amount:          draft.AmountDue,
		Currency:        draft.Currency,
		ProcessPayment:  true,
	})
	s.Require().ErrorIs(err, errStaleDraft, "a new payment on the stale draft is refused")

	s.Require().NoError(payments.validateInvoicePaymentEligibility(ctx, draft, &dto.CreatePaymentRequest{
		DestinationType: types.PaymentDestinationTypeInvoice,
		DestinationID:   draft.ID,
		Amount:          draft.AmountDue,
		Currency:        draft.Currency,
		ProcessPayment:  false,
	}), "a payment a gateway already collected is recorded")

	// A payment taken before the edit still lands.
	paid := s.draftPayment(draft, types.PaymentStatusSucceeded)

	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
	processor := &paymentProcessor{ServiceParams: params}
	s.Require().NoError(processor.handleInvoicePostProcessing(ctx, paid))

	finalized, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)
	s.Equal(types.PaymentStatusSucceeded, finalized.PaymentStatus)
	s.True(finalized.AmountPaid.Equal(paid.Amount), "amount paid %s", finalized.AmountPaid)

	for _, inst := range s.billedInstalments(draft.ID) {
		s.Contains(stale, inst.ID)
		s.Equal(types.InstalmentStatusCancelled, inst.InstalmentStatus)
		s.Nil(inst.InvoiceID)
	}
	s.Empty(s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))
}
