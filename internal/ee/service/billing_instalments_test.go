package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// termsRequest is a binding amount commitment of value per billing period on one price, from start to end, collected
// through the given terms.
func termsRequest(name, priceID, value string, start, end time.Time, terms ...dto.BillingTermInput) dto.CreateCommitmentRequest {
	req := floorRequest(name, priceID, value, start, end)
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	req.BillingTerms = terms
	return req
}

// wholeValueUpfront is one remainder term billing the commitment's whole value on billOn, or on its start when billOn
// is nil.
func wholeValueUpfront(billOn *time.Time) dto.BillingTermInput {
	return dto.BillingTermInput{
		Kind:      types.BillingTermKindOnce,
		ValueType: types.BillingTermValueRemainder,
		BillOn:    billOn,
	}
}

// invoiceAt prepares the invoice for [from, to) at the reference point.
func (s *CommitmentE2ESuite) invoiceAt(
	ref types.InvoiceReferencePoint,
	subscriptionID string,
	from, to time.Time,
) *dto.CreateInvoiceRequest {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), subscriptionID)
	s.Require().NoError(err)

	req, err := s.billing.PrepareSubscriptionInvoiceRequest(s.GetContext(), &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   sub,
		PeriodStart:    from,
		PeriodEnd:      to,
		ReferencePoint: ref,
		AsOf:           to,
	})
	s.Require().NoError(err)
	return req
}

func instalmentLines(req *dto.CreateInvoiceRequest) []dto.CreateInvoiceLineItemRequest {
	return lo.Filter(req.LineItems, func(line dto.CreateInvoiceLineItemRequest, _ int) bool {
		return line.BillingInstalmentID != nil
	})
}

// markInvoiced marks the request's instalments invoiced, as finalizing its invoice does.
func (s *CommitmentE2ESuite) markInvoiced(req *dto.CreateInvoiceRequest) {
	ids := lo.Map(instalmentLines(req), func(line dto.CreateInvoiceLineItemRequest, _ int) string {
		return *line.BillingInstalmentID
	})

	invoiced, err := s.GetStores().BillingInstalmentRepo.Claim(s.GetContext(), types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), time.Now(), ids)
	s.Require().NoError(err)
	s.Require().Len(invoiced, len(ids))
}

// storedInstalmentLines returns the instalment lines stored on an invoice.
func (s *CommitmentE2ESuite) storedInstalmentLines(invoiceID string) []*invoice.InvoiceLineItem {
	lines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), invoiceID)
	s.Require().NoError(err)

	return lo.Filter(lines, func(line *invoice.InvoiceLineItem, _ int) bool { return line.BillingInstalmentID != nil })
}

// invoicesWithReason returns the subscription's invoices raised for reason, skipped ones included.
func (s *CommitmentE2ESuite) invoicesWithReason(subscriptionID string, reason types.InvoiceBillingReason) []*invoice.Invoice {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	filter.BillingReason = reason
	filter.InvoiceStatus = []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
		types.InvoiceStatusSkipped,
	}

	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return invoices
}

