package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/events"
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
		EndDate:            end,
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
	fromEvents bool,
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
		fromEvents,
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

// meterUsage returns the meter_usage-backed usage of the whole period.
func (s *BillingServiceSuite) meterUsage(sub *subscription.Subscription) *dto.GetUsageBySubscriptionResponse {
	usage, err := s.subscriptionSvc().GetMeterUsageBySubscription(s.GetContext(), &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      sub.CurrentPeriodStart,
		EndTime:        sub.CurrentPeriodEnd,
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
	l := s.loaderFor(s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd, false)
	apiLine, _ := lo.Find(s.testData.subscription.LineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceID == s.testData.prices.apiCalls.ID
	})
	s.Equal(map[string]bool{apiLine.ID: true}, l.HandledLineIDs())

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.Len(lines, 2)
	within := loaderLinesOfKind(lines, types.CommitmentLineWithin)
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
	l := s.loaderFor(s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd, false)
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

	l := s.loaderFor(s.testData.subscription, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd, false)
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
	l := s.loaderFor(&callSub, s.usageResponse(s.testData.prices.apiCalls, "7000"), periodStart, periodEnd, false)
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
	l := s.loaderFor(sub, usage, from, to, false)
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
	lines, total, err := s.loaderFor(sub, usage, periodStart, periodEnd, false).InvoiceLines(ctx)
	s.Require().NoError(err)

	s.True(s.loaderLine(lines, types.CommitmentLineWithin, c1.ID).Quantity.Equal(dec("24")))
	s.True(s.loaderLine(lines, types.CommitmentLineWithin, c2.ID).Quantity.Equal(dec("44")))
	onDemand := loaderLinesOfKind(lines, types.CommitmentLineOnDemand)
	s.Require().Len(onDemand, 1)
	s.True(onDemand[0].Quantity.Equal(dec("7")))
	s.True(onDemand[0].Amount.Equal(dec("0.42")))
	// C1 runs all 44,640 August minutes; C2 runs 7 days x 720 minutes from 25 Aug IST.
	c1TrueUp := s.loaderLine(lines, types.CommitmentLineTrueUp, c1.ID)
	s.True(c1TrueUp.Quantity.Equal(dec("357096")), "c1 unused %s", c1TrueUp.Quantity)
	c2TrueUp := s.loaderLine(lines, types.CommitmentLineTrueUp, c2.ID)
	s.True(c2TrueUp.Quantity.Equal(dec("161236")), "c2 unused %s", c2TrueUp.Quantity)
	c1Period := s.loaderLine(lines, types.CommitmentLineWithin, c1.ID).Amount.Add(c1TrueUp.Amount)
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
		s.True(s.loaderLine(lines, types.CommitmentLineWithin, "cmt_a").Amount.Equal(dec("7")))
		s.True(s.loaderLine(lines, types.CommitmentLineWithin, "cmt_b").Amount.Equal(dec("8.5")))
		// One on-demand line per usage line: 6 from A's half and 3 from B's, at the shared $2 overage.
		onDemand := loaderLinesOfKind(lines, types.CommitmentLineOnDemand)
		s.Require().Len(onDemand, 1)
		s.True(onDemand[0].Quantity.Round(6).Equal(dec("9")), "on-demand %s", onDemand[0].Quantity)
		s.True(onDemand[0].Amount.Equal(dec("18")), "on-demand %s", onDemand[0].Amount)
		s.True(total.Equal(dec("33.5")), "total %s", total)
		return lines
	}

	fromMeterUsage := check(s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd, false))

	// The raw-events source reads the same split.
	fromEvents := check(s.loaderFor(sub, s.rawUsage(sub), periodStart, periodEnd, true))
	s.Require().Len(fromEvents, len(fromMeterUsage))
	for i := range fromEvents {
		s.Equal(fromMeterUsage[i].Metadata, fromEvents[i].Metadata)
		s.True(fromMeterUsage[i].Amount.Equal(fromEvents[i].Amount))
	}
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
				types.UsageSourceInvoiceCreation, false,
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

	l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd, false)
	stacks, allocs, err := l.Stacks(ctx)
	s.Require().NoError(err)
	s.Require().Len(stacks, 1)
	s.Equal([]time.Time{periodStart, startAt}, stacks[0].Windows)
	s.Equal([]int{-1, 0}, stacks[0].Commitments[0].PoolOf)
	s.Equal([]decimal.Decimal{dec("20"), dec("20")}, stacks[0].Lines[0].Usage)
	s.True(allocs[0].Commitments[0].Used.Equal(dec("20")), "used %s", allocs[0].Commitments[0].Used)
	s.True(allocs[0].Commitments[0].Capacity.Round(6).Equal(dec("34")), "capacity %s", allocs[0].Commitments[0].Capacity)
	s.True(allocs[0].OnDemand[0][0].Equal(dec("20")), "pre-start usage stays on-demand")
	s.True(allocs[0].OnDemand[0][1].IsZero())

	lines, total, err := l.InvoiceLines(ctx)
	s.Require().NoError(err)
	s.True(s.loaderLine(lines, types.CommitmentLineWithin, c.ID).Quantity.Equal(dec("20")))
	s.True(loaderLinesOfKind(lines, types.CommitmentLineOnDemand)[0].Quantity.Equal(dec("20")))
	// 20 within at $0.5, true-up of the $7 unused, and 20 on demand at the $1 price.
	s.True(total.Equal(dec("37")), "total %s", total)
}

// One pool spans every stretch a commitment is in force, so usage before a later commitment starts draws it down.
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

	l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd, false)
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
	s.Empty(loaderLinesOfKind(lines, types.CommitmentLineOnDemand))
	s.True(s.loaderLine(lines, types.CommitmentLineTrueUp, y.ID).Quantity.Round(6).Equal(dec("2")))
	// X 12.5 + Y 7.5 within + Y true-up 1.
	s.True(total.Equal(dec("21")), "total %s", total)
}

func TestCommitmentLoader_SpreadWindowCosts_ConservesLineCost(t *testing.T) {
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
