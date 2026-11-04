package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// upfrontPlusRemainder is the A3 shape: 300 on the start date, the rest in arrear instalments.
func upfrontPlusRemainder() []dto.BillingTermInput {
	return []dto.BillingTermInput{
		{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Value:     lo.ToPtr(dec("300")),
		},
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	}
}

// billingTermsRequest is a binding amount commitment of 100 a month for ten months from 1 Jan 2025, worth 1,000.
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

	s.Equal(types.BillingTermValueFixed, upfront.ValueType)
	s.True(lo.FromPtr(upfront.Value).Equal(dec("300")))
	s.Equal(types.BillingTermValueRemainder, remainder.ValueType)
	s.Nil(remainder.Value)

	instalments := s.storedInstalments()
	s.Require().Len(instalments, 11)
	s.Len(resp.Instalments, 11)

	total := decimal.Zero
	for _, i := range instalments {
		total = total.Add(i.Amount)
		s.Equal(resp.ID, i.OwnerID)
		s.Equal(types.BillingTermOwnerCommitment, i.OwnerType)
		s.Equal(lo.ToPtr(s.fx.sub.ID), i.SubscriptionID)
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
			Value:     lo.ToPtr(dec("1")),
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
	s.True(lo.FromPtr(upfront.Value).Equal(dec("300")))

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
		{"percentage term", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms[0].ValueType = types.BillingTermValuePercentage
		}},
		{"fixed term without a value", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms[0].Value = nil
		}},
		{"terms that miss the total", func(r *dto.CreateCommitmentRequest) { r.BillingTerms = r.BillingTerms[:1] }},
		{"two remainder terms", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms = append(r.BillingTerms, r.BillingTerms[1])
		}},
		{"an invoice_label template over 255 characters that renders shorter", func(r *dto.CreateCommitmentRequest) {
			r.BillingTerms[1].InvoiceLabel = lo.ToPtr(strings.Repeat("{n}", 86))
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

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, ist).UTC()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
	s.Require().NoError(err)

	sub.Timezone, sub.StartDate, sub.BillingAnchor = "Asia/Kolkata", start, start
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = start, time.Date(2025, 2, 1, 0, 0, 0, 0, ist).UTC()
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	line := *s.fx.lineAPI
	line.StartDate = start
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, &line))

	april := time.Date(2025, 4, 1, 0, 0, 0, 0, ist)
	req := s.billingTermsRequest()
	req.StartDate = start
	req.EndDate = lo.ToPtr(time.Date(2025, 11, 1, 0, 0, 0, 0, ist).UTC())
	req.BillingTerms = []dto.BillingTermInput{
		{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Value:     lo.ToPtr(dec("300")),
		},
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
			StartDate:      &april,
		},
	}

	utcApril := req
	utcApril.BillingTerms = slices.Clone(req.BillingTerms)
	utcApril.BillingTerms[1].StartDate = lo.ToPtr(time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC))
	_, err = s.svc.Create(ctx, utcApril)
	s.Require().Error(err, "1 Apr 00:00 UTC is no boundary of a Kolkata subscription")
	s.True(ierr.IsValidation(err), "got %v", err)

	resp, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	// 300 up front, then 700 over the seven months April to October.
	s.Require().Len(resp.Instalments, 8)
	first := resp.Instalments[1]
	s.True(first.ServicePeriodStart.Equal(april), "first step starts at %s", first.ServicePeriodStart)
	s.True(first.ServicePeriodEnd.Equal(time.Date(2025, 5, 1, 0, 0, 0, 0, ist)), "first step ends at %s", first.ServicePeriodEnd)
	s.True(first.BillOn.Equal(first.ServicePeriodEnd))
	s.True(first.Amount.Equal(dec("100")))
}

func (s *CommitmentServiceSuite) TestCreate_BillingTerms_StartDateMustBeABoundary() {
	req := s.billingTermsRequest()
	req.BillingTerms[1].StartDate = lo.ToPtr(req.StartDate.AddDate(0, 3, 14))

	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), "billing_terms[1].start_date")
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
		s.Equal(lo.ToPtr(sub.ID), i.SubscriptionID)
		s.Equal(s.customer.ID, i.CustomerID)
	}

	terms, err := s.GetStores().BillingTermRepo.List(s.GetContext(), types.BillingTermOwnerCommitment, owner.ID)
	s.Require().NoError(err)
	s.Len(terms, 2)
}

// halfUpfrontPlusRemainder replaces upfrontPlusRemainder with 500 fixed up front.
func halfUpfrontPlusRemainder() dto.ReplaceBillingTermsRequest {
	terms := upfrontPlusRemainder()
	terms[0].Value = lo.ToPtr(dec("500"))
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

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_KeepsInvoicedInstalments() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	oldTerms := s.storedTerms(created.ID)
	oldInstalments := s.storedInstalments()
	invoiced := s.claimFirstInstalment("inv_x")

	resp, err := s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	kept := s.instalmentByID(invoiced.ID)
	s.Equal(types.InstalmentStatusInvoiced, kept.InstalmentStatus, "an edit keeps the invoiced instalment")
	s.Equal("inv_x", lo.FromPtr(kept.InvoiceID))
	s.Equal("Instalment 1 of 11", kept.InvoiceLabel)
	for _, old := range oldInstalments[1:] {
		s.Equal(types.InstalmentStatusCancelled, s.instalmentByID(old.ID).InstalmentStatus, "instalment %s", old.ID)
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

	// The 300 invoiced leaves 700: 500 up front and ten instalments of 20.
	s.Require().Len(resp.Instalments, 12)
	s.Equal(invoiced.ID, resp.Instalments[0].ID, "the plan lists the invoiced instalment with the new ones")
	added := s.addedSince(oldInstalments)
	s.Require().Len(added, 11)
	s.True(sumInstalments(added).Equal(dec("700")), "new instalments add up to %s", sumInstalments(added))
	s.True(added[0].Amount.Equal(dec("500")))
	s.True(added[1].Amount.Equal(dec("20")))
	s.Equal("Instalment 2 of 12", added[0].InvoiceLabel, "new instalments are numbered after the invoiced one")
	s.Equal("Instalment 12 of 12", added[10].InvoiceLabel)
	s.True(s.liveTotal().Equal(dec("1000")), "the plan still adds up to the total")
}

// Invoiced instalments keep the numbers they were billed with, so a new instalment dated between two of them still
// takes the next number: it is billed after both.
func (s *CommitmentServiceSuite) TestReplaceBillingTerms_NumbersNewInstalmentsAfterTheInvoiced() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	rows := s.storedInstalments()
	slices.SortFunc(rows, func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) })
	_, err = s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_x", []string{rows[0].ID, rows[1].ID})
	s.Require().NoError(err)

	between := rows[0].BillOn.AddDate(0, 0, 14)
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, dto.ReplaceBillingTermsRequest{BillingTerms: []dto.BillingTermInput{{
		Kind:      types.BillingTermKindOnce,
		ValueType: types.BillingTermValueRemainder,
		BillOn:    &between,
	}}})
	s.Require().NoError(err)

	added := s.addedSince(rows)
	s.Require().Len(added, 1)
	s.Equal("Instalment 3 of 3", added[0].InvoiceLabel)
}