// A3 bills its $300 upfront and its first $70 at the end of January. A7 bills $300 a quarter in advance on a monthly
// subscription, so its April instalment is on the invoice issued at the 1 Apr boundary.
func (s *CommitmentE2ESuite) TestInstalments_ArrearAndAdvanceLanding() {
	feb, mar, apr := s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0), s.jan.AddDate(0, 3, 0)

	a3Price := s.planWith("a3", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	a3Sub := s.subscribe(a3Price, s.jan)
	a3Req := termsRequest("A3", a3Price.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), upfrontPlusRemainder()...)
	a3Req.InvoiceDescription = lo.ToPtr("Annual platform commitment")
	a3 := s.commit(a3Sub.ID, a3Req)

	january := s.invoiceFor(a3Sub.ID, s.jan, feb)
	s.True(january.AmountDue.Equal(dec("370")), "amount due %s", january.AmountDue)
	lines := instalmentLines(january)
	requireLines(s.T(), []wantLine{
		{types.CommitmentLineInstalment, "A3 - Instalment 1 of 11", "1", "300"},
		{types.CommitmentLineInstalment, "A3 - Instalment 2 of 11", "1", "70"},
	}, lines)

	upfront, first := a3.Instalments[0], a3.Instalments[1]
	s.Equal(upfront.ID, lo.FromPtr(lines[0].BillingInstalmentID))
	s.True(lines[0].PeriodStart.Equal(s.jan) && lines[0].PeriodEnd.Equal(s.jan.AddDate(0, 10, 0)), "the upfront covers the term")
	s.Equal(first.ID, lo.FromPtr(lines[1].BillingInstalmentID))
	s.True(lines[1].PeriodStart.Equal(s.jan) && lines[1].PeriodEnd.Equal(feb), "an arrear step covers its month")
	for _, line := range lines {
		s.Nil(line.PriceID)
		s.Equal(string(types.PRICE_TYPE_FIXED), lo.FromPtr(line.PriceType))
		s.Equal(a3.ID, lo.FromPtr(line.CommitmentID))
		s.Equal(string(types.InvoiceLineItemEntityTypeCommitment), lo.FromPtr(line.EntityType))
		s.Equal(a3.ID, lo.FromPtr(line.EntityID))
		s.Equal(a3.ID, line.Metadata[types.MetadataKeyCommitmentID])
		s.Equal("Annual platform commitment", line.Metadata[types.MetadataKeyDescription])
	}

	previewed := instalmentLines(s.invoiceAt(types.ReferencePointPreview, a3Sub.ID, s.jan, feb))
	s.Len(previewed, 2, "a preview lists the instalments due by the period end")
	s.Len(instalmentLines(s.invoiceAt(types.ReferencePointInternalPreview, a3Sub.ID, s.jan, feb)), 2)
	s.Empty(instalmentLines(s.invoiceAt(types.ReferencePointCancel, a3Sub.ID, s.jan, feb)))

	a7Price := s.planWith("a7", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	a7Sub := s.subscribe(a7Price, s.jan)
	s.commit(a7Sub.ID, termsRequest("A7", a7Price.ID, "100", s.jan, s.jan.AddDate(1, 0, 0), dto.BillingTermInput{
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueFixed,
		Value:          lo.ToPtr(dec("300")),
		PeriodCount:    lo.ToPtr(3),
		InvoiceCadence: lo.ToPtr(types.InvoiceCadenceAdvance),
	}))

	s.markInvoiced(s.invoiceFor(a7Sub.ID, s.jan, feb))
	s.Empty(instalmentLines(s.invoiceFor(a7Sub.ID, feb, mar)))

	march := s.invoiceFor(a7Sub.ID, mar, apr)
	quarter := instalmentLines(march)
	requireLines(s.T(), []wantLine{{types.CommitmentLineInstalment, "A7 - Instalment 2 of 4", "1", "300"}}, quarter)
	s.True(quarter[0].PeriodStart.Equal(apr) && quarter[0].PeriodEnd.Equal(s.jan.AddDate(0, 6, 0)), "an advance step covers the quarter it opens")
}

// A commitment from 15 Jan bills its first advance instalment on the invoice of the next boundary, 1 Feb, and on no
// other.
func (s *CommitmentE2ESuite) TestInstalments_MidPeriodAdvance() {
	feb, mar, apr := s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0), s.jan.AddDate(0, 3, 0)
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	tokens := s.planWith("mid_advance", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)

	// $93 a month is $51 for the 17 days from 15 Jan: 51 + 93 + 93 = 237, three advance instalments of $79.
	created := s.commit(sub.ID, termsRequest("Ramp", tokens.ID, "93", start, apr, dto.BillingTermInput{
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueFixed,
		Value:          lo.ToPtr(dec("79")),
		InvoiceCadence: lo.ToPtr(types.InvoiceCadenceAdvance),
	}))
	first := created.Instalments[0]
	s.True(first.BillOn.Equal(start), "an advance step bills at its start")

	s.Empty(instalmentLines(s.invoiceAt(types.ReferencePointPeriodStart, sub.ID, s.jan, feb)), "January opens before the commitment")

	january := s.invoiceFor(sub.ID, s.jan, feb)
	requireLines(s.T(), []wantLine{
		{types.CommitmentLineInstalment, "Ramp - Instalment 1 of 3", "1", "79"},
		{types.CommitmentLineInstalment, "Ramp - Instalment 2 of 3", "1", "79"},
	}, instalmentLines(january))
	s.Equal(first.ID, lo.FromPtr(instalmentLines(january)[0].BillingInstalmentID))
	s.markInvoiced(january)

	february := s.invoiceFor(sub.ID, feb, mar)
	requireLines(s.T(), []wantLine{
		{types.CommitmentLineInstalment, "Ramp - Instalment 3 of 3", "1", "79"},
	}, instalmentLines(february))
}

