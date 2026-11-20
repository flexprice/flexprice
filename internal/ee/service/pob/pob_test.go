package pob

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestDerivePOB(t *testing.T) {
	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	mar31 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	jun30 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		contractStatus types.ContractStatus
		serviceEnd     *time.Time
		want           types.PerformanceObligationStatus
	}{
		{name: "draft contract keeps the POB draft", contractStatus: types.ContractStatusDraft, serviceEnd: &mar31, want: types.PerformanceObligationStatusDraft},
		{name: "cancelled contract cancels the POB", contractStatus: types.ContractStatusCancelled, want: types.PerformanceObligationStatusCancelled},
		{name: "ended contract satisfies the POB", contractStatus: types.ContractStatusEnded, serviceEnd: &jun30, want: types.PerformanceObligationStatusSatisfied},
		{name: "open service is active", contractStatus: types.ContractStatusActive, want: types.PerformanceObligationStatusActive},
		{name: "service end not yet passed is active", contractStatus: types.ContractStatusActive, serviceEnd: &jun30, want: types.PerformanceObligationStatusActive},
		{name: "service end passed is satisfied", contractStatus: types.ContractStatusActive, serviceEnd: &mar31, want: types.PerformanceObligationStatusSatisfied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := domainPOB.NewBuilder(nil).WithServiceEnd(tt.serviceEnd).WithPOBStatus(types.PerformanceObligationStatusDraft).Build()
			c := domainContract.NewBuilder(nil).WithContractStatus(tt.contractStatus).Build()

			got := derivePOB(existing, c, now)

			require.Equal(t, tt.want, got.POBStatus())
			require.Equal(t, tt.serviceEnd, got.ServiceEnd(), "service end is kept")
		})
	}
}

type POBServiceSuite struct {
	suite.Suite
	ctx        context.Context
	pobService PerformanceObligationService
	pobRepo    *testutil.InMemoryPOBStore
	contract   *domainContract.Contract
}

func TestPOBService(t *testing.T) {
	suite.Run(t, new(POBServiceSuite))
}

func (s *POBServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	contracts := testutil.NewInMemoryContractStore()
	s.pobRepo = testutil.NewInMemoryPOBStore(contracts)
	s.pobService = NewPerformanceObligationService(s.pobRepo, contracts, nil, nil, nil, nil)
	s.contract = domainContract.NewBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTRACT)).
		WithContractStatus(types.ContractStatusDraft).
		WithStatus(types.StatusPublished).
		Build()
}

func (s *POBServiceSuite) TestCreateForContract() {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	pobs, err := s.pobService.CreateForContract(s.ctx, s.contract, []dto.CreatePOBRequest{
		{Name: "Platform", ProductID: "prod_platform", RecognitionMethod: types.RecognitionMethodStraightLine, ServiceStart: start, ServiceEnd: &end, AllocatedAmount: decimal.NewFromInt(60000)},
		{Name: "API usage", ProductID: "prod_api", RecognitionMethod: types.RecognitionMethodAsConsumed, ServiceStart: start},
	})
	s.Require().NoError(err)
	s.Require().Len(pobs, 2)

	got, err := s.pobRepo.GetByID(s.ctx, pobs[0].ID())
	s.Require().NoError(err)
	s.Equal(s.contract.ID(), got.ContractID())
	s.Equal("Platform", got.Name())
	s.Equal("prod_platform", got.ProductID())
	s.Equal(types.RecognitionMethodStraightLine, got.RecognitionMethod())
	s.True(got.ServiceStart().Equal(start))
	s.True(got.ServiceEnd().Equal(end))
	s.Equal("60000", got.AllocatedAmount().String())
	s.Equal(types.PerformanceObligationStatusDraft, got.POBStatus(), "a draft contract's POBs are drafts")
	s.True(pobs[1].AllocatedAmount().IsZero())
	s.Nil(pobs[1].ServiceEnd())
}

func (s *POBServiceSuite) TestSyncForContractFollowsContract() {
	past := time.Now().UTC().Add(-time.Hour)
	pobs, err := s.pobService.CreateForContract(s.ctx, s.contract, []dto.CreatePOBRequest{
		{Name: "Platform", ProductID: "prod_platform", RecognitionMethod: types.RecognitionMethodStraightLine, ServiceStart: past.AddDate(0, -1, 0)},
		{Name: "Setup", ProductID: "prod_setup", RecognitionMethod: types.RecognitionMethodPointInTime, ServiceStart: past.AddDate(0, -1, 0), ServiceEnd: &past},
	})
	s.Require().NoError(err)

	active := domainContract.NewBuilder(s.contract).WithContractStatus(types.ContractStatusActive).Build()
	synced, err := s.pobService.SyncForContract(s.ctx, active)
	s.Require().NoError(err)
	s.Len(synced, 2)

	open, err := s.pobRepo.GetByID(s.ctx, pobs[0].ID())
	s.Require().NoError(err)
	s.Equal(types.PerformanceObligationStatusActive, open.POBStatus())
	ended, err := s.pobRepo.GetByID(s.ctx, pobs[1].ID())
	s.Require().NoError(err)
	s.Equal(types.PerformanceObligationStatusSatisfied, ended.POBStatus())
}