// An invoiced instalment whose label took no number, such as a milestone's, leaves no number behind: the edit's
// instalments count only the numbered ones, even once the edit archived the milestone's term.
func (s *CommitmentServiceSuite) TestReplaceBillingTerms_NumbersOnlyAfterNumberedInvoicedInstalments() {
	ctx := s.GetContext()
	req := s.billingTermsRequest()
	req.BillingTerms[0].InvoiceLabel = lo.ToPtr("Go-live milestone")
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	rows := s.storedInstalments()
	milestone, ok := lo.Find(rows, func(i *billingterm.Instalment) bool { return i.InvoiceLabel == "Go-live milestone" })
	s.Require().True(ok)
	_, err = s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_x", []string{milestone.ID})
	s.Require().NoError(err)

	edit := dto.ReplaceBillingTermsRequest{BillingTerms: upfrontPlusRemainder()[1:]}
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, edit)
	s.Require().NoError(err)

	byBillOn := func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) }
	first := s.addedSince(rows)
	slices.SortFunc(first, byBillOn)
	s.Require().NotEmpty(first)
	s.Equal(fmt.Sprintf("Instalment 1 of %d", len(first)), first[0].InvoiceLabel)

	// A second edit reads the milestone's archived term.
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, edit)
	s.Require().NoError(err)

	second := s.addedSince(append(rows, first...))
	slices.SortFunc(second, byBillOn)
	s.Require().NotEmpty(second)
	s.Equal(fmt.Sprintf("Instalment 1 of %d", len(second)), second[0].InvoiceLabel)
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_SizesThePlanAfterTheCancellation() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	rows := s.storedInstalments()
	racing := s.racingCommitmentService(func(ctx context.Context) {
		_, err := s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_race", []string{rows[0].ID})
		s.Require().NoError(err)
	})

	_, err = racing.ReplaceBillingTerms(ctx, created.ID, dto.ReplaceBillingTermsRequest{BillingTerms: upfrontPlusRemainder()[1:]})
	s.Require().NoError(err)

	s.Equal(types.InstalmentStatusInvoiced, s.instalmentByID(rows[0].ID).InstalmentStatus)
	s.True(s.liveTotal().Equal(dec("1000")), "an instalment claimed before the cancellation is left out of the new plan")
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_RejectsAFullyInvoicedCommitment() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	ids := lo.Map(s.storedInstalments(), func(i *billingterm.Instalment, _ int) string { return i.ID })
	_, err = s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_x", ids)
	s.Require().NoError(err)

	_, err = s.svc.PreviewBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.requireValidation(err, "the commitment is fully invoiced")

	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.requireValidation(err, "the commitment is fully invoiced")
}

// Invoiced instalments count toward the plan's limit, which keeps every numbered label within its template's length.
func (s *CommitmentServiceSuite) TestReplaceBillingTerms_RejectsAPlanOverTheLimit() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	c, err := s.GetStores().CommitmentRepo.Get(ctx, created.ID)
	s.Require().NoError(err)

	invoiced := make([]*billingterm.Instalment, 0, billingterm.MaxInstalments-10)
	for range billingterm.MaxInstalments - 10 {
		inst := billingterm.NewOwnerInstalment(ctx, c).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)).
			WithAmount(dec("1")).
			WithBillOn(c.StartDate).
			WithInvoiceLabel("Instalment").
			Build()
		inst.InstalmentStatus = types.InstalmentStatusInvoiced
		inst.InvoiceID = lo.ToPtr("inv_x")
		invoiced = append(invoiced, inst)
	}
	_, err = s.GetStores().BillingInstalmentRepo.CreateMany(ctx, invoiced)
	s.Require().NoError(err)

	// The 490 invoiced and the 11 instalments of the new terms make 501.
	_, err = s.svc.PreviewBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.requireValidation(err, "the plan holds 501, more than the 500 allowed")
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_CanReplaceTwice() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	req := dto.ReplaceBillingTermsRequest{BillingTerms: upfrontPlusRemainder()}
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, req)
	s.Require().NoError(err)

	second, err := s.svc.ReplaceBillingTerms(ctx, created.ID, req)
	s.Require().NoError(err)
	s.Len(second.Instalments, 11)
	s.Len(s.storedTerms(created.ID), 2)
}

// Term dates are taken as given, so one with a fraction of a second is rejected rather than rounded.
func (s *CommitmentServiceSuite) TestReplaceBillingTerms_RejectsFractionalSeconds() {
	ctx := s.GetContext()
	req := s.billingTermsRequest()
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	old := s.storedInstalments()
	edit := halfUpfrontPlusRemainder()
	edit.BillingTerms[0].BillOn = lo.ToPtr(req.StartDate.Add(500 * time.Millisecond))
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, edit)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), "whole_second")
	s.Empty(s.addedSince(old))
}

