package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/events"
	invoicedomain "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *BillingServiceSuite) enableCommitmentsV2() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), true)
}

// newTestCommitment returns a binding per_period commitment scoped to one price, active from start to end.
func (s *BillingServiceSuite) newTestCommitment(id string, sub *subscription.Subscription, typ types.CommitmentType, value, priceID string, start, end time.Time) *commitment.Commitment {
	ctx := s.GetContext()
	return &commitment.Commitment{
		ID: id, SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Name: id, InvoiceLabel: "Commitment " + id,
		Currency: sub.Currency, ScopeEntityType: types.CommitmentScopePrice, ScopeEntityID: priceID,
		Type: typ, Value: dec(value), Measurement: types.CommitmentMeasurementBillingPeriod, Timezone: "UTC",
		Priority: 1, Binding: true, CollectionMethod: lo.ToPtr(types.CommitmentCollectionPerPeriod),
		StartDate: start, EndDate: end, CommitmentStatus: types.CommitmentStatusActive,
		OnEarlyTermination: lo.ToPtr(types.CommitmentTerminationForfeit),
		EnvironmentID:      types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func (s *BillingServiceSuite) storeCommitment(c *commitment.Commitment) {
	_, err := s.GetStores().CommitmentRepo.Create(s.GetContext(), c)
	s.Require().NoError(err)
}

func (s *BillingServiceSuite) createCommitment(_ context.Context, typ types.CommitmentType, value, priceID string, start, end time.Time) *commitment.Commitment {
	c := s.newTestCommitment(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT), s.testData.subscription, typ, value, priceID, start, end)
	s.storeCommitment(c)
	return c
}

// usageResponse returns one charge for the testData line item on p, with qty priced at p.
func (s *BillingServiceSuite) usageResponse(p *price.Price, qty string) *dto.GetUsageBySubscriptionResponse {
	li, ok := lo.Find(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem) bool { return li.PriceID == p.ID })
	s.Require().True(ok)
	amount := NewPriceService(s.service.(*billingService).ServiceParams).CalculateCost(s.GetContext(), p, dec(qty))
	return &dto.GetUsageBySubscriptionResponse{
		Currency: s.testData.subscription.Currency,
		Charges: []*dto.SubscriptionUsageByMetersResponse{{
			SubscriptionLineItemID: li.ID, Price: p, Quantity: dec(qty).InexactFloat64(), Amount: amount.InexactFloat64(),
		}},
	}
}

func (s *BillingServiceSuite) TestCommitmentV2_PerPeriodAmount_TrueUp() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	// $100 amount commitment on the api-calls price; 7000 calls cost $60 on the slab tiers.
	c := s.createCommitment(ctx, types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
	usage := s.usageResponse(s.testData.prices.apiCalls, "7000")
	lines, total, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.Len(lines, 2)
	within := linesByKind(lines, types.CommitmentLineWithin)
	s.Require().Len(within, 1)
	s.True(within[0].Amount.Equal(dec("60")), "within %s", within[0].Amount)
	s.True(within[0].Quantity.Equal(dec("7000")))
	s.Nil(within[0].CommitmentInfo)
	s.Equal(c.ID, lo.FromPtr(within[0].CommitmentID))
	trueUp := linesByKind(lines, types.CommitmentLineTrueUp)
	s.Require().Len(trueUp, 1)
	s.True(trueUp[0].Amount.Equal(dec("40")), "true-up %s", trueUp[0].Amount)
	s.True(total.Equal(dec("100")), "total %s", total)
}

func (s *BillingServiceSuite) TestCommitmentV2_FlagOff_InvoiceUnchanged() {
	ctx := s.GetContext()
	s.setupTestData()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	usage := s.usageResponse(s.testData.prices.apiCalls, "7000")
	before, beforeTotal, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.createCommitment(ctx, types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
	after, afterTotal, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.Equal(before, after)
	s.True(beforeTotal.Equal(afterTotal))
}

func (s *BillingServiceSuite) TestCommitmentV2_FlagOn_NoOverlap_InvoiceUnchanged() {
	ctx := s.GetContext()
	s.setupTestData()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	usage := s.usageResponse(s.testData.prices.apiCalls, "7000")
	before, beforeTotal, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.enableCommitmentsV2()
	// Starts after the period, and one terminated before it began.
	s.createCommitment(ctx, types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodEnd, periodEnd.AddDate(1, 0, 0))
	old := s.createCommitment(ctx, types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart.AddDate(0, -2, 0), periodEnd.AddDate(1, 0, 0))
	old.CommitmentStatus = types.CommitmentStatusTerminated
	old.TerminatedAt = lo.ToPtr(periodStart.AddDate(0, 0, -1))
	_, err = s.GetStores().CommitmentRepo.Update(ctx, old)
	s.Require().NoError(err)
	after, afterTotal, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.Equal(before, after)
	s.True(beforeTotal.Equal(afterTotal))
}

// A line with a legacy line-item commitment that is also in scope of a new commitment is billed only by the new one.
func (s *BillingServiceSuite) TestCommitmentV2_PrefersNewCommitmentOverLegacyLineCommitment() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	li, _ := lo.Find(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceID == s.testData.prices.apiCalls.ID
	})
	li.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
	li.CommitmentAmount = lo.ToPtr(dec("500"))
	li.CommitmentTrueUpEnabled = true
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	s.createCommitment(ctx, types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
	lines, total, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	for _, l := range lines {
		s.Nil(l.CommitmentInfo)
		s.NotEmpty(l.Metadata[types.MetadataKeyCommitmentLineKind])
	}
	s.True(total.Equal(dec("100")), "total %s", total)
}

