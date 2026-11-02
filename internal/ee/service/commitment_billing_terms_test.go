package service

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// upfrontPlusRemainder is the A3 shape: 30% on the start date, the rest in arrear instalments.
func upfrontPlusRemainder() []dto.BillingTermInput {
	return []dto.BillingTermInput{
		{
			Kind:       types.BillingTermKindOnce,
			ValueType:  types.BillingTermValuePercentage,
			Percentage: lo.ToPtr(dec("30")),
		},
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	}
}

// billingTermsRequest is a binding amount commitment of 100 a month for ten months from 1 Jan 2027, worth 1,000.
func (s *CommitmentServiceSuite) billingTermsRequest() dto.CreateCommitmentRequest {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, dec("100")
	req.EndDate = lo.ToPtr(req.StartDate.AddDate(0, 10, 0))
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	req.BillingTerms = upfrontPlusRemainder()
	return req
}

func (s *CommitmentServiceSuite) storedInstalments() []*billingterm.Instalment {
	rows, err := s.GetStores().BillingInstalmentRepo.List(s.GetContext(), &billingterm.InstalmentFilter{})
	s.Require().NoError(err)
	return rows
}

func (s *CommitmentServiceSuite) storedTerms(commitmentID string) []*billingterm.Term {
	rows, err := s.GetStores().BillingTermRepo.List(s.GetContext(), types.BillingTermOwnerCommitment, commitmentID)
	s.Require().NoError(err)
	return rows
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_StoresTermsAndInstalments() {
	resp, err := s.svc.Create(s.GetContext(), s.billingTermsRequest())
	s.Require().NoError(err)

	s.True(lo.FromPtr(resp.TotalAmount).Equal(dec("1000")), "got %s", lo.FromPtr(resp.TotalAmount))
	s.Equal(types.InstalmentDeliverySubscriptionInvoice, lo.FromPtr(resp.InstalmentInvoicing))
	s.Equal(types.CommitmentCollectionBillingTerms, lo.FromPtr(resp.CollectionMethod))

	stored, err := s.GetStores().CommitmentRepo.Get(s.GetContext(), resp.ID)
	s.Require().NoError(err)
	s.True(lo.FromPtr(stored.TotalAmount).Equal(dec("1000")))
	s.Equal(types.InstalmentDeliverySubscriptionInvoice, lo.FromPtr(stored.InstalmentInvoicing))

	terms := s.storedTerms(resp.ID)
	s.Require().Len(terms, 2)
	s.Len(resp.BillingTerms, 2)
	for _, term := range terms {
		s.Equal(types.BillingTermOwnerCommitment, term.OwnerType)
		s.Equal(resp.ID, term.OwnerID)
		s.Equal("bterm_", term.ID[:6])
	}

	upfront, remainder := terms[0], terms[1]
	if upfront.Kind != types.BillingTermKindOnce {
		upfront, remainder = remainder, upfront
	}

	s.Equal(types.BillingTermValueFixed, upfront.ValueType, "a percentage is stored as a fixed amount")
	s.True(lo.FromPtr(upfront.Amount).Equal(dec("300")))
	s.True(lo.FromPtr(upfront.Percentage).Equal(dec("30")), "the percentage entered is kept")
	s.Equal(types.BillingTermValueRemainder, remainder.ValueType)
	s.Nil(remainder.Amount)

	instalments := s.storedInstalments()
	s.Require().Len(instalments, 11)
	s.Len(resp.Instalments, 11)

	total := decimal.Zero
	for _, i := range instalments {
		total = total.Add(i.Amount)
		s.Equal(resp.ID, i.OwnerID)
		s.Equal(types.BillingTermOwnerCommitment, i.OwnerType)
		s.Equal(s.fx.sub.ID, i.SubscriptionID)
		s.Equal(s.fx.customer.ID, i.CustomerID)
		s.Equal(types.InstalmentStatusScheduled, i.InstalmentStatus)
		s.Equal(types.InstalmentDeliverySubscriptionInvoice, i.Delivery)
		s.Contains([]string{upfront.ID, remainder.ID}, lo.FromPtr(i.BillingTermID))
	}

	s.True(total.Equal(dec("1000")), "instalments add up to %s", total)

	jan := resp.StartDate
	s.True(instalments[0].Amount.Equal(dec("300")))
	s.True(instalments[0].BillOn.Equal(jan), "the upfront term bills on the start date")
	s.True(instalments[1].Amount.Equal(dec("70")))
	s.True(instalments[1].BillOn.Equal(jan.AddDate(0, 1, 0)), "arrear instalments bill at the step end")
	s.True(instalments[10].BillOn.Equal(*resp.EndDate))
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_QuantityCommitmentTotalsFromTheCommittedPrice() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	req.BillingTerms = []dto.BillingTermInput{
		{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Amount:    lo.ToPtr(dec("1")),
		},
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	}

	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	// 8 units × 0.048 × 12 periods = 4.608, rounded to the cent.
	s.True(lo.FromPtr(resp.TotalAmount).Equal(dec("4.61")), "got %s", lo.FromPtr(resp.TotalAmount))
	s.Require().Len(resp.Instalments, 13)

	total := decimal.Zero
	for _, i := range resp.Instalments {
		total = total.Add(i.Amount)
	}

	s.True(total.Equal(dec("4.61")), "instalments add up to %s", total)
}

func (s *CommitmentServiceSuite) TestPreview_BillingTerms_WritesNothing() {
	resp, err := s.svc.Preview(s.GetContext(), s.billingTermsRequest())
	s.Require().NoError(err)

	s.Len(resp.Instalments, 11)
	s.Require().Len(resp.BillingTerms, 2)
	s.True(lo.FromPtr(resp.TotalAmount).Equal(dec("1000")))
	s.True(resp.CommitmentValue.Equal(dec("1000")))

	upfront, ok := lo.Find(resp.BillingTerms, func(t *billingterm.Term) bool { return t.Kind == types.BillingTermKindOnce })
	s.Require().True(ok)
	s.True(lo.FromPtr(upfront.Amount).Equal(dec("300")), "a preview resolves a percentage to its amount")

	s.Empty(s.storedInstalments())
	s.Empty(s.storedTerms(resp.ID))
	n, err := s.GetStores().CommitmentRepo.Count(s.GetContext(), commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Zero(n)
}

func (s *CommitmentServiceSuite) TestPreview_BillingTerms_ThenCreateResolvesTheSameInstalments() {
	req := s.billingTermsRequest()
	preview, err := s.svc.Preview(s.GetContext(), req)
	s.Require().NoError(err)

	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.Require().Len(created.Instalments, len(preview.Instalments))
	for n, want := range preview.Instalments {
		s.True(created.Instalments[n].Amount.Equal(want.Amount), "instalment %d", n)
		s.True(created.Instalments[n].BillOn.Equal(want.BillOn), "instalment %d", n)
	}
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_Validation() {
	tests := []struct {
		name   string
		mutate func(r *dto.CreateCommitmentRequest)
	}{
		{"terms without collection_method", func(r *dto.CreateCommitmentRequest) { r.CollectionMethod = nil }},
		{"terms on a per_period commitment", func(r *dto.CreateCommitmentRequest) {
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
		}},
		{"billing_terms without terms", func(r *dto.CreateCommitmentRequest) { r.BillingTerms = nil }},
		{"non-binding", func(r *dto.CreateCommitmentRequest) { r.Binding = lo.ToPtr(false) }},
		{"missing end_date", func(r *dto.CreateCommitmentRequest) { r.EndDate = nil }},
		{"instalment_invoicing on per_period", func(r *dto.CreateCommitmentRequest) {
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
			r.BillingTerms = nil
			r.InstalmentInvoicing = lo.ToPtr(types.InstalmentDeliverySeparateInvoice)
		}},
		{"unknown instalment_invoicing", func(r *dto.CreateCommitmentRequest) {
			r.InstalmentInvoicing = lo.ToPtr(types.InstalmentDelivery("post"))
		}},
		{"percentage term with an amount", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms[0].Amount = lo.ToPtr(dec("300"))
		}},
		{"percentage term without a percentage", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms[0].Percentage = nil
		}},
		{"fixed term with a percentage", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms[0].ValueType = types.BillingTermValueFixed
		}},
		{"terms that miss the total", func(r *dto.CreateCommitmentRequest) { r.BillingTerms = r.BillingTerms[:1] }},
		{"two remainder terms", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms = append(r.BillingTerms, r.BillingTerms[1])
		}},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			req := s.billingTermsRequest()
			tc.mutate(&req)

			_, err := s.svc.Create(s.GetContext(), req)
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)
			s.Empty(s.storedInstalments())
		})
	}
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_RejectedWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)

	_, err := s.svc.Create(s.GetContext(), s.billingTermsRequest())
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_SeparateInvoiceDelivery() {
	req := s.billingTermsRequest()
	req.InstalmentInvoicing = lo.ToPtr(types.InstalmentDeliverySeparateInvoice)

	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.Equal(types.InstalmentDeliverySeparateInvoice, lo.FromPtr(resp.InstalmentInvoicing))
	for _, i := range s.storedInstalments() {
		s.Equal(types.InstalmentDeliverySeparateInvoice, i.Delivery)
	}
}