// A commitment that began three months ago and runs seven more, worth 1,000. An edit's undated terms start at the
// period holding now: 300 on its start, then 700 over the seven periods left, billed at each one's end.
func (s *CommitmentServiceSuite) TestReplaceBillingTerms_UndatedTermsStartAtTheCurrentPeriod() {
	ctx := s.GetContext()
	now := clock.Now().UTC()
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	req := s.billingTermsRequest()
	req.StartDate = current.AddDate(0, -3, 0)
	req.EndDate = lo.ToPtr(current.AddDate(0, 7, 0))
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	old := s.storedInstalments()
	s.True(old[0].BillOn.Equal(req.StartDate), "a new commitment's undated terms start at its start")

	edit := dto.ReplaceBillingTermsRequest{BillingTerms: upfrontPlusRemainder()}
	preview, err := s.svc.PreviewBillingTerms(ctx, created.ID, edit)
	s.Require().NoError(err)

	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, edit)
	s.Require().NoError(err)

	added := s.addedSince(old)
	s.Require().Len(added, 8)
	s.True(added[0].BillOn.Equal(current), "the once term bills at the current period's start, got %s", added[0].BillOn)
	s.True(added[0].Amount.Equal(dec("300")))
	for i, inst := range added[1:] {
		s.True(inst.BillOn.Equal(current.AddDate(0, i+1, 0)), "instalment %d bills on %s", i+1, inst.BillOn)
		s.True(inst.Amount.Equal(dec("100")))
	}

	s.True(s.liveTotal().Equal(dec("1000")))
	s.Equal(
		lo.Map(preview.Instalments, func(i *billingterm.Instalment, _ int) time.Time { return i.BillOn }),
		lo.Map(added, func(i *billingterm.Instalment, _ int) time.Time { return i.BillOn }),
		"the preview plans the same dates",
	)
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_PastItsEndUndatedTermsStartAtTheCommitmentStart() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)
	s.Require().True(created.EndDate.Before(clock.Now()))

	old := s.storedInstalments()
	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	added := s.addedSince(old)
	s.Require().NotEmpty(added)
	s.True(added[0].BillOn.Equal(created.StartDate))
}

func (s *CommitmentServiceSuite) TestReplaceBillingTerms_InvalidTermsChangeNothing() {
	ctx := s.GetContext()
	created, err := s.svc.Create(ctx, s.billingTermsRequest())
	s.Require().NoError(err)

	terms := s.storedTerms(created.ID)
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	for name, req := range map[string]dto.ReplaceBillingTermsRequest{
		"no terms":         {},
		"terms miss total": {BillingTerms: upfrontPlusRemainder()[:1]},
		"percentage term": {BillingTerms: []dto.BillingTermInput{
			{
				Kind:      types.BillingTermKindOnce,
				ValueType: types.BillingTermValuePercentage,
				Value:     lo.ToPtr(dec("30")),
			},
			upfrontPlusRemainder()[1],
		}},
	} {
		_, err := s.svc.ReplaceBillingTerms(ctx, created.ID, req)
		s.Require().Error(err, name)
		s.True(ierr.IsValidation(err), "%s: got %v", name, err)
	}

	// The in-memory store does not roll back, so only the terms show that nothing was saved.
	s.Equal(terms, s.storedTerms(created.ID))
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

	terms := s.storedTerms(created.ID)
	invoiced := s.claimFirstInstalment("inv_x")
	stored := s.storedInstalments()

	resp, err := s.svc.PreviewBillingTerms(ctx, created.ID, dto.ReplaceBillingTermsRequest{
		BillingTerms: []dto.BillingTermInput{
			{
				Kind:      types.BillingTermKindOnce,
				ValueType: types.BillingTermValueFixed,
				Value:     lo.ToPtr(dec("500")),
			},
			upfrontPlusRemainder()[1],
		},
	})
	s.Require().NoError(err)

	s.Require().Len(resp.Instalments, 12, "the invoiced instalment and eleven new ones")
	s.Equal(invoiced.ID, resp.Instalments[0].ID)
	s.True(sumInstalments(resp.Instalments[1:]).Equal(dec("700")))
	s.Require().Len(resp.BillingTerms, 2)

	upfront, ok := lo.Find(resp.BillingTerms, func(t *billingterm.Term) bool { return t.Kind == types.BillingTermKindOnce })
	s.Require().True(ok)
	s.True(lo.FromPtr(upfront.Value).Equal(dec("500")))

	s.Equal(terms, s.storedTerms(created.ID))
	s.Equal(stored, s.storedInstalments(), "no instalment was cancelled or added")
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
		{"cancelled", []types.InstalmentStatus{types.InstalmentStatusCancelled}, 10, []types.InstalmentStatus{types.InstalmentStatusCancelled}},
		{"scheduled", []types.InstalmentStatus{types.InstalmentStatusScheduled}, 11, []types.InstalmentStatus{types.InstalmentStatusScheduled}},
		{"invoiced", []types.InstalmentStatus{types.InstalmentStatusInvoiced}, 1, []types.InstalmentStatus{types.InstalmentStatusInvoiced}},
		{
			"scheduled and cancelled",
			[]types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusCancelled},
			21,
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

// A replayed create returns the commitment with its billing terms and instalments, like the original.
func (s *CommitmentServiceSuite) TestCreate_ReplayReturnsBillingTerms() {
	req := s.billingTermsRequest()
	req.IdempotencyKey = lo.ToPtr("cmt-terms-replay")
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	replayed, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Equal(created.ID, replayed.ID)
	s.Len(replayed.BillingTerms, len(created.BillingTerms))
	s.Len(replayed.Instalments, len(created.Instalments))
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

	_, err = s.svc.ReplaceBillingTerms(ctx, created.ID, halfUpfrontPlusRemainder())
	s.Require().NoError(err)

	events := publisher.Events()
	s.Require().Len(events, 22, "billing_terms.updated, 10 cancelled and 11 created")
	s.Equal(types.WebhookEventBillingTermsUpdated, events[0].EventName)
	s.Equal(created.ID, events[0].EntityID)

	cancelled := s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCancelled)
	s.ElementsMatch(lo.Map(before[1:], func(i *billingterm.Instalment, _ int) string { return i.ID }), cancelled,
		"only the scheduled instalments are cancelled")

	createdIDs := s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated)
	s.ElementsMatch(lo.Map(s.addedSince(before), func(i *billingterm.Instalment, _ int) string { return i.ID }), createdIDs)

	names := lo.Map(events, func(e *types.WebhookEvent, _ int) types.WebhookEventName { return e.EventName })
	s.Equal(types.WebhookEventBillingTermsUpdated, names[0])
	s.Equal(types.WebhookEventBillingInstalmentCancelled, names[1])
	s.Equal(types.WebhookEventBillingInstalmentCreated, names[21], "cancellations precede the new instalments")
}

// yearOfTermsRequest is a binding amount commitment of 100 a month for 2025, worth 1,200, collected through terms.
func (s *CommitmentServiceSuite) yearOfTermsRequest(terms ...dto.BillingTermInput) dto.CreateCommitmentRequest {
	req := s.billingTermsRequest()
	req.EndDate = lo.ToPtr(req.StartDate.AddDate(1, 0, 0))
	req.BillingTerms = terms
	return req
}

