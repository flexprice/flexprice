package pob

import (
	"context"
	"testing"
	"time"

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestDeriveRecognitionMethod(t *testing.T) {
	tests := []struct {
		name          string
		priceType     types.PriceType
		billingPeriod types.BillingPeriod
		want          types.RecognitionMethod
	}{
		{"fixed monthly", types.PRICE_TYPE_FIXED, types.BILLING_PERIOD_MONTHLY, types.RecognitionMethodStraightLine},
		{"fixed annual", types.PRICE_TYPE_FIXED, types.BILLING_PERIOD_ANNUAL, types.RecognitionMethodStraightLine},
		{"fixed one-time", types.PRICE_TYPE_FIXED, types.BILLING_PERIOD_ONETIME, types.RecognitionMethodPointInTime},
		{"usage monthly", types.PRICE_TYPE_USAGE, types.BILLING_PERIOD_MONTHLY, types.RecognitionMethodAsConsumed},
		{"usage one-time", types.PRICE_TYPE_USAGE, types.BILLING_PERIOD_ONETIME, types.RecognitionMethodAsConsumed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			li := &subscription.SubscriptionLineItem{PriceType: tt.priceType, BillingPeriod: tt.billingPeriod}
			assert.Equal(t, tt.want, deriveRecognitionMethod(li))
		})
	}
}

func TestDerivePOB(t *testing.T) {
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	mar31 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	jun30 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	may10 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		method         types.RecognitionMethod
		contractStatus types.ContractStatus
		contractEnded  *time.Time
		lineItemEnds   []time.Time
		wantStatus     types.PerformanceObligationStatus
		wantServiceEnd *time.Time
	}{
		{name: "draft contract keeps the POB draft", method: types.RecognitionMethodStraightLine, contractStatus: types.ContractStatusDraft, lineItemEnds: []time.Time{{}}, wantStatus: types.PerformanceObligationStatusDraft},
		{name: "cancelled contract cancels the POB", method: types.RecognitionMethodStraightLine, contractStatus: types.ContractStatusCancelled, lineItemEnds: []time.Time{{}}, wantStatus: types.PerformanceObligationStatusCancelled},
		{name: "active contract with an open line item is active", method: types.RecognitionMethodStraightLine, contractStatus: types.ContractStatusActive, lineItemEnds: []time.Time{mar31, {}}, wantStatus: types.PerformanceObligationStatusActive},
		{name: "service end is the latest line item end, satisfied once passed", method: types.RecognitionMethodStraightLine, contractStatus: types.ContractStatusActive, lineItemEnds: []time.Time{start, mar31}, wantStatus: types.PerformanceObligationStatusSatisfied, wantServiceEnd: &mar31},
		{name: "service end not yet passed stays active", method: types.RecognitionMethodStraightLine, contractStatus: types.ContractStatusActive, lineItemEnds: []time.Time{mar31, jun30}, wantStatus: types.PerformanceObligationStatusActive, wantServiceEnd: &jun30},
		{name: "open line item of an ended contract ends with it", method: types.RecognitionMethodStraightLine, contractStatus: types.ContractStatusEnded, contractEnded: &may10, lineItemEnds: []time.Time{{}}, wantStatus: types.PerformanceObligationStatusActive, wantServiceEnd: &may10},
		{name: "point_in_time is delivered at its start", method: types.RecognitionMethodPointInTime, contractStatus: types.ContractStatusActive, lineItemEnds: []time.Time{{}}, wantStatus: types.PerformanceObligationStatusSatisfied, wantServiceEnd: &start},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := domainPOB.NewBuilder(nil).
				WithRecognitionMethod(tt.method).
				WithPOBStatus(types.PerformanceObligationStatusDraft).
				Build()
			c := domainContract.NewBuilder(nil).WithContractStatus(tt.contractStatus).WithEndedAt(tt.contractEnded).Build()
			lineItems := lo.Map(tt.lineItemEnds, func(end time.Time, _ int) *subscription.SubscriptionLineItem {
				return &subscription.SubscriptionLineItem{StartDate: start, EndDate: end}
			})

			got := derivePOB(existing, lineItems, c, now)

			require.Equal(t, tt.wantStatus, got.POBStatus())
			require.Equal(t, tt.wantServiceEnd, got.ServiceEnd(), "service_end")
			require.Equal(t, tt.method, got.RecognitionMethod())
		})
	}
}