func TestEarnedByPOB(t *testing.T) {
	d := decimal.RequireFromString
	at := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }
	seats := domainPOB.NewBuilder(nil).WithID("pob_seats").WithRecognitionMethod(types.RecognitionMethodStraightLine).WithAmortizationMethod(types.AmortizationDaily).Build()
	setup := domainPOB.NewBuilder(nil).WithID("pob_setup").WithRecognitionMethod(types.RecognitionMethodPointInTime).WithAmortizationMethod(types.AmortizationDaily).Build()
	lineItemPOBs := map[string]*domainPOB.PerformanceObligation{"sli_seats": seats, "sli_setup": setup}

	line := func(id, sli, amount string, start, end *time.Time) *invoice.InvoiceLineItem {
		return &invoice.InvoiceLineItem{ID: id, SubscriptionLineItemID: lo.ToPtr(sli), Amount: d(amount), PeriodStart: start, PeriodEnd: end}
	}
	inv := func(id string, invoiceType types.InvoiceType, finalizedAt time.Time, lines ...*invoice.InvoiceLineItem) *invoice.Invoice {
		return &invoice.Invoice{ID: id, InvoiceType: invoiceType, Currency: "usd", FinalizedAt: lo.ToPtr(finalizedAt), LineItems: lines}
	}
	fact := func(sli, net string, day time.Time) *revenuefact.RevenueFact {
		return &revenuefact.RevenueFact{SubLineItemID: lo.ToPtr(sli), NetAmount: d(net), Day: day, DecompositionMode: types.Marginal}
	}

	april := inv("inv_apr", types.InvoiceTypeSubscription, at(4, 1),
		line("l_apr", "sli_seats", "90", lo.ToPtr(at(4, 1)), lo.ToPtr(at(5, 1))),
		line("l_setup", "sli_setup", "50", nil, nil),
		line("l_other", "sli_unknown", "999", lo.ToPtr(at(4, 1)), lo.ToPtr(at(5, 1))))
	upgrade := inv("inv_upgrade", types.InvoiceTypeOneOff, at(4, 21),
		line("l_credit", "sli_seats", "-30", lo.ToPtr(at(4, 21)), lo.ToPtr(at(5, 1))),
		line("l_charge", "sli_seats", "40", lo.ToPtr(at(4, 21)), lo.ToPtr(at(5, 1))))

	tests := []struct {
		name   string
		inputs *earningInputs
		asOf   time.Time
		want   map[string]string
	}{
		{
			name:   "invoice lines and proration lines add, each over its own period",
			inputs: &earningInputs{invoices: []*invoice.Invoice{april, upgrade}},
			asOf:   at(5, 1),
			want:   map[string]string{"pob_seats": "100", "pob_setup": "50"},
		},
		{
			name:   "a line without a period earns at finalization",
			inputs: &earningInputs{invoices: []*invoice.Invoice{april}},
			asOf:   at(4, 2),
			want:   map[string]string{"pob_seats": "3", "pob_setup": "50"},
		},
		{
			name: "un-invoiced days accrue from facts; invoiced days do not count twice",
			inputs: &earningInputs{
				invoices: []*invoice.Invoice{april, upgrade},
				facts:    []*revenuefact.RevenueFact{fact("sli_seats", "3", at(4, 30)), fact("sli_seats", "5", at(5, 2)), fact("sli_unknown", "7", at(5, 2))},
			},
			asOf: at(5, 3),
			want: map[string]string{"pob_seats": "105", "pob_setup": "50"},
		},
		{
			name: "a day covered only by a proration line still accrues its fact",
			inputs: &earningInputs{
				invoices: []*invoice.Invoice{upgrade},
				facts:    []*revenuefact.RevenueFact{fact("sli_seats", "4", at(4, 25))},
			},
			asOf: at(5, 1),
			want: map[string]string{"pob_seats": "14"},
		},
		{
			name: "a period_only fact spreads over its period",
			inputs: &earningInputs{facts: []*revenuefact.RevenueFact{{
				SubLineItemID: lo.ToPtr("sli_seats"), NetAmount: d("31"), DecompositionMode: types.PeriodOnly,
				Day: at(5, 1), PeriodStart: at(5, 1), PeriodEnd: at(5, 31),
			}}},
			asOf: at(5, 11),
			want: map[string]string{"pob_seats": "10"},
		},
		{
			name: "inclusive tax comes off through the invoice's line nets",
			inputs: &earningInputs{
				invoices: []*invoice.Invoice{inv("inv_tax", types.InvoiceTypeSubscription, at(4, 1), line("l_tax", "sli_seats", "110", lo.ToPtr(at(4, 1)), lo.ToPtr(at(5, 1))))},
				taxes:    []*taxapplied.TaxApplied{{EntityType: types.TaxRateEntityTypeInvoice, EntityID: "inv_tax", TaxAmount: d("10"), TaxBehavior: types.TaxBehaviorInclusive}},
			},
			asOf: at(5, 1),
			want: map[string]string{"pob_seats": "100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := earnedByPOB(lineItemPOBs, tt.inputs, tt.asOf, time.UTC)

			require.Len(t, got, len(tt.want))
			for id, want := range tt.want {
				require.True(t, d(want).Equal(got[id].Round(2)), "%s: want %s, got %s", id, want, got[id])
			}
		})
	}
}