// With nothing else due when January opens, the invoice issued then still bills the upfront instalment.
func (s *CommitmentE2ESuite) TestInstalments_OnlyInvoiceWithInstalments() {
	tokens := s.planWith("only_instalments", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), upfrontPlusRemainder()...))

	s.requireInvoice(s.invoiceAt(types.ReferencePointPeriodStart, sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "300",
		wantLine{types.CommitmentLineInstalment, "Platform - Instalment 1 of 11", "1", "300"},
	)
}

// An upfront instalment of a trialing subscription is not on the $0 trial-start invoice; the trial-end invoice bills it.
func (s *CommitmentE2ESuite) TestInstalments_TrialStartCarriesNone() {
	ctx := s.GetContext()
	tokens := s.planWith("trial", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	resp, err := s.subscriptions.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.customer.ID,
		PlanID:             tokens.EntityID,
		StartDate:          lo.ToPtr(s.jan),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		TrialPeriodDays:    lo.ToPtr(14),
		Commitments: []dto.CreateCommitmentRequest{
			termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 6, 0), wholeValueUpfront(nil)),
		},
	})
	s.Require().NoError(err)
	s.Require().Equal(types.SubscriptionStatusTrialing, resp.SubscriptionStatus)

	trialStart := s.invoicesWithReason(resp.ID, types.InvoiceBillingReasonSubscriptionTrialStart)
	s.Require().Len(trialStart, 1)
	s.True(trialStart[0].AmountDue.IsZero())
	s.Empty(s.storedInstalmentLines(trialStart[0].ID))

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	trialEnd, err := s.subscriptions.(*subscriptionService).ProcessSingleSubscriptionTrialEnd(ctx, sub, sub.TrialEnd.Add(time.Hour))
	s.Require().NoError(err)
	s.Require().NotNil(trialEnd)

	instalments, err := s.GetStores().BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{SubscriptionIDs: []string{resp.ID}})
	s.Require().NoError(err)
	s.Require().Len(instalments, 1)

	billed := s.storedInstalmentLines(trialEnd.ID)
	s.Require().Len(billed, 1)
	s.Equal(instalments[0].ID, lo.FromPtr(billed[0].BillingInstalmentID))
	s.True(billed[0].Amount.Equal(instalments[0].Amount), "billed %s of %s", billed[0].Amount, instalments[0].Amount)
}

// An instalment dated while the subscription is paused waits: the paused stretch bills nothing, and the invoice issued
// at the resume carries it.
func (s *CommitmentE2ESuite) TestInstalments_PauseDefersToResume() {
	ctx := s.GetContext()

	// Resuming runs on the real clock, so the subscription starts two months back and pauses in the month before this.
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -2, 0)
	pauseStart := start.AddDate(0, 1, 5)

	tokens := s.planWith("paused", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, start)
	s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", start, start.AddDate(0, 6, 0),
		wholeValueUpfront(lo.ToPtr(pauseStart.AddDate(0, 0, 5))),
	))

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	pause := &subscription.SubscriptionPause{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PAUSE),
		SubscriptionID:      sub.ID,
		PauseStatus:         types.PauseStatusActive,
		PauseMode:           types.PauseModeImmediate,
		PauseStart:          pauseStart,
		OriginalPeriodStart: start.AddDate(0, 1, 0),
		OriginalPeriodEnd:   start.AddDate(0, 2, 0),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, pause))

	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	sub.PauseStatus = types.PauseStatusActive
	sub.ActivePauseID = lo.ToPtr(pause.ID)
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = pauseStart, start.AddDate(0, 2, 0)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err = s.subscriptions.ResumeSubscription(ctx, sub.ID, &dto.ResumeSubscriptionRequest{})
	s.Require().NoError(err)

	paused := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionPaused)
	s.Require().NotEmpty(paused)
	for _, inv := range paused {
		s.Empty(s.storedInstalmentLines(inv.ID), "paused stretch %s-%s", inv.PeriodStart, inv.PeriodEnd)
	}

	resumed := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonSubscriptionResume)
	s.Require().Len(resumed, 1)
	s.Len(s.storedInstalmentLines(resumed[0].ID), 1)
}