// settlementsOf returns the commitment's settlement instalments.
func (s *CommitmentServiceSuite) settlementsOf(commitmentID string) []*billingterm.Instalment {
	return lo.Filter(s.storedInstalments(), func(inst *billingterm.Instalment, _ int) bool {
		return inst.OwnerID == commitmentID && inst.Kind == types.InstalmentKindSettlement
	})
}

// $1,200 for 2025 has earned $400 by 1 May. Terminating cancels every scheduled instalment and settles only what was
// earned but never invoiced; invoiced instalments that bill more stay as they are.
func (s *CommitmentServiceSuite) TestTerminate_BillingTerms_Settlements() {
	jan := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := jan.AddDate(1, 0, 0)
	may := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	upfrontPlusArrears := []dto.BillingTermInput{
		{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Value:     lo.ToPtr(dec("300")),
		},
		{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueFixed,
			Value:          lo.ToPtr(dec("75")),
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	}

	tests := []struct {
		name           string
		terms          []dto.BillingTermInput
		invoiced       int // the first instalments in billing order, invoiced before the termination
		at             time.Time
		wantCancelled  int
		wantSettlement string // empty when nothing is settled
	}{
		{
			name:     "all upfront, billed",
			terms:    []dto.BillingTermInput{wholeValueUpfront(nil)},
			invoiced: 1,
			at:       may,
		},
		{
			name:          "$300 upfront and 4 of 12 $75 arrears billed",
			terms:         upfrontPlusArrears,
			invoiced:      5,
			at:            may,
			wantCancelled: 8,
		},
		{
			name:           "everything at term end",
			terms:          []dto.BillingTermInput{wholeValueUpfront(&end)},
			at:             may,
			wantCancelled:  1,
			wantSettlement: "400",
		},
		{
			name:           "everything at term end, terminated mid-April",
			terms:          []dto.BillingTermInput{wholeValueUpfront(&end)},
			at:             time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC),
			wantCancelled:  1,
			wantSettlement: "346.67",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			created, err := s.svc.Create(ctx, s.yearOfTermsRequest(tt.terms...))
			s.Require().NoError(err)

			billed := instalmentIDs(s.storedInstalments()[:tt.invoiced])
			_, err = s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_billed", billed)
			s.Require().NoError(err)

			publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
			publisher.Reset()
			resp, err := s.svc.Terminate(ctx, created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &tt.at})
			s.Require().NoError(err)
			s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)

			byStatus := lo.GroupBy(s.storedInstalments(), func(inst *billingterm.Instalment) types.InstalmentStatus {
				return inst.InstalmentStatus
			})
			s.Empty(byStatus[types.InstalmentStatusScheduled], "nothing stays scheduled")
			s.Len(byStatus[types.InstalmentStatusCancelled], tt.wantCancelled)
			kept := lo.Filter(byStatus[types.InstalmentStatusInvoiced], func(inst *billingterm.Instalment, _ int) bool {
				return inst.Kind == types.InstalmentKindInstalment
			})
			s.ElementsMatch(billed, instalmentIDs(kept), "invoiced instalments are kept")

			events := publisher.Events()
			s.ElementsMatch(instalmentIDs(byStatus[types.InstalmentStatusCancelled]),
				s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCancelled))

			settlements := s.settlementsOf(created.ID)
			if tt.wantSettlement == "" {
				s.Empty(settlements)
				s.Nil(resp.TerminationInvoiceID, "nothing is refunded or billed")
				s.Empty(s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated))
				return
			}

			s.Require().Len(settlements, 1)
			settlement := settlements[0]
			s.True(settlement.Amount.Equal(dec(tt.wantSettlement)), "settlement %s", settlement.Amount)
			s.Nil(settlement.BillingTermID)
			s.Equal(types.InstalmentBillTriggerDate, settlement.BillTrigger)
			s.True(settlement.BillOn.Equal(tt.at), "bill_on %s", settlement.BillOn)
			s.True(settlement.ServicePeriodStart.Equal(jan) && settlement.ServicePeriodEnd.Equal(tt.at), "the term up to termination")
			s.Equal(types.InstalmentDeliverySeparateInvoice, settlement.Delivery)
			s.Equal("Settlement", settlement.InvoiceLabel)
			s.Equal(types.InstalmentStatusInvoiced, settlement.InstalmentStatus)
			s.Require().NotNil(resp.TerminationInvoiceID)
			s.Equal(*resp.TerminationInvoiceID, lo.FromPtr(settlement.InvoiceID))

			inv, err := s.GetStores().InvoiceRepo.Get(ctx, *resp.TerminationInvoiceID)
			s.Require().NoError(err)
			s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
			s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
			s.Equal(string(types.InvoiceBillingReasonBillingTerms), inv.BillingReason)
			s.True(inv.AmountDue.Equal(dec(tt.wantSettlement)), "amount due %s", inv.AmountDue)
			s.True(inv.PeriodStart.Equal(jan) && inv.PeriodEnd.Equal(tt.at), "the invoice covers the settlement")

			lines := s.invoiceLines(inv.ID)
			s.Require().Len(lines, 1)
			s.requireLine(lines[0], types.CommitmentLineSettlement, tt.wantSettlement, created.ID)
			s.Equal(settlement.ID, lo.FromPtr(lines[0].BillingInstalmentID))
			s.Equal(created.InvoiceLabel+" - Settlement", lo.FromPtr(lines[0].DisplayName))

			s.Equal([]string{settlement.ID}, s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentCreated))
			s.Equal([]string{settlement.ID}, s.instalmentEventIDs(events, types.WebhookEventBillingInstalmentInvoiced))
		})
	}
}