// fixedDateSub stores an August 2026 monthly subscription for the test customer with the given usage lines.
func (s *BillingServiceSuite) fixedDateSub(id string, lines ...*subscription.SubscriptionLineItem) *subscription.Subscription {
	ctx := s.GetContext()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		ID: id, PlanID: s.testData.plan.ID, CustomerID: s.testData.customer.ID, StartDate: start, BillingAnchor: start,
		CurrentPeriodStart: start, CurrentPeriodEnd: start.AddDate(0, 1, 0), Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	for _, li := range lines {
		li.SubscriptionID, li.CustomerID, li.StartDate = sub.ID, sub.CustomerID, start
		li.EntityID, li.EntityType, li.PlanDisplayName = s.testData.plan.ID, types.SubscriptionLineItemEntityTypePlan, s.testData.plan.Name
		li.PriceType, li.Currency, li.BillingPeriod, li.InvoiceCadence = types.PRICE_TYPE_USAGE, "usd", sub.BillingPeriod, types.InvoiceCadenceArrear
		li.BaseModel = types.GetDefaultBaseModel(ctx)
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lines))
	sub.LineItems = lines
	return sub
}

func (s *BillingServiceSuite) usagePriceFor(id string, m *meter.Meter, amount string) *price.Price {
	p := &price.Price{
		ID: id, Amount: dec(amount), Currency: "usd", EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: s.testData.plan.ID,
		Type: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	if m != nil {
		p.MeterID = m.ID
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
	return p
}

func (s *BillingServiceSuite) TestCommitmentV2_StackedWindowed_Simplismart() {
	// Stacked GPU reservations at reduced scale: per-minute meter; C1 = 8 all day (p1, 0.048); C2 = 32, 09:00-21:00 IST from 25 Aug (p2, 0.0491667).
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()

	gpuMeter := &meter.Meter{
		ID: "meter_gpu_minutes", Name: "GPU minutes", EventName: "gpu",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeMinute},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, gpuMeter))
	h100 := s.usagePriceFor("price_h100", gpuMeter, "0.06")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_h100", PriceID: h100.ID, MeterID: gpuMeter.ID,
		MeterDisplayName: gpuMeter.Name, DisplayName: "H100"}
	sub := s.fixedDateSub("sub_e9", li)
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd

	ist, err := time.LoadLocation("Asia/Kolkata")
	s.Require().NoError(err)
	insert := func(at time.Time, gpus int64) {
		id := s.GetUUID()
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event: events.Event{ID: id, TenantID: sub.TenantID, EnvironmentID: sub.EnvironmentID, EventName: gpuMeter.EventName,
				ExternalCustomerID: s.testData.customer.ExternalID, CustomerID: sub.CustomerID, Timestamp: at.UTC(), IngestedAt: at.UTC()},
			MeterID: gpuMeter.ID, QtyTotal: decimal.NewFromInt(gpus), UniqueHash: id,
		}}))
	}
	insert(time.Date(2026, 8, 25, 3, 0, 0, 0, ist), 10)
	insert(time.Date(2026, 8, 25, 10, 0, 0, 0, ist), 45)
	insert(time.Date(2026, 8, 25, 11, 0, 0, 0, ist), 20)

	p1 := s.usagePriceFor("price_c1_committed", gpuMeter, "0.048")
	p2 := s.usagePriceFor("price_c2_committed", gpuMeter, "0.0491667")
	c1 := s.newTestCommitment("cmt_c1", sub, types.COMMITMENT_TYPE_QUANTITY, "8", h100.ID, periodStart, periodEnd)
	c1.Measurement, c1.CommittedPriceID = types.CommitmentMeasurementWindow, lo.ToPtr(p1.ID)
	c2 := s.newTestCommitment("cmt_c2", sub, types.COMMITMENT_TYPE_QUANTITY, "32", h100.ID, time.Date(2026, 8, 25, 0, 0, 0, 0, ist), periodEnd)
	c2.Measurement, c2.CommittedPriceID, c2.Priority = types.CommitmentMeasurementWindow, lo.ToPtr(p2.ID), 2
	c2.Timezone, c2.ActiveWindows = "Asia/Kolkata", types.ActiveWindows{{Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 21}}}
	s.storeCommitment(c1)
	s.storeCommitment(c2)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID, Price: h100, Quantity: 75,
	}}}
	lines, total, err := s.service.CalculateMeterUsageCharges(ctx, sub, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)

	byCmt := func(kind types.CommitmentLineKind, id string) dto.CreateInvoiceLineItemRequest {
		found, ok := lo.Find(linesByKind(lines, kind), func(l dto.CreateInvoiceLineItemRequest) bool { return lo.FromPtr(l.CommitmentID) == id })
		s.Require().True(ok, "no %s line for %s", kind, id)
		return found
	}
	s.True(byCmt(types.CommitmentLineWithin, c1.ID).Quantity.Equal(dec("24")))
	s.True(byCmt(types.CommitmentLineWithin, c2.ID).Quantity.Equal(dec("44")))
	onDemand := linesByKind(lines, types.CommitmentLineOnDemand)
	s.Require().Len(onDemand, 1)
	s.True(onDemand[0].Quantity.Equal(dec("7")))
	s.True(onDemand[0].Amount.Equal(dec("0.42")))
	// C1 runs all 44,640 August minutes; C2 runs 7 days x 720 minutes from 25 Aug IST.
	s.True(byCmt(types.CommitmentLineTrueUp, c1.ID).Quantity.Equal(dec("357096")), "c1 unused %s", byCmt(types.CommitmentLineTrueUp, c1.ID).Quantity)
	s.True(byCmt(types.CommitmentLineTrueUp, c2.ID).Quantity.Equal(dec("161236")), "c2 unused %s", byCmt(types.CommitmentLineTrueUp, c2.ID).Quantity)
	c1Period := byCmt(types.CommitmentLineWithin, c1.ID).Amount.Add(byCmt(types.CommitmentLineTrueUp, c1.ID).Amount)
	s.True(c1Period.Equal(dec("17141.76")), "c1 period %s", c1Period)
	sum := decimal.Zero
	for _, l := range lines {
		sum = sum.Add(l.Amount)
	}
	s.True(total.Equal(sum))
}

