package service

import (
	"slices"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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