// A recurring term may start on a boundary of a subscription whose billing periods start at local midnight.
func (s *CommitmentServiceSuite) TestCreate_BillingTerms_KolkataBoundary() {
	ctx := s.GetContext()
	ist, err := time.LoadLocation("Asia/Kolkata")
	s.Require().NoError(err)

	start := time.Date(2027, 1, 1, 0, 0, 0, 0, ist).UTC()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
	s.Require().NoError(err)

	sub.Timezone, sub.StartDate, sub.BillingAnchor = "Asia/Kolkata", start, start
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = start, time.Date(2027, 2, 1, 0, 0, 0, 0, ist).UTC()
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	line := *s.fx.lineAPI
	line.StartDate = start
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, &line))

	april := time.Date(2027, 4, 1, 0, 0, 0, 0, ist)
	req := s.billingTermsRequest()
	req.StartDate = start
	req.EndDate = lo.ToPtr(time.Date(2027, 11, 1, 0, 0, 0, 0, ist).UTC())
	req.BillingTerms = []dto.BillingTermInput{
		{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Amount:    lo.ToPtr(dec("300")),
		},
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
			StartsOn:       &april,
		},
	}

	utcApril := req
	utcApril.BillingTerms = slices.Clone(req.BillingTerms)
	utcApril.BillingTerms[1].StartsOn = lo.ToPtr(time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC))
	_, err = s.svc.Create(ctx, utcApril)
	s.Require().Error(err, "1 Apr 00:00 UTC is no boundary of a Kolkata subscription")
	s.True(ierr.IsValidation(err), "got %v", err)

	resp, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	// 300 up front, then 700 over the seven months April to October.
	s.Require().Len(resp.Instalments, 8)
	first := resp.Instalments[1]
	s.True(first.ServicePeriodStart.Equal(april), "first step starts at %s", first.ServicePeriodStart)
	s.True(first.ServicePeriodEnd.Equal(time.Date(2027, 5, 1, 0, 0, 0, 0, ist)), "first step ends at %s", first.ServicePeriodEnd)
	s.True(first.BillOn.Equal(first.ServicePeriodEnd))
	s.True(first.Amount.Equal(dec("100")))
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_StartsOnMustBeABoundary() {
	req := s.billingTermsRequest()
	req.BillingTerms[1].StartsOn = lo.ToPtr(req.StartDate.AddDate(0, 3, 14))

	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), "billing_terms[1].starts_on")
	s.Empty(s.storedInstalments())
}