func (s *BillingServiceSuite) TestCommitmentV2_BillRemaining_Settlement() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_flat_api", PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub("sub_settle", li)
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd

	// $100/month Aug-Oct, terminated on 16 Aug: 15 of 31 days earned, the rest of the $300 is settled.
	c := s.newTestCommitment("cmt_settle", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, periodStart, periodStart.AddDate(0, 3, 0))
	c.OnEarlyTermination = lo.ToPtr(types.CommitmentTerminationBillRemaining)
	c.CommitmentStatus = types.CommitmentStatusTerminated
	c.TerminatedAt = lo.ToPtr(time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC))
	s.storeCommitment(c)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID, Price: p, Quantity: 20, Amount: 20,
	}}}
	lines, total, err := s.service.CalculateMeterUsageCharges(ctx, sub, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.True(linesByKind(lines, types.CommitmentLineWithin)[0].Amount.Equal(dec("20")))
	trueUp := linesByKind(lines, types.CommitmentLineTrueUp)
	s.Require().Len(trueUp, 1)
	s.True(trueUp[0].Amount.Equal(dec("28.39")), "true-up %s", trueUp[0].Amount) // 48.39 earned - 20 used
	settlement := linesByKind(lines, types.CommitmentLineSettlement)
	s.Require().Len(settlement, 1)
	s.True(settlement[0].Amount.Equal(dec("251.61")), "settlement %s", settlement[0].Amount)
	s.True(total.Equal(dec("300")), "total %s", total)
}

func TestSpreadWindowCosts_ConservesLineCost(t *testing.T) {
	third := func(q decimal.Decimal) decimal.Decimal { return q.Mul(dec("0.333333")) }
	got := spreadWindowCosts([]decimal.Decimal{dec("1"), dec("1"), dec("1"), dec("0")}, dec("1.00"), third)
	require.Len(t, got, 4)
	sum := decimal.Zero
	for _, v := range got {
		sum = sum.Add(v)
	}
	require.True(t, sum.Equal(dec("1.00")), "sum %s", sum)
	require.True(t, got[0].Equal(dec("0.333333")))
	require.True(t, got[2].Equal(dec("0.333334")), "residue lands on the last non-zero window, got %s", got[2])
	require.True(t, got[3].IsZero())

	// No usage in any window: the line cost (if any) lands on the last window.
	got = spreadWindowCosts([]decimal.Decimal{dec("0"), dec("0")}, dec("0"), third)
	require.True(t, got[0].IsZero() && got[1].IsZero())
}

// A call that does not carry an in-scope line (another cadence group, or an already-invoiced arrear item) leaves it alone.
func (s *BillingServiceSuite) TestCommitmentV2_LinesOutsideTheCallAreNotBilled() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	s.createCommitment(ctx, types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
	usage := s.usageResponse(s.testData.prices.apiCalls, "7000")
	callSub := *s.testData.subscription
	callSub.LineItems = lo.Filter(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceID != s.testData.prices.apiCalls.ID
	})
	lines, total, err := s.service.CalculateMeterUsageCharges(ctx, &callSub, usage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.Empty(lines)
	s.True(total.IsZero(), "total %s", total)
}

// A window that is not inside one commitment period never gets the commitment's capacity or true-up.
func (s *BillingServiceSuite) TestCommitmentV2_WindowAcrossCommitmentPeriodsIsSkipped() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_flat_api", PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub("sub_span", li)
	c := s.newTestCommitment("cmt_span", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(0, 3, 0))
	s.storeCommitment(c)
	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID, Price: p, Quantity: 20, Amount: 20,
	}}}
	from, to := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	lines, total, err := s.service.CalculateMeterUsageCharges(ctx, sub, usage, from, to, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.Require().Len(lines, 1)
	s.Empty(lines[0].Metadata[types.MetadataKeyCommitmentLineKind])
	s.True(total.Equal(dec("20")), "total %s", total)
}

// settleFixture stores an Aug 2026 subscription with a $100/month bill_remaining commitment from Aug to Oct.
func (s *BillingServiceSuite) settleFixture(id string) (*subscription.Subscription, *commitment.Commitment, *dto.GetUsageBySubscriptionResponse) {
	p := s.usagePriceFor("price_"+id, s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_" + id, PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub(id, li)
	c := s.newTestCommitment("cmt_"+id, sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(0, 3, 0))
	c.OnEarlyTermination = lo.ToPtr(types.CommitmentTerminationBillRemaining)
	s.storeCommitment(c)
	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID, Price: p, Quantity: 20, Amount: 20,
	}}}
	return sub, c, usage
}

