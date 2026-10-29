package service

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/events"
	invoicedomain "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *BillingServiceSuite) enableCommitmentsV2() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), true)
}

// newTestCommitment returns a binding per_period commitment scoped to one price, active from start to end.
func (s *BillingServiceSuite) newTestCommitment(
	id string,
	sub *subscription.Subscription,
	typ types.CommitmentType,
	value string,
	priceID string,
	start time.Time,
	end time.Time,
) *commitment.Commitment {
	ctx := s.GetContext()
	return &commitment.Commitment{
		ID:                 id,
		SubscriptionID:     sub.ID,
		CustomerID:         sub.CustomerID,
		Name:               id,
		InvoiceLabel:       "Commitment " + id,
		Currency:           sub.Currency,
		ScopeEntityType:    types.CommitmentScopePrice,
		ScopeEntityID:      priceID,
		Type:               typ,
		Value:              dec(value),
		Measurement:        types.CommitmentMeasurementBillingPeriod,
		Timezone:           "UTC",
		Priority:           1,
		Binding:            true,
		CollectionMethod:   lo.ToPtr(types.CommitmentCollectionPerPeriod),
		StartDate:          start,
		EndDate:            lo.ToPtr(end),
		CommitmentStatus:   types.CommitmentStatusActive,
		OnEarlyTermination: lo.ToPtr(types.CommitmentTerminationForfeit),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

func (s *BillingServiceSuite) storeCommitment(c *commitment.Commitment) {
	_, err := s.GetStores().CommitmentRepo.Create(s.GetContext(), c)
	s.Require().NoError(err)
}

func (s *BillingServiceSuite) createCommitment(
	typ types.CommitmentType,
	value string,
	priceID string,
	start time.Time,
	end time.Time,
) *commitment.Commitment {
	c := s.newTestCommitment(
		types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT),
		s.testData.subscription,
		typ,
		value,
		priceID,
		start,
		end,
	)
	s.storeCommitment(c)
	return c
}

// usageResponse returns one charge for the testData line item on p, with qty priced at p.
func (s *BillingServiceSuite) usageResponse(p *price.Price, qty string) *dto.GetUsageBySubscriptionResponse {
	li, ok := lo.Find(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceID == p.ID
	})
	s.Require().True(ok)
	amount := NewPriceService(s.service.(*billingService).ServiceParams).CalculateCost(s.GetContext(), p, dec(qty))
	return &dto.GetUsageBySubscriptionResponse{
		Currency: s.testData.subscription.Currency,
		Charges: []*dto.SubscriptionUsageByMetersResponse{{
			SubscriptionLineItemID: li.ID,
			Price:                  p,
			Quantity:               dec(qty).InexactFloat64(),
			Amount:                 amount.InexactFloat64(),
		}},
	}
}

// loaderFor builds the loader for the full period with every bucket elapsed.
func (s *BillingServiceSuite) loaderFor(
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	from, to time.Time,
) *commitmentLoader {
	l, err := s.service.(*billingService).newCommitmentLoader(
		s.GetContext(),
		sub,
		from,
		to,
		to,
		rawChargesByLine(sub, usage),
		nil,
		types.UsageSourceInvoiceCreation,
	)
	s.Require().NoError(err)
	return l
}

func loaderLinesOfKind(lines []dto.CreateInvoiceLineItemRequest, kind types.CommitmentLineKind) []dto.CreateInvoiceLineItemRequest {
	return lo.Filter(lines, func(l dto.CreateInvoiceLineItemRequest, _ int) bool {
		return l.Metadata[types.MetadataKeyCommitmentLineKind] == string(kind)
	})
}

func (s *BillingServiceSuite) loaderLine(
	lines []dto.CreateInvoiceLineItemRequest,
	kind types.CommitmentLineKind,
	commitmentID string,
) dto.CreateInvoiceLineItemRequest {
	found, ok := lo.Find(loaderLinesOfKind(lines, kind), func(l dto.CreateInvoiceLineItemRequest) bool {
		return lo.FromPtr(l.CommitmentID) == commitmentID
	})
	s.Require().True(ok, "no %s line for %s", kind, commitmentID)
	return found
}

// fixedDateSub stores an August 2026 monthly subscription for the test customer with the given usage lines.
func (s *BillingServiceSuite) fixedDateSub(
	id string,
	lines ...*subscription.SubscriptionLineItem,
) *subscription.Subscription {
	ctx := s.GetContext()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		ID:                 id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          start,
		BillingAnchor:      start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   start.AddDate(0, 1, 0),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
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
		ID:                 id,
		Amount:             dec(amount),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	if m != nil {
		p.MeterID = m.ID
	}

	s.Require().NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
	return p
}

func (s *BillingServiceSuite) subscriptionSvc() SubscriptionService {
	return NewSubscriptionService(s.service.(*billingService).ServiceParams)
}

// countingMeterUsageRepo counts the meter_usage reads a billing run makes.
type countingMeterUsageRepo struct {
	events.MeterUsageRepository
	reads int
}

func (r *countingMeterUsageRepo) GetUsage(
	ctx context.Context,
	params *events.MeterUsageQueryParams,
) (*events.MeterUsageAggregationResult, error) {
	r.reads++
	return r.MeterUsageRepository.GetUsage(ctx, params)
}

func (r *countingMeterUsageRepo) GetUsageForBucketedMeters(
	ctx context.Context,
	params *events.MeterUsageQueryParams,
) (*events.AggregationResult, error) {
	r.reads++
	return r.MeterUsageRepository.GetUsageForBucketedMeters(ctx, params)
}

// seedAPICalls records n api calls at `at`, both as raw events and as meter_usage rows.
func (s *BillingServiceSuite) seedAPICalls(sub *subscription.Subscription, at time.Time, n int) {
	ctx := s.GetContext()
	for i := 0; i < n; i++ {
		event := events.Event{
			ID:                 s.GetUUID(),
			TenantID:           sub.TenantID,
			EnvironmentID:      sub.EnvironmentID,
			EventName:          s.testData.meters.apiCalls.EventName,
			ExternalCustomerID: s.testData.customer.ExternalID,
			CustomerID:         sub.CustomerID,
			Timestamp:          at,
			IngestedAt:         at,
			Properties:         map[string]interface{}{},
		}
		s.Require().NoError(s.eventRepo.InsertEvent(ctx, &event))
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event:      event,
			MeterID:    s.testData.meters.apiCalls.ID,
			QtyTotal:   decimal.NewFromInt(1),
			UniqueHash: event.ID,
		}}))
	}
}

// apiFixture stores an Aug 2026 subscription with a flat $1 api line, 20 calls on 5 Aug and 20 on 20 Aug.
func (s *BillingServiceSuite) apiFixture(id string) (*subscription.Subscription, *price.Price) {
	p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{
		ID:          "subs_line_flat_api",
		PriceID:     p.ID,
		MeterID:     s.testData.meters.apiCalls.ID,
		DisplayName: "API",
	}
	sub := s.fixedDateSub(id, li)
	s.seedAPICalls(sub, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), 20)
	s.seedAPICalls(sub, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), 20)
	return sub, p
}

// meterUsage returns the meter_usage-backed usage of the whole period, as an internal (billing) caller: it
// feeds charges into loader tests, so it must not itself run the commitment breakdown.
func (s *BillingServiceSuite) meterUsage(sub *subscription.Subscription) *dto.GetUsageBySubscriptionResponse {
	usage, err := s.subscriptionSvc().GetMeterUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      sub.CurrentPeriodStart,
		EndTime:        sub.CurrentPeriodEnd,
		Source:         string(types.UsageSourceInvoiceCreation),
	})
	s.Require().NoError(err)
	return usage
}

// rawUsage returns the raw-events usage of the whole period.
func (s *BillingServiceSuite) rawUsage(sub *subscription.Subscription) *dto.GetUsageBySubscriptionResponse {
	usage, err := s.subscriptionSvc().GetUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      sub.CurrentPeriodStart,
		EndTime:        sub.CurrentPeriodEnd,
	})
	s.Require().NoError(err)
	return usage
}

func (s *BillingServiceSuite) TestCommitmentLoader_AmountTrueUp() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	// $100 amount commitment on the api-calls price; 7000 calls cost $60 on the slab tiers.
	c := s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT,
		"100",
		s.testData.prices.apiCalls.ID,
		periodStart,
		periodStart.AddDate(1, 0, 0),
	)
	l := s.loaderFor(s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd)
	apiLine, _ := lo.Find(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceID == s.testData.prices.apiCalls.ID
	})
	s.Equal(map[string]bool{apiLine.ID: true}, l.HandledLineIDs())

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.Len(lines, 2)
	within := loaderLinesOfKind(lines, types.CommitmentLineUtilized)
	s.Require().Len(within, 1)
	s.True(within[0].Amount.Equal(dec("60")), "within %s", within[0].Amount)
	s.True(within[0].Quantity.Equal(dec("7000")))
	s.Equal(c.ID, lo.FromPtr(within[0].CommitmentID))
	trueUp := loaderLinesOfKind(lines, types.CommitmentLineTrueUp)
	s.Require().Len(trueUp, 1)
	s.True(trueUp[0].Amount.Equal(dec("40")), "true-up %s", trueUp[0].Amount)
	s.True(total.Equal(dec("100")), "total %s", total)

	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	s.Require().Len(allocs, 1)
	s.Require().Len(stacks[0].Lines[0].Usage, 1)
	s.True(stacks[0].Lines[0].Usage[0].Equal(dec("60")))
}

func (s *BillingServiceSuite) TestCommitmentLoader_FlagOff_Empty() {
	s.setupTestData()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT,
		"100",
		s.testData.prices.apiCalls.ID,
		periodStart,
		periodStart.AddDate(1, 0, 0),
	)
	l := s.loaderFor(s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd)
	s.Empty(l.HandledLineIDs())
	lines, total, err := l.InvoiceLines(s.GetContext())
	s.Require().NoError(err)
	s.Empty(lines)
	s.True(total.IsZero())
}

func (s *BillingServiceSuite) TestCommitmentLoader_NoOverlap_Empty() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	// Starts after the period, and one terminated before it began.
	s.createCommitment(types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodEnd, periodEnd.AddDate(1, 0, 0))
	old := s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT,
		"100",
		s.testData.prices.apiCalls.ID,
		periodStart.AddDate(0, -2, 0),
		periodEnd.AddDate(1, 0, 0),
	)
	old.CommitmentStatus = types.CommitmentStatusTerminated
	old.TerminatedAt = lo.ToPtr(periodStart.AddDate(0, 0, -1))
	_, err := s.GetStores().CommitmentRepo.Update(ctx, old)
	s.Require().NoError(err)

	l := s.loaderFor(s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd)
	s.Empty(l.HandledLineIDs())
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Empty(stacks)
	s.Empty(allocs)
}

// A call that does not carry an in-scope line (another cadence group, or an already-invoiced arrear item) leaves it alone.
func (s *BillingServiceSuite) TestCommitmentLoader_LinesOutsideTheCallAreNotHandled() {
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT,
		"100",
		s.testData.prices.apiCalls.ID,
		periodStart,
		periodStart.AddDate(1, 0, 0),
	)
	callSub := *s.testData.subscription
	callSub.LineItems = lo.Filter(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceID != s.testData.prices.apiCalls.ID
	})
	l := s.loaderFor(&callSub, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd)
	s.Empty(l.HandledLineIDs())
	lines, total, err := l.InvoiceLines(s.GetContext())
	s.Require().NoError(err)
	s.Empty(lines)
	s.True(total.IsZero(), "total %s", total)
}