func (s *CommitmentE2ESuite) TestCreateSubscription_InlineBillingTerms() {
	tokens := s.planWith("inline_terms", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.05")
	req := floorRequest("Launch floor", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0))
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	req.BillingTerms = upfrontPlusRemainder()

	sub := s.subscribe(tokens, s.jan, req)

	commitments, err := s.commitments.List(s.GetContext(), &commitment.Filter{SubscriptionIDs: []string{sub.ID}})
	s.Require().NoError(err)
	s.Require().Len(commitments.Items, 1)
	owner := commitments.Items[0]
	s.True(lo.FromPtr(owner.TotalAmount).Equal(dec("1000")))

	instalments, err := s.GetStores().BillingInstalmentRepo.List(s.GetContext(), &billingterm.InstalmentFilter{
		OwnerType: types.BillingTermOwnerCommitment,
		OwnerIDs:  []string{owner.ID},
	})
	s.Require().NoError(err)
	s.Require().Len(instalments, 11)
	for _, i := range instalments {
		s.Equal(sub.ID, i.SubscriptionID)
		s.Equal(s.customer.ID, i.CustomerID)
	}

	terms, err := s.GetStores().BillingTermRepo.List(s.GetContext(), types.BillingTermOwnerCommitment, owner.ID)
	s.Require().NoError(err)
	s.Len(terms, 2)
}

