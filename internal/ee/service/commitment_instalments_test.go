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

// yearlyArrearRequest commits 100 a month for a year from 1 Jan 2027, worth 1,200, billed as twelve arrear instalments
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
	claimed, err := s.GetStores().BillingInstalmentRepo.Claim(s.GetContext(), "inv_x", ids)
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

func (s *CommitmentServiceSuite) TestReplanInstalments_ReplacesScheduledOnly() {
	ctx := s.GetContext()
	created, before := s.fiveOfTwelveInvoiced()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	sep := time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC)
	resp, err := s.svc.ReplanInstalments(ctx, created.ID, dto.ReplanInstalmentsRequest{Instalments: []dto.ReplanInstalmentInput{
		{Amount: dec("200"), BillTrigger: lo.ToPtr(types.InstalmentBillTriggerManual)},
		{Amount: dec("300"), BillOn: &sep, Description: lo.ToPtr("Catch-up")},
		{Amount: dec("200"), BillOn: &sep},
	}})
	s.Require().NoError(err)

	for _, invoiced := range before[:5] {
		got := s.instalmentByID(invoiced.ID)
		s.Equal(types.InstalmentStatusInvoiced, got.InstalmentStatus, "an invoiced instalment is untouched")
		s.Equal("inv_x", lo.FromPtr(got.InvoiceID))
	}

	for _, old := range before[5:] {
		s.Equal(types.InstalmentStatusCancelled, s.instalmentByID(old.ID).InstalmentStatus, "instalment %s", old.ID)
	}

	replanned := s.addedSince(before)
	s.Require().Len(replanned, 3)
	s.Equal(
		[]string{"Catch-up", billingterm.DefaultOnceDescription, billingterm.DefaultOnceDescription},
		lo.Map(replanned, func(inst *billingterm.Instalment, _ int) string { return inst.Description }),
	)

	s.True(replanned[0].Amount.Equal(dec("300")))
	s.True(replanned[1].Amount.Equal(dec("200")))
	s.Equal(types.InstalmentBillTriggerDate, replanned[1].BillTrigger)
	s.True(replanned[0].BillOn.Equal(sep) && replanned[1].BillOn.Equal(sep), "equal dates keep the order of the request")
	s.Less(replanned[0].ID, replanned[1].ID)

	manual := replanned[2]
	s.Equal(types.InstalmentBillTriggerManual, manual.BillTrigger)
	s.Nil(manual.TriggeredAt)
	s.True(manual.BillOn.Equal(*created.EndDate), "an untriggered milestone waits at the end of the term")

	for _, inst := range replanned {
		s.Nil(inst.BillingTermID)
		s.Equal(types.InstalmentKindInstalment, inst.Kind)
		s.Equal(types.InstalmentStatusScheduled, inst.InstalmentStatus)
		s.Equal(types.BillingTermOwnerCommitment, inst.OwnerType)
		s.Equal(created.ID, inst.OwnerID)
		s.Equal(created.CustomerID, inst.CustomerID)
		s.Equal(created.SubscriptionID, inst.SubscriptionID)
		s.Equal(created.Currency, inst.Currency)
		s.Equal(lo.FromPtr(created.InstalmentInvoicing), inst.Delivery)
		s.True(inst.ServicePeriodStart.Equal(created.StartDate))
		s.True(inst.ServicePeriodEnd.Equal(*created.EndDate))
	}

	s.True(s.liveTotal().Equal(dec("1200")), "invoiced and replanned instalments add up to the total")
	s.Equal(
		lo.Map(append(before[:5:5], replanned...), func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		lo.Map(resp.Items, func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		"the response is the plan: five invoiced and three new instalments in billing order",
	)

	events := publisher.Events()
	s.Require().Len(events, 10, "seven cancelled and three created")
	s.ElementsMatch(
		lo.Map(before[5:], func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCancelled),
	)
	s.Equal(
		lo.Map(replanned, func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated),
	)
	s.Equal(types.WebhookEventBillingInstalmentCancelled, events[0].EventName)
	s.Equal(types.WebhookEventBillingInstalmentCreated, events[9].EventName, "cancellations precede the new instalments")

	got, err := s.GetStores().CommitmentRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	s.True(lo.FromPtr(got.TotalAmount).Equal(dec("1200")), "the total is unchanged")
}

func (s *CommitmentServiceSuite) TestReplanInstalments_AcceptsTheTermEdges() {
	created, _ := s.fiveOfTwelveInvoiced()

	resp, err := s.svc.ReplanInstalments(s.GetContext(), created.ID, dto.ReplanInstalmentsRequest{Instalments: []dto.ReplanInstalmentInput{
		{Amount: dec("350"), BillOn: lo.ToPtr(created.StartDate)},
		{Amount: dec("350"), BillOn: created.EndDate},
	}})
	s.Require().NoError(err)
	s.Len(resp.Items, 7)
}

func (s *CommitmentServiceSuite) TestReplanInstalments_Rejections() {
	created, _ := s.fiveOfTwelveInvoiced()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	sep := time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC)
	date := func(amount string, billOn time.Time) dto.ReplanInstalmentInput {
		return dto.ReplanInstalmentInput{Amount: dec(amount), BillOn: &billOn}
	}

	tests := []struct {
		name        string
		instalments []dto.ReplanInstalmentInput
	}{
		{"before the term", []dto.ReplanInstalmentInput{date("700", created.StartDate.Add(-time.Second))}},
		{"after the term", []dto.ReplanInstalmentInput{date("700", created.EndDate.Add(time.Second))}},
		{"finer than the currency", []dto.ReplanInstalmentInput{date("350.005", sep), date("349.995", sep)}},
		{"a zero amount", []dto.ReplanInstalmentInput{date("700", sep), date("0", sep)}},
		{"a date instalment without bill_on", []dto.ReplanInstalmentInput{{Amount: dec("700")}}},
		{"a manual instalment with bill_on", []dto.ReplanInstalmentInput{
			{Amount: dec("700"), BillOn: &sep, BillTrigger: lo.ToPtr(types.InstalmentBillTriggerManual)},
		}},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			before := s.storedInstalments()
			publisher.Reset()

			_, err := s.svc.ReplanInstalments(s.GetContext(), created.ID, dto.ReplanInstalmentsRequest{Instalments: tc.instalments})
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)
			s.Equal(before, s.storedInstalments(), "a rejected re-plan changes nothing")
			s.Empty(publisher.Events())
		})
	}
}