func (s *BillingServiceSuite) settlementsFor(sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, from, to time.Time) []dto.CreateInvoiceLineItemRequest {
	lines, _, err := s.service.CalculateMeterUsageCharges(s.GetContext(), sub, usage, from, to, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	return linesByKind(lines, types.CommitmentLineSettlement)
}

// A period-end cancel settles on the final invoice, before and after the commitment is terminated, and never on the next.
func (s *BillingServiceSuite) TestCommitmentV2_PeriodEndCancel_SettlesOnce() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, c, usage := s.settleFixture("sub_eop")
	aug, sep, oct := sub.CurrentPeriodStart, sub.CurrentPeriodEnd, sub.CurrentPeriodEnd.AddDate(0, 1, 0)
	sub.CancelAt, sub.CancelAtPeriodEnd = lo.ToPtr(sep), true

	before := s.settlementsFor(sub, usage, aug, sep)
	s.Require().Len(before, 1)
	s.True(before[0].Amount.Equal(dec("200")), "settlement %s", before[0].Amount)

	s.Require().NoError(NewCommitmentService(s.service.(*billingService).ServiceParams).TerminateForSubscription(ctx, sub.ID, sep))
	stored, err := s.GetStores().CommitmentRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.Require().NotNil(stored.TerminatedAt)

	after := s.settlementsFor(sub, usage, aug, sep)
	s.Require().Len(after, 1)
	s.True(after[0].Amount.Equal(before[0].Amount))
	s.Empty(s.settlementsFor(sub, usage, sep, oct))
}

// Terminated on its start date, the commitment settles on its first invoice only.
func (s *BillingServiceSuite) TestCommitmentV2_TerminatedAtStart_SettlesOnce() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, c, usage := s.settleFixture("sub_t0")
	jul, aug, sep := sub.CurrentPeriodStart.AddDate(0, -1, 0), sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	c.CommitmentStatus, c.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(aug)
	_, err := s.GetStores().CommitmentRepo.Update(ctx, c)
	s.Require().NoError(err)

	s.Empty(s.settlementsFor(sub, usage, jul, aug))
	got := s.settlementsFor(sub, usage, aug, sep)
	s.Require().Len(got, 1)
	s.True(got[0].Amount.Equal(dec("300")), "settlement %s", got[0].Amount)
	s.Empty(s.settlementsFor(sub, usage, sep, sep.AddDate(0, 1, 0)))
}

// An immediate cancel bills the prorated true-up and the bill_remaining settlement on the final invoice.
func (s *EntitlementQuantityTestSuite) TestCommitmentV2_ImmediateCancel_SettlesOnFinalInvoice() {
	ctx := s.GetContext()
	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		ID: "sub_cmt_cancel", PlanID: s.testData.plan.ID, CustomerID: s.testData.customer.ID, StartDate: aug,
		BillingAnchor: aug, CurrentPeriodStart: aug, CurrentPeriodEnd: aug.AddDate(0, 1, 0), Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := *s.testData.usageSubLineItem
	li.ID, li.SubscriptionID, li.StartDate = "subs_line_cmt_cancel", sub.ID, aug
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{&li}))

	// 500 calls at $0.02 = $10 of usage.
	records := make([]*events.MeterUsage, 0, 500)
	for i := 0; i < 500; i++ {
		id := types.GenerateUUIDWithPrefix("mu")
		ts := aug.Add(24 * time.Hour)
		records = append(records, &events.MeterUsage{
			Event: events.Event{ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: s.testData.meter.EventName, CustomerID: s.testData.customer.ID,
				ExternalCustomerID: s.testData.customer.ExternalID, Timestamp: ts, IngestedAt: ts},
			MeterID: s.testData.meter.ID, QtyTotal: decimal.NewFromInt(1), UniqueHash: id,
		})
	}
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))

	c := &commitment.Commitment{
		ID: "cmt_cancel", SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Name: "Platform", InvoiceLabel: "Platform",
		Currency: "usd", ScopeEntityType: types.CommitmentScopePrice, ScopeEntityID: li.PriceID,
		Type: types.COMMITMENT_TYPE_AMOUNT, Value: dec("100"), Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone: "UTC", Priority: 1, Binding: true, CollectionMethod: lo.ToPtr(types.CommitmentCollectionPerPeriod),
		StartDate: aug, EndDate: aug.AddDate(0, 3, 0), CommitmentStatus: types.CommitmentStatusActive,
		OnEarlyTermination: lo.ToPtr(types.CommitmentTerminationBillRemaining),
		EnvironmentID:      types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().CommitmentRepo.Create(ctx, c)
	s.Require().NoError(err)

	_, err = s.subService.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelAt:                       lo.ToPtr(time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)),
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
	})
	s.Require().NoError(err)

	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = sub.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	inv, ok := lo.Find(invoices, func(i *invoicedomain.Invoice) bool { return i.InvoiceType == types.InvoiceTypeSubscription })
	s.Require().True(ok, "final invoice not found")
	resp, err := s.invoiceSvc.GetInvoice(ctx, inv.ID)
	s.Require().NoError(err)

	amountOf := func(kind types.CommitmentLineKind) []decimal.Decimal {
		var out []decimal.Decimal
		for _, l := range resp.LineItems {
			if l.Metadata[types.MetadataKeyCommitmentLineKind] == string(kind) {
				out = append(out, l.Amount)
			}
		}
		return out
	}
	within, trueUp, settle := amountOf(types.CommitmentLineWithin), amountOf(types.CommitmentLineTrueUp), amountOf(types.CommitmentLineSettlement)
	s.Require().Len(within, 1)
	s.True(within[0].Equal(dec("10")), "within %s", within[0])
	// 15 of 31 days earned = $48.39; settlement is the rest of $300.
	s.Require().Len(trueUp, 1)
	s.True(trueUp[0].Equal(dec("38.39")), "true-up %s", trueUp[0])
	s.Require().Len(settle, 1)
	s.True(settle[0].Equal(dec("251.61")), "settlement %s", settle[0])
}