// halfUpfrontPlusRemainder replaces upfrontPlusRemainder with 500 fixed up front.
func halfUpfrontPlusRemainder() dto.ReplaceBillingTermsRequest {
	terms := upfrontPlusRemainder()
	terms[0].ValueType, terms[0].Percentage, terms[0].Amount = types.BillingTermValueFixed, nil, lo.ToPtr(dec("500"))
	return dto.ReplaceBillingTermsRequest{BillingTerms: terms}
}

func (s *CommitmentServiceSuite) claimFirstInstalment(invoiceID string) *billingterm.Instalment {
	rows := s.storedInstalments()
	s.Require().NotEmpty(rows)

	claimed, err := s.GetStores().BillingInstalmentRepo.Claim(s.GetContext(), invoiceID, []string{rows[0].ID})
	s.Require().NoError(err)
	s.Require().Len(claimed, 1)
	return claimed[0]
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_CancelsAllAndRegenerates() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	oldTerms := s.storedTerms(created.ID)
	oldInstalments := s.storedInstalments()
	invoiced := s.claimFirstInstalment("inv_x")

	resp, err := s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)
	s.Equal([]string{"inv_x"}, resp.InvoicesToReview)

	for _, old := range oldInstalments {
		got, err := s.GetStores().BillingInstalmentRepo.Get(ctx, old.ID)
		s.Require().NoError(err)
		s.Equal(types.InstalmentStatusCancelled, got.InstalmentStatus, "instalment %s", old.ID)
		if old.ID == invoiced.ID {
			s.Equal("inv_x", lo.FromPtr(got.InvoiceID), "a cancelled instalment keeps its invoice")
		}
	}

	archived := s.GetStores().BillingTermRepo.(*testutil.InMemoryBillingTermStore)
	for _, old := range oldTerms {
		got, err := archived.Get(ctx, old.ID)
		s.Require().NoError(err)
		s.Equal(types.StatusArchived, got.Status)
	}

	published := s.storedTerms(created.ID)
	s.Require().Len(published, 2)
	s.ElementsMatch(lo.Map(published, func(t *billingterm.Term, _ int) string { return t.ID }),
		lo.Map(resp.BillingTerms, func(t *billingterm.Term, _ int) string { return t.ID }))
	for _, term := range published {
		s.False(lo.Contains(lo.Map(oldTerms, func(t *billingterm.Term, _ int) string { return t.ID }), term.ID))
	}

	live := lo.Filter(s.storedInstalments(), func(i *billingterm.Instalment, _ int) bool {
		return i.InstalmentStatus == types.InstalmentStatusScheduled
	})
	s.Require().Len(live, 11)
	s.Len(resp.Instalments, 11)

	total := decimal.Zero
	for n, i := range resp.Instalments {
		total = total.Add(i.Amount)
		s.Equal(live[n].ID, i.ID, "the response lists the stored instalments in billing order")
		s.Equal(types.InstalmentStatusScheduled, i.InstalmentStatus)
	}

	s.True(total.Equal(dec("1000")), "new instalments add up to %s", total)
	s.True(resp.Instalments[0].Amount.Equal(dec("500")))
	s.True(resp.Instalments[1].Amount.Equal(dec("50")))

	got, err := s.GetStores().CommitmentRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	s.True(lo.FromPtr(got.TotalAmount).Equal(dec("1000")), "the total is unchanged")
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_CanReplaceTwice() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	req := dto.ReplaceBillingTermsRequest{BillingTerms: upfrontPlusRemainder()}
	first, err := s.svc.ReplaceBillingTerms(ctx, created.ID, req)
	s.Require().NoError(err)
	s.Empty(first.InvoicesToReview)

	second, err := s.svc.ReplaceBillingTerms(ctx, created.ID, req)
	s.Require().NoError(err)
	s.Len(second.Instalments, 11)
	s.Len(s.storedTerms(created.ID), 2)
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_InvalidTermsChangeNothing() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	terms, instalments := s.storedTerms(created.ID), s.storedInstalments()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	for name, req := range map[string]dto.ReplaceBillingTermsRequest{
		"no terms":         {},
		"terms miss total": {BillingTerms: upfrontPlusRemainder()[:1]},
		"percentage with amount": {BillingTerms: []dto.BillingTermInput{
			{
				Kind:       types.BillingTermKindOnce,
				ValueType:  types.BillingTermValuePercentage,
				Percentage: lo.ToPtr(dec("30")),
				Amount:     lo.ToPtr(dec("300")),
			},
			upfrontPlusRemainder()[1],
		}},
	} {
		_, err := s.svc.ReplaceBillingTerms(ctx, created.ID, req)
		s.Require().Error(err, name)
		s.True(ierr.IsValidation(err), "%s: got %v", name, err)
	}

	s.Equal(terms, s.storedTerms(created.ID))
	s.Equal(instalments, s.storedInstalments())
	s.Empty(publisher.Events(), "a rejected edit publishes nothing")
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_QuantityCommitmentUsesTheCommittedPrice() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	req.BillingTerms = []dto.BillingTermInput{
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	}

	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	resp, err := s.svc.ReplaceBillingTerms(ctx, created.ID, dto.ReplaceBillingTermsRequest{BillingTerms: []dto.BillingTermInput{
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
			Split:          lo.ToPtr(types.BillingTermSplitProportional),
		},
	}})
	s.Require().NoError(err)

	total := decimal.Zero
	for _, i := range resp.Instalments {
		total = total.Add(i.Amount)
	}

	s.True(total.Equal(dec("4.61")), "instalments add up to %s", total)
}