// A settlement and a cancellation fee go on one invoice, spanning the months the settlement covers and the rest of
// the term the fee covers.
func (s *CommitmentServiceSuite) TestTerminate_BillingTerms_SettlementWithFee() {
	ctx := s.GetContext()
	jan := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := jan.AddDate(1, 0, 0)
	may := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	created, err := s.svc.Create(ctx, s.yearOfTermsRequest(wholeValueUpfront(&end)))
	s.Require().NoError(err)

	resp, err := s.svc.Terminate(ctx, created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &may,
		CancellationFee: fixedFee("250"),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp.TerminationInvoiceID)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, *resp.TerminationInvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal(string(types.InvoiceBillingReasonProration), inv.BillingReason)
	s.True(inv.AmountDue.Equal(dec("650")), "amount due %s", inv.AmountDue)
	s.True(inv.PeriodStart.Equal(jan) && inv.PeriodEnd.Equal(end), "period %v to %v", inv.PeriodStart, inv.PeriodEnd)

	lines := s.invoiceLines(inv.ID)
	s.Require().Len(lines, 2)
	byKind := lo.KeyBy(lines, func(line *invoice.InvoiceLineItem) string {
		return line.Metadata[types.MetadataKeyCommitmentLineKind]
	})

	settlementLine := byKind[string(types.CommitmentLineSettlement)]
	s.Require().NotNil(settlementLine)
	s.requireLine(settlementLine, types.CommitmentLineSettlement, "400", created.ID)
	s.True(settlementLine.PeriodStart.Equal(jan) && settlementLine.PeriodEnd.Equal(may))

	feeLine := byKind[string(types.CommitmentLineCancellationFee)]
	s.Require().NotNil(feeLine)
	s.requireLine(feeLine, types.CommitmentLineCancellationFee, "250", created.ID)
	s.True(feeLine.PeriodStart.Equal(may) && feeLine.PeriodEnd.Equal(end))

	settlements := s.settlementsOf(created.ID)
	s.Require().Len(settlements, 1)
	s.Equal(lo.FromPtr(settlementLine.BillingInstalmentID), settlements[0].ID)
	s.Equal(types.InstalmentStatusInvoiced, settlements[0].InstalmentStatus)
	s.Equal(inv.ID, lo.FromPtr(settlements[0].InvoiceID))
}

// The fee's bounds hold for the fee only: once the subscription has moved on to June, a fee for 1 May is rejected,
// while the settlement alone is billed.
func (s *CommitmentServiceSuite) TestTerminate_BillingTerms_FeeBoundsLeaveTheSettlementAlone() {
	may, jun := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		fee     *dto.CancellationFeeInput
		wantErr bool
	}{
		{name: "with a fee", fee: fixedFee("250"), wantErr: true},
		{name: "without a fee", fee: nil},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			created, err := s.svc.Create(ctx, s.yearOfTermsRequest(wholeValueUpfront(&end)))
			s.Require().NoError(err)

			sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
			s.Require().NoError(err)
			sub.CurrentPeriodStart, sub.CurrentPeriodEnd = jun, jun.AddDate(0, 1, 0)
			s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

			resp, err := s.svc.Terminate(ctx, created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &may, CancellationFee: tt.fee})
			if tt.wantErr {
				s.requireValidation(err, "current billing period")
				return
			}

			s.Require().NoError(err)
			s.Require().NotNil(resp.TerminationInvoiceID)
			inv, err := s.GetStores().InvoiceRepo.Get(ctx, *resp.TerminationInvoiceID)
			s.Require().NoError(err)
			s.True(inv.AmountDue.Equal(dec("400")), "amount due %s", inv.AmountDue)
		})
	}
}

// separateInvoiceCommitment commits 100 a month for two years to a new subscription and bills half of its 2,400
// upfront and half on a milestone nobody has triggered, each on its own invoice.
func (s *CommitmentE2ESuite) separateInvoiceCommitment(name string) (*dto.SubscriptionResponse, *dto.CommitmentResponse) {
	tokens := s.planWith(name, types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)

	half := dto.BillingTermInput{
		Kind:      types.BillingTermKindOnce,
		ValueType: types.BillingTermValueFixed,
		Value:     lo.ToPtr(dec("1200")),
	}
	milestone := half
	milestone.BillTrigger = lo.ToPtr(types.InstalmentBillTriggerManual)

	req := termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(2, 0, 0), half, milestone)
	req.InstalmentInvoicing = lo.ToPtr(types.InstalmentDeliverySeparateInvoice)
	committed := s.commit(sub.ID, req)
	s.Require().Len(committed.Instalments, 2)
	return sub, committed
}

func (s *CommitmentE2ESuite) setSubscriptionStatus(id string, status types.SubscriptionStatus) {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)

	sub.SubscriptionStatus = status
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
}

func (s *CommitmentE2ESuite) instalmentStatus(id string) *billingterm.Instalment {
	inst, err := s.commitments.GetInstalment(s.GetContext(), id)
	s.Require().NoError(err)
	return inst
}

// An environment whose commitments_v2 setting cannot be read fails the run, and the other environments are still billed.
func (s *CommitmentE2ESuite) TestBillDueInstalments_ReportsAnUnreadableSetting() {
	sub, _ := s.separateInvoiceCommitment("unreadable")
	other := types.SetEnvironmentID(s.GetContext(), "env_unreadable")
	s.Require().NoError(s.GetStores().SettingsRepo.Create(other, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyCommitmentsV2,
		Value:         map[string]interface{}{"enabled": "yes"},
		EnvironmentID: "env_unreadable",
		BaseModel:     types.GetDefaultBaseModel(other),
	}))

	err := s.commitments.BillDueInstalments(s.GetContext())
	s.Require().ErrorContains(err, "env_unreadable")
	s.Len(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms), 1)
}

// B1: the upfront instalment is due, so one run bills it on a finalized one-off invoice and claims it; the milestone
// waits for its trigger and a second run issues nothing.
func (s *CommitmentE2ESuite) TestBillDueInstalments_IssuesOneOffInvoices() {
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	sub, committed := s.separateInvoiceCommitment("issue")
	upfront, milestone := committed.Instalments[0], committed.Instalments[1]

	publisher.Reset()
	s.Require().NoError(s.commitments.BillDueInstalments(context.Background()), "a run needs no tenant on its context")

	invoices := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms)
	s.Require().Len(invoices, 1)
	inv := invoices[0]
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.Equal(sub.CustomerID, inv.CustomerID)
	s.True(inv.AmountDue.Equal(dec("1200")), "amount due %s", inv.AmountDue)
	s.True(inv.PeriodStart.Equal(upfront.ServicePeriodStart) && inv.PeriodEnd.Equal(upfront.ServicePeriodEnd))

	lines := s.storedInstalmentLines(inv.ID)
	s.Require().Len(lines, 1)
	s.Equal(upfront.ID, lo.FromPtr(lines[0].BillingInstalmentID))
	s.Equal(committed.ID, lo.FromPtr(lines[0].CommitmentID))
	s.Equal(string(types.InvoiceLineItemEntityTypeCommitment), lo.FromPtr(lines[0].EntityType))
	s.Equal("Platform - Instalment 1 of 2", lo.FromPtr(lines[0].DisplayName))

	billed := s.instalmentStatus(upfront.ID)
	s.Equal(types.InstalmentStatusInvoiced, billed.InstalmentStatus)
	s.Equal(inv.ID, lo.FromPtr(billed.InvoiceID))
	s.Equal(types.InstalmentStatusScheduled, s.instalmentStatus(milestone.ID).InstalmentStatus, "an untriggered milestone is not due")
	s.Equal([]string{upfront.ID}, s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))

	publisher.Reset()
	s.Require().NoError(s.commitments.BillDueInstalments(s.GetContext()))
	s.Len(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms), 1, "a billed instalment is not billed again")
	s.Empty(s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))
}