// futureSettleFixture stores an Aug 2026 subscription with a $100/month bill_remaining commitment from Sep to Dec.
func (s *BillingServiceSuite) futureSettleFixture(id string) (*subscription.Subscription, *commitment.Commitment, *dto.GetUsageBySubscriptionResponse) {
	sub, c, usage := s.settleFixture(id)
	c.StartDate, c.EndDate = sub.CurrentPeriodEnd, sub.CurrentPeriodEnd.AddDate(0, 3, 0)
	_, err := s.GetStores().CommitmentRepo.Update(s.GetContext(), c)
	s.Require().NoError(err)
	return sub, c, usage
}

// Cancelled mid-August before the commitment starts: the final invoice settles the whole value once.
func (s *BillingServiceSuite) TestCommitmentV2_ImmediateCancelBeforeStart_SettlesOnce() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, c, usage := s.futureSettleFixture("sub_pre_imm")
	aug, cancelAt := sub.CurrentPeriodStart, time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)

	s.Require().NoError(NewCommitmentService(s.service.(*billingService).ServiceParams).TerminateForSubscription(ctx, sub.ID, cancelAt))
	stored, err := s.GetStores().CommitmentRepo.Get(ctx, c.ID)
	s.Require().NoError(err)
	s.True(stored.TerminatedAt.Equal(cancelAt), "terminated_at keeps the requested date")

	// Built before the subscription records its end, then recomputed after.
	first := s.settlementsFor(sub, usage, aug, cancelAt)
	s.Require().Len(first, 1)
	s.True(first[0].Amount.Equal(dec("300")), "settlement %s", first[0].Amount)
	sub.EndDate, sub.CancelAt = lo.ToPtr(cancelAt), lo.ToPtr(cancelAt)
	again := s.settlementsFor(sub, usage, aug, cancelAt)
	s.Require().Len(again, 1)
	s.True(again[0].Amount.Equal(first[0].Amount))
}

// A period-end cancel at 1 Sep for a commitment starting 1 Sep settles on the August invoice only.
func (s *BillingServiceSuite) TestCommitmentV2_PeriodEndCancelAtStart_SettlesOnce() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, _, usage := s.futureSettleFixture("sub_pre_eop")
	aug, sep := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	sub.CancelAt, sub.CancelAtPeriodEnd = lo.ToPtr(sep), true

	before := s.settlementsFor(sub, usage, aug, sep)
	s.Require().Len(before, 1)
	s.True(before[0].Amount.Equal(dec("300")), "settlement %s", before[0].Amount)

	s.Require().NoError(NewCommitmentService(s.service.(*billingService).ServiceParams).TerminateForSubscription(ctx, sub.ID, sep))
	after := s.settlementsFor(sub, usage, aug, sep)
	s.Require().Len(after, 1)
	s.True(after[0].Amount.Equal(before[0].Amount))
	s.Empty(s.settlementsFor(sub, usage, sep, sep.AddDate(0, 1, 0)))
}

// An immediate cancel before a commitment starts bills its whole value as a settlement on the final invoice.
func (s *EntitlementQuantityTestSuite) TestCommitmentV2_ImmediateCancelBeforeStart_SettlesOnFinalInvoice() {
	ctx := s.GetContext()
	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		ID: "sub_cmt_cancel_pre", PlanID: s.testData.plan.ID, CustomerID: s.testData.customer.ID, StartDate: aug,
		BillingAnchor: aug, CurrentPeriodStart: aug, CurrentPeriodEnd: aug.AddDate(0, 1, 0), Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := *s.testData.usageSubLineItem
	li.ID, li.SubscriptionID, li.StartDate = "subs_line_cmt_cancel_pre", sub.ID, aug
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{&li}))

	sep := aug.AddDate(0, 1, 0)
	c := &commitment.Commitment{
		ID: "cmt_cancel_pre", SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Name: "Platform", InvoiceLabel: "Platform",
		Currency: "usd", ScopeEntityType: types.CommitmentScopePrice, ScopeEntityID: li.PriceID,
		Type: types.COMMITMENT_TYPE_AMOUNT, Value: dec("100"), Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone: "UTC", Priority: 1, Binding: true, CollectionMethod: lo.ToPtr(types.CommitmentCollectionPerPeriod),
		StartDate: sep, EndDate: sep.AddDate(0, 3, 0), CommitmentStatus: types.CommitmentStatusActive,
		OnEarlyTermination: lo.ToPtr(types.CommitmentTerminationBillRemaining),
		EnvironmentID:      types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().CommitmentRepo.Create(ctx, c)
	s.Require().NoError(err)

	_, err = s.subService.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelAt:                       lo.ToPtr(time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)),
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicyGenerateInvoice,
	})
	s.Require().NoError(err)

	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = sub.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	var settlements []decimal.Decimal
	for _, inv := range invoices {
		resp, err := s.invoiceSvc.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)
		for _, l := range resp.LineItems {
			if l.Metadata[types.MetadataKeyCommitmentLineKind] == string(types.CommitmentLineSettlement) {
				s.Equal(types.InvoiceTypeSubscription, inv.InvoiceType)
				settlements = append(settlements, l.Amount)
			}
		}
	}
	s.Require().Len(settlements, 1)
	s.True(settlements[0].Equal(dec("300")), "settlement %s", settlements[0])
}