// A window that is not inside one commitment period never gets the commitment's capacity or true-up.
func (s *BillingServiceSuite) TestCommitmentLoader_WindowAcrossCommitmentPeriodsIsSkipped() {
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{
		ID:          "subs_line_flat_api",
		PriceID:     p.ID,
		MeterID:     s.testData.meters.apiCalls.ID,
		DisplayName: "API",
	}
	sub := s.fixedDateSub("sub_span", li)
	s.storeCommitment(s.newTestCommitment(
		"cmt_span",
		sub,
		types.COMMITMENT_TYPE_AMOUNT,
		"100",
		p.ID,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodStart.AddDate(0, 3, 0),
	))
	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID,
		Price:                  p,
		Quantity:               20,
		Amount:                 20,
	}}}
	from, to := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	l := s.loaderFor(sub, usage, from, to)
	s.Empty(l.HandledLineIDs())
	lines, _, err := l.InvoiceLines(s.GetContext())
	s.Require().NoError(err)
	s.Empty(lines)
}

func (s *BillingServiceSuite) TestCommitmentLoader_StackedWindowed_Simplismart() {
	// Stacked GPU reservations at reduced scale: per-minute meter; C1 = 8 all day (p1, 0.048); C2 = 32, 09:00-21:00 IST from 25 Aug (p2, 0.0491667).
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()

	gpuMeter := &meter.Meter{
		ID:        "meter_gpu_minutes",
		Name:      "GPU minutes",
		EventName: "gpu",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "gpus",
			BucketSize: types.WindowSizeMinute,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, gpuMeter))
	h100 := s.usagePriceFor("price_h100", gpuMeter, "0.06")
	li := &subscription.SubscriptionLineItem{
		ID:               "subs_line_h100",
		PriceID:          h100.ID,
		MeterID:          gpuMeter.ID,
		MeterDisplayName: gpuMeter.Name,
		DisplayName:      "H100",
	}
	sub := s.fixedDateSub("sub_e9", li)
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd

	ist, err := time.LoadLocation("Asia/Kolkata")
	s.Require().NoError(err)
	insert := func(at time.Time, gpus int64) {
		id := s.GetUUID()
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event: events.Event{
				ID:                 id,
				TenantID:           sub.TenantID,
				EnvironmentID:      sub.EnvironmentID,
				EventName:          gpuMeter.EventName,
				ExternalCustomerID: s.testData.customer.ExternalID,
				CustomerID:         sub.CustomerID,
				Timestamp:          at.UTC(),
				IngestedAt:         at.UTC(),
			},
			MeterID:    gpuMeter.ID,
			QtyTotal:   decimal.NewFromInt(gpus),
			UniqueHash: id,
		}}))
	}

	insert(time.Date(2026, 8, 25, 3, 0, 0, 0, ist), 10)
	insert(time.Date(2026, 8, 25, 10, 0, 0, 0, ist), 45)
	insert(time.Date(2026, 8, 25, 11, 0, 0, 0, ist), 20)

	p1 := s.usagePriceFor("price_c1_committed", gpuMeter, "0.048")
	p2 := s.usagePriceFor("price_c2_committed", gpuMeter, "0.0491667")
	c1 := s.newTestCommitment("cmt_c1", sub, types.COMMITMENT_TYPE_QUANTITY, "8", h100.ID, periodStart, periodEnd)
	c1.Measurement, c1.CommittedPriceID = types.CommitmentMeasurementWindow, lo.ToPtr(p1.ID)
	c2 := s.newTestCommitment(
		"cmt_c2",
		sub,
		types.COMMITMENT_TYPE_QUANTITY,
		"32",
		h100.ID,
		time.Date(2026, 8, 25, 0, 0, 0, 0, ist),
		periodEnd,
	)
	c2.Measurement, c2.CommittedPriceID, c2.Priority = types.CommitmentMeasurementWindow, lo.ToPtr(p2.ID), 2
	c2.Timezone = "Asia/Kolkata"
	c2.ActiveWindows = types.ActiveWindows{
		{
			Start: types.Bucket{Hour: 9},
			End:   types.Bucket{Hour: 21},
		},
	}
	s.storeCommitment(c1)
	s.storeCommitment(c2)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID,
		Price:                  h100,
		Quantity:               75,
	}}}
	lines, total, err := s.loaderFor(sub, usage, periodStart, periodEnd).InvoiceLines(ctx)
	s.Require().NoError(err)

	s.True(s.loaderLine(lines, types.CommitmentLineUtilized, c1.ID).Quantity.Equal(dec("24")))
	s.True(s.loaderLine(lines, types.CommitmentLineUtilized, c2.ID).Quantity.Equal(dec("44")))
	overage := loaderLinesOfKind(lines, types.CommitmentLineOverage)
	s.Require().Len(overage, 1)
	s.True(overage[0].Quantity.Equal(dec("7")))
	s.True(overage[0].Amount.Equal(dec("0.42")))
	// C1 runs all 44,640 August minutes; C2 runs 7 days x 720 minutes from 25 Aug IST.
	c1TrueUp := s.loaderLine(lines, types.CommitmentLineTrueUp, c1.ID)
	s.True(c1TrueUp.Quantity.Equal(dec("357096")), "c1 unused %s", c1TrueUp.Quantity)
	c2TrueUp := s.loaderLine(lines, types.CommitmentLineTrueUp, c2.ID)
	s.True(c2TrueUp.Quantity.Equal(dec("161236")), "c2 unused %s", c2TrueUp.Quantity)
	c1Period := s.loaderLine(lines, types.CommitmentLineUtilized, c1.ID).Amount.Add(c1TrueUp.Amount)
	s.True(c1Period.Equal(dec("17141.76")), "c1 period %s", c1Period)
	sum := decimal.Zero
	for _, l := range lines {
		sum = sum.Add(l.Amount)
	}

	s.True(total.Equal(sum))
}

// A commitment terminated mid-period still allocates up to terminated_at; its replacement takes over after.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodSplitAtReplacement() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, p := s.apiFixture("sub_replacement")
	switchAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd

	committed := s.usagePriceFor("price_replacement_committed", s.testData.meters.apiCalls, "0.5")
	// Overlapping commitments in a stack share one overage rate.
	overagePrice := s.usagePriceFor("price_replacement_overage", s.testData.meters.apiCalls, "2")
	newCommitment := func(id string, start time.Time) *commitment.Commitment {
		c := s.newTestCommitment(id, sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, start, periodEnd)
		c.CommittedPriceID = lo.ToPtr(committed.ID)
		c.OveragePriceID = lo.ToPtr(overagePrice.ID)
		return c
	}

	a := newCommitment("cmt_a", periodStart)
	a.CommitmentStatus = types.CommitmentStatusTerminated
	a.TerminatedAt = lo.ToPtr(switchAt)
	s.storeCommitment(a)
	s.storeCommitment(newCommitment("cmt_b", switchAt))

	// A holds 14 units (1-15 Aug of 31 days) and B 17 (15 Aug-1 Sep); each half's 20 calls fill its own commitment.
	check := func(l *commitmentLoader) []dto.CreateInvoiceLineItemRequest {
		lines, total, err := l.InvoiceLines(ctx)
		s.Require().NoError(err)
		s.True(s.loaderLine(lines, types.CommitmentLineUtilized, "cmt_a").Amount.Equal(dec("7")))
		s.True(s.loaderLine(lines, types.CommitmentLineUtilized, "cmt_b").Amount.Equal(dec("8.5")))
		// One overage line per usage line: 6 from A's half and 3 from B's, at the shared $2 overage.
		overage := loaderLinesOfKind(lines, types.CommitmentLineOverage)
		s.Require().Len(overage, 1)
		s.True(overage[0].Quantity.Round(6).Equal(dec("9")), "overage %s", overage[0].Quantity)
		s.True(overage[0].Amount.Equal(dec("18")), "overage %s", overage[0].Amount)
		s.True(total.Equal(dec("33.5")), "total %s", total)
		return lines
	}

	check(s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd))
}

func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodSplitQueries() {
	tests := []struct {
		name      string
		switchAt  *time.Time
		wantReads int
	}{
		{
			name:      "no commitment boundary inside the period reads nothing",
			wantReads: 0,
		},
		{
			name:      "a boundary inside the period reads the line once for the first half",
			switchAt:  lo.ToPtr(time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)),
			wantReads: 1,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupTestData()
			s.enableCommitmentsV2()
			sub, p := s.apiFixture("sub_queries")
			periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
			if tt.switchAt == nil {
				s.storeCommitment(s.newTestCommitment("cmt_a", sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, periodStart, periodEnd))
			} else {
				a := s.newTestCommitment("cmt_a", sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, periodStart, periodEnd)
				a.CommitmentStatus, a.TerminatedAt = types.CommitmentStatusTerminated, tt.switchAt
				s.storeCommitment(a)
				s.storeCommitment(s.newTestCommitment("cmt_b", sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, *tt.switchAt, periodEnd))
			}

			usage := s.meterUsage(sub)
			counter := &countingMeterUsageRepo{MeterUsageRepository: s.GetStores().MeterUsageRepo}
			params := s.service.(*billingService).ServiceParams
			params.MeterUsageRepo = counter
			l, err := NewBillingService(params).(*billingService).newCommitmentLoader(
				s.GetContext(), sub, periodStart, periodEnd, periodEnd, rawChargesByLine(sub, usage), nil,
				types.UsageSourceInvoiceCreation,
			)
			s.Require().NoError(err)
			lines, _, err := l.InvoiceLines(s.GetContext())
			s.Require().NoError(err)

			s.NotEmpty(lines)
			s.Equal(tt.wantReads, counter.reads)
		})
	}
}

// A billing-period commitment starting mid-period allocates only the usage after its start.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodStartsMidPeriod() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, p := s.apiFixture("sub_mid")
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	startAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	committed := s.usagePriceFor("price_mid_committed", s.testData.meters.apiCalls, "0.5")
	// 62 over the whole month is 34 for the 17 days from 15 Aug, more than the 20 calls made in them.
	c := s.newTestCommitment("cmt_mid", sub, types.COMMITMENT_TYPE_QUANTITY, "62", p.ID, startAt, periodEnd)
	c.CommittedPriceID = lo.ToPtr(committed.ID)
	s.storeCommitment(c)

	l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd)
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	s.Equal([]time.Time{periodStart, startAt}, stacks[0].Windows)
	s.Equal([]int{-1, 0}, stacks[0].Commitments[0].PoolOf)
	s.Equal([]decimal.Decimal{dec("20"), dec("20")}, stacks[0].Lines[0].Usage)
	s.True(allocs[0].Commitments[0].Used.Equal(dec("20")), "used %s", allocs[0].Commitments[0].Used)
	s.True(allocs[0].Commitments[0].Capacity.Round(6).Equal(dec("34")), "capacity %s", allocs[0].Commitments[0].Capacity)
	s.True(allocs[0].Overage[0][0].Equal(dec("20")), "pre-start usage stays overage")
	s.True(allocs[0].Overage[0][1].IsZero())

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.True(s.loaderLine(lines, types.CommitmentLineUtilized, c.ID).Quantity.Equal(dec("20")))
	s.True(loaderLinesOfKind(lines, types.CommitmentLineOverage)[0].Quantity.Equal(dec("20")))
	// 20 within at $0.5, true-up of the $7 unused, and 20 overage at the $1 price.
	s.True(total.Equal(dec("37")), "total %s", total)
}

