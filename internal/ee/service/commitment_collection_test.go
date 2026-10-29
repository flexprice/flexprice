package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCommitmentCollectionProjection(t *testing.T) {
	tests := []struct {
		name                                             string
		typ                                              types.CommitmentType
		method                                           *types.CommitmentCollectionMethod
		binding                                          bool
		used, capacity, unitRate, floor                  string
		clipped                                          bool
		wantUtilized, wantTrueUp, wantOverage, wantValue string
	}{
		{name: "legacy per-period default", typ: types.COMMITMENT_TYPE_AMOUNT, binding: true, used: "80", capacity: "100", unitRate: "1", floor: "100", wantUtilized: "80", wantTrueUp: "20", wantOverage: "0", wantValue: "80"},
		{name: "legacy empty per-period default", typ: types.COMMITMENT_TYPE_AMOUNT, method: lo.ToPtr(types.CommitmentCollectionMethod("")), binding: true, used: "80", capacity: "100", unitRate: "1", floor: "100", wantUtilized: "80", wantTrueUp: "20", wantOverage: "0", wantValue: "80"},
		{name: "scheduled amount below capacity", typ: types.COMMITMENT_TYPE_AMOUNT, method: lo.ToPtr(types.CommitmentCollectionBillingTerms), binding: true, used: "80", capacity: "100", unitRate: "1", floor: "100", wantUtilized: "0", wantTrueUp: "0", wantOverage: "0", wantValue: "80"},
		{name: "scheduled amount overflow", typ: types.COMMITMENT_TYPE_AMOUNT, method: lo.ToPtr(types.CommitmentCollectionBillingTerms), binding: true, used: "130", capacity: "100", unitRate: "1", floor: "100", wantUtilized: "0", wantTrueUp: "0", wantOverage: "30", wantValue: "100"},
		{name: "scheduled zero usage", typ: types.COMMITMENT_TYPE_AMOUNT, method: lo.ToPtr(types.CommitmentCollectionBillingTerms), binding: true, used: "0", capacity: "100", unitRate: "1", floor: "100", wantUtilized: "0", wantTrueUp: "0", wantOverage: "0", wantValue: "0"},
		{name: "scheduled quantity", typ: types.COMMITMENT_TYPE_QUANTITY, method: lo.ToPtr(types.CommitmentCollectionBillingTerms), binding: true, used: "800", capacity: "1000", unitRate: "0.10", floor: "100", wantUtilized: "0", wantTrueUp: "0", wantOverage: "0", wantValue: "80"},
		{name: "scheduled clipped quantity preserves economic cap", typ: types.COMMITMENT_TYPE_QUANTITY, method: lo.ToPtr(types.CommitmentCollectionBillingTerms), binding: true, used: "80", capacity: "100", unitRate: "0.10", floor: "7.50", clipped: true, wantUtilized: "0", wantTrueUp: "0", wantOverage: "0", wantValue: "7.50"},
		{name: "nonbinding unchanged", typ: types.COMMITMENT_TYPE_AMOUNT, binding: false, used: "80", capacity: "100", unitRate: "1", floor: "100", wantUtilized: "80", wantTrueUp: "0", wantOverage: "0", wantValue: "80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := invoiceLine("collection", tt.used, tt.used, tt.used)
			sc := committed("collection", tt.binding, tt.floor, []int{0}, []*usageLine{line}, tt.capacity)
			sc.C.Type, sc.C.CollectionMethod = tt.typ, tt.method
			sc.CommittedPrice.Amount = dec(tt.unitRate)
			sc.Period.Clipped = tt.clipped
			stack := &commitmentStack{Type: tt.typ, Measurement: types.CommitmentMeasurementBillingPeriod, Windows: engineWindows(1), Lines: []*usageLine{line}, Commitments: []*stackedCommitment{sc}}
			alloc, err := allocate(stack)
			require.NoError(t, err)
			cost := withCommittedPrices(func(_ *price.Price, q decimal.Decimal) decimal.Decimal { return q })
			renderer := lineRenderer{currency: "usd", cost: cost}
			value := decimal.Zero
			for _, row := range renderer.commitmentUtilizedLines(stack, sc, alloc.Commitments[0]) {
				value = value.Add(row.Amount)
			}
			require.True(t, value.Equal(dec(tt.wantValue)), "economic value %s", value)
			amounts := map[types.CommitmentLineKind]decimal.Decimal{}
			for _, row := range renderStack(t, stack, cost) {
				kind := types.CommitmentLineKind(row.Metadata[types.MetadataKeyCommitmentLineKind])
				amounts[kind] = amounts[kind].Add(row.Amount)
				if tt.method != nil && *tt.method == types.CommitmentCollectionBillingTerms && kind == types.CommitmentLineUtilized {
					require.NotNil(t, row.CommitmentInfo)
					require.NotNil(t, row.CommitmentInfo.ValueWithin)
					require.True(t, row.CommitmentInfo.ValueWithin.Equal(dec(tt.wantValue)))
					require.False(t, row.CommitmentInfo.TrueUpEnabled)
				}
			}
			for kind, want := range map[types.CommitmentLineKind]string{types.CommitmentLineUtilized: tt.wantUtilized, types.CommitmentLineTrueUp: tt.wantTrueUp, types.CommitmentLineOverage: tt.wantOverage} {
				require.True(t, amounts[kind].Equal(dec(want)), "%s: %s", kind, amounts[kind])
			}
			require.Equal(t, tt.binding && (tt.method == nil || *tt.method == "" || *tt.method == types.CommitmentCollectionPerPeriod), commitmentTrueUpEnabled(sc.C))
		})
	}
}