func (s *CommitmentServiceSuite) TestPreviewBillingTerms_WritesNothing() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	terms, instalments := s.storedTerms(created.ID), s.storedInstalments()
	s.claimFirstInstalment("inv_x")
	invoiced := s.storedInstalments()

	resp, err := s.svc.PreviewBillingTerms(ctx, created.ID, dto.ReplaceBillingTermsRequest{
		BillingTerms: []dto.BillingTermInput{
			{
				Kind:       types.BillingTermKindOnce,
				ValueType:  types.BillingTermValuePercentage,
				Percentage: lo.ToPtr(dec("50")),
			},
			upfrontPlusRemainder()[1],
		},
	})
	s.Require().NoError(err)

	s.Equal([]string{"inv_x"}, resp.InvoicesToReview)
	s.Len(resp.Instalments, 11)
	s.Require().Len(resp.BillingTerms, 2)

	upfront, ok := lo.Find(resp.BillingTerms, func(t *billingterm.Term) bool { return t.Kind == types.BillingTermKindOnce })
	s.Require().True(ok)
	s.True(lo.FromPtr(upfront.Amount).Equal(dec("500")), "a preview resolves a percentage to its amount")

	s.Equal(terms, s.storedTerms(created.ID))
	s.Equal(invoiced, s.storedInstalments(), "no instalment was cancelled or added")
	s.NotEqual(instalments, invoiced)
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_RejectsInactiveOrPerPeriod() {
	ctx := s.GetContext()
	req := halfUpfrontPlusRemainder()

	perPeriodReq := s.billingTermsRequest()
	perPeriodReq.CollectionMethod, perPeriodReq.BillingTerms, perPeriodReq.Priority = nil, nil, 1
	perPeriod, err := s.svc.Create(ctx, perPeriodReq)
	s.Require().NoError(err)

	terminated, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	at := terminated.StartDate.AddDate(0, 5, 0)
	_, err = s.svc.Terminate(ctx, terminated.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	before := s.storedInstalments()
	for name, id := range map[string]string{"per_period": perPeriod.ID, "terminated": terminated.ID} {
		_, err := s.svc.ReplaceBillingTerms(ctx, id, req)
		s.Require().Error(err, name)
		s.True(ierr.IsInvalidOperation(err), "%s: got %v", name, err)

		_, err = s.svc.PreviewBillingTerms(ctx, id, req)
		s.Require().Error(err, name)
		s.True(ierr.IsInvalidOperation(err), "%s preview: got %v", name, err)
	}

	s.Equal(before, s.storedInstalments())

	_, err = s.svc.ReplaceBillingTerms(ctx, "cmt_missing", req)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)

	setCommitmentsV2(s.T(), ctx, s.GetStores(), false)
	_, err = s.svc.ReplaceBillingTerms(ctx, terminated.ID, req)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestGetBillingTerms_And_ListInstalments() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	replaced, err := s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	got, err := s.svc.GetBillingTerms(ctx, created.ID)
	s.Require().NoError(err)
	s.Empty(got.InvoicesToReview)
	s.Len(got.BillingTerms, 2, "archived terms are not listed")
	s.ElementsMatch(
		lo.Map(replaced.BillingTerms, func(t *billingterm.Term, _ int) string { return t.ID }),
		lo.Map(got.BillingTerms, func(t *billingterm.Term, _ int) string { return t.ID }),
	)
	s.Require().Len(got.Instalments, 11, "cancelled instalments are not part of the current plan")
	s.True(slices.IsSortedFunc(got.Instalments, func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) }))

	listed, err := s.svc.ListInstalments(ctx, created.ID, nil)
	s.Require().NoError(err)
	s.Len(listed.Items, 22, "history keeps the cancelled instalments")
	s.True(slices.IsSortedFunc(listed.Items, func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) }))

	cancelled := lo.Filter(listed.Items, func(i *billingterm.Instalment, _ int) bool {
		return i.InstalmentStatus == types.InstalmentStatusCancelled
	})
	s.Len(cancelled, 11)

	one, err := s.svc.GetInstalment(ctx, listed.Items[0].ID)
	s.Require().NoError(err)
	s.Equal(listed.Items[0].ID, one.ID)

	_, err = s.svc.GetInstalment(ctx, "binst_missing")
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)

	_, err = s.svc.GetBillingTerms(ctx, "cmt_missing")
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)

	_, err = s.svc.ListInstalments(ctx, "cmt_missing", nil)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestListInstalments_FiltersByStatus() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	s.claimFirstInstalment("inv_x")
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	tests := []struct {
		name     string
		statuses []types.InstalmentStatus
		want     int
		wantOnly []types.InstalmentStatus
	}{
		{"every status", nil, 22, nil},
		{"cancelled", []types.InstalmentStatus{types.InstalmentStatusCancelled}, 11, []types.InstalmentStatus{types.InstalmentStatusCancelled}},
		{"scheduled", []types.InstalmentStatus{types.InstalmentStatusScheduled}, 11, []types.InstalmentStatus{types.InstalmentStatusScheduled}},
		{"invoiced", []types.InstalmentStatus{types.InstalmentStatusInvoiced}, 0, nil},
		{
			"scheduled and cancelled",
			[]types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusCancelled},
			22,
			[]types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusCancelled},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			listed, err := s.svc.ListInstalments(ctx, created.ID, tc.statuses)
			s.Require().NoError(err)
			s.Len(listed.Items, tc.want)

			for _, i := range listed.Items {
				if tc.wantOnly != nil {
					s.Contains(tc.wantOnly, i.InstalmentStatus)
				}
			}
		})
	}
}