// A billing-period commitment ending mid-period allocates only the usage before its end; the rest bills as overage.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodEndsMidPeriod() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, p := s.apiFixture("sub_mid_end")
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	endAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	committed := s.usagePriceFor("price_mid_end_committed", s.testData.meters.apiCalls, "0.5")
	// 62 over the whole month is 28 for the 14 days to 15 Aug, more than the 20 calls made in them.
	c := s.newTestCommitment("cmt_mid_end", sub, types.COMMITMENT_TYPE_QUANTITY, "62", p.ID, periodStart, endAt)
	c.CommittedPriceID = lo.ToPtr(committed.ID)
	s.storeCommitment(c)

	l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd)
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	s.False(stacks[0].Commitments[0].Period.Clipped)
	s.Equal([]time.Time{periodStart, endAt}, stacks[0].Windows)
	s.Equal([]int{0, -1}, stacks[0].Commitments[0].PoolOf)
	s.True(allocs[0].Commitments[0].Used.Equal(dec("20")), "used %s", allocs[0].Commitments[0].Used)
	s.True(allocs[0].Commitments[0].Capacity.Round(6).Equal(dec("28")), "capacity %s", allocs[0].Commitments[0].Capacity)
	s.True(allocs[0].Overage[0][0].IsZero())
	s.True(allocs[0].Overage[0][1].Equal(dec("20")), "post-end usage stays overage")

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.True(s.loaderLine(lines, types.CommitmentLineUtilized, c.ID).Quantity.Equal(dec("20")))
	s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID).Amount.Equal(dec("4")))
	s.True(loaderLinesOfKind(lines, types.CommitmentLineOverage)[0].Quantity.Equal(dec("20")))
	// 20 within at $0.5, true-up of the $4 unused, and 20 overage at the $1 price.
	s.True(total.Equal(dec("34")), "total %s", total)
}

// One pool spans every span a commitment applies to, so usage before a later commitment starts draws it down.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodPoolsAcrossTheSplit() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, p := s.apiFixture("sub_pool")
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	startAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	committed := s.usagePriceFor("price_pool_committed", s.testData.meters.apiCalls, "0.5")
	x := s.newTestCommitment("cmt_x", sub, types.COMMITMENT_TYPE_QUANTITY, "25", p.ID, periodStart, periodEnd)
	x.CommittedPriceID = lo.ToPtr(committed.ID)
	y := s.newTestCommitment("cmt_y", sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, startAt, periodEnd)
	y.CommittedPriceID, y.Priority = lo.ToPtr(committed.ID), 2
	s.storeCommitment(x)
	s.storeCommitment(y)

	l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd)
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	s.Len(stacks[0].Windows, 2)
	px, py := stacks[0].Commitments[0], stacks[0].Commitments[1]
	s.Equal("cmt_x", px.C.ID)
	s.Len(px.Pools, 1)
	s.Equal([]int{0, 0}, px.PoolOf)
	s.Equal([]int{-1, 0}, py.PoolOf)

	// X's 25 serve 20 before the split and 5 after; Y takes the other 15 of the 17 it holds.
	s.True(allocs[0].Commitments[0].Used.Round(6).Equal(dec("25")), "X used %s", allocs[0].Commitments[0].Used)
	s.True(allocs[0].Commitments[1].Used.Round(6).Equal(dec("15")), "Y used %s", allocs[0].Commitments[1].Used)
	s.True(allocs[0].Commitments[1].Unused.Round(6).Equal(dec("2")), "Y unused %s", allocs[0].Commitments[1].Unused)

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.Empty(loaderLinesOfKind(lines, types.CommitmentLineOverage))
	s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, y.ID).Quantity.Round(6).Equal(dec("2")))
	// X 12.5 + Y 7.5 within + Y true-up 1.
	s.True(total.Equal(dec("21")), "total %s", total)
}

// A paused and resumed subscription invoices one period in two parts; each bills its share of the floor.
func (s *BillingServiceSuite) TestCommitmentLoader_PartialInvoicesSumToThePeriodFloor() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_flat_api", PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub("sub_partial", li)
	c := s.newTestCommitment("cmt_partial", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(0, 3, 0))
	s.storeCommitment(c)

	split := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		from, to time.Time
		want     string
	}{
		{from: sub.CurrentPeriodStart, to: split, want: "48.39"},
		{from: split, to: sub.CurrentPeriodEnd, want: "51.61"},
		{from: sub.CurrentPeriodStart, to: sub.CurrentPeriodEnd, want: "100"},
	}

	for _, tt := range tests {
		l := s.loaderFor(sub, &dto.GetUsageBySubscriptionResponse{Currency: "usd"}, tt.from, tt.to)
		lines, total, err := l.InvoiceLines(ctx)
		s.Require().NoError(err)
		s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID).Amount.Equal(dec(tt.want)), "window %s-%s", tt.from, tt.to)
		s.True(total.Equal(dec(tt.want)), "total %s for %s-%s", total, tt.from, tt.to)
	}
}

// A $100 floor ending 15 Oct is worth 14 of October's 31 days, $45.16; invoicing October in two parts bills it exactly.
func (s *BillingServiceSuite) TestCommitmentLoader_PartialInvoicesSumToTheLastPeriodFloor() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_flat_api", PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub("sub_last_partial", li)
	oct, nov := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := s.newTestCommitment("cmt_last_partial", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	s.storeCommitment(c)

	split := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		from, to time.Time
		want     string
	}{
		{from: oct, to: split, want: "29.03"},
		{from: split, to: nov, want: "16.13"},
		{from: oct, to: nov, want: "45.16"},
	}

	for _, tt := range tests {
		l := s.loaderFor(sub, &dto.GetUsageBySubscriptionResponse{Currency: "usd"}, tt.from, tt.to)
		lines, total, err := l.InvoiceLines(ctx)
		s.Require().NoError(err)
		s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID).Amount.Equal(dec(tt.want)), "window %s-%s", tt.from, tt.to)
		s.True(total.Equal(dec(tt.want)), "total %s for %s-%s", total, tt.from, tt.to)
	}
}

// istDailyWindowFixture stores an Asia/Kolkata subscription billed from 1 Aug IST with a daily-bucket line under a
// 10-a-day window commitment, and returns usage keyed the way ClickHouse buckets raw events: at IST midnight.
func (s *BillingServiceSuite) istDailyWindowFixture() (*subscription.Subscription, *commitment.Commitment, *dto.GetUsageBySubscriptionResponse) {
	ctx := s.GetContext()
	ist, err := time.LoadLocation("Asia/Kolkata")
	s.Require().NoError(err)

	dayMeter := &meter.Meter{
		ID:          "meter_ist_daily",
		Name:        "Daily GPU",
		EventName:   "ist_daily",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeDay},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, dayMeter))
	p := s.usagePriceFor("price_ist_daily", dayMeter, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_ist_daily", PriceID: p.ID, MeterID: dayMeter.ID, DisplayName: "GPU"}
	sub := s.fixedDateSub("sub_ist_daily", li)

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, ist).UTC()
	sub.Timezone, sub.StartDate, sub.BillingAnchor = "Asia/Kolkata", start, start
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = start, time.Date(2026, 9, 1, 0, 0, 0, 0, ist).UTC()
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
	li.StartDate = start
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))

	committed := s.usagePriceFor("price_ist_daily_committed", dayMeter, "0.5")
	c := s.newTestCommitment("cmt_ist_daily", sub, types.COMMITMENT_TYPE_QUANTITY, "10", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	c.Measurement, c.CommittedPriceID = types.CommitmentMeasurementWindow, lo.ToPtr(committed.ID)
	s.storeCommitment(c)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID,
		Price:                  p,
		BucketedUsageResult: &events.AggregationResult{Results: []events.UsageResult{
			{WindowSize: time.Date(2026, 8, 5, 0, 0, 0, 0, ist), Value: dec("15")},
			{WindowSize: time.Date(2026, 8, 20, 0, 0, 0, 0, ist), Value: dec("4")},
		}},
	}}}
	return sub, c, usage
}

// meter_usage buckets in UTC, so the grid is UTC days; charges keyed at IST midnight join the UTC day containing them.
func (s *BillingServiceSuite) TestCommitmentLoader_DailyGridIsUTCForChargesKeyedInSubscriptionTimezone() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, c, usage := s.istDailyWindowFixture()

	stacks, allocs, err := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd).Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	s.Len(stacks[0].Windows, 32)
	s.True(stacks[0].Windows[4].Equal(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)), "5 Aug 00:00 IST is in window %s", stacks[0].Windows[4])
	s.True(stacks[0].Lines[0].Usage[4].Equal(dec("15")))
	s.True(stacks[0].Lines[0].Usage[19].Equal(dec("4")))

	// 10 of 5 Aug's 15 and all 4 of 20 Aug's fall within the commitment; the other 5 are overage.
	s.True(allocs[0].Commitments[0].Used.Equal(dec("14")), "used %s", allocs[0].Commitments[0].Used)
	s.True(allocs[0].Overage[0][4].Equal(dec("5")))
	s.True(allocs[0].Commitments[0].Capacity.Equal(dec("310")), "capacity %s", allocs[0].Commitments[0].Capacity)
	s.Equal(c.ID, stacks[0].Commitments[0].C.ID)
}

// Terminated at 20:00 UTC on 9 Aug, the UTC grid holds nine daily buckets from the period start;
// the floor follows the pools, so the true-up is exactly the unused capacity.
func (s *BillingServiceSuite) TestCommitmentLoader_WindowFloorMatchesPoolCapacity() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, c, usage := s.istDailyWindowFixture()
	c.CommitmentStatus, c.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC))
	_, err := s.GetStores().CommitmentRepo.Update(ctx, c)
	s.Require().NoError(err)

	l := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	_, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.True(allocs[0].Commitments[0].Capacity.Equal(dec("90")), "capacity %s", allocs[0].Commitments[0].Capacity)
	s.True(allocs[0].Commitments[0].Used.Equal(dec("10")))

	lines, _, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	within, trueUp := s.loaderLine(lines, types.CommitmentLineUtilized, c.ID), s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID)
	s.True(trueUp.Quantity.Equal(dec("80")), "true-up quantity %s", trueUp.Quantity)
	s.True(trueUp.Amount.Equal(dec("40")), "true-up %s", trueUp.Amount)
	s.True(within.Amount.Add(trueUp.Amount).Equal(dec("45")), "floor %s", within.Amount.Add(trueUp.Amount))
}

// An instant off the grid lands in the window containing it; one past the elapsed period is left out.
func (s *BillingServiceSuite) TestCommitmentLoader_BucketsJoinTheWindowContainingThem() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, _, usage := s.istDailyWindowFixture()

	// meter_usage buckets in UTC, so its grid is UTC days and the IST-midnight instants sit 5h30 before them.
	l := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	l.asOf = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	windows, got := stacks[0].Windows, stacks[0].Lines[0].Usage
	s.True(windows[0].Equal(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)), "first window %s", windows[0])
	s.True(windows[4].Equal(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)))
	s.True(got[4].Equal(dec("15")), "4 Aug UTC holds 5 Aug 00:00 IST, got %v", got)
	s.True(stacks[0].Lines[0].Quantity.Equal(dec("15")), "20 Aug is past the elapsed period")
	s.True(allocs[0].Commitments[0].Used.Equal(dec("10")))
}

