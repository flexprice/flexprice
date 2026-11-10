package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// yearlyArrearRequest commits 100 a month for a year from 1 Jan 2025, worth 1,200, billed as twelve arrear instalments
// of 100 on subscription invoices.
func (s *CommitmentServiceSuite) yearlyArrearRequest() dto.CreateCommitmentRequest {
	req := s.billingTermsRequest()
	req.EndDate = lo.ToPtr(req.StartDate.AddDate(1, 0, 0))
	req.BillingTerms = []dto.BillingTermInput{{
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueRemainder,
		InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
	}}

	return req
}

// fiveOfTwelveInvoiced creates the yearly arrear commitment and invoices its first five instalments on inv_x.
func (s *CommitmentServiceSuite) fiveOfTwelveInvoiced() (*dto.CommitmentResponse, []*billingterm.Instalment) {
	created, err := s.svc.Create(s.GetContext(), s.yearlyArrearRequest())
	s.Require().NoError(err)

	before := s.storedInstalments()
	s.Require().Len(before, 12)

	ids := lo.Map(before[:5], func(inst *billingterm.Instalment, _ int) string { return inst.ID })
	claimed, err := s.GetStores().BillingInstalmentRepo.Claim(s.GetContext(), "inv_x", time.Now(), ids)
	s.Require().NoError(err)
	s.Require().Len(claimed, 5)

	return created, s.storedInstalments()
}

func (s *CommitmentServiceSuite) instalmentByID(id string) *billingterm.Instalment {
	inst, err := s.GetStores().BillingInstalmentRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return inst
}

// addedSince returns the stored instalments that are not in before, in billing order.
func (s *CommitmentServiceSuite) addedSince(before []*billingterm.Instalment) []*billingterm.Instalment {
	return lo.Filter(s.storedInstalments(), func(inst *billingterm.Instalment, _ int) bool {
		return !lo.ContainsBy(before, func(old *billingterm.Instalment) bool { return old.ID == inst.ID })
	})
}

// liveTotal is what the commitment's instalments that are not cancelled add up to.
func (s *CommitmentServiceSuite) liveTotal() decimal.Decimal {
	total := decimal.Zero
	for _, inst := range s.storedInstalments() {
		if inst.InstalmentStatus != types.InstalmentStatusCancelled {
			total = total.Add(inst.Amount)
		}
	}

	return total
}

