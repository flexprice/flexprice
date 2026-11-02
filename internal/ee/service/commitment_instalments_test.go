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

	sep := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
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
	sep := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
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
	sep := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)

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
	sep := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
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
		{Amount: dec("1000"), BillOn: lo.ToPtr(time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC))},
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

	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
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

	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
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

	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
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
	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

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
	inTerm := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

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

	// The fixtures sit in 2025; move the term around today so that now falls inside it.
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
	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
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

	for _, billOn := range []time.Time{created.StartDate, *created.EndDate} {
		triggered, err := s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &billOn})
		s.Require().NoError(err)
		s.True(triggered.BillOn.Equal(billOn))
	}
}

// A triggered milestone stays scheduled until an invoice claims it, so the trigger can move its date.
func (s *CommitmentServiceSuite) TestTriggerInstalment_MovesAScheduledMilestone() {
	created, milestone := s.milestoneOf(s.milestoneRequest())
	first := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	second := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)

	_, err := s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &first})
	s.Require().NoError(err)

	triggered, err := s.svc.TriggerInstalment(s.GetContext(), created.ID, milestone.ID, dto.TriggerInstalmentRequest{BillOn: &second})
	s.Require().NoError(err)
	s.True(triggered.BillOn.Equal(second))
	s.NotNil(triggered.TriggeredAt)
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
	claimed, err := repo.Claim(ctx, "inv_x", []string{invoicedMilestone.ID})
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

	signing := dto.BillingTermInput{
		Kind:       types.BillingTermKindOnce,
		ValueType:  types.BillingTermValuePercentage,
		Percentage: lo.ToPtr(dec("50")),
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