// dailyGPUPrice stores a daily-bucket MAX meter and a slab-tiered usage price on it; each bucket walks the tiers alone.
func (s *BillingServiceSuite) dailyGPUPrice(id string, tiers ...price.PriceTier) *price.Price {
	ctx := s.GetContext()
	m := &meter.Meter{
		ID:          "meter_" + id,
		Name:        "Daily GPU",
		EventName:   id,
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeDay},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	p := s.usagePriceFor("price_"+id, m, "0")
	p.BillingModel, p.TierMode, p.Tiers = types.BILLING_MODEL_TIERED, types.BILLING_TIER_SLAB, tiers
	s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
	return p
}

// bucketCharge is a charge for li with one daily bucket per value, keyed at UTC midnight of the given August days.
func bucketCharge(li *subscription.SubscriptionLineItem, p *price.Price, days map[int]string) *dto.SubscriptionUsageByMetersResponse {
	var results []events.UsageResult
	for d, v := range days {
		results = append(results, events.UsageResult{WindowSize: time.Date(2026, 8, d, 0, 0, 0, 0, time.UTC), Value: dec(v)})
	}

	return &dto.SubscriptionUsageByMetersResponse{
		SubscriptionLineItemID: li.ID,
		Price:                  p,
		BucketedUsageResult:    &events.AggregationResult{Results: results},
	}
}

// A billing-period commitment over a bucketed price prices each bucket on its own: the covered unit comes off the
// first bucket and the rest of each bucket is charged at the margin above what was covered in it.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodPricesEachBucketAtTheMargin() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.dailyGPUPrice("gpu_margin",
		price.PriceTier{UpTo: lo.ToPtr(uint64(100)), UnitAmount: dec("1")},
		price.PriceTier{UnitAmount: dec("0.1")},
	)
	li := &subscription.SubscriptionLineItem{ID: "subs_line_gpu_margin", PriceID: p.ID, MeterID: p.MeterID, DisplayName: "GPU"}
	sub := s.fixedDateSub("sub_gpu_margin", li)
	committed := s.usagePriceFor("price_gpu_margin_committed", nil, "0.5")
	c := s.newTestCommitment("cmt_gpu_margin", sub, types.COMMITMENT_TYPE_QUANTITY, "1", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	c.CommittedPriceID = lo.ToPtr(committed.ID)
	s.storeCommitment(c)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{
		bucketCharge(li, p, map[int]string{5: "60", 20: "60"}),
	}}
	l := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	stacks, _, err := l.Stacks(ctx)
	s.Require().NoError(err)
	line := stacks[0].Lines[0]
	s.Len(stacks[0].Windows, 3) // 1 Aug, then the 5 Aug and 20 Aug buckets
	s.True(line.PerWindow)
	s.True(line.Usage[1].Equal(dec("60")) && line.Usage[2].Equal(dec("60")), "usage %v", line.Usage)
	s.True(line.Cost.Equal(dec("120")), "each 60-unit bucket costs 60 on its own, got %s", line.Cost)

	// Bucket one: cost(60)-cost(1) = 59; bucket two: cost(60)-cost(0) = 60. The collapsed 119 would cost 100 + 1.90.
	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	within := s.loaderLine(lines, types.CommitmentLineUtilized, c.ID)
	s.True(within.Quantity.Equal(dec("1")) && within.Amount.Equal(dec("0.5")), "within %s for %s", within.Amount, within.Quantity)
	overage := loaderLinesOfKind(lines, types.CommitmentLineOverage)
	s.Require().Len(overage, 1)
	s.True(overage[0].Quantity.Equal(dec("119")), "overage quantity %s", overage[0].Quantity)
	s.True(overage[0].Amount.Equal(dec("119")), "overage %s", overage[0].Amount)
	s.Empty(loaderLinesOfKind(lines, types.CommitmentLineTrueUp))
	s.True(total.Equal(dec("119.5")), "total %s", total)
}

// In a billing-period stack mixing bucketed and unbucketed lines, the unbucketed line's usage sits at the start of its
// span, so it draws the shared pool before the buckets that follow it.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodMixedLinesShareOnePool() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	gpu := s.dailyGPUPrice("gpu_mixed", price.PriceTier{UnitAmount: dec("1")})
	gpuLine := &subscription.SubscriptionLineItem{ID: "subs_line_gpu_mixed", PriceID: gpu.ID, MeterID: gpu.MeterID, DisplayName: "GPU"}
	api := s.usagePriceFor("price_flat_api", s.testData.meters.apiCalls, "1")
	apiLine := &subscription.SubscriptionLineItem{ID: "subs_line_flat_api", PriceID: api.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub("sub_mixed", gpuLine, apiLine)
	c := s.newTestCommitment("cmt_mixed", sub, types.COMMITMENT_TYPE_AMOUNT, "50", "", sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	c.ScopeEntityType = types.CommitmentScopeSubscription
	s.storeCommitment(c)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{
		bucketCharge(gpuLine, gpu, map[int]string{5: "60"}),
		{SubscriptionLineItemID: apiLine.ID, Price: api, Quantity: 40, Amount: 40},
	}}
	l := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	byItem := lo.SliceToMap(stacks[0].Lines, func(line *usageLine) (string, int) { return line.Item.ID, lo.IndexOf(stacks[0].Lines, line) })
	gpuAt, apiAt := byItem[gpuLine.ID], byItem[apiLine.ID]
	s.True(stacks[0].Lines[apiAt].Usage[0].Equal(dec("40")), "api usage %v", stacks[0].Lines[apiAt].Usage)
	s.False(stacks[0].Lines[apiAt].PerWindow)
	s.True(stacks[0].Lines[gpuAt].Usage[1].Equal(dec("60")), "gpu usage %v", stacks[0].Lines[gpuAt].Usage)

	// 1 Aug: the api's 40 leave 10 of the 50; 5 Aug: the bucket's 60 take those 10 and 50 go to overage.
	s.True(allocs[0].Overage[apiAt][0].IsZero())
	s.True(allocs[0].Overage[gpuAt][1].Equal(dec("50")), "gpu overage %s", allocs[0].Overage[gpuAt][1])
	_, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.True(total.Equal(dec("100")), "total %s", total)
}

// A tiered committed price prorates each partial invoice's floor as the step in the period's cumulative floor, so the
// halves sum to the whole period's floor instead of each walking the tiers from zero.
func (s *BillingServiceSuite) TestCommitmentLoader_TieredPartialFloorsSumToThePeriodFloor() {
	ctx := s.GetContext()
	tiered := func(id string, firstTier uint64) string {
		p, err := s.GetStores().PriceRepo.Get(ctx, id)
		s.Require().NoError(err)
		p.BillingModel, p.TierMode = types.BILLING_MODEL_TIERED, types.BILLING_TIER_SLAB
		p.Tiers = []price.PriceTier{{UpTo: lo.ToPtr(firstTier), UnitAmount: dec("1")}, {UnitAmount: dec("0.5")}}
		s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
		return p.ID
	}

	none := &dto.GetUsageBySubscriptionResponse{Currency: "usd"}
	s.Run("billing period", func() {
		s.setupTestData()
		s.enableCommitmentsV2()
		sub, p := s.apiFixture("sub_tiered_floor")
		committed := s.usagePriceFor("price_tiered_floor_committed", s.testData.meters.apiCalls, "0")
		c := s.newTestCommitment("cmt_tiered_floor", sub, types.COMMITMENT_TYPE_QUANTITY, "62", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
		c.CommittedPriceID = lo.ToPtr(tiered(committed.ID, 30))
		s.storeCommitment(c)

		// 62 units a month: 30 by 16 Aug cost 30, all 62 cost 30 + 32 x 0.50 = 46, so the second half bills 16.
		split := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
		for _, tt := range []struct {
			from, to time.Time
			want     string
		}{
			{from: sub.CurrentPeriodStart, to: split, want: "30"},
			{from: split, to: sub.CurrentPeriodEnd, want: "16"},
			{from: sub.CurrentPeriodStart, to: sub.CurrentPeriodEnd, want: "46"},
		} {
			_, total, err := s.loaderFor(sub, none, tt.from, tt.to).InvoiceLines(ctx)
			s.Require().NoError(err)
			s.True(total.Equal(dec(tt.want)), "total %s for %s-%s", total, tt.from, tt.to)
		}
	})

	s.Run("window", func() {
		s.setupTestData()
		s.enableCommitmentsV2()
		sub, _, _ := s.istDailyWindowFixture()
		tiered("price_ist_daily_committed", 200)

		// 10 a day: 150 by 16 Aug IST cost 150, all 310 cost 200 + 110 x 0.50 = 255, so the second half bills 105.
		split := time.Date(2026, 8, 15, 18, 30, 0, 0, time.UTC)
		for _, tt := range []struct {
			from, to time.Time
			want     string
		}{
			{from: sub.CurrentPeriodStart, to: split, want: "150"},
			{from: split, to: sub.CurrentPeriodEnd, want: "105"},
			{from: sub.CurrentPeriodStart, to: sub.CurrentPeriodEnd, want: "255"},
		} {
			_, total, err := s.loaderFor(sub, none, tt.from, tt.to).InvoiceLines(ctx)
			s.Require().NoError(err)
			s.True(total.Equal(dec(tt.want)), "total %s for %s-%s", total, tt.from, tt.to)
		}
	})
}

// With usage, each partial invoice of a binding commitment bills at most its floor, so the partials still sum to the
// whole period's bill even though each values its usage from the committed price's first tier.
func (s *BillingServiceSuite) TestCommitmentLoader_PartialWithinIsCappedAtTheFloor() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, p := s.apiFixture("sub_capped_within")
	committed := s.usagePriceFor("price_capped_within_committed", s.testData.meters.apiCalls, "0")
	committed.BillingModel, committed.TierMode = types.BILLING_MODEL_TIERED, types.BILLING_TIER_SLAB
	committed.Tiers = []price.PriceTier{{UpTo: lo.ToPtr(uint64(30)), UnitAmount: dec("1")}, {UnitAmount: dec("0.5")}}
	s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, committed, false))
	c := s.newTestCommitment("cmt_capped_within", sub, types.COMMITMENT_TYPE_QUANTITY, "62", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	c.CommittedPriceID = lo.ToPtr(committed.ID)
	s.storeCommitment(c)

	// Floors are 30 and 46-30 = 16. 20 calls in each half: the first values at 20 and trues up 10; the second's 20
	// within is capped at 16. The whole period: 40 calls within at 30 + 10 x 0.50 = 35, true-up 11, also 46.
	split := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		from, to  time.Time
		withinQty string
		within    string
		total     string
	}{
		{from: sub.CurrentPeriodStart, to: split, withinQty: "20", within: "20", total: "30"},
		{from: split, to: sub.CurrentPeriodEnd, withinQty: "20", within: "16", total: "16"},
		{from: sub.CurrentPeriodStart, to: sub.CurrentPeriodEnd, withinQty: "40", within: "35", total: "46"},
	} {
		lines, total, err := s.loaderFor(sub, s.rawUsageWindow(sub, tt.from, tt.to), tt.from, tt.to).InvoiceLines(ctx)
		s.Require().NoError(err)
		within := s.loaderLine(lines, types.CommitmentLineUtilized, c.ID)
		s.True(within.Quantity.Equal(dec(tt.withinQty)), "within quantity %s for %s-%s", within.Quantity, tt.from, tt.to)
		s.True(within.Amount.Equal(dec(tt.within)), "within %s for %s-%s", within.Amount, tt.from, tt.to)
		s.True(total.Equal(dec(tt.total)), "total %s for %s-%s", total, tt.from, tt.to)
	}
}