// The total is checked after the scheduled instalments are cancelled, so the real transaction rolls them back; the
// in-memory one does not, which is why only the absence of new instalments and events is asserted.
func (s *CommitmentServiceSuite) TestReplanInstalments_RejectsAPlanThatMissesTheTotal() {
	created, before := s.fiveOfTwelveInvoiced()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	sep := time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC)

	for name, amount := range map[string]string{
		"short of the total":             "699",
		"over the total":                 "701",
		"the total without the invoiced": "1200",
	} {
		s.Run(name, func() {
			publisher.Reset()

			_, err := s.svc.ReplanInstalments(s.GetContext(), created.ID, dto.ReplanInstalmentsRequest{
				Instalments: []dto.ReplanInstalmentInput{{Amount: dec(amount), BillOn: &sep}},
			})
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)
			s.Empty(s.addedSince(before))
			s.Empty(publisher.Events())
		})
	}
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

func (s *CommitmentServiceSuite) TestReplanInstalments_SizesThePlanAfterTheCancellation() {
	created, before := s.fiveOfTwelveInvoiced()
	sep := time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC)
	repo := s.GetStores().BillingInstalmentRepo

	// A claim that lands just before the cancellation makes six instalments invoiced, leaving 600 to plan.
	racing := s.racingCommitmentService(func(ctx context.Context) {
		_, err := repo.Claim(ctx, "inv_race", []string{before[5].ID})
		s.Require().NoError(err)
	})

	_, err := racing.ReplanInstalments(s.GetContext(), created.ID, dto.ReplanInstalmentsRequest{
		Instalments: []dto.ReplanInstalmentInput{{Amount: dec("700"), BillOn: &sep}},
	})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Empty(s.addedSince(before))
	s.Equal(types.InstalmentStatusInvoiced, s.instalmentByID(before[5].ID).InstalmentStatus)

	racing = s.racingCommitmentService(nil)
	_, err = racing.ReplanInstalments(s.GetContext(), created.ID, dto.ReplanInstalmentsRequest{
		Instalments: []dto.ReplanInstalmentInput{{Amount: dec("600"), BillOn: &sep}},
	})
	s.Require().NoError(err)
	s.True(s.liveTotal().Equal(dec("1200")))
}