type POBServiceSuite struct {
	suite.Suite
	ctx        context.Context
	pobService PerformanceObligationService
	pobRepo    *testutil.InMemoryPOBStore
	sub        *subscription.Subscription
	contract   *domainContract.Contract
}

func TestPOBService(t *testing.T) {
	suite.Run(t, new(POBServiceSuite))
}

func (s *POBServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.pobRepo = testutil.NewInMemoryPOBStore(testutil.NewInMemoryContractStore())
	s.pobService = NewPerformanceObligationService(s.pobRepo, nil, nil, nil, nil, nil)
	s.sub = &subscription.Subscription{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		BaseModel: types.GetDefaultBaseModel(s.ctx),
	}
	s.contract = domainContract.NewBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTRACT)).
		WithContractStatus(types.ContractStatusActive).
		WithStatus(types.StatusPublished).
		Build()
}

func (s *POBServiceSuite) addLineItem(priceType types.PriceType, start time.Time, predecessorID string) *subscription.SubscriptionLineItem {
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.sub.ID,
		PriceID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		PriceType:      priceType,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		StartDate:      start,
		Metadata:       map[string]string{},
		BaseModel:      types.GetDefaultBaseModel(s.ctx),
	}
	if predecessorID != "" {
		li.PredecessorLineItemID = lo.ToPtr(predecessorID)
	}
	s.sub.LineItems = append(s.sub.LineItems, li)
	return li
}

func (s *POBServiceSuite) countPOBs() int {
	count, err := s.pobRepo.Count(s.ctx, pobRepo.NewFilter())
	s.Require().NoError(err)
	return count
}

func (s *POBServiceSuite) pobOf(lineItemID string) *domainPOB.PerformanceObligation {
	p, err := s.pobRepo.GetByLineItemID(s.ctx, lineItemID)
	s.Require().NoError(err)
	return p
}

func (s *POBServiceSuite) TestOnePOBPerLineItemAndIdempotent() {
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fixed := s.addLineItem(types.PRICE_TYPE_FIXED, jan, "")
	usage := s.addLineItem(types.PRICE_TYPE_USAGE, jan, "")

	first, err := s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)
	second, err := s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)

	s.Len(first, 2)
	s.Len(second, 2)
	s.Equal(2, s.countPOBs())
	s.Equal(types.RecognitionMethodStraightLine, s.pobOf(fixed.ID).RecognitionMethod())
	s.Equal(types.RecognitionMethodAsConsumed, s.pobOf(usage.ID).RecognitionMethod())
	s.Equal(types.PerformanceObligationStatusActive, s.pobOf(fixed.ID).POBStatus())
	s.Equal(s.contract.ID(), s.pobOf(fixed.ID).ContractID())
}

func (s *POBServiceSuite) TestSuccessorJoinsPredecessorWithSameMethod() {
	predecessor := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	successor := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), predecessor.ID)

	_, err := s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)

	s.Equal(1, s.countPOBs())
	s.Equal(s.pobOf(predecessor.ID).ID(), s.pobOf(successor.ID).ID())
	associations, err := s.pobRepo.ListAssociations(s.ctx, s.pobOf(predecessor.ID).ID())
	s.Require().NoError(err)
	s.Len(associations, 2)
}

func (s *POBServiceSuite) TestSuccessorJoinsPredecessorFromLegacyMetadata() {
	predecessor := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	successor := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), "")
	successor.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID] = predecessor.ID

	_, err := s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)

	s.Equal(1, s.countPOBs())
	s.Equal(s.pobOf(predecessor.ID).ID(), s.pobOf(successor.ID).ID())
}