// ClickHouse weeks start on Sunday. In a period starting on Saturday 1 Aug 2026, the week keyed 26 Jul (holding 1 Aug)
// and the week keyed 2 Aug each keep a window of their own, so each still prices alone as normal billing prices it.
func (s *BillingServiceSuite) TestCommitmentLoader_BillingPeriodWeeklyBucketsKeepTheirOwnWindows() {
	tests := []struct {
		name    string
		typ     types.CommitmentType
		value   string
		overage string
		total   string
	}{
		{
			// 60 + 70 cost 60 + 70 = 130 per bucket; merged, 130 would cost 100 + 30 x 0.10 = 103.
			name:    "amount stack bills the normal path's 130",
			typ:     types.COMMITMENT_TYPE_AMOUNT,
			value:   "100",
			overage: "30",
			total:   "130",
		},
		{
			// The unit comes off the 26 Jul week: cost(60)-cost(1) = 59, then cost(70) = 70.
			name:    "quantity stack prices overage per bucket",
			typ:     types.COMMITMENT_TYPE_QUANTITY,
			value:   "1",
			overage: "129",
			total:   "129.5",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			s.setupTestData()
			s.enableCommitmentsV2()
			m := &meter.Meter{
				ID:          "meter_gpu_weekly",
				Name:        "Weekly GPU",
				EventName:   "gpu_weekly",
				Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeWeek},
				BaseModel:   types.GetDefaultBaseModel(ctx),
			}
			s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
			p := s.usagePriceFor("price_gpu_weekly", m, "0")
			p.BillingModel, p.TierMode = types.BILLING_MODEL_TIERED, types.BILLING_TIER_SLAB
			p.Tiers = []price.PriceTier{{UpTo: lo.ToPtr(uint64(100)), UnitAmount: dec("1")}, {UnitAmount: dec("0.1")}}
			s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
			li := &subscription.SubscriptionLineItem{ID: "subs_line_gpu_weekly", PriceID: p.ID, MeterID: m.ID, DisplayName: "GPU"}
			sub := s.fixedDateSub("sub_gpu_weekly", li)
			c := s.newTestCommitment("cmt_gpu_weekly", sub, tt.typ, tt.value, p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
			if tt.typ == types.COMMITMENT_TYPE_QUANTITY {
				c.CommittedPriceID = lo.ToPtr(s.usagePriceFor("price_gpu_weekly_committed", nil, "0.5").ID)
			}
			s.storeCommitment(c)

			usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
				SubscriptionLineItemID: li.ID,
				Price:                  p,
				BucketedUsageResult: &events.AggregationResult{Results: []events.UsageResult{
					{WindowSize: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC), Value: dec("60")},
					{WindowSize: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Value: dec("70")},
				}},
			}}}
			lines, total, err := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd).InvoiceLines(ctx)
			s.Require().NoError(err)
			overage := loaderLinesOfKind(lines, types.CommitmentLineOverage)
			s.Require().Len(overage, 1)
			s.True(overage[0].Amount.Equal(dec(tt.overage)), "overage %s", overage[0].Amount)
			s.True(total.Equal(dec(tt.total)), "total %s", total)
		})
	}
}

func TestSpreadCost_ConservesTotalProportionally(t *testing.T) {
	decs := func(vs ...string) []decimal.Decimal {
		return lo.Map(vs, func(v string, _ int) decimal.Decimal { return dec(v) })
	}

	tests := []struct {
		name    string
		weights []decimal.Decimal
		total   decimal.Decimal
		want    []decimal.Decimal
	}{
		{
			name:    "weights summing to the total are kept as they are",
			weights: decs("0.333333", "0.333333", "0.333334", "0"),
			total:   dec("1.00"),
			want:    decs("0.333333", "0.333333", "0.333334", "0"),
		},
		{
			name:    "equal weights split the total and leave the residue on the last positive one",
			weights: decs("1", "1", "1", "0"),
			total:   dec("1.00"),
			want:    decs("0.3333333333333333", "0.3333333333333334", "0.3333333333333333", "0"),
		},
		{
			name:    "a volume-priced period cost below its spans' stand-alone prices splits by quantity",
			weights: decs("20", "10"),
			total:   dec("30"),
			want:    decs("20", "10"),
		},
		{
			name:    "a tiny last weight never goes negative",
			weights: decs("1", "0.0000000000000000001"),
			total:   dec("0.01"),
			want:    decs("0.01", "0"),
		},
		{
			name:    "no positive weight puts the total on the last window",
			weights: decs("0", "0"),
			total:   dec("5"),
			want:    decs("0", "5"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := spreadCost(tt.weights, tt.total)
			require.Len(t, got, len(tt.want))
			sum := decimal.Zero
			for i, v := range got {
				require.False(t, v.IsNegative(), "window %d is %s", i, v)
				require.True(t, v.Equal(tt.want[i]), "window %d: want %s, got %s", i, tt.want[i], v)
				sum = sum.Add(v)
			}

			require.True(t, sum.Equal(tt.total), "sum %s", sum)
		})
	}
}

// A volume price charges each span more on its own than the period as a whole; spans still sum to the period cost.
func (s *BillingServiceSuite) TestCommitmentLoader_AmountSpansSpreadThePeriodCost() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	p := s.usagePriceFor("price_volume_api", s.testData.meters.apiCalls, "0")
	p.BillingModel, p.TierMode = types.BILLING_MODEL_TIERED, types.BILLING_TIER_VOLUME
	p.Tiers = []price.PriceTier{
		{UpTo: lo.ToPtr(uint64(20)), UnitAmount: dec("2")},
		{UnitAmount: dec("1")},
	}
	s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
	li := &subscription.SubscriptionLineItem{ID: "subs_line_volume_api", PriceID: p.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "API"}
	sub := s.fixedDateSub("sub_volume", li)
	s.seedAPICalls(sub, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), 20)
	s.seedAPICalls(sub, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), 10)
	startAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	c := s.newTestCommitment("cmt_volume", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, startAt, sub.CurrentPeriodEnd)
	s.storeCommitment(c)

	// 30 calls cost $30 at the volume rate; alone, the 20 before the start would cost $40.
	l := s.loaderFor(sub, s.meterUsage(sub), sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	stacks, _, err := l.Stacks(ctx)
	s.Require().NoError(err)
	usage := stacks[0].Lines[0].Usage
	s.Require().Len(usage, 2)
	s.True(usage[0].Equal(dec("20")) && usage[1].Equal(dec("10")), "spans %v", usage)

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.True(s.loaderLine(lines, types.CommitmentLineUtilized, c.ID).Amount.Equal(dec("10")))
	overage := loaderLinesOfKind(lines, types.CommitmentLineOverage)
	s.Require().Len(overage, 1)
	s.True(overage[0].Amount.Equal(dec("20")), "overage %s", overage[0].Amount)
	// $100 for the 17 days from 15 Aug is $54.84, of which $10 is used.
	s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID).Amount.Equal(dec("44.84")))
	s.True(total.Equal(dec("74.84")), "total %s", total)
}

// The states below are only reachable by bypassing create validation, so the loader fails closed on them.
func (s *BillingServiceSuite) TestCommitmentLoader_FailsClosedOnLineInTwoStacks() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub, p := s.apiFixture("sub_two_stacks")
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	switchAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	// An amount commitment terminated mid-period, then a quantity commitment on the same line.
	committed := s.usagePriceFor("price_two_stacks_committed", s.testData.meters.apiCalls, "0.5")
	a := s.newTestCommitment("cmt_amount", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, periodStart, periodEnd)
	a.CommitmentStatus, a.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(switchAt)
	b := s.newTestCommitment("cmt_quantity", sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, switchAt, periodEnd)
	b.CommittedPriceID = lo.ToPtr(committed.ID)
	s.storeCommitment(a)
	s.storeCommitment(b)

	_, err := s.service.(*billingService).newCommitmentLoader(
		s.GetContext(), sub, periodStart, periodEnd, periodEnd, rawChargesByLine(sub, s.meterUsage(sub)), nil,
		types.UsageSourceInvoiceCreation,
	)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	for _, want := range []string{"subs_line_flat_api", "cmt_amount", "cmt_quantity"} {
		s.Contains(err.Error(), want)
	}
}

func (s *BillingServiceSuite) TestCommitmentLoader_FailsClosedOnGroupedBucketedPrice() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()
	grouped := &meter.Meter{
		ID:          "meter_grouped_gpu",
		Name:        "Grouped GPU",
		EventName:   "grouped_gpu",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeHour, GroupBy: "region"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, grouped))
	p := s.usagePriceFor("price_grouped_gpu", grouped, "1")
	li := &subscription.SubscriptionLineItem{ID: "subs_line_grouped_gpu", PriceID: p.ID, MeterID: grouped.ID, DisplayName: "GPU"}
	sub := s.fixedDateSub("sub_grouped", li)
	s.storeCommitment(s.newTestCommitment("cmt_grouped", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd))

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{SubscriptionLineItemID: li.ID, Price: p}}}
	_, _, err := s.loaderFor(sub, usage, sub.CurrentPeriodStart, sub.CurrentPeriodEnd).InvoiceLines(ctx)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	s.Contains(err.Error(), li.ID)
}

func (s *BillingServiceSuite) TestCommitmentLoader_FailsClosedOnDifferentOverageRates() {
	tests := []struct {
		name    string
		overage []string // overage rate per commitment, "" for none
	}{
		{name: "two different rates", overage: []string{"2", "3"}},
		{name: "a rate and none", overage: []string{"2", ""}},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupTestData()
			s.enableCommitmentsV2()
			sub, p := s.apiFixture("sub_overage")
			periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
			switchAt := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
			committed := s.usagePriceFor("price_overage_committed", s.testData.meters.apiCalls, "0.5")
			for i, id := range []string{"cmt_first", "cmt_second"} {
				start := periodStart
				if i == 1 {
					start = switchAt
				}

				c := s.newTestCommitment(id, sub, types.COMMITMENT_TYPE_QUANTITY, "31", p.ID, start, periodEnd)
				c.CommittedPriceID = lo.ToPtr(committed.ID)
				if tt.overage[i] != "" {
					c.OveragePriceID = lo.ToPtr(s.usagePriceFor("price_"+id+"_overage", s.testData.meters.apiCalls, tt.overage[i]).ID)
				}

				if i == 0 {
					c.CommitmentStatus, c.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(switchAt)
				}

				s.storeCommitment(c)
			}

			l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd)
			_, _, err := l.Stacks(s.GetContext())
			s.Require().Error(err)
			s.True(ierr.IsInvalidOperation(err), "got %v", err)
			s.Contains(err.Error(), "cmt_first")
			s.Contains(err.Error(), "cmt_second")
		})
	}
}