// inactiveCommitments are a per_period commitment and a terminated billing_terms one, which have no plan to change.
func (s *CommitmentServiceSuite) inactiveCommitments() map[string]string {
	ctx := s.GetContext()
	perPeriodReq := s.billingTermsRequest()
	perPeriodReq.CollectionMethod, perPeriodReq.BillingTerms, perPeriodReq.Priority = nil, nil, 1
	perPeriod, err := s.svc.Create(ctx, perPeriodReq)
	s.Require().NoError(err)

	terminated, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	at := terminated.StartDate.AddDate(0, 5, 0)
	_, err = s.svc.Terminate(ctx, terminated.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	return map[string]string{"per_period": perPeriod.ID, "terminated": terminated.ID}
}

// racingInstalmentRepo runs a hook before the next Cancel or Update, standing in for an invoice that finalizes and
// claims an instalment at that moment.
type racingInstalmentRepo struct {
	billingterm.InstalmentRepository

	race func(ctx context.Context)
}

func (r *racingInstalmentRepo) fire(ctx context.Context) {
	if race := r.race; race != nil {
		r.race = nil
		race(ctx)
	}
}

func (r *racingInstalmentRepo) Cancel(
	ctx context.Context,
	ownerType types.BillingTermOwnerType,
	ownerID string,
	statuses []types.InstalmentStatus,
) ([]*billingterm.Instalment, error) {
	r.fire(ctx)
	return r.InstalmentRepository.Cancel(ctx, ownerType, ownerID, statuses)
}

func (r *racingInstalmentRepo) Update(ctx context.Context, instalment *billingterm.Instalment) (*billingterm.Instalment, error) {
	r.fire(ctx)
	return r.InstalmentRepository.Update(ctx, instalment)
}

// racingCommitmentService is the commitment service over a repo that runs race before its next write.
func (s *CommitmentServiceSuite) racingCommitmentService(race func(ctx context.Context)) CommitmentService {
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.BillingInstalmentRepo = &racingInstalmentRepo{InstalmentRepository: params.BillingInstalmentRepo, race: race}
	return NewCommitmentService(params)
}

// milestoneRequest is billingTermsRequest as a 300 upfront and a 700 milestone: one manual instalment holding the rest.
func (s *CommitmentServiceSuite) milestoneRequest() dto.CreateCommitmentRequest {
	req := s.billingTermsRequest()
	req.BillingTerms = []dto.BillingTermInput{
		req.BillingTerms[0],
		{
			Kind:        types.BillingTermKindOnce,
			ValueType:   types.BillingTermValueRemainder,
			BillTrigger: lo.ToPtr(types.InstalmentBillTriggerManual),
		},
	}

	return req
}

// milestoneOf creates a milestone commitment and returns it with its manual instalment.
func (s *CommitmentServiceSuite) milestoneOf(req dto.CreateCommitmentRequest) (*dto.CommitmentResponse, *billingterm.Instalment) {
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	milestone, ok := lo.Find(created.Instalments, func(inst *billingterm.Instalment) bool {
		return inst.BillTrigger == types.InstalmentBillTriggerManual
	})
	s.Require().True(ok, "the commitment has a milestone")
	s.Require().True(milestone.BillOn.Equal(*created.EndDate), "an untriggered milestone waits at the end of the term")
	s.Require().Nil(milestone.TriggeredAt)

	return created, milestone
}

func (s *CommitmentServiceSuite) TestTriggerInstalment_SetsBillOnAndTriggeredAt() {
	ctx := s.GetContext()
	created, milestone := s.milestoneOf(s.milestoneRequest())
	before := s.storedInstalments()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	billOn := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	asked := clock.Now()
	triggered, err := s.svc.TriggerInstalment(ctx, created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &billOn})
	s.Require().NoError(err)

	s.True(triggered.BillOn.Equal(billOn))
	s.Require().NotNil(triggered.TriggeredAt)
	s.False(triggered.TriggeredAt.Before(asked), "triggered_at is when the trigger was made, got %s", triggered.TriggeredAt)
	s.WithinDuration(asked, *triggered.TriggeredAt, time.Minute)
	s.True(triggered.Amount.Equal(dec("700")), "the amount is unchanged")
	s.Equal(milestone.ID, triggered.ID)
	s.Equal(types.InstalmentBillTriggerManual, triggered.BillTrigger)
	s.Equal(types.InstalmentStatusScheduled, triggered.InstalmentStatus)
	s.Equal(s.instalmentByID(milestone.ID), triggered, "the response is the stored instalment")

	for _, other := range before {
		if other.ID != milestone.ID {
			s.Equal(other, s.instalmentByID(other.ID), "instalment %s is untouched", other.ID)
		}
	}

	events := publisher.Events()
	s.Require().Len(events, 1)
	s.Equal(types.WebhookEventBillingInstalmentUpdated, events[0].EventName)
	s.Equal([]string{milestone.ID}, s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentUpdated))
}

func (s *CommitmentServiceSuite) TestTriggerInstalment_TermEdgesAreInside() {
	created, milestone := s.milestoneOf(s.milestoneRequest())

	for _, billOn := range []time.Time{*created.EndDate, created.StartDate} {
		triggered, err := s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &billOn})
		s.Require().NoError(err)
		s.True(triggered.BillOn.Equal(billOn))
	}
}

