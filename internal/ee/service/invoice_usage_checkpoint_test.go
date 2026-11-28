package service

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type failingCheckpointWriter struct {
	meterusagecheckpoint.Repository
}

func (r failingCheckpointWriter) Upsert(context.Context, *meterusagecheckpoint.MeterUsageCheckpoint) (*meterusagecheckpoint.MeterUsageCheckpoint, error) {
	return nil, errors.New("checkpoint write unavailable")
}

func (s *InvoiceServiceSuite) checkpointCycleDraft(id string, amount decimal.Decimal, cutoff time.Time, rolled bool) *invoice.Invoice {
	ctx := s.GetContext()
	svc := s.service.(*invoiceService)
	svc.MeterUsageRepo = s.GetStores().MeterUsageRepo
	svc.MeterUsageCheckpointRepo = s.GetStores().MeterUsageCheckpointRepo
	start := cutoff.AddDate(0, -1, 0)
	currentStart, currentEnd := start, cutoff
	if rolled {
		currentStart, currentEnd = cutoff, cutoff.AddDate(0, 1, 0)
	}
	m := &meter.Meter{
		ID: "meter_" + id, EventName: "storage_changed", ResetUsage: types.ResetUsageNever,
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "quantity"}, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	p := &price.Price{
		ID: "price_" + id, Amount: amount, Currency: "usd", EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID: s.testData.plan.ID, Type: types.PRICE_TYPE_USAGE, MeterID: m.ID,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
	sub := &subscription.Subscription{
		ID: id, PlanID: s.testData.plan.ID, CustomerID: s.testData.customer.ID, StartDate: start,
		CurrentPeriodStart: currentStart, CurrentPeriodEnd: currentEnd, BillingAnchor: start,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, ProrationBehavior: types.ProrationBehaviorNone,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID: "li_" + id, SubscriptionID: sub.ID, CustomerID: sub.CustomerID, EntityID: sub.PlanID,
		EntityType: types.SubscriptionLineItemEntityTypePlan, PriceID: p.ID, PriceType: p.Type, MeterID: m.ID,
		Quantity: decimal.NewFromInt(1), Currency: "usd", BillingPeriod: sub.BillingPeriod,
		InvoiceCadence: types.InvoiceCadenceArrear, StartDate: start, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	records := []*events.MeterUsage{}
	for i, quantity := range []int64{100, -30} {
		records = append(records, &events.MeterUsage{
			Event: events.Event{ID: types.GenerateUUID(), TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				ExternalCustomerID: s.testData.customer.ExternalID, EventName: m.EventName, Timestamp: start.Add(time.Duration(i+1) * time.Hour)},
			MeterID: m.ID, QtyTotal: decimal.NewFromInt(quantity),
		})
	}
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
	draft, err := s.service.CreateDraftInvoiceForSubscription(ctx, dto.CreateSubscriptionDraftInvoiceRequest{
		SubscriptionID: sub.ID, PeriodStart: start, PeriodEnd: cutoff, ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.Require().NoError(err)
	stored, err := s.invoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	return stored
}

func (s *InvoiceServiceSuite) TestComputeCompletedCycleSavesUsageCheckpoint() {
	now := time.Now().UTC()
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, amount := range []decimal.Decimal{decimal.NewFromInt(1), decimal.Zero} {
		id := "sub_checkpoint_" + amount.String()
		draft := s.checkpointCycleDraft(id, amount, cutoff, true)
		params := meterusagecheckpoint.GetLatestBeforeParams{CustomerID: s.testData.customer.ID, MeterID: "meter_" + id,
			AggregationType: types.AggregationSum, Before: cutoff.Add(time.Second)}
		before, err := s.GetStores().MeterUsageCheckpointRepo.GetLatestBefore(s.GetContext(), params)
		s.Require().NoError(err)
		s.Nil(before, "empty draft creation must not save a checkpoint")
		for attempt := 0; attempt < 2; attempt++ {
			computed, skipped, err := s.service.ComputeInvoice(s.GetContext(), draft.ID, nil)
			s.Require().NoError(err)
			s.Equal(amount.IsZero(), skipped)
			saved, err := s.GetStores().MeterUsageCheckpointRepo.GetLatestBefore(s.GetContext(), params)
			s.Require().NoError(err)
			s.Require().NotNil(saved)
			s.True(saved.Total.Equal(decimal.NewFromInt(70)), "checkpoint must retain signed usage even for a zero-price invoice")
			s.True(saved.AggregatedUntil.Equal(cutoff), "use invoice period, not the advanced subscription period")
			s.Require().NotNil(saved.InvoiceID)
			s.Equal(computed.ID, *saved.InvoiceID)
		}
	}
}

func (s *InvoiceServiceSuite) TestComputeOpenCycleDoesNotSaveUsageCheckpoint() {
	cutoff := time.Now().UTC().AddDate(0, 0, 1)
	draft := s.checkpointCycleDraft("sub_checkpoint_open", decimal.NewFromInt(1), cutoff, false)
	_, _, err := s.service.ComputeInvoice(s.GetContext(), draft.ID, nil)
	s.Require().NoError(err)
	saved, err := s.GetStores().MeterUsageCheckpointRepo.GetLatestBefore(s.GetContext(), meterusagecheckpoint.GetLatestBeforeParams{
		CustomerID: s.testData.customer.ID, MeterID: "meter_sub_checkpoint_open", AggregationType: types.AggregationSum, Before: cutoff.Add(time.Second),
	})
	s.Require().NoError(err)
	s.Nil(saved)
}

func (s *InvoiceServiceSuite) TestCheckpointFailureDoesNotFailInvoiceComputation() {
	now := time.Now().UTC()
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	draft := s.checkpointCycleDraft("sub_checkpoint_failure", decimal.NewFromInt(1), cutoff, true)
	s.service.(*invoiceService).MeterUsageCheckpointRepo = failingCheckpointWriter{Repository: s.GetStores().MeterUsageCheckpointRepo}
	computed, skipped, err := s.service.ComputeInvoice(s.GetContext(), draft.ID, nil)
	s.Require().NoError(err)
	s.False(skipped)
	s.NotNil(computed.LastComputedAt)
}