// wiredCharges prices the subscription's usage through both billing paths for its current period.
func (s *BillingServiceSuite) wiredCharges() (raw *dto.CalculateUsageChargesResult, meterLines []dto.CreateInvoiceLineItemRequest, meterTotal decimal.Decimal) {
	sub := s.testData.subscription
	from, to := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	raw, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
		Subscription: sub,
		Usage:        s.rawUsage(sub),
		PeriodStart:  from,
		PeriodEnd:    to,
	})
	s.Require().NoError(err)

	meterLines, meterTotal, err = s.service.CalculateMeterUsageCharges(
		s.GetContext(), sub, s.meterUsage(sub), from, to, types.UsageSourceInvoiceCreation, nil,
	)
	s.Require().NoError(err)
	return raw, meterLines, meterTotal
}

func (s *BillingServiceSuite) requireSameBytes(want, got interface{}) {
	wantJSON, err := json.Marshal(want)
	s.Require().NoError(err)
	gotJSON, err := json.Marshal(got)
	s.Require().NoError(err)
	s.True(bytes.Equal(wantJSON, gotJSON), "want %s\ngot  %s", wantJSON, gotJSON)
}

// A subscription without commitments bills the same lines whether or not the setting is on.
func (s *BillingServiceSuite) TestCommitmentWiring_NoCommitmentsIdenticalWithSettingOnAndOff() {
	s.setupTestData()
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	offRaw, offLines, offTotal := s.wiredCharges()
	s.Require().NotEmpty(offRaw.LineItems)
	s.Require().NotEmpty(offLines)

	s.enableCommitmentsV2()
	onRaw, onLines, onTotal := s.wiredCharges()
	s.requireSameBytes(offRaw, onRaw)
	s.requireSameBytes(offLines, onLines)
	s.True(offTotal.Equal(onTotal), "total %s vs %s", offTotal, onTotal)
}

// Commitments that are off, not yet started or already terminated leave both paths unchanged.
func (s *BillingServiceSuite) TestCommitmentWiring_InertCommitmentsLeaveInvoiceUnchanged() {
	tests := []struct {
		name  string
		setup func(periodStart, periodEnd time.Time)
	}{
		{
			name: "setting off",
			setup: func(periodStart, _ time.Time) {
				s.createCommitment(
					types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
				)
			},
		},
		{
			name: "setting on, no overlapping commitment",
			setup: func(periodStart, periodEnd time.Time) {
				s.enableCommitmentsV2()
				s.createCommitment(
					types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodEnd, periodEnd.AddDate(1, 0, 0),
				)
				old := s.createCommitment(
					types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID,
					periodStart.AddDate(0, -2, 0), periodEnd.AddDate(1, 0, 0),
				)
				old.CommitmentStatus = types.CommitmentStatusTerminated
				old.TerminatedAt = lo.ToPtr(periodStart.AddDate(0, 0, -1))
				_, err := s.GetStores().CommitmentRepo.Update(s.GetContext(), old)
				s.Require().NoError(err)
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupTestData()
			sub := s.testData.subscription
			beforeRaw, beforeLines, beforeTotal := s.wiredCharges()

			tt.setup(sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
			afterRaw, afterLines, afterTotal := s.wiredCharges()
			s.requireSameBytes(beforeRaw, afterRaw)
			s.requireSameBytes(beforeLines, afterLines)
			s.True(beforeTotal.Equal(afterTotal))
		})
	}
}

func (s *BillingServiceSuite) TestCommitmentWiring_MeterUsagePathAppliesCommitment() {
	s.setupTestData()
	s.enableCommitmentsV2()
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	c := s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
	)

	// 7000 calls cost $60 on the slab tiers.
	lines, total, err := s.service.CalculateMeterUsageCharges(
		s.GetContext(), s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"),
		periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil,
	)
	s.Require().NoError(err)
	s.Require().Len(lines, 2)
	s.True(s.loaderLine(lines, types.CommitmentLineUtilized, c.ID).Amount.Equal(dec("60")))
	s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID).Amount.Equal(dec("40")))
	s.True(total.Equal(dec("100")), "total %s", total)
}

// A line with a legacy line-item commitment that a new commitment also scopes is billed only by the new one.
func (s *BillingServiceSuite) TestCommitmentWiring_PrefersNewCommitmentOverLegacyLineCommitment() {
	s.setupTestData()
	s.enableCommitmentsV2()
	li, _ := lo.Find(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceID == s.testData.prices.apiCalls.ID
	})
	li.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
	li.CommitmentAmount = lo.ToPtr(dec("500"))
	li.CommitmentTrueUpEnabled = true
	periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
	s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
	)

	lines, total, err := s.service.CalculateMeterUsageCharges(
		s.GetContext(), s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"),
		periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil,
	)
	s.Require().NoError(err)
	for _, l := range lines {
		s.Nil(l.CommitmentInfo)
		s.NotEmpty(l.Metadata[types.MetadataKeyCommitmentLineKind])
	}

	s.True(total.Equal(dec("100")), "total %s", total)
}

// A commitment whose scoped lines bill on another cadence group contributes nothing to this call.
func (s *BillingServiceSuite) TestCommitmentWiring_OtherCadenceGroupContributesNothing() {
	s.setupTestData()
	s.enableCommitmentsV2()
	monthlyPrice := s.usagePriceFor("price_cadence_monthly", s.testData.meters.apiCalls, "1")
	annualPrice := s.usagePriceFor("price_cadence_annual", s.testData.meters.apiCalls, "1")
	monthly := &subscription.SubscriptionLineItem{
		ID: "subs_line_cadence_monthly", PriceID: monthlyPrice.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "Monthly",
	}
	annual := &subscription.SubscriptionLineItem{
		ID: "subs_line_cadence_annual", PriceID: annualPrice.ID, MeterID: s.testData.meters.apiCalls.ID, DisplayName: "Annual",
	}
	sub := s.fixedDateSub("sub_cadence", monthly, annual)
	annual.BillingPeriod = types.BILLING_PERIOD_ANNUAL
	s.storeCommitment(s.newTestCommitment(
		"cmt_cadence", sub, types.COMMITMENT_TYPE_AMOUNT, "100", annualPrice.ID, sub.StartDate, sub.StartDate.AddDate(1, 0, 0),
	))

	// The call carries only the monthly group.
	callSub := *sub
	callSub.LineItems = []*subscription.SubscriptionLineItem{monthly}
	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: monthly.ID,
		Price:                  monthlyPrice,
		Quantity:               20,
		Amount:                 20,
	}}}

	loader, err := s.service.(*billingService).newCommitmentLoader(
		s.GetContext(), &callSub, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, sub.CurrentPeriodEnd,
		rawChargesByLine(&callSub, usage), nil, types.UsageSourceInvoiceCreation,
	)
	s.Require().NoError(err)
	s.Empty(loader.HandledLineIDs())
	loaderLines, _, err := loader.InvoiceLines(s.GetContext())
	s.Require().NoError(err)
	s.Empty(loaderLines)

	raw, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
		Subscription: &callSub,
		Usage:        usage,
		PeriodStart:  sub.CurrentPeriodStart,
		PeriodEnd:    sub.CurrentPeriodEnd,
	})
	s.Require().NoError(err)
	s.Require().Len(raw.LineItems, 1)
	s.Empty(raw.LineItems[0].Metadata[types.MetadataKeyCommitmentLineKind])
	s.True(raw.TotalAmount.Equal(dec("20")), "total %s", raw.TotalAmount)

	metered := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: usage.Charges}
	lines, total, err := s.service.CalculateMeterUsageCharges(
		s.GetContext(), &callSub, metered, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, types.UsageSourceInvoiceCreation, nil,
	)
	s.Require().NoError(err)
	s.Require().Len(lines, 1)
	s.Empty(lines[0].Metadata[types.MetadataKeyCommitmentLineKind])
	s.True(total.Equal(dec("20")), "total %s", total)
}

// The raw-events path shares the meter-usage path's line intersection and containment guard.
func (s *BillingServiceSuite) TestCommitmentWiring_RawPathScopeGuards() {
	s.Run("lines outside the call", func() {
		s.setupTestData()
		s.enableCommitmentsV2()
		periodStart, periodEnd := s.testData.subscription.CurrentPeriodStart, s.testData.subscription.CurrentPeriodEnd
		s.createCommitment(
			types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
		)
		callSub := *s.testData.subscription
		callSub.LineItems = lo.Filter(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
			return li.PriceID != s.testData.prices.apiCalls.ID
		})
		res, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
			Subscription: &callSub,
			Usage:        s.usageResponse(s.testData.prices.apiCalls, "7000"),
			PeriodStart:  periodStart,
			PeriodEnd:    periodEnd,
		})
		s.Require().NoError(err)
		s.Empty(res.LineItems)
		s.True(res.TotalAmount.IsZero(), "total %s", res.TotalAmount)
	})

	s.Run("window across commitment periods", func() {
		s.setupTestData()
		s.enableCommitmentsV2()
		p := s.usagePriceFor("price_flat_api_span", s.testData.meters.apiCalls, "1")
		li := &subscription.SubscriptionLineItem{
			ID:          "subs_line_flat_api_span",
			PriceID:     p.ID,
			MeterID:     s.testData.meters.apiCalls.ID,
			DisplayName: "API",
		}
		sub := s.fixedDateSub("sub_span", li)
		s.storeCommitment(s.newTestCommitment(
			"cmt_span", sub, types.COMMITMENT_TYPE_AMOUNT, "100", p.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(0, 3, 0),
		))
		usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
			Price:    p,
			Quantity: 20,
			Amount:   20,
		}}}
		res, err := s.service.CalculateUsageCharges(s.GetContext(), &dto.CalculateUsageChargesParams{
			Subscription: sub,
			Usage:        usage,
			PeriodStart:  time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
			PeriodEnd:    time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		})
		s.Require().NoError(err)
		s.Require().Len(res.LineItems, 1)
		s.Empty(res.LineItems[0].Metadata[types.MetadataKeyCommitmentLineKind])
		s.True(res.TotalAmount.Equal(dec("20")), "total %s", res.TotalAmount)
	})
}

func commitmentLinesOf(lines []dto.CreateInvoiceLineItemRequest) []dto.CreateInvoiceLineItemRequest {
	return lo.Filter(lines, func(l dto.CreateInvoiceLineItemRequest, _ int) bool {
		return l.Metadata[types.MetadataKeyCommitmentLineKind] != ""
	})
}