// TestEarnedToDate runs the contract → POB → invoice/fact path through the in-memory stores.
func TestEarnedToDate(t *testing.T) {
	ctx := testutil.SetupContext()
	at := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }

	contracts := testutil.NewInMemoryContractStore()
	pobs := testutil.NewInMemoryPOBStore(contracts)
	subs := testutil.NewInMemorySubscriptionStore()
	invoices := testutil.NewInMemoryInvoiceStore()
	facts := testutil.NewInMemoryRevenueFactStore()
	service := NewPerformanceObligationService(pobs, contracts, subs, invoices, testutil.NewInMemoryTaxAppliedStore(), facts)

	sub := &subscription.Subscription{ID: "subs_1", Timezone: "UTC", BaseModel: types.GetDefaultBaseModel(ctx)}
	require.NoError(t, subs.Create(ctx, sub))
	lineItem := func(id string, priceType types.PriceType) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{ID: id, SubscriptionID: sub.ID, PriceID: "price_" + id, PriceType: priceType,
			BillingPeriod: types.BILLING_PERIOD_MONTHLY, StartDate: at(4, 1), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	sub.LineItems = []*subscription.SubscriptionLineItem{lineItem("sli_seats", types.PRICE_TYPE_FIXED), lineItem("sli_usage", types.PRICE_TYPE_USAGE)}

	c := domainContract.NewBuilder(nil).WithID("contract_1").WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.CreateWithAssociation(ctx, c, domainContract.NewAssociationBuilder(nil).
		WithID("contract_assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID(sub.ID).WithStatus(types.StatusPublished).Build()))
	// Earning still reads line items through associations; seed one POB per line item.
	for _, li := range sub.LineItems {
		require.NoError(t, pobs.CreateWithAssociation(ctx,
			domainPOB.NewBuilder(nil).WithID("pob_"+li.ID).WithContractID(c.ID()).WithName(li.ID).WithRecognitionMethod(lo.Ternary(li.PriceType == types.PRICE_TYPE_USAGE, types.RecognitionMethodAsConsumed, types.RecognitionMethodStraightLine)).
				WithAmortizationMethod(types.AmortizationPeriodEvenProrated).WithPOBStatus(types.PerformanceObligationStatusActive).WithStatus(types.StatusPublished).Build(),
			domainPOB.NewAssociationBuilder(nil).WithID("pob_assoc_"+li.ID).WithEntityType(types.RevRecEntityTypeSubscriptionLineItem).WithEntityID(li.ID).WithStatus(types.StatusPublished).Build()))
	}

	newInvoice := func(id string, status types.InvoiceStatus, lines ...*invoice.InvoiceLineItem) *invoice.Invoice {
		return &invoice.Invoice{ID: id, SubscriptionID: lo.ToPtr(sub.ID), InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: status,
			Currency: "usd", FinalizedAt: lo.ToPtr(at(4, 1)), LineItems: lines, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	newLine := func(id, sli, amount string) *invoice.InvoiceLineItem {
		return &invoice.InvoiceLineItem{ID: id, SubscriptionLineItemID: lo.ToPtr(sli), Amount: decimal.RequireFromString(amount),
			PeriodStart: lo.ToPtr(at(4, 1)), PeriodEnd: lo.ToPtr(at(5, 1)), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	require.NoError(t, invoices.CreateWithLineItems(ctx, newInvoice("inv_apr", types.InvoiceStatusFinalized, newLine("l_seats", "sli_seats", "90"), newLine("l_usage", "sli_usage", "50"))))
	require.NoError(t, invoices.CreateWithLineItems(ctx, newInvoice("inv_void", types.InvoiceStatusVoided, newLine("l_void", "sli_seats", "1000"))))
	require.NoError(t, facts.UpsertProvisional(ctx, []*revenuefact.RevenueFact{{
		SubscriptionID: sub.ID, SubLineItemID: lo.ToPtr("sli_seats"), PriceID: lo.ToPtr("price_sli_seats"),
		NetAmount: decimal.NewFromInt(5), Day: at(5, 2), PeriodStart: at(5, 1), PeriodEnd: at(5, 31), DecompositionMode: types.Marginal,
	}, {
		SubscriptionID: sub.ID, SubLineItemID: lo.ToPtr("sli_usage"), PriceID: lo.ToPtr("price_sli_usage"),
		NetAmount: decimal.NewFromInt(7), Day: at(5, 2), PeriodStart: at(5, 1), PeriodEnd: at(5, 31), DecompositionMode: types.Marginal,
	}}))

	byDate, err := service.EarnedToDate(ctx, c.ID(), []time.Time{at(4, 16), at(5, 3)})
	require.NoError(t, err)
	require.Len(t, byDate, 2, "one map per asOf")
	midApril, earned := byDate[0], byDate[1]

	seats, err := pobs.GetByLineItemID(ctx, "sli_seats")
	require.NoError(t, err)
	usage, err := pobs.GetByLineItemID(ctx, "sli_usage")
	require.NoError(t, err)
	require.Len(t, earned, 2, "every POB of the contract is present")
	require.Equal(t, "95", earned[seats.ID()].String(), "April invoice plus the May 2 accrual; the voided invoice never earns")
	require.Equal(t, "57", earned[usage.ID()].String(), "usage: April's invoiced 50 plus the May 2 accrual of 7")
	require.Equal(t, "45", midApril[seats.ID()].String(), "15 of April's 30 days; the May fact is not yet earned")
	require.Equal(t, "25", midApril[usage.ID()].String(), "usage earns daily: 15 of April's 30 days")
}

func TestReconcileAndRead(t *testing.T) {
	ctx := testutil.SetupContext()
	contracts := testutil.NewInMemoryContractStore()
	pobs := testutil.NewInMemoryPOBStore(contracts)
	service := NewPerformanceObligationService(pobs, contracts, nil, nil, nil, nil)

	c := domainContract.NewBuilder(nil).WithID("contract_1").WithCustomerID("cust_1").WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.Create(ctx, c))
	for id, method := range map[string]types.RecognitionMethod{"prod_platform": types.RecognitionMethodStraightLine, "prod_api": types.RecognitionMethodAsConsumed} {
		require.NoError(t, pobs.Create(ctx, domainPOB.NewBuilder(nil).WithID("pob_"+id).WithContractID(c.ID()).WithProductID(id).WithName(id).
			WithRecognitionMethod(method).WithPOBStatus(types.PerformanceObligationStatusDraft).WithStatus(types.StatusPublished).Build()))
	}

	// Reconcile activates the POBs of the active contract a missed hook left as drafts, and a second run changes nothing.
	require.NoError(t, service.Reconcile(ctx, c.ID()))
	require.NoError(t, service.Reconcile(ctx, c.ID()))
	all, err := service.GetPerformanceObligations(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all.Items, 2)

	got, err := service.GetPerformanceObligation(ctx, "pob_prod_platform")
	require.NoError(t, err)
	require.Equal(t, c.ID(), got.ContractID)
	require.Equal(t, "prod_platform", got.ProductID)
	require.Equal(t, types.PerformanceObligationStatusActive, got.POBStatus)

	filter := pobRepo.NewFilter()
	filter.CustomerIDs = []string{"cust_1"}
	filter.RecognitionMethods = []types.RecognitionMethod{types.RecognitionMethodAsConsumed}
	usageOnly, err := service.GetPerformanceObligations(ctx, filter)
	require.NoError(t, err)
	require.Len(t, usageOnly.Items, 1)
	require.Equal(t, types.RecognitionMethodAsConsumed, usageOnly.Items[0].RecognitionMethod)

	filter.CustomerIDs = []string{"cust_other"}
	none, err := service.GetPerformanceObligations(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, none.Items)
}
