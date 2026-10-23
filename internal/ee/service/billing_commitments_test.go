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
		types.UsageSourceInvoiceCreation, false,
	)
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	for _, want := range []string{"subs_line_flat_api", "cmt_amount", "cmt_quantity"} {
		s.Contains(err.Error(), want)
	}
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

			l := s.loaderFor(sub, s.meterUsage(sub), periodStart, periodEnd, false)
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
	s.True(s.loaderLine(lines, types.CommitmentLineWithin, c.ID).Amount.Equal(dec("60")))
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
		rawChargesByLine(&callSub, usage), nil, types.UsageSourceInvoiceCreation, true,
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
		c.StartDate, c.EndDate = aug, aug.AddDate(0, 3, 0)
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
	// Within $4 and $3 on the same line item, plus $3 on-demand.
	s.requireStableRecalculation(inv, 3, "10")
}