// Terminated before the subscription started, the commitment settles its whole value on the first invoice only.
func (s *BillingServiceSuite) TestCommitmentV2_TerminatedBeforeSubscriptionStart_SettlesOnce() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, c, usage := s.settleFixture("sub_pre_sub")
	jul, aug, sep := sub.CurrentPeriodStart.AddDate(0, -1, 0), sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	c.CommitmentStatus, c.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	_, err := s.GetStores().CommitmentRepo.Update(ctx, c)
	s.Require().NoError(err)

	s.Empty(s.settlementsFor(sub, usage, jul, aug))
	got := s.settlementsFor(sub, usage, aug, sep)
	s.Require().Len(got, 1)
	s.True(got[0].Amount.Equal(dec("300")), "settlement %s", got[0].Amount)
	s.Empty(s.settlementsFor(sub, usage, sep, sep.AddDate(0, 1, 0)))
}

func TestClipCommitmentPeriod_TerminatedBeforeStartEarnsNothing(t *testing.T) {
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for _, m := range []types.CommitmentMeasurement{types.CommitmentMeasurementBillingPeriod, types.CommitmentMeasurementWindow} {
		c := &commitment.Commitment{Type: types.COMMITMENT_TYPE_AMOUNT, Value: dec("100"), Measurement: m,
			StartDate: aug, EndDate: aug.AddDate(0, 1, 0), TerminatedAt: lo.ToPtr(aug.AddDate(0, 0, -10))}
		p := commitmentPeriod{Start: aug, End: aug.AddDate(0, 1, 0), FullStart: aug, FullEnd: aug.AddDate(0, 1, 0)}
		got := clipCommitmentPeriod(c, p, *c.TerminatedAt, commitmentRates{Committed: dec("1")}, types.WindowSizeDay, aug)
		require.True(t, got.Amount.IsZero() && got.Capacity.IsZero(), "%s: amount %s", m, got.Amount)
		require.False(t, got.End.Before(got.Start))
		require.True(t, remainingValue(c, []commitmentPeriod{{Start: aug, End: aug.AddDate(0, 1, 0), FullStart: aug,
			FullEnd: aug.AddDate(0, 1, 0), Amount: dec("100")}}, commitmentRates{Committed: dec("1")}, types.WindowSizeDay, aug).Equal(dec("100")))
	}
}

func (s *BillingServiceSuite) subscriptionSvc() SubscriptionService {
	return NewSubscriptionService(s.service.(*billingService).ServiceParams)
}

func (s *BillingServiceSuite) rawUsage(from, to time.Time) *dto.GetUsageBySubscriptionResponse {
	usage, err := s.subscriptionSvc().GetUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: s.testData.subscription.ID, StartTime: from, EndTime: to,
	})
	s.Require().NoError(err)
	return usage
}

func (s *BillingServiceSuite) rawUsageCharges(usage *dto.GetUsageBySubscriptionResponse, from, to time.Time) *dto.CalculateUsageChargesResult {
	res, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
		Subscription: s.testData.subscription, Usage: usage, PeriodStart: from, PeriodEnd: to,
	})
	s.Require().NoError(err)
	return res
}

func commitmentLinesOf(lines []dto.CreateInvoiceLineItemRequest) []dto.CreateInvoiceLineItemRequest {
	return lo.Filter(lines, func(l dto.CreateInvoiceLineItemRequest, _ int) bool {
		return l.Metadata[types.MetadataKeyCommitmentLineKind] != ""
	})
}

func (s *BillingServiceSuite) TestCommitmentV2_PreviewMatchesInvoice() {
	tests := []struct {
		name  string
		setup func(periodStart time.Time)
		kinds []types.CommitmentLineKind
	}{
		{
			name: "amount commitment with true-up",
			setup: func(periodStart time.Time) {
				s.createCommitment(s.GetContext(), types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
			},
			kinds: []types.CommitmentLineKind{types.CommitmentLineWithin, types.CommitmentLineTrueUp},
		},
		{
			name: "quantity commitment with on-demand",
			setup: func(periodStart time.Time) {
				committed := s.usagePriceFor("price_parity_committed", s.testData.meters.apiCalls, "0.01")
				overage := s.usagePriceFor("price_parity_overage", s.testData.meters.apiCalls, "0.03")
				c := s.newTestCommitment(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT), s.testData.subscription,
					types.COMMITMENT_TYPE_QUANTITY, "300", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
				c.CommittedPriceID, c.OveragePriceID = lo.ToPtr(committed.ID), lo.ToPtr(overage.ID)
				s.storeCommitment(c)
			},
			kinds: []types.CommitmentLineKind{types.CommitmentLineWithin, types.CommitmentLineOnDemand},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			s.setupTestData()
			s.enableCommitmentsV2()
			periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
			tt.setup(periodStart)

			raw := s.rawUsageCharges(s.rawUsage(periodStart, periodEnd), periodStart, periodEnd)
			meterUsage, err := s.subscriptionSvc().GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: s.testData.subscription.ID, StartTime: periodStart, EndTime: periodEnd,
			})
			s.Require().NoError(err)
			lines, total, err := s.service.CalculateMeterUsageCharges(ctx, s.testData.subscription, meterUsage, periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil)
			s.Require().NoError(err)

			preview, invoice := commitmentLinesOf(raw.LineItems), commitmentLinesOf(lines)
			s.Require().Len(preview, len(tt.kinds))
			s.Require().Len(invoice, len(preview))
			for i := range preview {
				s.Equal(string(tt.kinds[i]), preview[i].Metadata[types.MetadataKeyCommitmentLineKind])
				s.Equal(invoice[i].Metadata, preview[i].Metadata)
				s.Equal(lo.FromPtr(invoice[i].CommitmentID), lo.FromPtr(preview[i].CommitmentID))
				s.Equal(lo.FromPtr(invoice[i].DisplayName), lo.FromPtr(preview[i].DisplayName))
				s.True(invoice[i].Quantity.Equal(preview[i].Quantity), "quantity %s vs %s", invoice[i].Quantity, preview[i].Quantity)
				s.True(invoice[i].Amount.Equal(preview[i].Amount), "amount %s vs %s", invoice[i].Amount, preview[i].Amount)
			}
			s.Len(raw.LineItems, len(preview), "in-scope usage is billed only through commitment lines")
			s.True(raw.TotalAmount.Equal(total), "total %s vs %s", raw.TotalAmount, total)
		})
	}
}