// Two instalments of one commitment covering the same month, as two terms can produce them, stay two lines however
// often the draft is recalculated.
func (s *CommitmentE2ESuite) TestInstalments_ReconcileKeepsSamePeriodPair() {
	ctx := s.GetContext()
	feb := s.jan.AddDate(0, 1, 0)
	tokens := s.planWith("same_period", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	created := s.commit(sub.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), upfrontPlusRemainder()...))

	// Usage puts a subscription line on the draft, so recalculation updates its lines in place.
	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")

	_, err := s.GetStores().BillingInstalmentRepo.Cancel(ctx, types.BillingTermOwnerCommitment, created.ID,
		[]types.InstalmentStatus{types.InstalmentStatusScheduled})
	s.Require().NoError(err)

	pair := make([]*billingterm.Instalment, 0, 2)
	for n, amount := range []string{"400", "600"} {
		pair = append(pair, &billingterm.Instalment{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT),
			OwnerType:          types.BillingTermOwnerCommitment,
			OwnerID:            created.ID,
			CustomerID:         s.customer.ID,
			SubscriptionID:     lo.ToPtr(sub.ID),
			Currency:           "usd",
			Kind:               types.InstalmentKindInstalment,
			BillTrigger:        types.InstalmentBillTriggerDate,
			BillOn:             s.jan.AddDate(0, 0, 10*n),
			Amount:             dec(amount),
			InvoiceLabel:       "January payment",
			ServicePeriodStart: s.jan,
			ServicePeriodEnd:   feb,
			Delivery:           types.InstalmentDeliverySubscriptionInvoice,
			InstalmentStatus:   types.InstalmentStatusScheduled,
			EnvironmentID:      types.GetEnvironmentID(ctx),
			BaseModel:          types.GetDefaultBaseModel(ctx),
		})
	}
	_, err = s.GetStores().BillingInstalmentRepo.CreateMany(ctx, pair)
	s.Require().NoError(err)

	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, err := invoices.CreateDraftInvoiceForSubscription(ctx, dto.CreateSubscriptionDraftInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    s.jan,
		PeriodEnd:      feb,
		ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.Require().NoError(err)
	_, _, err = invoices.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)

	want := []string{pair[0].ID, pair[1].ID}
	for range 2 {
		recalculated, err := invoices.RecalculateInvoiceV2(ctx, draft.ID, false)
		s.Require().NoError(err)
		s.True(recalculated.Subtotal.Equal(decimal.NewFromInt(1000)), "subtotal %s", recalculated.Subtotal)

		billed := lo.Map(s.storedInstalmentLines(draft.ID), func(line *invoice.InvoiceLineItem, _ int) string {
			return *line.BillingInstalmentID
		})
		s.ElementsMatch(want, billed)
	}
}

// With commitments_v2 off, no invoice bills instalments.
func (s *CommitmentE2ESuite) TestInstalments_DisabledSettingAddsNothing() {
	feb := s.jan.AddDate(0, 1, 0)
	tokens := s.planWith("disabled", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), upfrontPlusRemainder()...))

	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)

	s.Empty(instalmentLines(s.invoiceAt(types.ReferencePointPeriodStart, sub.ID, s.jan, feb)))
	s.Empty(instalmentLines(s.invoiceFor(sub.ID, s.jan, feb)))
}

// A term can deliver differently from its commitment: the upfront bills on its own invoice on its date while the
// monthly instalments go on the subscription invoices.
func (s *CommitmentE2ESuite) TestDelivery_PerTerm() {
	ctx := s.GetContext()
	start := s.jan.AddDate(0, 0, 14)
	tokens := s.planWith("delivery_per_term", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	upfront := dto.BillingTermInput{
		Kind:                types.BillingTermKindOnce,
		ValueType:           types.BillingTermValueFixed,
		Value:               lo.ToPtr(dec("300")),
		InstalmentInvoicing: lo.ToPtr(types.InstalmentDeliverySeparateInvoice),
	}
	monthly := dto.BillingTermInput{
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueRemainder,
		InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
	}
	committed := s.commit(sub.ID, termsRequest("Platform", tokens.ID, "100", start, start.AddDate(1, 0, 0), upfront, monthly))
	first, second := committed.Instalments[0], committed.Instalments[1]
	s.Require().Equal(types.InstalmentDeliverySeparateInvoice, first.Delivery)
	s.Require().Equal(types.InstalmentDeliverySubscriptionInvoice, second.Delivery)

	s.Require().NoError(s.commitments.BillDueInstalments(ctx))
	separate := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms)
	s.Require().Len(separate, 1, "only the upfront bills on its own invoice")
	s.requireBilledBy(separate[0].ID, []string{first.ID})

	january := s.billPeriod(sub.ID, s.jan)
	s.requireBilledBy(january.ID, []string{second.ID})
}