func (s *CommitmentServiceSuite) TestReplanInstalments_RejectsInactiveOrPerPeriod() {
	ctx := s.GetContext()
	req := dto.ReplanInstalmentsRequest{Instalments: []dto.ReplanInstalmentInput{
		{Amount: dec("1000"), BillOn: lo.ToPtr(time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC))},
	}}

	for name, id := range s.inactiveCommitments() {
		before := s.storedInstalments()
		_, err := s.svc.ReplanInstalments(ctx, id, req)
		s.Require().Error(err, name)
		s.True(ierr.IsInvalidOperation(err), "%s: got %v", name, err)
		s.Equal(before, s.storedInstalments())
	}

	_, err := s.svc.ReplanInstalments(ctx, "cmt_missing", req)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)

	activeReq := s.yearlyArrearRequest()
	activeReq.Priority = 2
	created, err := s.svc.Create(ctx, activeReq)
	s.Require().NoError(err)

	setCommitmentsV2(s.T(), ctx, s.GetStores(), false)
	_, err = s.svc.ReplanInstalments(ctx, created.ID, req)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

// B5: of $1,200 billed as 12 × $100 in arrear, five are invoiced. Bringing $300 forward adds a $300 instalment now and
// cancels months 6 to 8, so the total does not move.
func (s *CommitmentServiceSuite) TestBringForwardInstalments_B5() {
	ctx := s.GetContext()
	created, before := s.fiveOfTwelveInvoiced()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	now := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)
	resp, err := s.svc.BringForwardInstalments(ctx, created.ID, dto.BringForwardInstalmentsRequest{Amount: dec("300"), BillOn: &now})
	s.Require().NoError(err)

	for _, untouched := range append(before[:5:5], before[8:]...) {
		got := s.instalmentByID(untouched.ID)
		s.Equal(untouched.InstalmentStatus, got.InstalmentStatus, "instalment %s", untouched.ID)
		s.True(got.Amount.Equal(dec("100")))
	}

	for _, old := range before[5:8] {
		got := s.instalmentByID(old.ID)
		s.Equal(types.InstalmentStatusCancelled, got.InstalmentStatus, "instalment %s", old.ID)
		s.True(got.Amount.Equal(dec("100")), "a cancelled instalment keeps its amount")
	}

	s.Require().Len(s.addedSince(before), 1)
	added := s.addedSince(before)[0]

	s.True(added.Amount.Equal(dec("300")))
	s.True(added.BillOn.Equal(now))
	s.Equal(types.InstalmentBillTriggerDate, added.BillTrigger)
	s.Equal(types.InstalmentKindInstalment, added.Kind)
	s.Equal(types.InstalmentStatusScheduled, added.InstalmentStatus)
	s.Equal("Payment brought forward", added.Description)
	s.Nil(added.BillingTermID)
	s.Equal(created.ID, added.OwnerID)
	s.Equal(created.CustomerID, added.CustomerID)
	s.Equal(created.SubscriptionID, added.SubscriptionID)
	s.Equal(lo.FromPtr(created.InstalmentInvoicing), added.Delivery)
	s.True(added.ServicePeriodStart.Equal(before[5].ServicePeriodStart), "it pays for the months it replaces")
	s.True(added.ServicePeriodEnd.Equal(before[7].ServicePeriodEnd))

	s.True(s.liveTotal().Equal(dec("1200")), "the total is unchanged")
	live := lo.Filter(s.storedInstalments(), func(inst *billingterm.Instalment, _ int) bool {
		return inst.InstalmentStatus != types.InstalmentStatusCancelled
	})
	s.Equal(
		lo.Map(live, func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		lo.Map(resp.Items, func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		"the response is the plan in billing order",
	)

	events := publisher.Events()
	s.Require().Len(events, 4, "three cancelled and one created")
	s.Empty(s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentUpdated))
	s.Equal(
		lo.Map(before[5:8], func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCancelled),
	)
	s.Equal([]string{added.ID}, s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated))
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_PartialReduction() {
	ctx := s.GetContext()
	created, before := s.fiveOfTwelveInvoiced()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	now := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)
	_, err := s.svc.BringForwardInstalments(ctx, created.ID, dto.BringForwardInstalmentsRequest{Amount: dec("250"), BillOn: &now})
	s.Require().NoError(err)

	for _, old := range before[5:8] {
		got := s.instalmentByID(old.ID)
		s.Equal(types.InstalmentStatusCancelled, got.InstalmentStatus, "instalment %s", old.ID)
		s.True(got.Amount.Equal(dec("100")), "an amount is never changed in place")
	}

	for _, untouched := range before[8:] {
		s.True(s.instalmentByID(untouched.ID).Amount.Equal(dec("100")))
	}

	added := s.addedSince(before)
	s.Require().Len(added, 2)
	replacement, broughtForward := added[0], added[1]
	if replacement.Description == "Payment brought forward" {
		replacement, broughtForward = broughtForward, replacement
	}

	partlyReduced := before[7]
	s.NotEqual(partlyReduced.ID, replacement.ID)
	s.True(replacement.Amount.Equal(dec("50")), "got %s", replacement.Amount)
	s.True(replacement.BillOn.Equal(partlyReduced.BillOn), "what is left keeps its date")
	s.Equal(partlyReduced.BillTrigger, replacement.BillTrigger)
	s.Equal(partlyReduced.BillingTermID, replacement.BillingTermID)
	s.Equal(partlyReduced.Description, replacement.Description)
	s.Equal(partlyReduced.Kind, replacement.Kind)
	s.Equal(partlyReduced.Delivery, replacement.Delivery)
	s.Equal(types.InstalmentStatusScheduled, replacement.InstalmentStatus)
	s.True(replacement.ServicePeriodStart.Equal(partlyReduced.ServicePeriodStart))
	s.True(replacement.ServicePeriodEnd.Equal(partlyReduced.ServicePeriodEnd))

	s.Equal("Payment brought forward", broughtForward.Description)
	s.True(broughtForward.Amount.Equal(dec("250")))
	s.True(broughtForward.ServicePeriodStart.Equal(before[5].ServicePeriodStart))
	s.True(broughtForward.ServicePeriodEnd.Equal(before[7].ServicePeriodEnd), "the partly reduced month is part of what it pays for")
	s.True(s.liveTotal().Equal(dec("1200")))

	claimed, err := s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_stale", []string{partlyReduced.ID})
	s.Require().NoError(err)
	s.Empty(claimed, "a draft invoice computed earlier cannot claim the cancelled instalment")

	events := publisher.Events()
	s.Require().Len(events, 5, "three cancelled and two created")
	s.Empty(s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentUpdated))
	s.Equal(
		lo.Map(before[5:8], func(inst *billingterm.Instalment, _ int) string { return inst.ID }),
		s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCancelled),
	)
	s.Equal([]string{replacement.ID, broughtForward.ID}, s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated))
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_FailsWhenAnInstalmentIsClaimedFirst() {
	created, before := s.fiveOfTwelveInvoiced()
	repo := s.GetStores().BillingInstalmentRepo
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	racing := s.racingCommitmentService(func(ctx context.Context) {
		_, err := repo.Claim(ctx, "inv_race", []string{before[5].ID})
		s.Require().NoError(err)
	})

	now := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)
	_, err := racing.BringForwardInstalments(s.GetContext(), created.ID, dto.BringForwardInstalmentsRequest{Amount: dec("300"), BillOn: &now})
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)

	claimed := s.instalmentByID(before[5].ID)
	s.Equal(types.InstalmentStatusInvoiced, claimed.InstalmentStatus, "a claimed instalment is not overwritten")
	s.Equal("inv_race", lo.FromPtr(claimed.InvoiceID))
	s.Empty(s.addedSince(before))
	s.Empty(publisher.Events())
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_EverythingScheduled() {
	created, before := s.fiveOfTwelveInvoiced()
	now := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)

	_, err := s.svc.BringForwardInstalments(s.GetContext(), created.ID, dto.BringForwardInstalmentsRequest{Amount: dec("700"), BillOn: &now})
	s.Require().NoError(err)

	for _, old := range before[5:] {
		s.Equal(types.InstalmentStatusCancelled, s.instalmentByID(old.ID).InstalmentStatus, "instalment %s", old.ID)
	}

	s.True(s.liveTotal().Equal(dec("1200")))
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_Rejections() {
	created, _ := s.fiveOfTwelveInvoiced()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	inTerm := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		req  dto.BringForwardInstalmentsRequest
	}{
		{"more than is scheduled", dto.BringForwardInstalmentsRequest{Amount: dec("700.01"), BillOn: &inTerm}},
		{"a zero amount", dto.BringForwardInstalmentsRequest{BillOn: &inTerm}},
		{"a negative amount", dto.BringForwardInstalmentsRequest{Amount: dec("-100"), BillOn: &inTerm}},
		{"finer than the currency", dto.BringForwardInstalmentsRequest{Amount: dec("100.001"), BillOn: &inTerm}},
		{"before the term", dto.BringForwardInstalmentsRequest{Amount: dec("100"), BillOn: lo.ToPtr(created.StartDate.Add(-time.Second))}},
		{"after the term", dto.BringForwardInstalmentsRequest{Amount: dec("100"), BillOn: lo.ToPtr(created.EndDate.Add(time.Second))}},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			before := s.storedInstalments()
			publisher.Reset()

			_, err := s.svc.BringForwardInstalments(s.GetContext(), created.ID, tc.req)
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)
			s.Equal(before, s.storedInstalments(), "a rejected bring forward changes nothing")
			s.Empty(publisher.Events())
		})
	}
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_BillsNowByDefault() {
	ctx := s.GetContext()
	created, before := s.fiveOfTwelveInvoiced()

	// The fixtures sit in 2027; move the term around today so that now falls inside it.
	stored, err := s.GetStores().CommitmentRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	stored.StartDate = clock.Now().AddDate(0, -1, 0)
	stored.EndDate = lo.ToPtr(clock.Now().AddDate(0, 11, 0))

	asked := clock.Now()
	_, err = s.svc.BringForwardInstalments(ctx, created.ID, dto.BringForwardInstalmentsRequest{Amount: dec("100")})
	s.Require().NoError(err)

	s.Require().Len(s.addedSince(before), 1)
	added := s.addedSince(before)[0]
	s.False(added.BillOn.Before(asked), "bill_on defaults to now, got %s", added.BillOn)
	s.WithinDuration(asked, added.BillOn, time.Minute)
	s.Equal(types.InstalmentStatusCancelled, s.instalmentByID(before[5].ID).InstalmentStatus)
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_DefaultNowOutsideTheTermIsRejected() {
	ctx := s.GetContext()
	created, _ := s.fiveOfTwelveInvoiced()

	stored, err := s.GetStores().CommitmentRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	stored.StartDate = clock.Now().AddDate(0, 1, 0)
	stored.EndDate = lo.ToPtr(clock.Now().AddDate(1, 0, 0))

	_, err = s.svc.BringForwardInstalments(ctx, created.ID, dto.BringForwardInstalmentsRequest{Amount: dec("100")})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestBringForwardInstalments_RejectsInactiveOrPerPeriod() {
	ctx := s.GetContext()
	now := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)
	req := dto.BringForwardInstalmentsRequest{Amount: dec("100"), BillOn: &now}

	for name, id := range s.inactiveCommitments() {
		before := s.storedInstalments()
		_, err := s.svc.BringForwardInstalments(ctx, id, req)
		s.Require().Error(err, name)
		s.True(ierr.IsInvalidOperation(err), "%s: got %v", name, err)
		s.Equal(before, s.storedInstalments())
	}

	_, err := s.svc.BringForwardInstalments(ctx, "cmt_missing", req)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)

	activeReq := s.yearlyArrearRequest()
	activeReq.Priority = 2
	created, err := s.svc.Create(ctx, activeReq)
	s.Require().NoError(err)

	setCommitmentsV2(s.T(), ctx, s.GetStores(), false)
	_, err = s.svc.BringForwardInstalments(ctx, created.ID, req)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}