func (s *POBServiceSuite) TestSuccessorWithDifferentMethodGetsNewPOB() {
	predecessor := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	successor := s.addLineItem(types.PRICE_TYPE_USAGE, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), predecessor.ID)

	_, err := s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)

	s.Equal(2, s.countPOBs())
	s.NotEqual(s.pobOf(predecessor.ID).ID(), s.pobOf(successor.ID).ID())
}

func (s *POBServiceSuite) TestStatusFollowsContract() {
	li := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	draft := domainContract.NewBuilder(s.contract).WithContractStatus(types.ContractStatusDraft).Build()

	_, err := s.pobService.SyncForSubscription(s.ctx, s.sub, draft)
	s.Require().NoError(err)
	s.Equal(types.PerformanceObligationStatusDraft, s.pobOf(li.ID).POBStatus())

	_, err = s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)
	s.Equal(types.PerformanceObligationStatusActive, s.pobOf(li.ID).POBStatus())
}

func (s *POBServiceSuite) TestPOBOfDroppedLineItemIsSatisfiedOnceItsServiceEnds() {
	li := s.addLineItem(types.PRICE_TYPE_FIXED, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	li.EndDate = time.Now().UTC().Add(time.Hour)

	_, err := s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)
	p := s.pobOf(li.ID)
	s.Equal(types.PerformanceObligationStatusActive, p.POBStatus())
	s.Require().NotNil(p.ServiceEnd())

	// The renewal drops the ended line item from sub.LineItems after its service end has passed.
	past := time.Now().UTC().Add(-time.Hour)
	s.Require().NoError(s.pobRepo.Update(s.ctx, domainPOB.NewBuilder(p).WithServiceEnd(&past).Build()))
	s.sub.LineItems = nil

	_, err = s.pobService.SyncForSubscription(s.ctx, s.sub, s.contract)
	s.Require().NoError(err)
	s.Equal(types.PerformanceObligationStatusSatisfied, s.pobOf(li.ID).POBStatus())
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
	_, err := service.SyncForSubscription(ctx, sub, c)
	require.NoError(t, err)

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
	subs := testutil.NewInMemorySubscriptionStore()
	lineItems := testutil.NewInMemorySubscriptionLineItemStore()
	subs.SetLineItemStore(lineItems)
	service := NewPerformanceObligationService(pobs, contracts, subs, nil, nil, nil)

	sub := &subscription.Subscription{ID: "subs_1", CustomerID: "cust_1", BaseModel: types.GetDefaultBaseModel(ctx)}
	require.NoError(t, subs.Create(ctx, sub))
	for _, li := range []*subscription.SubscriptionLineItem{
		{ID: "sli_seats", SubscriptionID: sub.ID, PriceID: "price_1", PriceType: types.PRICE_TYPE_FIXED, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BaseModel: types.GetDefaultBaseModel(ctx)},
		{ID: "sli_usage", SubscriptionID: sub.ID, PriceID: "price_2", PriceType: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BaseModel: types.GetDefaultBaseModel(ctx)},
	} {
		require.NoError(t, lineItems.Create(ctx, li))
	}
	c := domainContract.NewBuilder(nil).WithID("contract_1").WithCustomerID("cust_1").WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.CreateWithAssociation(ctx, c, domainContract.NewAssociationBuilder(nil).
		WithID("contract_assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID(sub.ID).WithStatus(types.StatusPublished).Build()))

	// Reconcile builds the POBs a missed hook would have, and a second run changes nothing.
	require.NoError(t, service.Reconcile(ctx, c.ID()))
	require.NoError(t, service.Reconcile(ctx, c.ID()))
	all, err := service.GetPerformanceObligations(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all.Items, 2)

	seats, err := pobs.GetByLineItemID(ctx, "sli_seats")
	require.NoError(t, err)
	got, err := service.GetPerformanceObligation(ctx, seats.ID())
	require.NoError(t, err)
	require.Equal(t, c.ID(), got.ContractID)
	require.Equal(t, types.PerformanceObligationStatusActive, got.POBStatus)
	require.Len(t, got.Associations, 1)
	require.Equal(t, "sli_seats", got.Associations[0].EntityID)

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