func TestAmortizeScheduledCommitmentValueSeparatelyFromCharges(t *testing.T) {
	combos := []*events.DetailedUsageAnalytic{
		{SubLineItemID: "li", TotalUsage: dec("60"), Points: []events.UsageAnalyticPoint{{Usage: dec("20")}, {Usage: dec("40")}}},
		{SubLineItemID: "li", TotalUsage: dec("20"), Points: []events.UsageAnalyticPoint{{Usage: dec("20")}}},
	}
	data := &AnalyticsData{Analytics: combos, LineTotals: map[string]decimal.Decimal{"li": dec("8")}, LineCommitments: map[string]*types.CommitmentInfo{"li": {Type: types.COMMITMENT_TYPE_AMOUNT, ValueWithin: lo.ToPtr(dec("80")), ComputedOverageAmount: dec("8")}}}
	amortizeLineTotals(data)
	for i, want := range []string{"60", "20"} {
		info := combos[i].CommitmentInfo
		require.NotNil(t, info.ValueWithin)
		require.True(t, info.ValueWithin.Equal(dec(want)))
		require.False(t, info.TrueUpEnabled)
		require.True(t, info.ComputedCommitmentUtilizedAmount.IsZero())
		require.True(t, info.ComputedTrueUpAmount.IsZero())
		require.True(t, combos[i].TotalCost.Equal(dec(want).Div(dec("10"))))
		sum := decimal.Zero
		for _, point := range combos[i].Points {
			require.True(t, point.ComputedCommitmentUtilizedAmount.IsZero())
			require.True(t, point.ComputedTrueUpAmount.IsZero())
			require.True(t, point.ComputedOverageAmount.Equal(point.Cost))
			sum = sum.Add(point.Cost)
		}
		require.True(t, sum.Equal(combos[i].TotalCost))
	}
}