// Only an active subscription is billed: the instalments of a draft, incomplete, trialing or paused one stay
// scheduled and bill on the first run after it is active again.
func (s *CommitmentE2ESuite) TestBillDueInstalments_SkipsNonActiveSubscriptions() {
	waiting := map[types.SubscriptionStatus]*dto.SubscriptionResponse{}
	for _, status := range []types.SubscriptionStatus{
		types.SubscriptionStatusDraft,
		types.SubscriptionStatusIncomplete,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	} {
		sub, _ := s.separateInvoiceCommitment(string(status))
		s.setSubscriptionStatus(sub.ID, status)
		waiting[status] = sub
	}

	s.Require().NoError(s.commitments.BillDueInstalments(s.GetContext()))
	for status, sub := range waiting {
		s.Empty(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms), "a %s subscription is not billed", status)
	}

	for _, sub := range waiting {
		s.setSubscriptionStatus(sub.ID, types.SubscriptionStatusActive)
	}

	s.Require().NoError(s.commitments.BillDueInstalments(s.GetContext()))
	for status, sub := range waiting {
		s.Len(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms), 1, "a resumed %s subscription is billed", status)
	}
}

// Voiding the invoice returns the instalment to scheduled with a new updated_at, so the next run bills it on a new
// invoice under a new idempotency key.
func (s *CommitmentE2ESuite) TestBillDueInstalments_RebillsAfterVoid() {
	ctx := s.GetContext()
	sub, committed := s.separateInvoiceCommitment("void")
	upfront := committed.Instalments[0]

	s.Require().NoError(s.commitments.BillDueInstalments(ctx))
	first := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms)
	s.Require().Len(first, 1)

	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	_, err := invoices.VoidInvoice(ctx, first[0].ID, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)
	s.Equal(types.InstalmentStatusScheduled, s.instalmentStatus(upfront.ID).InstalmentStatus)

	s.Require().NoError(s.commitments.BillDueInstalments(ctx))
	second := s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms)
	s.Require().Len(second, 1, "the voided invoice no longer counts")
	s.NotEqual(first[0].ID, second[0].ID)
	s.NotEqual(lo.FromPtr(first[0].IdempotencyKey), lo.FromPtr(second[0].IdempotencyKey))
	s.Contains(lo.FromPtr(second[0].IdempotencyKey), "billing_instalment_"+upfront.ID+"_")

	rebilled := s.instalmentStatus(upfront.ID)
	s.Equal(types.InstalmentStatusInvoiced, rebilled.InstalmentStatus)
	s.Equal(second[0].ID, lo.FromPtr(rebilled.InvoiceID))
}

func (s *CommitmentE2ESuite) TestBillDueInstalments_IgnoresDisabledEnvironments() {
	ctx := s.GetContext()
	sub, committed := s.separateInvoiceCommitment("disabled")
	setCommitmentsV2(s.T(), ctx, s.GetStores(), false)

	s.Require().NoError(s.commitments.BillDueInstalments(ctx))
	s.Empty(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms))
	s.Equal(types.InstalmentStatusScheduled, s.instalmentStatus(committed.Instalments[0].ID).InstalmentStatus)

	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
	s.Require().NoError(s.commitments.BillDueInstalments(ctx))
	s.Len(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms), 1)
}

// unreadableCommitmentRepo fails every read of one commitment.
type unreadableCommitmentRepo struct {
	commitment.Repository

	id string
}

func (r *unreadableCommitmentRepo) Get(ctx context.Context, id string) (*commitment.Commitment, error) {
	if id == r.id {
		return nil, ierr.NewError("commitment could not be read").Mark(ierr.ErrDatabase)
	}

	return r.Repository.Get(ctx, id)
}

// An instalment that cannot be billed is reported once the run finishes, and does not stop the others.
func (s *CommitmentE2ESuite) TestBillDueInstalments_ContinuesPastAFailure() {
	ctx := s.GetContext()
	brokenSub, broken := s.separateInvoiceCommitment("broken")
	healthySub, healthy := s.separateInvoiceCommitment("healthy")

	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.CommitmentRepo = &unreadableCommitmentRepo{Repository: params.CommitmentRepo, id: broken.ID}

	err := NewCommitmentService(params).BillDueInstalments(ctx)
	s.Require().Error(err)
	s.Contains(err.Error(), broken.Instalments[0].ID)

	s.Empty(s.invoicesWithReason(brokenSub.ID, types.InvoiceBillingReasonBillingTerms))
	s.Equal(types.InstalmentStatusScheduled, s.instalmentStatus(broken.Instalments[0].ID).InstalmentStatus)
	s.Len(s.invoicesWithReason(healthySub.ID, types.InvoiceBillingReasonBillingTerms), 1)
	s.Equal(types.InstalmentStatusInvoiced, s.instalmentStatus(healthy.Instalments[0].ID).InstalmentStatus)
}

// An instalment whose subscription or commitment is gone is skipped without failing the run, and stays scheduled.
func (s *CommitmentE2ESuite) TestBillDueInstalments_SkipsMissingSubscriptionOrCommitment() {
	ctx := s.GetContext()
	goneSub, ofGoneSub := s.separateInvoiceCommitment("gone_subscription")
	archivedSub, archived := s.separateInvoiceCommitment("archived_commitment")
	s.Require().NoError(s.GetStores().SubscriptionRepo.(*testutil.InMemorySubscriptionStore).InMemoryStore.Delete(ctx, goneSub.ID))
	s.Require().NoError(s.GetStores().CommitmentRepo.Delete(ctx, archived.ID))

	s.Require().NoError(s.commitments.BillDueInstalments(ctx))

	for _, sub := range []*dto.SubscriptionResponse{goneSub, archivedSub} {
		s.Empty(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms))
	}
	for _, committed := range []*dto.CommitmentResponse{ofGoneSub, archived} {
		s.Equal(types.InstalmentStatusScheduled, s.instalmentStatus(committed.Instalments[0].ID).InstalmentStatus)
	}
}