// A triggered milestone stays scheduled until an invoice claims it, so the trigger can move its date earlier. It
// cannot move it later: a draft computed since may already bill it.
func (s *CommitmentServiceSuite) TestTriggerInstalment_MovesAScheduledMilestoneEarlier() {
	created, milestone := s.milestoneOf(s.milestoneRequest())
	first := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	earlier := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	later := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)

	_, err := s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &first})
	s.Require().NoError(err)

	triggered, err := s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &earlier})
	s.Require().NoError(err)
	s.True(triggered.BillOn.Equal(earlier))
	s.NotNil(triggered.TriggeredAt)

	_, err = s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &later})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.True(s.instalmentByID(milestone.ID).BillOn.Equal(earlier), "a rejected trigger keeps bill_on")
}

func (s *CommitmentServiceSuite) TestTriggerInstalment_Rejections() {
	ctx := s.GetContext()
	repo := s.GetStores().BillingInstalmentRepo
	inTerm := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	created, milestone := s.milestoneOf(s.milestoneRequest())
	dated, ok := lo.Find(created.Instalments, func(inst *billingterm.Instalment) bool {
		return inst.BillTrigger == types.InstalmentBillTriggerDate
	})
	s.Require().True(ok)

	otherReq := s.milestoneRequest()
	otherReq.Priority = 2
	_, otherMilestone := s.milestoneOf(otherReq)

	invoicedReq := s.milestoneRequest()
	invoicedReq.Priority = 3
	invoicedCommitment, invoicedMilestone := s.milestoneOf(invoicedReq)
	claimed, err := repo.Claim(ctx, "inv_x", time.Now(), []string{invoicedMilestone.ID})
	s.Require().NoError(err)
	s.Require().Len(claimed, 1)

	cancelledReq := s.milestoneRequest()
	cancelledReq.Priority = 4
	cancelledCommitment, cancelledMilestone := s.milestoneOf(cancelledReq)
	_, err = repo.Cancel(ctx, types.BillingTermOwnerCommitment, cancelledCommitment.ID, []types.InstalmentStatus{types.InstalmentStatusScheduled})
	s.Require().NoError(err)

	tests := []struct {
		name         string
		commitmentID string
		instalmentID string
		billOn       time.Time
		check        func(error) bool
	}{
		{"a date instalment", created.ID, dated.ID, inTerm, ierr.IsInvalidOperation},
		{"an invoiced milestone", invoicedCommitment.ID, invoicedMilestone.ID, inTerm, ierr.IsInvalidOperation},
		{"a cancelled milestone", cancelledCommitment.ID, cancelledMilestone.ID, inTerm, ierr.IsInvalidOperation},
		{"another commitment's milestone", created.ID, otherMilestone.ID, inTerm, ierr.IsNotFound},
		{"an unknown instalment", created.ID, "binst_missing", inTerm, ierr.IsNotFound},
		{"after the end of the term", created.ID, milestone.ID, created.EndDate.Add(time.Second), ierr.IsValidation},
		{"before the start of the term", created.ID, milestone.ID, created.StartDate.Add(-time.Second), ierr.IsValidation},
	}

	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	for _, tc := range tests {
		s.Run(tc.name, func() {
			before := s.storedInstalments()
			publisher.Reset()

			_, err := s.svc.TriggerInstalment(ctx, tc.commitmentID, tc.instalmentID, dto.TriggerInstalmentRequest{BillOn: &tc.billOn})
			s.Require().Error(err)
			s.True(tc.check(err), "got %v", err)
			s.Equal(before, s.storedInstalments(), "a rejected trigger changes nothing")
			s.Empty(publisher.Events())
		})
	}
}