func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_BillingTerms() {
	for _, quantity := range []string{"0", "80", "130"} {
		s.Run(quantity, func() {
			s.SetupTest()
			s.setupTestData()
			s.enableCommitmentsV2()
			p := s.usagePriceFor("price_scheduled_breakdown", s.testData.meters.apiCalls, "1")
			sub := s.fixedDateSub("sub_scheduled_breakdown", &subscription.SubscriptionLineItem{ID: "li_scheduled_breakdown", PriceID: p.ID, MeterID: p.MeterID})
			s.testData.subscription = sub
			c := s.newTestCommitment("cmt_scheduled_breakdown", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(1, 0, 0))
			c.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
			s.storeCommitment(c)
			usage := s.usageResponse(p, quantity)
			l := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
			got, err := l.UsageBreakdown(s.GetContext())
			s.Require().NoError(err)
			s.Require().Len(got, 1)
			b := got[0]
			wantUsed := decimal.Min(dec(quantity), dec("100"))
			s.True(b.Used.Amount.Equal(wantUsed), "used %s", b.Used.Amount)
			s.True(b.Unused.Amount.Equal(dec("100").Sub(wantUsed)), "unused %s", b.Unused.Amount)
			s.True(b.BilledAmount.IsZero())
			s.True(b.TrueUpAmount.IsZero())
			s.False(b.TrueUpEnabled)
			s.Equal(types.CommitmentCollectionBillingTerms, b.CollectionMethod)
		})
	}
}

func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_BillingTermsQuantity() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub := s.testData.subscription
	committedPrice := s.usagePriceFor("price_scheduled_quantity", s.testData.meters.apiCalls, "0.10")
	c := s.newTestCommitment("cmt_scheduled_quantity", sub, types.COMMITMENT_TYPE_QUANTITY, "1000", s.testData.prices.apiCalls.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(1, 0, 0))
	c.CommittedPriceID = lo.ToPtr(committedPrice.ID)
	c.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	s.storeCommitment(c)
	l := s.loaderFor(sub, s.usageResponse(s.testData.prices.apiCalls, "800"), sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	got, err := l.UsageBreakdown(s.GetContext())
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	b := got[0]
	s.True(b.Committed.Quantity.Round(0).Equal(dec("1000")))
	s.True(b.Used.Quantity.Equal(dec("800")))
	s.True(b.Unused.Quantity.Round(0).Equal(dec("200")))
	s.True(b.Used.Amount.Equal(dec("80")), "used amount %s", b.Used.Amount)
	s.True(b.Unused.Amount.Equal(dec("20")), "unused amount %s", b.Unused.Amount)
	s.True(b.BilledAmount.IsZero())
	s.True(b.TrueUpAmount.IsZero())
	s.False(b.TrueUpEnabled)
}

func (s *BillingServiceSuite) TestCommitmentLoader_CollectionPolicy() {
	for _, tt := range []struct {
		name    string
		method  *types.CommitmentCollectionMethod
		wantErr bool
	}{
		{"legacy NULL", nil, false},
		{"legacy empty", lo.ToPtr(types.CommitmentCollectionMethod("")), false},
		{"unknown nonempty", lo.ToPtr(types.CommitmentCollectionMethod("unsupported")), true},
	} {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.enableCommitmentsV2()
			p := s.usagePriceFor("price_collection_policy", s.testData.meters.apiCalls, "1")
			sub := s.fixedDateSub("sub_collection_policy", &subscription.SubscriptionLineItem{ID: "li_collection_policy", PriceID: p.ID, MeterID: p.MeterID})
			s.testData.subscription = sub
			c := s.newTestCommitment("cmt_collection_policy", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(1, 0, 0))
			c.CollectionMethod = tt.method
			s.storeCommitment(c)
			usage := s.usageResponse(p, "80")
			loader, err := s.service.(*billingService).newCommitmentLoader(s.GetContext(), sub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, sub.CurrentPeriodEnd, rawChargesByLine(sub, usage), nil, types.UsageSourceInvoiceCreation)
			if tt.wantErr {
				s.True(ierr.IsInvalidOperation(err), "invalid policy must not silently remove a binding true-up: %v", err)
				return
			}
			s.Require().NoError(err)
			_, total, err := loader.InvoiceLines(s.GetContext())
			s.Require().NoError(err)
			s.True(total.Equal(dec("100")), "legacy collection must preserve its binding floor: %s", total)
		})
	}
}