// The raw-events preview and the meter-usage invoice bill the same commitment lines.
func (s *BillingServiceSuite) TestCommitmentWiring_PreviewMatchesInvoice() {
	tests := []struct {
		name  string
		setup func(periodStart time.Time)
		kinds []types.CommitmentLineKind
	}{
		{
			name: "amount commitment with true-up",
			setup: func(periodStart time.Time) {
				s.createCommitment(
					types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
				)
			},
			kinds: []types.CommitmentLineKind{types.CommitmentLineUtilized, types.CommitmentLineTrueUp},
		},
		{
			name: "quantity commitment with overage",
			setup: func(periodStart time.Time) {
				committed := s.usagePriceFor("price_parity_committed", s.testData.meters.apiCalls, "0.01")
				overage := s.usagePriceFor("price_parity_overage", s.testData.meters.apiCalls, "0.03")
				c := s.newTestCommitment(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT), s.testData.subscription,
					types.COMMITMENT_TYPE_QUANTITY, "300", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0))
				c.CommittedPriceID, c.OveragePriceID = lo.ToPtr(committed.ID), lo.ToPtr(overage.ID)
				s.storeCommitment(c)
			},
			kinds: []types.CommitmentLineKind{types.CommitmentLineUtilized, types.CommitmentLineOverage},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupTestData()
			s.enableCommitmentsV2()
			tt.setup(s.testData.subscription.CurrentPeriodStart)

			raw, lines, total := s.wiredCharges()
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

// recalcFixture stores an Aug 2026 subscription with $10 of usage (500 calls at $0.02), the given commitments
// on its usage price, and a draft invoice for August holding one stale usage line.
func (s *EntitlementQuantityTestSuite) recalcFixture(
	id string,
	commitments ...*commitment.Commitment,
) *invoicedomain.Invoice {
	ctx := s.GetContext()
	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	sep := aug.AddDate(0, 1, 0)
	sub := &subscription.Subscription{
		ID:                 "sub_" + id,
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          aug,
		BillingAnchor:      aug,
		CurrentPeriodStart: aug,
		CurrentPeriodEnd:   sep,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	li := *s.testData.usageSubLineItem
	li.ID, li.SubscriptionID, li.StartDate = "subs_line_"+id, sub.ID, aug
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{&li}))

	records := make([]*events.MeterUsage, 0, 500)
	for i := 0; i < 500; i++ {
		muID := types.GenerateUUIDWithPrefix("mu")
		ts := aug.Add(24 * time.Hour)
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID:                 muID,
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				EventName:          s.testData.meter.EventName,
				CustomerID:         s.testData.customer.ID,
				ExternalCustomerID: s.testData.customer.ExternalID,
				Timestamp:          ts,
				IngestedAt:         ts,
			},
			MeterID:    s.testData.meter.ID,
			QtyTotal:   decimal.NewFromInt(1),
			UniqueHash: muID,
		})
	}

	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))

	for _, c := range commitments {
		c.SubscriptionID, c.CustomerID, c.ScopeEntityID = sub.ID, sub.CustomerID, li.PriceID
		c.StartDate, c.EndDate = aug, lo.ToPtr(aug.AddDate(0, 3, 0))
		_, err := s.GetStores().CommitmentRepo.Create(ctx, c)
		s.Require().NoError(err)
	}

	bp := string(types.BILLING_PERIOD_MONTHLY)
	inv := &invoicedomain.Invoice{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:     sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusDraft,
		PaymentStatus:  types.PaymentStatusPending,
		Currency:       "usd",
		BillingPeriod:  &bp,
		PeriodStart:    &aug,
		PeriodEnd:      &sep,
		BaseModel:      types.GetDefaultBaseModel(ctx),
		LineItems: []*invoicedomain.InvoiceLineItem{{
			ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
			CustomerID:             sub.CustomerID,
			MeterID:                lo.ToPtr(s.testData.meter.ID),
			PriceID:                lo.ToPtr(li.PriceID),
			DisplayName:            lo.ToPtr("stale"),
			Amount:                 dec("1"),
			Quantity:               dec("1"),
			Currency:               "usd",
			EnvironmentID:          types.GetEnvironmentID(ctx),
			PeriodStart:            &aug,
			PeriodEnd:              &sep,
			SubscriptionLineItemID: lo.ToPtr(li.ID),
			BaseModel:              types.GetDefaultBaseModel(ctx),
		}},
	}

	s.Require().NoError(s.invoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

func (s *EntitlementQuantityTestSuite) recalcCommitment(id string, value string, priority int) *commitment.Commitment {
	ctx := s.GetContext()
	return &commitment.Commitment{
		ID:                 id,
		Name:               id,
		InvoiceLabel:       id,
		Currency:           "usd",
		ScopeEntityType:    types.CommitmentScopePrice,
		Type:               types.COMMITMENT_TYPE_AMOUNT,
		Value:              dec(value),
		Measurement:        types.CommitmentMeasurementBillingPeriod,
		Timezone:           "UTC",
		Priority:           priority,
		Binding:            true,
		CollectionMethod:   lo.ToPtr(types.CommitmentCollectionPerPeriod),
		CommitmentStatus:   types.CommitmentStatusActive,
		OnEarlyTermination: lo.ToPtr(types.CommitmentTerminationForfeit),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

// requireStableRecalculation recalculates the draft three times and checks the stored lines stay the same.
func (s *EntitlementQuantityTestSuite) requireStableRecalculation(
	inv *invoicedomain.Invoice,
	wantLines int,
	wantTotal string,
) {
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

func (s *EntitlementQuantityTestSuite) TestCommitmentWiring_RecalculateDraft_TrueUpStaysStable() {
	inv := s.recalcFixture("recalc_trueup", s.recalcCommitment("cmt_recalc_100", "100", 1))
	// Within $10 plus a $90 true-up.
	s.requireStableRecalculation(inv, 2, "100")
}

func (s *EntitlementQuantityTestSuite) TestCommitmentWiring_RecalculateDraft_StackedStaysStable() {
	inv := s.recalcFixture("recalc_stack",
		s.recalcCommitment("cmt_recalc_4", "4", 1), s.recalcCommitment("cmt_recalc_3", "3", 2))
	// Within $4 and $3 on the same line item, plus $3 overage.
	s.requireStableRecalculation(inv, 3, "10")
}

// rawUsageWindow returns the raw-events usage of [from, to), with the commitment breakdown for that window.
func (s *BillingServiceSuite) rawUsageWindow(sub *subscription.Subscription, from, to time.Time) *dto.GetUsageBySubscriptionResponse {
	usage, err := s.subscriptionSvc().GetUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      from,
		EndTime:        to,
	})
	s.Require().NoError(err)
	return usage
}

func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_AmountCommitment() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub := s.testData.subscription
	periodStart := sub.CurrentPeriodStart
	// $100 amount commitment; the 500 recorded api calls cost $10.
	c := s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
	)

	usage := s.rawUsage(sub)
	s.Require().Len(usage.Commitments, 1)
	b := usage.Commitments[0]
	s.Equal(c.ID, b.CommitmentID)
	s.Equal(c.InvoiceLabel, b.InvoiceLabel)
	s.Equal(types.COMMITMENT_TYPE_AMOUNT, b.Type)
	s.Equal(types.CommitmentMeasurementBillingPeriod, b.Measurement)
	s.True(b.Binding)
	s.Equal("usd", b.Currency)
	s.True(b.AppliesFrom.Equal(periodStart), "applies from %s", b.AppliesFrom)
	s.True(b.AppliesTo.Equal(sub.CurrentPeriodEnd), "applies to %s", b.AppliesTo)
	s.True(b.Committed.Amount.Equal(dec("100")), "period amount %s", b.Committed.Amount)
	// The now-based fixture prorates capacity by a fraction of a second, so compare it rounded.
	s.True(types.RoundToCurrencyPrecision(b.Committed.Quantity, "usd").Equal(dec("100")), "capacity %s", b.Committed.Quantity)
	s.True(b.Used.Quantity.Equal(dec("10")), "used %s", b.Used)
	s.True(b.Used.Quantity.Add(b.Unused.Quantity).Equal(b.Committed.Quantity), "used %s + unused %s", b.Used.Quantity, b.Unused.Quantity)
	s.True(b.Used.Amount.Equal(dec("10")), "value within %s", b.Used.Amount)
	s.True(b.Unused.Amount.Equal(dec("90")), "value unused %s", b.Unused.Amount)
	s.True(b.Used.Amount.Add(b.Unused.Amount).Equal(b.Committed.Amount))
	s.True(b.BilledAmount.Equal(dec("100")), "billed %s", b.BilledAmount)

	// A window ending inside the open period projects to its end: the breakdown reports the
	// period invoice's capacity and floor, not a preview clipped at the query time.
	partial := s.rawUsageWindow(sub, periodStart, time.Now().UTC())
	s.Require().Len(partial.Commitments, 1)
	s.True(partial.Commitments[0].Used.Amount.Equal(b.Used.Amount))
	s.True(partial.Commitments[0].Unused.Amount.Equal(b.Unused.Amount))
	s.True(partial.Commitments[0].BilledAmount.Equal(b.BilledAmount))
	s.True(partial.Commitments[0].AppliesTo.Equal(sub.CurrentPeriodEnd), "applies to %s", partial.Commitments[0].AppliesTo)
}

// An open-ended commitment applies to the end of the queried period, at that period's full floor, whether the
// window covers the whole period or stops inside it.
func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_OpenEndedAppliesToThePeriodEnd() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub := s.testData.subscription
	periodStart := sub.CurrentPeriodStart
	c := s.newTestCommitment(
		types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT),
		sub,
		types.COMMITMENT_TYPE_AMOUNT,
		"100",
		s.testData.prices.apiCalls.ID,
		periodStart,
		time.Time{},
	)
	c.EndDate = nil
	s.storeCommitment(c)

	for _, usage := range []*dto.GetUsageBySubscriptionResponse{
		s.rawUsage(sub),
		s.rawUsageWindow(sub, periodStart, time.Now().UTC()),
	} {
		s.Require().Len(usage.Commitments, 1)
		b := usage.Commitments[0]
		s.Equal(c.ID, b.CommitmentID)
		s.True(b.AppliesFrom.Equal(periodStart), "applies from %s", b.AppliesFrom)
		s.True(b.AppliesTo.Equal(sub.CurrentPeriodEnd), "applies to %s", b.AppliesTo)
		s.True(b.Committed.Amount.Equal(dec("100")), "period amount %s", b.Committed.Amount)
		s.True(b.BilledAmount.Equal(dec("100")), "billed %s", b.BilledAmount)
	}
}

// The public usage API reads meter_usage; its response carries the breakdown, while internal callers
// (req.Source set) and lifetime windows skip it.
func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_MeterUsagePath() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub := s.testData.subscription
	periodStart := sub.CurrentPeriodStart
	c := s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
	)

	usage, err := s.subscriptionSvc().GetMeterUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      sub.CurrentPeriodStart,
		EndTime:        sub.CurrentPeriodEnd,
	})
	s.Require().NoError(err)
	s.Require().Len(usage.Commitments, 1)
	s.Equal(c.ID, usage.Commitments[0].CommitmentID)
	s.True(usage.Commitments[0].Committed.Amount.Equal(dec("100")), "period amount %s", usage.Commitments[0].Committed.Amount)
	s.True(usage.Commitments[0].BilledAmount.Equal(dec("100")), "billed %s", usage.Commitments[0].BilledAmount)

	internal, err := s.subscriptionSvc().GetMeterUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      sub.CurrentPeriodStart,
		EndTime:        sub.CurrentPeriodEnd,
		Source:         string(types.UsageSourceInvoiceCreation),
	})
	s.Require().NoError(err)
	s.Nil(internal.Commitments)
}

// Usage over capacity fills the commitment: ValueWithin reaches PeriodAmount and nothing is unused.
func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_UsageOverCapacity() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub := s.testData.subscription
	// $5 amount commitment against $10 of usage.
	c := s.createCommitment(
		types.COMMITMENT_TYPE_AMOUNT, "5", s.testData.prices.apiCalls.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(1, 0, 0),
	)

	usage := s.rawUsage(sub)
	s.Require().Len(usage.Commitments, 1)
	b := usage.Commitments[0]
	s.Equal(c.ID, b.CommitmentID)
	s.True(b.Committed.Amount.Equal(dec("5")), "period amount %s", b.Committed.Amount)
	s.True(b.Used.Amount.Equal(dec("5")), "value within %s", b.Used.Amount)
	s.True(b.Unused.Amount.IsZero(), "value unused %s", b.Unused.Amount)
	s.True(types.RoundToCurrencyPrecision(b.Unused.Quantity, "usd").IsZero(), "unused %s", b.Unused.Quantity)
	s.True(b.BilledAmount.Equal(dec("5")), "billed %s", b.BilledAmount)
}