// settlementOf returns the commitment's only settlement instalment.
func (s *CommitmentE2ESuite) settlementOf(commitmentID string) *billingterm.Instalment {
	listed, err := s.commitments.ListInstalments(s.GetContext(), commitmentID, nil)
	s.Require().NoError(err)

	settlements := lo.Filter(listed.Items, func(inst *billingterm.Instalment, _ int) bool {
		return inst.Kind == types.InstalmentKindSettlement
	})
	s.Require().Len(settlements, 1)
	return settlements[0]
}

// Cancelled at once on 11 Jan with the default skip policy, the subscription gets no final invoice, but the 10 days
// the commitment ran are settled on their own invoice. The voided January draft only carried scheduled instalments,
// which the termination cancelled, so voiding it reschedules none.
func (s *CommitmentE2ESuite) TestCancelSubscriptionImmediately_SkipPolicyBillsSettlement() {
	ctx := s.GetContext()
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	draft, commitmentID := s.januaryDraft(invoices, "cancel_now")
	carried := instalmentIDs(s.billedInstalments(draft.ID))

	publisher.Reset()
	cancelAt := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	_, err := s.subscriptions.CancelSubscription(ctx, lo.FromPtr(draft.SubscriptionID), &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
		CancelAt:         &cancelAt,
	})
	s.Require().NoError(err)

	voided, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusVoided, voided.InvoiceStatus)
	for _, id := range carried {
		s.Equal(types.InstalmentStatusCancelled, s.instalmentStatus(id).InstalmentStatus)
	}
	s.Empty(s.instalmentEvents(types.WebhookEventBillingInstalmentUpdated), "the void reschedules nothing")

	// $100 a month for 10 of 31 days.
	settlement := s.settlementOf(commitmentID)
	s.True(settlement.Amount.Equal(dec("32.26")), "settlement %s", settlement.Amount)
	s.True(settlement.ServicePeriodStart.Equal(s.jan) && settlement.ServicePeriodEnd.Equal(cancelAt))
	s.Equal(types.InstalmentStatusInvoiced, settlement.InstalmentStatus)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, lo.FromPtr(settlement.InvoiceID))
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.True(inv.AmountDue.Equal(dec("32.26")), "amount due %s", inv.AmountDue)
	s.Equal([]string{settlement.ID}, s.instalmentEvents(types.WebhookEventBillingInstalmentInvoiced))
}

// Cancelled at the end of January and rolled over by processSubscriptionPeriod, which finalizes the January invoice
// before terminating: that invoice bills the $30 due on 1 Jan, and the settlement only the other $70 January earned.
func (s *CommitmentE2ESuite) TestCancelSubscriptionAtPeriodEnd_KeepsBillingUntilEnd() {
	ctx := s.GetContext()
	feb, end := s.jan.AddDate(0, 1, 0), s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("cancel_at_end", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, end,
		dto.BillingTermInput{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Value:     lo.ToPtr(dec("30")),
		},
		dto.BillingTermInput{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueRemainder,
			BillOn:    &end,
		},
	))
	s.Require().Len(committed.Instalments, 2)
	upfront, rest := committed.Instalments[0], committed.Instalments[1]

	_, err := s.subscriptions.CancelSubscription(ctx, created.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeEndOfPeriod,
	})
	s.Require().NoError(err)

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.subscriptions.(*subscriptionService).processSubscriptionPeriod(ctx, sub, feb.Add(time.Hour)))

	billed := s.instalmentStatus(upfront.ID)
	s.Equal(types.InstalmentStatusInvoiced, billed.InstalmentStatus)
	january, err := s.GetStores().InvoiceRepo.Get(ctx, lo.FromPtr(billed.InvoiceID))
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeSubscription, january.InvoiceType)
	s.True(january.PeriodStart.Equal(s.jan) && january.PeriodEnd.Equal(feb), "the final period invoice bills the upfront")

	s.Equal(types.InstalmentStatusCancelled, s.instalmentStatus(rest.ID).InstalmentStatus)

	settlement := s.settlementOf(committed.ID)
	s.True(settlement.Amount.Equal(dec("70")), "settlement %s", settlement.Amount)
	s.True(settlement.ServicePeriodStart.Equal(s.jan) && settlement.ServicePeriodEnd.Equal(feb))
	s.Equal(types.InstalmentStatusInvoiced, settlement.InstalmentStatus)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, lo.FromPtr(settlement.InvoiceID))
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.True(inv.AmountDue.Equal(dec("70")), "amount due %s", inv.AmountDue)

	terminated, err := s.commitments.Get(ctx, committed.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, terminated.CommitmentStatus)
	s.True(terminated.TerminatedAt.Equal(feb), "terminated at %s", terminated.TerminatedAt)
}

// Cancelled at once on 11 Jan with a final invoice and no open draft, the subscription's cancel invoice for 1 to 11 Jan
// and the settlement for the same days are two invoices: the settlement is not a proration the cancel invoice could
// mistake for its own.
func (s *CommitmentE2ESuite) TestCancelSubscriptionImmediately_GenerateInvoicePolicyBillsSettlementApart() {
	ctx := s.GetContext()
	end := s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("cancel_with_invoice", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, end, wholeValueUpfront(&end)))
	s.record(s.tokens, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), "1000")

	cancelAt := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	_, err := s.subscriptions.CancelSubscription(ctx, created.ID, &dto.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
		CancelAt:                       &cancelAt,
	})
	s.Require().NoError(err)

	settlement := s.settlementOf(committed.ID)
	s.True(settlement.Amount.Equal(dec("32.26")), "settlement %s", settlement.Amount)
	s.Equal(types.InstalmentStatusInvoiced, settlement.InstalmentStatus)

	cancelInvoices := s.invoicesWithReason(created.ID, types.InvoiceBillingReasonProration)
	s.Require().Len(cancelInvoices, 1)
	s.Equal(types.InvoiceTypeSubscription, cancelInvoices[0].InvoiceType)
	s.NotEqual(lo.FromPtr(settlement.InvoiceID), cancelInvoices[0].ID)
	s.Empty(s.storedInstalmentLines(cancelInvoices[0].ID), "the cancel invoice carries no instalment")
}