func (s *MeterUsageServiceSuite) TestGroupBy_BillingTermsKeepsValueAndSuppressesTrueUp() {
	for _, mixed := range []bool{false, true} {
		s.Run(map[bool]string{false: "scheduled only", true: "mixed collection"}[mixed], func() {
			s.SetupTest()
			ctx := s.GetContext()
			setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
			m := s.createMeterWithAggregation(ctx, "meter_scheduled", "api_call", types.AggregationSum)
			p := s.createPriceForMeter(ctx, "price_scheduled", m.ID, dec("1"))
			s.createLineItemForMeter(ctx, "li_scheduled", m.ID, p.ID)
			capacity := "100"
			if mixed {
				capacity = "50"
			}
			c := s.newCommitment("cmt_scheduled", types.CommitmentScopePrice, p.ID, capacity)
			c.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
			s.storeCommitment(c)
			if mixed {
				other := s.newCommitment("cmt_period", types.CommitmentScopePrice, p.ID, "70")
				other.Priority = 2
				s.storeCommitment(other)
			}
			for i, source := range []string{"trainer", "inference"} {
				s.insertMeterUsageWithProps(ctx, m.ID, s.customer.ExternalID, source, time.Date(2026, 1, 5+i, 10, 0, 0, 0, time.UTC), 40, nil)
			}
			resp := s.detailedAnalytics([]string{m.ID}, nil, []string{"source"}, "")
			s.Require().Len(resp.Items, 2)
			wantTotal := decimal.Zero
			if mixed {
				wantTotal = dec("70")
			}
			s.True(resp.TotalCost.Equal(wantTotal), "total %s", resp.TotalCost)
			for _, item := range resp.Items {
				info := item.CommitmentInfo
				s.Require().NotNil(info)
				s.Equal(mixed, info.TrueUpEnabled)
				if mixed {
					s.True(info.ComputedTrueUpAmount.Equal(dec("20")), "true-up %s", info.ComputedTrueUpAmount)
					s.True(info.ComputedCommitmentUtilizedAmount.Equal(dec("15")), "utilized %s", info.ComputedCommitmentUtilizedAmount)
				} else {
					s.True(info.ComputedTrueUpAmount.IsZero())
					s.True(info.ComputedCommitmentUtilizedAmount.IsZero())
				}
				s.Require().NotNil(info.ValueWithin)
				s.True(info.ValueWithin.Equal(dec("40")), "economic %s", info.ValueWithin)
			}
		})
	}
}

func TestGroupedScheduledCommitmentsRetainCoveredValue(t *testing.T) {
	line := invoiceLine("shared", "80", "80", "80")
	stack := &commitmentStack{Type: types.COMMITMENT_TYPE_AMOUNT, Measurement: types.CommitmentMeasurementBillingPeriod,
		Windows: engineWindows(1), Lines: []*usageLine{line}}
	for _, id := range []string{"first", "second"} {
		sc := committed(id, true, "40", []int{0}, stack.Lines, "40")
		sc.C.Type = types.COMMITMENT_TYPE_AMOUNT
		sc.C.InvoiceLabel = "Reserved usage"
		sc.C.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
		stack.Commitments = append(stack.Commitments, sc)
	}
	rows := renderStack(t, stack, withCommittedPrices(func(_ *price.Price, q decimal.Decimal) decimal.Decimal { return q }))
	require.Len(t, rows, 2)
	grouped := mergeLineItemsByBillingPeriod(rows)
	require.Len(t, grouped, 1)
	require.True(t, grouped[0].Amount.IsZero())
	require.Equal(t, "80", grouped[0].Quantity.String())
	require.NotNil(t, grouped[0].CommitmentInfo.ValueWithin)
	require.Equal(t, "80", grouped[0].CommitmentInfo.ValueWithin.String())
	for _, row := range rows {
		require.Equal(t, "40", row.CommitmentInfo.ValueWithin.String(), "grouping leaves source rows unchanged")
	}
	for _, values := range [][2]*decimal.Decimal{{nil, nil}, {nil, lo.ToPtr(dec("40"))}, {lo.ToPtr(dec("40")), nil}} {
		got := mergeCommitmentInfo(&types.CommitmentInfo{ValueWithin: values[0]}, &types.CommitmentInfo{ValueWithin: values[1]})
		if values[0] == nil && values[1] == nil {
			require.Nil(t, got.ValueWithin, "legacy rows keep the field omitted")
		} else {
			require.Equal(t, "40", got.ValueWithin.String())
		}
	}
}