func (s *BillingServiceSuite) TestCommitmentV2_UsageAPIBreakdown() {
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	// $100 amount commitment; the 500 recorded api calls cost $10.
	c := s.createCommitment(s.GetContext(), types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))

	usage := s.rawUsage(periodStart, periodEnd)
	s.Require().Len(usage.Commitments, 1)
	b := usage.Commitments[0]
	s.Equal(c.ID, b.CommitmentID)
	s.Equal(c.InvoiceLabel, b.InvoiceLabel)
	s.True(b.PeriodAmount.Equal(dec("100")), "period amount %s", b.PeriodAmount)
	// The now-based fixture prorates capacity by a fraction of a second, so compare it rounded.
	s.True(types.RoundToCurrencyPrecision(b.Capacity, "usd").Equal(dec("100")), "capacity %s", b.Capacity)
	s.True(b.Used.Equal(dec("10")), "used %s", b.Used)
	s.True(b.Used.Add(b.Unused).Equal(b.Capacity), "used %s + unused %s", b.Used, b.Unused)
	s.True(b.ValueWithin.Equal(dec("10")), "value within %s", b.ValueWithin)
	s.True(b.ValueUnused.Equal(dec("90")), "value unused %s", b.ValueUnused)
	s.True(b.ValueWithin.Add(b.ValueUnused).Equal(b.PeriodAmount))
	s.True(b.BilledAmount.Equal(dec("100")), "billed %s", b.BilledAmount)

	// A mid-period window reports what the preview for that window bills.
	now := time.Now().UTC()
	partial := s.rawUsage(periodStart, now)
	s.Require().Len(partial.Commitments, 1)
	preview := s.rawUsageCharges(partial, periodStart, now)
	within := linesByKind(preview.LineItems, types.CommitmentLineWithin)
	trueUp := linesByKind(preview.LineItems, types.CommitmentLineTrueUp)
	s.Require().Len(within, 1)
	s.Require().Len(trueUp, 1)
	s.True(partial.Commitments[0].ValueWithin.Equal(within[0].Amount))
	s.True(partial.Commitments[0].ValueUnused.Equal(trueUp[0].Amount))
	s.True(partial.Commitments[0].BilledAmount.Equal(preview.TotalAmount))
}

func (s *BillingServiceSuite) TestCommitmentV2_RawPathAndUsageAPIUnchangedWithoutCommitments() {
	tests := []struct {
		name  string
		setup func(periodStart, periodEnd time.Time)
	}{
		{
			name: "flag off",
			setup: func(periodStart, _ time.Time) {
				s.createCommitment(s.GetContext(), types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
			},
		},
		{
			name: "flag on, no overlapping commitment",
			setup: func(_, periodEnd time.Time) {
				s.enableCommitmentsV2()
				s.createCommitment(s.GetContext(), types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodEnd, periodEnd.AddDate(1, 0, 0))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupTestData()
			periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
			beforeUsage := s.rawUsage(periodStart, periodEnd)
			before := s.rawUsageCharges(beforeUsage, periodStart, periodEnd)

			tt.setup(periodStart, periodEnd)
			afterUsage := s.rawUsage(periodStart, periodEnd)
			after := s.rawUsageCharges(afterUsage, periodStart, periodEnd)

			s.Nil(afterUsage.Commitments)
			s.Equal(beforeUsage, afterUsage)
			s.Equal(before, after)
			s.NotEmpty(after.LineItems)
		})
	}
}

// The raw-events path shares the meter-usage path's line-item intersection and containment guard.
func (s *BillingServiceSuite) TestCommitmentV2_RawPathScopeGuards() {
	s.Run("lines outside the call", func() {
		s.setupTestData()
		s.enableCommitmentsV2()
		periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
		s.createCommitment(s.GetContext(), types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
		callSub := *s.testData.subscription
		callSub.LineItems = lo.Filter(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
			return li.PriceID != s.testData.prices.apiCalls.ID
		})
		res, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
			Subscription: &callSub, Usage: s.usageResponse(s.testData.prices.apiCalls, "7000"), PeriodStart: periodStart, PeriodEnd: periodEnd,
		})
		s.Require().NoError(err)
		s.Empty(res.LineItems)
		s.True(res.TotalAmount.IsZero(), "total %s", res.TotalAmount)
	})
	s.Run("window across commitment periods", func() {
		s.setupTestData()
		s.enableCommitmentsV2()
		p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
		li := &subscription.SubscriptionLineItem{ID: "subs_line_flat_api", PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
		sub := s.fixedDateSub("sub_span", li)
		s.storeCommitment(s.newTestCommitment("cmt_span", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(0, 3, 0)))
		usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{Price: p, Quantity: 20, Amount: 20}}}
		res, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
			Subscription: sub, Usage: usage,
			PeriodStart: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		})
		s.Require().NoError(err)
		s.Require().Len(res.LineItems, 1)
		s.Empty(res.LineItems[0].Metadata[types.MetadataKeyCommitmentLineKind])
		s.True(res.TotalAmount.Equal(dec("20")), "total %s", res.TotalAmount)
	})
}