func (s *CommitmentServiceSuite) TestTriggerInstalment_RejectsInactiveOrPerPeriod() {
	ctx := s.GetContext()
	billOn := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	req := dto.TriggerInstalmentRequest{BillOn: &billOn}

	for name, id := range s.inactiveCommitments() {
		before := s.storedInstalments()
		_, err := s.svc.TriggerInstalment(ctx, id, "binst_any", req)
		s.Require().Error(err, name)
		s.True(ierr.IsInvalidOperation(err), "%s: got %v", name, err)
		s.Equal(before, s.storedInstalments())
	}

	_, err := s.svc.TriggerInstalment(ctx, "cmt_missing", "binst_any", req)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)

	activeReq := s.milestoneRequest()
	activeReq.Priority = 2
	created, milestone := s.milestoneOf(activeReq)

	before := s.storedInstalments()
	setCommitmentsV2(s.T(), ctx, s.GetStores(), false)
	_, err = s.svc.TriggerInstalment(ctx, created.ID, milestone.ID, req)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	s.Equal(before, s.storedInstalments())
}

// milestoneCommitment commits 100 a month from 1 Jan 2026 to end, billed as half at signing and half at go-live, a
// milestone nobody has triggered. Both instalments go on subscription invoices.
func (s *CommitmentE2ESuite) milestoneCommitment(name string, end time.Time) (*dto.SubscriptionResponse, *dto.CommitmentResponse) {
	tokens := s.planWith(name, types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)

	months := (end.Year()-s.jan.Year())*12 + int(end.Month()-s.jan.Month())
	signing := dto.BillingTermInput{
		Kind:      types.BillingTermKindOnce,
		ValueType: types.BillingTermValueFixed,
		Value:     lo.ToPtr(decimal.NewFromInt(int64(50 * months))),
	}
	goLive := dto.BillingTermInput{
		Kind:        types.BillingTermKindOnce,
		ValueType:   types.BillingTermValueRemainder,
		BillTrigger: lo.ToPtr(types.InstalmentBillTriggerManual),
	}

	committed := s.commit(sub.ID, termsRequest(name, tokens.ID, "100", s.jan, end, signing, goLive))
	s.Require().Len(committed.Instalments, 2)
	s.Require().Equal(types.InstalmentBillTriggerManual, committed.Instalments[1].BillTrigger)
	s.Require().True(committed.Instalments[1].BillOn.Equal(end), "an untriggered milestone waits at the end of the term")

	return sub, committed
}

// billsInstalment reports whether the subscription's invoice for [from, to) carries a line for the instalment, and
// returns the line.
func (s *CommitmentE2ESuite) billsInstalment(
	subscriptionID, instalmentID string,
	from, to time.Time,
) (dto.CreateInvoiceLineItemRequest, bool) {
	return lo.Find(instalmentLines(s.invoiceFor(subscriptionID, from, to)), func(line dto.CreateInvoiceLineItemRequest) bool {
		return lo.FromPtr(line.BillingInstalmentID) == instalmentID
	})
}