func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_QuantityCommitmentStack() {
	s.setupTestData()
	s.enableCommitmentsV2()
	sub := s.testData.subscription
	committed := s.usagePriceFor("price_breakdown_committed", s.testData.meters.apiCalls, "0.01")
	// 300 of the 500 calls are committed at $0.01; the other 200 are billed on the line's own price.
	c := s.newTestCommitment(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT), sub,
		types.COMMITMENT_TYPE_QUANTITY, "300", s.testData.prices.apiCalls.ID, sub.CurrentPeriodStart, sub.CurrentPeriodStart.AddDate(1, 0, 0))
	c.CommittedPriceID = lo.ToPtr(committed.ID)
	s.storeCommitment(c)

	usage := s.rawUsage(sub)
	s.Require().Len(usage.Commitments, 1)
	b := usage.Commitments[0]
	s.True(b.Committed.Amount.Equal(dec("3")), "period amount %s", b.Committed.Amount)
	s.True(b.Committed.Quantity.Round(0).Equal(dec("300")), "capacity %s", b.Committed.Quantity)
	s.True(b.Used.Quantity.Round(0).Equal(dec("300")), "used %s", b.Used)
	s.True(b.Used.Amount.Equal(dec("3")), "value within %s", b.Used.Amount)
	s.True(b.Unused.Amount.IsZero(), "value unused %s", b.Unused.Amount)
	s.True(b.BilledAmount.Equal(dec("3")), "billed %s", b.BilledAmount)
}

func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_NilWithoutApplicableCommitments() {
	tests := []struct {
		name  string
		setup func(periodStart, periodEnd time.Time)
	}{
		{
			name: "flag off",
			setup: func(periodStart, _ time.Time) {
				s.createCommitment(
					types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodStart, periodStart.AddDate(1, 0, 0),
				)
			},
		},
		{
			name: "flag on, no commitment",
			setup: func(_, _ time.Time) {
				s.enableCommitmentsV2()
			},
		},
		{
			name: "flag on, no overlapping commitment",
			setup: func(_, periodEnd time.Time) {
				s.enableCommitmentsV2()
				s.createCommitment(
					types.COMMITMENT_TYPE_AMOUNT, "100", s.testData.prices.apiCalls.ID, periodEnd, periodEnd.AddDate(1, 0, 0),
				)
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.setupTestData()
			sub := s.testData.subscription
			tt.setup(sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
			s.Nil(s.rawUsage(sub).Commitments)
		})
	}
}

// A window commitment mid-period counts only elapsed buckets as capacity; the invoice at period end
// then trues up to the whole period amount.
func (s *BillingServiceSuite) TestCommitmentUsageBreakdown_MidPeriodWindow() {
	ctx := s.GetContext()
	s.setupTestData()
	s.enableCommitmentsV2()

	dayMeter := &meter.Meter{
		ID:        "meter_daily_gpu",
		Name:      "Daily GPU",
		EventName: "daily_gpu",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "gpus",
			BucketSize: types.WindowSizeDay,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, dayMeter))
	usagePrice := s.usagePriceFor("price_daily_gpu", dayMeter, "1")
	committedPrice := s.usagePriceFor("price_daily_gpu_committed", dayMeter, "0.5")
	li := &subscription.SubscriptionLineItem{
		ID:               "subs_line_daily_gpu",
		PriceID:          usagePrice.ID,
		MeterID:          dayMeter.ID,
		MeterDisplayName: dayMeter.Name,
		DisplayName:      "Daily GPU",
	}
	sub := s.fixedDateSub("sub_midperiod", li)
	periodStart, periodEnd := sub.CurrentPeriodStart, sub.CurrentPeriodEnd

	insert := func(at time.Time, gpus int64) {
		id := s.GetUUID()
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event: events.Event{
				ID:                 id,
				TenantID:           sub.TenantID,
				EnvironmentID:      sub.EnvironmentID,
				EventName:          dayMeter.EventName,
				ExternalCustomerID: s.testData.customer.ExternalID,
				CustomerID:         sub.CustomerID,
				Timestamp:          at,
				IngestedAt:         at,
			},
			MeterID:    dayMeter.ID,
			QtyTotal:   decimal.NewFromInt(gpus),
			UniqueHash: id,
		}}))
	}
	insert(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), 15)
	insert(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC), 4)

	// 10 GPUs per day committed at $0.50, over a 31-day period.
	c := s.newTestCommitment("cmt_daily_gpu", sub, types.COMMITMENT_TYPE_QUANTITY, "10", usagePrice.ID, periodStart, periodEnd)
	c.Measurement, c.CommittedPriceID = types.CommitmentMeasurementWindow, lo.ToPtr(committedPrice.ID)
	s.storeCommitment(c)

	usage := &dto.GetUsageBySubscriptionResponse{Currency: "usd", Charges: []*dto.SubscriptionUsageByMetersResponse{{
		SubscriptionLineItemID: li.ID,
		Price:                  usagePrice,
	}}}
	breakdownAt := func(asOf time.Time) *dto.CommitmentUsageBreakdown {
		l, err := s.service.(*billingService).newCommitmentLoader(
			ctx, sub, periodStart, periodEnd, asOf, rawChargesByLine(sub, usage), nil, types.UsageSourceInvoiceCreation,
		)
		s.Require().NoError(err)
		out, err := l.UsageBreakdown(ctx)
		s.Require().NoError(err)
		s.Require().Len(out, 1)
		return out[0]
	}

	// Aug 1-10 have elapsed: 10 buckets of capacity, of which the 15 GPUs on Aug 2 fill one.
	mid := breakdownAt(time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC))
	s.True(mid.Committed.Amount.Equal(dec("155")), "period amount %s", mid.Committed.Amount)
	s.True(mid.Committed.Quantity.Equal(dec("100")), "capacity %s", mid.Committed.Quantity)
	s.True(mid.Used.Quantity.Equal(dec("10")), "used %s", mid.Used)
	s.True(mid.Unused.Quantity.Equal(dec("90")), "unused %s", mid.Unused)
	s.True(mid.Used.Amount.Equal(dec("5")), "value within %s", mid.Used.Amount)
	s.True(mid.Unused.Amount.Equal(dec("150")), "value unused %s", mid.Unused.Amount)

	// With the whole period elapsed the capacity is every bucket and Aug 20's usage counts.
	full := breakdownAt(periodEnd)
	s.True(full.Committed.Quantity.Equal(dec("310")), "capacity %s", full.Committed.Quantity)
	s.True(full.Used.Quantity.Equal(dec("14")), "used %s", full.Used)
	s.True(full.Used.Amount.Equal(dec("7")), "value within %s", full.Used.Amount)
	s.True(full.BilledAmount.Equal(dec("155")), "billed %s", full.BilledAmount)

	// The invoice at period end trues up to the whole period amount.
	lines, _, err := s.service.CalculateMeterUsageCharges(
		ctx, sub, s.meterUsage(sub), periodStart, periodEnd, types.UsageSourceInvoiceCreation, nil,
	)
	s.Require().NoError(err)
	within := s.loaderLine(lines, types.CommitmentLineUtilized, c.ID)
	trueUp := s.loaderLine(lines, types.CommitmentLineTrueUp, c.ID)
	s.True(within.Amount.Add(trueUp.Amount).Equal(dec("155")), "within %s + true-up %s", within.Amount, trueUp.Amount)
	s.True(within.Amount.Equal(full.Used.Amount), "invoice within %s vs breakdown %s", within.Amount, full.Used.Amount)

	// The API path projects to the window's end on its own: a mid-period query reports full-window
	// capacity and the matching floor, not capacity elapsed so far.
	api, err := s.service.(*billingService).commitmentUsageBreakdown(ctx, sub, usage, periodStart, periodEnd)
	s.Require().NoError(err)
	s.Require().Len(api, 1)
	s.True(api[0].Committed.Quantity.Equal(dec("310")), "api capacity %s", api[0].Committed.Quantity)
	s.True(api[0].Committed.Amount.Equal(dec("155")), "api floor %s", api[0].Committed.Amount)
	s.True(api[0].Used.Quantity.Equal(dec("14")), "api used %s", api[0].Used.Quantity)
	s.True(api[0].BilledAmount.Equal(dec("155")), "api billed %s", api[0].BilledAmount)

	// A to-date query projects capacity to the period end but counts only usage inside its window:
	// Aug 20's GPUs stay out of an Aug 11 cutoff even though the loader re-reads bucketed usage.
	toDate, err := s.service.(*billingService).commitmentUsageBreakdown(
		ctx, sub, usage, periodStart, time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	)
	s.Require().NoError(err)
	s.Require().Len(toDate, 1)
	s.True(toDate[0].Committed.Quantity.Equal(dec("310")), "to-date capacity %s", toDate[0].Committed.Quantity)
	s.True(toDate[0].Committed.Amount.Equal(dec("155")), "to-date floor %s", toDate[0].Committed.Amount)
	s.True(toDate[0].Used.Quantity.Equal(dec("10")), "to-date used %s", toDate[0].Used.Quantity)

	// A window starting mid-period misses earlier usage, so it keeps clipped-preview semantics:
	// six days of capacity, none of it used.
	midStart, err := s.service.(*billingService).commitmentUsageBreakdown(
		ctx, sub, usage,
		time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	)
	s.Require().NoError(err)
	s.Require().Len(midStart, 1)
	s.True(midStart[0].Committed.Quantity.Equal(dec("60")), "mid-start capacity %s", midStart[0].Committed.Quantity)
	s.True(midStart[0].Committed.Amount.Equal(dec("30")), "mid-start floor %s", midStart[0].Committed.Amount)
	s.True(midStart[0].Used.Quantity.IsZero(), "mid-start used %s", midStart[0].Used.Quantity)
}

func TestRawChargesByLine_SplitsChargesAcrossLinesSharingAPrice(t *testing.T) {
	shared := &price.Price{ID: "price_shared"}
	sub := &subscription.Subscription{LineItems: []*subscription.SubscriptionLineItem{
		{ID: "li_old", PriceID: shared.ID, PriceType: types.PRICE_TYPE_USAGE},
		{ID: "li_new", PriceID: shared.ID, PriceType: types.PRICE_TYPE_USAGE},
	}}
	usage := &dto.GetUsageBySubscriptionResponse{Charges: []*dto.SubscriptionUsageByMetersResponse{
		{SubscriptionLineItemID: "li_old", Price: shared},
		{SubscriptionLineItemID: "li_new", Price: shared},
		{SubscriptionLineItemID: "li_new", Price: shared},
	}}

	got := rawChargesByLine(sub, usage)
	require.Len(t, got["li_old"], 1)
	require.Len(t, got["li_new"], 2)

	untagged := &dto.GetUsageBySubscriptionResponse{Charges: []*dto.SubscriptionUsageByMetersResponse{{Price: shared}}}
	got = rawChargesByLine(sub, untagged)
	require.Len(t, got["li_old"], 1)
	require.Len(t, got["li_new"], 1)
}