// recalcFixture stores an Aug 2026 subscription with $10 of usage (500 calls at $0.02), the given commitments
// on its usage price, and a draft invoice for August holding one stale usage line.
func (s *EntitlementQuantityTestSuite) recalcFixture(id string, commitments ...*commitment.Commitment) *invoicedomain.Invoice {
	ctx := s.GetContext()
	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sep := aug.AddDate(0, 1, 0)
	sub := &subscription.Subscription{
		ID: "sub_" + id, PlanID: s.testData.plan.ID, CustomerID: s.testData.customer.ID, StartDate: aug,
		BillingAnchor: aug, CurrentPeriodStart: aug, CurrentPeriodEnd: sep, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := *s.testData.usageSubLineItem
	li.ID, li.SubscriptionID, li.StartDate = "subs_line_"+id, sub.ID, aug
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{&li}))

	records := make([]*events.MeterUsage, 0, 500)
	for i := 0; i < 500; i++ {
		muID := types.GenerateUUIDWithPrefix("mu")
		ts := aug.Add(24 * time.Hour)
		records = append(records, &events.MeterUsage{
			Event: events.Event{ID: muID, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: s.testData.meter.EventName, CustomerID: s.testData.customer.ID,
				ExternalCustomerID: s.testData.customer.ExternalID, Timestamp: ts, IngestedAt: ts},
			MeterID: s.testData.meter.ID, QtyTotal: decimal.NewFromInt(1), UniqueHash: muID,
		})
	}
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))

	for _, c := range commitments {
		c.SubscriptionID, c.CustomerID, c.ScopeEntityID = sub.ID, sub.CustomerID, li.PriceID
		c.StartDate, c.EndDate = aug, aug.AddDate(0, 3, 0)
		_, err := s.GetStores().CommitmentRepo.Create(ctx, c)
		s.Require().NoError(err)
	}

	bp := string(types.BILLING_PERIOD_MONTHLY)
	inv := &invoicedomain.Invoice{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE), CustomerID: sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID), InvoiceType: types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusDraft, PaymentStatus: types.PaymentStatusPending, Currency: "usd",
		BillingPeriod: &bp, PeriodStart: &aug, PeriodEnd: &sep, BaseModel: types.GetDefaultBaseModel(ctx),
		LineItems: []*invoicedomain.InvoiceLineItem{{
			ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM), CustomerID: sub.CustomerID,
			MeterID: lo.ToPtr(s.testData.meter.ID), PriceID: lo.ToPtr(li.PriceID), DisplayName: lo.ToPtr("stale"),
			Amount: dec("1"), Quantity: dec("1"), Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx),
			PeriodStart: &aug, PeriodEnd: &sep, SubscriptionLineItemID: lo.ToPtr(li.ID),
			BaseModel: types.GetDefaultBaseModel(ctx),
		}},
	}
	s.Require().NoError(s.invoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

func (s *EntitlementQuantityTestSuite) recalcCommitment(id string, value string, priority int) *commitment.Commitment {
	ctx := s.GetContext()
	return &commitment.Commitment{
		ID: id, Name: id, InvoiceLabel: id, Currency: "usd", ScopeEntityType: types.CommitmentScopePrice,
		Type: types.COMMITMENT_TYPE_AMOUNT, Value: dec(value), Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone: "UTC", Priority: priority, Binding: true, CollectionMethod: lo.ToPtr(types.CommitmentCollectionPerPeriod),
		CommitmentStatus: types.CommitmentStatusActive, OnEarlyTermination: lo.ToPtr(types.CommitmentTerminationForfeit),
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

// requireStableRecalculation recalculates the draft three times and checks the stored lines stay the same.
func (s *EntitlementQuantityTestSuite) requireStableRecalculation(inv *invoicedomain.Invoice, wantLines int, wantTotal string) {
	ctx := s.GetContext()
	for i := 0; i < 3; i++ {
		_, err := s.invoiceSvc.RecalculateInvoiceV2(ctx, inv.ID, false)
		s.Require().NoError(err, "recalculation %d", i+1)

		// GetForUpdate reads the published line-item rows, as the database does.
		stored, err := s.invoiceRepo.GetForUpdate(ctx, inv.ID)
		s.Require().NoError(err)
		sum := decimal.Zero
		for _, l := range stored.LineItems {
			sum = sum.Add(l.Amount)
		}
		s.Require().Len(stored.LineItems, wantLines, "recalculation %d line count", i+1)
		s.True(sum.Equal(stored.Total), "recalculation %d: lines %s, total %s", i+1, sum, stored.Total)
		s.True(stored.Total.Equal(dec(wantTotal)), "recalculation %d: total %s", i+1, stored.Total)
	}
}

func (s *EntitlementQuantityTestSuite) TestCommitmentV2_RecalculateDraft_TrueUpStaysStable() {
	inv := s.recalcFixture("recalc_trueup", s.recalcCommitment("cmt_recalc_100", "100", 1))
	// Within $10 plus a $90 true-up.
	s.requireStableRecalculation(inv, 2, "100")
}

func (s *EntitlementQuantityTestSuite) TestCommitmentV2_RecalculateDraft_StackedStaysStable() {
	inv := s.recalcFixture("recalc_stack",
		s.recalcCommitment("cmt_recalc_4", "4", 1), s.recalcCommitment("cmt_recalc_3", "3", 2))
	// Within $4 and $3 on the same line item, plus $3 on-demand.
	s.requireStableRecalculation(inv, 3, "10")
}