// B2: 50% at signing, 50% at go-live. Triggering the milestone bills it now, so it lands on the invoice that closes the
// current period and on none before.
func (s *CommitmentE2ESuite) TestTriggerInstalment_B2() {
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	sub, committed := s.milestoneCommitment("b2", s.jan.AddDate(10, 0, 0))
	milestone := committed.Instalments[1]
	s.True(milestone.Amount.Equal(dec("6000")), "got %s", milestone.Amount)

	asked := clock.Now()
	thisMonth := time.Date(asked.UTC().Year(), asked.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	_, billed := s.billsInstalment(sub.ID, milestone.ID, thisMonth, thisMonth.AddDate(0, 1, 0))
	s.False(billed, "an untriggered milestone is not billed before the end of the term")

	publisher.Reset()
	triggered, err := s.commitments.TriggerInstalment(s.GetContext(), committed.ID, milestone.ID, dto.TriggerInstalmentRequest{})
	s.Require().NoError(err)

	s.False(triggered.BillOn.Before(asked), "bill_on defaults to now, got %s", triggered.BillOn)
	s.WithinDuration(asked, triggered.BillOn, time.Minute)
	s.Require().NotNil(triggered.TriggeredAt)
	s.WithinDuration(asked, *triggered.TriggeredAt, time.Minute)
	s.Equal(types.InstalmentStatusScheduled, triggered.InstalmentStatus)
	s.Equal([]string{milestone.ID}, s.instalmentEvents(types.WebhookEventBillingInstalmentUpdated))

	// The period is read from the stored bill_on, so a trigger just past a month boundary still finds its invoice:
	// the first to close on or after bill_on.
	billOn := triggered.BillOn.UTC()
	closes := time.Date(billOn.Year(), billOn.Month(), 1, 0, 0, 0, 0, time.UTC)
	if billOn.After(closes) {
		closes = closes.AddDate(0, 1, 0)
	}
	opens := closes.AddDate(0, -1, 0)

	line, billed := s.billsInstalment(sub.ID, milestone.ID, opens, closes)
	s.Require().True(billed, "the triggered milestone is on the next subscription invoice")
	s.True(line.Amount.Equal(dec("6000")))
	s.Equal(string(types.CommitmentLineInstalment), line.Metadata[types.MetadataKeyCommitmentLineKind])

	_, billed = s.billsInstalment(sub.ID, milestone.ID, opens.AddDate(0, -1, 0), opens)
	s.False(billed, "an earlier invoice does not carry it")
}

func (s *CommitmentE2ESuite) TestTriggerInstalment_LandsOnTheInvoiceAfterItsDate() {
	sub, committed := s.milestoneCommitment("dated", s.jan.AddDate(2, 0, 0))
	milestone := committed.Instalments[1]
	jun, jul := s.jan.AddDate(0, 5, 0), s.jan.AddDate(0, 6, 0)

	billOn := jun.AddDate(0, 0, 14)
	_, err := s.commitments.TriggerInstalment(s.GetContext(), committed.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &billOn})
	s.Require().NoError(err)

	_, billed := s.billsInstalment(sub.ID, milestone.ID, jun.AddDate(0, -1, 0), jun)
	s.False(billed, "the invoice that closes before its date does not carry it")

	_, billed = s.billsInstalment(sub.ID, milestone.ID, jun, jul)
	s.True(billed, "the invoice that closes after its date does")

	_, billed = s.billsInstalment(sub.ID, milestone.ID, jul, jul.AddDate(0, 1, 0))
	s.True(billed, "and so does every later one until an invoice is finalized with it")
}

func (s *CommitmentE2ESuite) TestTriggerInstalment_UntriggeredMilestoneBillsAtTermEnd() {
	end := s.jan.AddDate(1, 0, 0)
	sub, committed := s.milestoneCommitment("closing", end)
	milestone := committed.Instalments[1]

	_, billed := s.billsInstalment(sub.ID, milestone.ID, end.AddDate(0, -2, 0), end.AddDate(0, -1, 0))
	s.False(billed, "the invoice before the last does not carry it")

	line, billed := s.billsInstalment(sub.ID, milestone.ID, end.AddDate(0, -1, 0), end)
	s.Require().True(billed, "the invoice that closes the term does")
	s.True(line.Amount.Equal(dec("600")), "got %s", line.Amount)
}

// The term ended in mid-2026, so the default bill_on of now is outside it.
func (s *CommitmentE2ESuite) TestTriggerInstalment_DefaultNowOutsideTheTermIsRejected() {
	_, committed := s.milestoneCommitment("past", s.jan.AddDate(0, 6, 0))
	milestone := committed.Instalments[1]

	_, err := s.commitments.TriggerInstalment(s.GetContext(), committed.ID, milestone.ID, dto.TriggerInstalmentRequest{})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Nil(s.instalmentStatus(milestone.ID).TriggeredAt)

	billOn := s.jan.AddDate(0, 3, 0)
	triggered, err := s.commitments.TriggerInstalment(s.GetContext(), committed.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &billOn})
	s.Require().NoError(err)
	s.True(triggered.BillOn.Equal(billOn))
}