func (s *CommitmentServiceSuite) TestGetBillingTerms_ReadsWhenDisabledAndEmptyForPerPeriod() {
	ctx := s.GetContext()
	perPeriod, err := s.svc.Create(ctx, validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	setCommitmentsV2(s.T(), ctx, s.GetStores(), false)

	got, err := s.svc.GetBillingTerms(ctx, perPeriod.ID)
	s.Require().NoError(err)
	s.Empty(got.BillingTerms)
	s.Empty(got.Instalments)

	listed, err := s.svc.ListInstalments(ctx, perPeriod.ID, nil)
	s.Require().NoError(err)
	s.Empty(listed.Items)
}

// instalmentEventIDs returns the instalment ids carried by the published events with the given name, in order.
func (s *CommitmentServiceSuite) instalmentEventIDs(events []*types.WebhookEvent, name types.WebhookEventName) []string {
	var ids []string
	for _, event := range events {
		if event.EventName != name {
			continue
		}

		s.Equal(types.SystemEntityTypeBillingInstalment, event.EntityType)

		var payload webhookDto.InternalBillingInstalmentEvent
		s.Require().NoError(json.Unmarshal(event.Payload, &payload))
		s.Equal(event.EntityID, payload.InstalmentID)
		s.Equal(types.GetTenantID(s.GetContext()), payload.TenantID)
		ids = append(ids, payload.InstalmentID)
	}

	return ids
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_PublishesEvents() {
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	req := s.billingTermsRequest()
	req.IdempotencyKey = lo.ToPtr("cmt-terms-events")
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	events := publisher.Events()
	s.Require().Len(events, 13, "commitment.created, billing_terms.created and one event per instalment")
	s.Equal(types.WebhookEventCommitmentCreated, events[0].EventName)
	s.Equal(types.WebhookEventBillingTermsCreated, events[1].EventName)
	s.Equal(types.SystemEntityTypeCommitment, events[1].EntityType)
	s.Equal(created.ID, events[1].EntityID)

	var terms webhookDto.InternalBillingTermsEvent
	s.Require().NoError(json.Unmarshal(events[1].Payload, &terms))
	s.Equal(types.BillingTermOwnerCommitment, terms.OwnerType)
	s.Equal(created.ID, terms.OwnerID)
	s.Equal(types.GetTenantID(s.GetContext()), terms.TenantID)

	stored := s.storedInstalments()
	s.Require().Len(stored, 11)
	s.ElementsMatch(
		lo.Map(stored, func(i *billingterm.Instalment, _ int) string { return i.ID }),
		s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated),
	)

	_, err = s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Len(publisher.Events(), 13, "a replay of the same key publishes nothing")
}

func (s *CommitmentServiceSuite) TestCreate_PerPeriodCommitment_PublishesNoBillingTermEvents() {
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	events := publisher.Events()
	s.Require().Len(events, 1)
	s.Equal(types.WebhookEventCommitmentCreated, events[0].EventName)
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_PublishesEvents() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	before := s.storedInstalments()
	s.claimFirstInstalment("inv_x")

	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	resp, err := s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	events := publisher.Events()
	s.Require().Len(events, 23, "billing_terms.updated, 11 cancelled and 11 created")
	s.Equal(types.WebhookEventBillingTermsUpdated, events[0].EventName)
	s.Equal(created.ID, events[0].EntityID)

	cancelled := s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCancelled)
	s.ElementsMatch(lo.Map(before, func(i *billingterm.Instalment, _ int) string { return i.ID }), cancelled,
		"every replaced instalment is reported, invoiced ones included")

	createdIDs := s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated)
	s.ElementsMatch(lo.Map(resp.Instalments, func(i *billingterm.Instalment, _ int) string { return i.ID }), createdIDs)

	names := lo.Map(events, func(e *types.WebhookEvent, _ int) types.WebhookEventName { return e.EventName })
	s.Equal(types.WebhookEventBillingTermsUpdated, names[0])
	s.Equal(types.WebhookEventBillingInstalmentCancelled, names[1])
	s.Equal(types.WebhookEventBillingInstalmentCreated, names[22], "cancellations precede the new instalments")
}