// A commitment that ended on 21 Jan still has its instalment due that day scheduled for the January invoice when the
// subscription is cancelled at once on 25 Jan. No January invoice follows, so a settlement bills it.
func (s *CommitmentE2ESuite) TestCancelSubscriptionImmediately_SettlesAnEndedCommitment() {
	ctx := s.GetContext()
	end := time.Date(2026, 1, 21, 0, 0, 0, 0, time.UTC)
	tokens := s.planWith("cancel_after_end", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, end, wholeValueUpfront(&end)))
	s.Require().Len(committed.Instalments, 1)
	due := committed.Instalments[0]

	cancelAt := time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)
	_, err := s.subscriptions.CancelSubscription(ctx, created.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
		CancelAt:         &cancelAt,
	})
	s.Require().NoError(err)

	s.Equal(types.InstalmentStatusCancelled, s.instalmentStatus(due.ID).InstalmentStatus)

	settlement := s.settlementOf(committed.ID)
	s.True(settlement.Amount.Equal(due.Amount), "settlement %s, due %s", settlement.Amount, due.Amount)
	s.True(settlement.ServicePeriodStart.Equal(s.jan) && settlement.ServicePeriodEnd.Equal(end))
	s.Equal(types.InstalmentStatusInvoiced, settlement.InstalmentStatus)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, lo.FromPtr(settlement.InvoiceID))
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.True(inv.AmountDue.Equal(due.Amount), "amount due %s", inv.AmountDue)

	ended, err := s.commitments.Get(ctx, committed.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, ended.CommitmentStatus, "an ended commitment is not terminated")
}

// Voiding a settlement's invoice puts the settlement back on schedule, and the next run bills it again though the
// subscription is cancelled.
func (s *CommitmentE2ESuite) TestBillDueInstalments_RebillsAVoidedSettlement() {
	ctx := s.GetContext()
	end := s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("rebill_settlement", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, end, wholeValueUpfront(&end)))

	cancelAt := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	_, err := s.subscriptions.CancelSubscription(ctx, created.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
		CancelAt:         &cancelAt,
	})
	s.Require().NoError(err)

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	s.Require().Equal(types.SubscriptionStatusCancelled, sub.SubscriptionStatus)

	settlement := s.settlementOf(committed.ID)
	voided := lo.FromPtr(settlement.InvoiceID)
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	_, err = invoices.VoidInvoice(ctx, voided, dto.InvoiceVoidRequest{})
	s.Require().NoError(err)
	s.Equal(types.InstalmentStatusScheduled, s.instalmentStatus(settlement.ID).InstalmentStatus)

	s.Require().NoError(s.commitments.BillDueInstalments(ctx))

	rebilled := s.instalmentStatus(settlement.ID)
	s.Equal(types.InstalmentStatusInvoiced, rebilled.InstalmentStatus)
	s.NotEqual(voided, lo.FromPtr(rebilled.InvoiceID))

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, lo.FromPtr(rebilled.InvoiceID))
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.Equal(string(types.InvoiceBillingReasonBillingTerms), inv.BillingReason)
	s.True(inv.AmountDue.Equal(dec("32.26")), "amount due %s", inv.AmountDue)
}

// A cancellation at period end fires before the period's draft is finalized, so the January draft still carries the
// arrear instalment due at its end when the commitment terminates. Termination cancels it, and the settlement bills
// what January earned less what finalized invoices billed. Recomputing the draft drops the cancelled line, so the
// draft finalizes without billing it twice.
func (s *CommitmentE2ESuite) TestCancelSubscriptionAtPeriodEnd_SettlesTheOpenDraftsInstalments() {
	ctx := s.GetContext()
	feb, end := s.jan.AddDate(0, 1, 0), s.jan.AddDate(1, 0, 0)
	invoices := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite))
	tokens := s.planWith("cancel_open_draft", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	created := s.subscribe(tokens, s.jan)
	committed := s.commit(created.ID, termsRequest("Platform", tokens.ID, "100", s.jan, end,
		dto.BillingTermInput{
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
			Value:     lo.ToPtr(dec("30")),
		},
		dto.BillingTermInput{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueRemainder,
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	))
	upfront, arrear := committed.Instalments[0], committed.Instalments[1]
	s.Require().True(arrear.Amount.Equal(dec("97.5")) && arrear.BillOn.Equal(feb), "%s on %s", arrear.Amount, arrear.BillOn)

	// The upfront was billed when January opened.
	_, err := s.GetStores().BillingInstalmentRepo.Claim(ctx, "inv_opening", []string{upfront.ID})
	s.Require().NoError(err)
	s.record(s.tokens, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "1500")

	_, err = s.subscriptions.CancelSubscription(ctx, created.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeEndOfPeriod,
	})
	s.Require().NoError(err)

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, created.ID)
	s.Require().NoError(err)
	draft, _, err := invoices.GetOrComputeCurrentPeriodDraft(ctx, sub)
	s.Require().NoError(err)
	s.Equal([]string{arrear.ID}, instalmentIDs(s.billedInstalments(draft.ID)))

	// The cancellation check marks the subscription cancelled, then terminates its resources at cancel_at.
	s.Require().True(lo.FromPtr(sub.CancelAt).Equal(feb), "cancel_at %v", sub.CancelAt)
	s.setSubscriptionStatus(created.ID, types.SubscriptionStatusCancelled)
	s.Require().NoError(s.subscriptions.TerminateSubscriptionResources(ctx, dto.TerminateSubscriptionResourcesRequest{
		SubscriptionID: created.ID,
		EffectiveDate:  *sub.CancelAt,
	}))

	s.Equal(types.InstalmentStatusCancelled, s.instalmentStatus(arrear.ID).InstalmentStatus)
	settlement := s.settlementOf(committed.ID)
	s.True(settlement.Amount.Equal(dec("70")), "January earned 100, 30 of it already invoiced; got %s", settlement.Amount)
	s.Equal(types.InstalmentStatusInvoiced, settlement.InstalmentStatus)

	_, _, err = invoices.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)
	s.Empty(s.storedInstalmentLines(draft.ID), "the recompute drops the cancelled instalment")
	s.Require().NoError(invoices.FinalizeInvoice(ctx, draft.ID, dto.FinalizeInvoiceRequest{}))

	finalized, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)
	s.True(finalized.AmountDue.Equal(dec("50")), "only the overage past the $100 floor, got %s", finalized.AmountDue)
}
