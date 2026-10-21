package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type EventDeletionServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc          *eventDeletionService
	requestRepo  *testutil.InMemoryEventDeletionRequestStore
	logRepo      *testutil.InMemoryDeletedEventsLogStore
	periodStart  time.Time
	periodEnd    time.Time
	insidePeriod time.Time
}

func TestEventDeletionService(t *testing.T) {
	suite.Run(t, new(EventDeletionServiceSuite))
}

func (s *EventDeletionServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.periodStart = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)
	s.insidePeriod = time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	s.requestRepo = testutil.NewInMemoryEventDeletionRequestStore()
	s.logRepo = testutil.NewInMemoryDeletedEventsLogStore()
	s.svc = s.newService()
}

func (s *EventDeletionServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.requestRepo.Clear()
	s.logRepo.Clear()
}

func (s *EventDeletionServiceSuite) params() ServiceParams {
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		Locker:                       s.GetLocker(),
		CustomerRepo:                 s.GetStores().CustomerRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		EventDeletionRequestRepo:     s.requestRepo,
		EventDeletionLogRepo:         s.logRepo,
	}
}

// newService returns the service with the pre-resolve wait disabled, so a test
// does not sit out the suppression cache interval.
func (s *EventDeletionServiceSuite) newService() *eventDeletionService {
	svc := NewEventDeletionService(s.params()).(*eventDeletionService)
	svc.suppressionWait = 0
	return svc
}

// ---------- fixtures ----------

func (s *EventDeletionServiceSuite) seedCustomer(id, externalID string) *customer.Customer {
	cust := &customer.Customer{
		ID:         id,
		ExternalID: externalID,
		Name:       id,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))
	return cust
}

func (s *EventDeletionServiceSuite) seedMeteredFeature(featureID, meterID, eventName string) *feature.Feature {
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), &meter.Meter{
		ID:        meterID,
		Name:      meterID,
		EventName: eventName,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}))
	feat := &feature.Feature{
		ID:        featureID,
		Name:      featureID,
		MeterID:   meterID,
		Type:      types.FeatureTypeMetered,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), feat))
	return feat
}

// seedSubscription creates a subscription with one usage line item per meter.
func (s *EventDeletionServiceSuite) seedSubscription(subID, customerID string, meterIDs ...string) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 subID,
		CustomerID:         customerID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		StartDate:          s.periodStart,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}

	items := make([]*subscription.SubscriptionLineItem, 0, len(meterIDs))
	for i, meterID := range meterIDs {
		items = append(items, &subscription.SubscriptionLineItem{
			ID:             subID + "_li_" + meterID,
			SubscriptionID: subID,
			CustomerID:     customerID,
			MeterID:        meterID,
			PriceType:      types.PRICE_TYPE_USAGE,
			Currency:       "usd",
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			Quantity:       decimal.NewFromInt(int64(i + 1)),
			BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
		})
	}

	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, items))
	return sub
}

func (s *EventDeletionServiceSuite) seedInvoice(
	id, customerID string,
	subID *string,
	status types.InvoiceStatus,
	periodStart, periodEnd time.Time,
) *invoice.Invoice {
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      customerID,
		SubscriptionID:  subID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   status,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(10),
		AmountRemaining: decimal.NewFromInt(10),
		Total:           decimal.NewFromInt(10),
		Subtotal:        decimal.NewFromInt(10),
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

func (s *EventDeletionServiceSuite) seedMarketplaceMapping(customerID string, provider types.IntegrationProviderType) {
	s.NoError(s.GetStores().EntityIntegrationMappingRepo.Create(s.GetContext(), &entityintegrationmapping.EntityIntegrationMapping{
		ID:               "eim_" + customerID,
		EntityID:         customerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(provider),
		ProviderEntityID: "provider_" + customerID,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *EventDeletionServiceSuite) request() *dto.DeleteEventsRequest {
	return &dto.DeleteEventsRequest{
		ExternalCustomerIDs: []string{"ext_1"},
		PeriodStart:         s.periodStart,
		PeriodEnd:           s.periodEnd,
	}
}

// stateRows returns every state row the suite's repo holds.
func (s *EventDeletionServiceSuite) stateRows() []*events.DeletionRequest {
	rows, err := s.requestRepo.ListAll(s.GetContext(), types.NewNoLimitEventDeletionRequestFilter())
	s.NoError(err)
	return rows
}

func (s *EventDeletionServiceSuite) assertNoStateRow() {
	s.Empty(s.stateRows(), "a refused request must leave no state row behind")
}

func (s *EventDeletionServiceSuite) assertStateRow(status types.EventDeletionStatus, reason types.EventDeletionFailureReason) {
	rows := s.stateRows()
	s.Len(rows, 1)
	s.Equal(status, rows[0].DeletionStatus)
	if status == types.EventDeletionStatusCompleted {
		s.NotNil(rows[0].CompletedAt)
		s.Nil(rows[0].FailureReason)
		return
	}
	s.NotNil(rows[0].FailedAt)
	s.Require().NotNil(rows[0].FailureReason)
	s.Equal(reason.String(), *rows[0].FailureReason)
}

func (s *EventDeletionServiceSuite) assertNothingDeleted() {
	s.Empty(s.logRepo.Logs(), "no audit row may be written when the request is refused")
	s.Zero(s.logRepo.MeterUsageMutations)
	s.Zero(s.logRepo.EventMutations)
}

// ---------- validation and resolution ----------

func (s *EventDeletionServiceSuite) TestDelete_RejectsUnknownCustomer() {
	_, err := s.svc.Delete(s.GetContext(), s.request())

	s.Error(err)
	s.True(ierr.IsValidation(err))
	s.assertNoStateRow()
	s.assertNothingDeleted()
}

func (s *EventDeletionServiceSuite) TestDelete_RejectsUnknownFeature() {
	s.seedCustomer("cust_1", "ext_1")

	req := s.request()
	req.FeatureIDs = []string{"feat_missing"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err)
	s.True(ierr.IsValidation(err))
	s.assertNoStateRow()
}

func (s *EventDeletionServiceSuite) TestDelete_RejectsFeatureWithNoMeter() {
	s.seedCustomer("cust_1", "ext_1")
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), &feature.Feature{
		ID:        "feat_static",
		Name:      "static",
		Type:      types.FeatureTypeStatic,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}))

	req := s.request()
	req.FeatureIDs = []string{"feat_static"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err)
	s.True(ierr.IsValidation(err), "a request narrowing to no meter must be refused, not widened")
	s.assertNoStateRow()
}

// ---------- marketplace guard ----------

func (s *EventDeletionServiceSuite) TestDelete_RefusesMarketplaceCustomer() {
	for _, provider := range []types.IntegrationProviderType{
		types.IntegrationProviderTypeAWSMarketplace,
		types.IntegrationProviderTypeGCPMarketplace,
		types.IntegrationProviderTypeAzureMarketplace,
	} {
		s.Run(string(provider), func() {
			s.SetupTest()
			s.seedCustomer("cust_1", "ext_1")
			s.seedMarketplaceMapping("cust_1", provider)
			s.logRepo.SeedUsage(testutil.SeededUsage{
				EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
				Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5),
			})

			_, err := s.svc.Delete(s.GetContext(), s.request())

			s.Error(err)
			s.True(ierr.IsInvalidOperation(err))
			s.assertNoStateRow()
			s.assertNothingDeleted()
		})
	}
}

func (s *EventDeletionServiceSuite) TestDelete_AllowsNonMarketplaceMapping() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMarketplaceMapping("cust_1", types.IntegrationProviderTypeStripe)
	s.logRepo.SeedUsage(testutil.SeededUsage{
		EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
		Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5),
	})

	resp, err := s.svc.Delete(s.GetContext(), s.request())

	s.NoError(err)
	s.Equal(1, resp.DeletedEvents)
}

// ---------- finalized invoice guard ----------

func (s *EventDeletionServiceSuite) TestDelete_FinalizedInvoiceOverlapShapes() {
	tests := []struct {
		name        string
		periodStart time.Time
		periodEnd   time.Time
		blocks      bool
	}{
		{
			name:        "invoice period identical to the request",
			periodStart: s.periodStart,
			periodEnd:   s.periodEnd,
			blocks:      true,
		},
		{
			name:        "invoice starts before and ends inside",
			periodStart: s.periodStart.AddDate(0, 0, -10),
			periodEnd:   s.periodStart.AddDate(0, 0, 5),
			blocks:      true,
		},
		{
			name:        "invoice starts inside and ends after",
			periodStart: s.periodEnd.AddDate(0, 0, -5),
			periodEnd:   s.periodEnd.AddDate(0, 0, 10),
			blocks:      true,
		},
		{
			name:        "invoice contains the request period",
			periodStart: s.periodStart.AddDate(0, 0, -10),
			periodEnd:   s.periodEnd.AddDate(0, 0, 10),
			blocks:      true,
		},
		{
			name:        "invoice ends exactly at period_start",
			periodStart: s.periodStart.AddDate(0, 0, -10),
			periodEnd:   s.periodStart,
			blocks:      true,
		},
		{
			name:        "invoice entirely before the request period",
			periodStart: s.periodStart.AddDate(0, 0, -20),
			periodEnd:   s.periodStart.AddDate(0, 0, -1),
			blocks:      false,
		},
		{
			name:        "invoice entirely after the request period",
			periodStart: s.periodEnd.AddDate(0, 0, 1),
			periodEnd:   s.periodEnd.AddDate(0, 0, 20),
			blocks:      false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.seedCustomer("cust_1", "ext_1")
			s.seedInvoice("inv_1", "cust_1", nil, types.InvoiceStatusFinalized, tt.periodStart, tt.periodEnd)
			s.logRepo.SeedUsage(testutil.SeededUsage{
				EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
				Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5),
			})

			_, err := s.svc.Delete(s.GetContext(), s.request())

			if tt.blocks {
				s.Error(err)
				s.True(ierr.IsInvalidOperation(err))
				s.assertNoStateRow()
				s.assertNothingDeleted()
				return
			}
			s.NoError(err)
			s.assertStateRow(types.EventDeletionStatusCompleted, "")
		})
	}
}

func (s *EventDeletionServiceSuite) TestDelete_NonFinalizedInvoiceDoesNotBlock() {
	for _, status := range []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusVoided,
	} {
		s.Run(string(status), func() {
			s.SetupTest()
			s.seedCustomer("cust_1", "ext_1")
			s.seedInvoice("inv_1", "cust_1", nil, status, s.periodStart, s.periodEnd)
			s.logRepo.SeedUsage(testutil.SeededUsage{
				EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
				Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5),
			})

			_, err := s.svc.Delete(s.GetContext(), s.request())

			s.NoError(err)
			s.assertStateRow(types.EventDeletionStatusCompleted, "")
		})
	}
}

// A finalized invoice on a subscription the deletion cannot touch is no reason to
// refuse, so the feature-narrowed guard is subscription level.
func (s *EventDeletionServiceSuite) TestDelete_FeatureNarrowedGuardIgnoresUnrelatedSubscription() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")
	s.seedMeteredFeature("feat_b", "meter_b", "event_b")
	s.seedSubscription("subs_a", "cust_1", "meter_a")
	s.seedSubscription("subs_b", "cust_1", "meter_b")
	s.seedInvoice("inv_b", "cust_1", lo.ToPtr("subs_b"), types.InvoiceStatusFinalized, s.periodStart, s.periodEnd)

	s.logRepo.SeedUsage(testutil.SeededUsage{
		EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
		Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5),
	})

	req := s.request()
	req.FeatureIDs = []string{"feat_a"}

	resp, err := s.svc.Delete(s.GetContext(), req)

	s.NoError(err, "only subs_a carries meter_a, so inv_b must not block")
	s.Equal(1, resp.DeletedEvents)
}

func (s *EventDeletionServiceSuite) TestDelete_FeatureNarrowedGuardBlocksOnOwnSubscription() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")
	s.seedSubscription("subs_a", "cust_1", "meter_a")
	s.seedInvoice("inv_a", "cust_1", lo.ToPtr("subs_a"), types.InvoiceStatusFinalized, s.periodStart, s.periodEnd)

	req := s.request()
	req.FeatureIDs = []string{"feat_a"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err)
	s.True(ierr.IsInvalidOperation(err))
	s.assertNoStateRow()
}

// An inherited child owns no line items, so the meter lives on the parent and the
// parent's finalized invoice must block.
func (s *EventDeletionServiceSuite) TestDelete_ParentSubscriptionInvoiceBlocks() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")

	parent := s.seedSubscription("subs_parent", "cust_1", "meter_a")
	child := s.seedSubscription("subs_child", "cust_1")
	child.ParentSubscriptionID = lo.ToPtr(parent.ID)
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), child))

	s.seedInvoice("inv_parent", "cust_1", lo.ToPtr(parent.ID), types.InvoiceStatusFinalized, s.periodStart, s.periodEnd)

	req := s.request()
	req.FeatureIDs = []string{"feat_a"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err)
	s.True(ierr.IsInvalidOperation(err))
}

func (s *EventDeletionServiceSuite) TestDelete_CancelledSubscriptionInvoiceBlocks() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")

	sub := s.seedSubscription("subs_a", "cust_1", "meter_a")
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	s.seedInvoice("inv_a", "cust_1", lo.ToPtr("subs_a"), types.InvoiceStatusFinalized, s.periodStart, s.periodEnd)

	req := s.request()
	req.FeatureIDs = []string{"feat_a"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err, "a churned customer's cancelled subscription still holds finalized invoices")
	s.True(ierr.IsInvalidOperation(err))
}

// ---------- one deletion per customer ----------

func (s *EventDeletionServiceSuite) TestDelete_RefusesWhenAnotherDeletionIsInProgress() {
	s.seedCustomer("cust_1", "ext_1")
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_existing",
		RequestID:          "req_existing",
		ExternalCustomerID: "ext_1",
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          s.periodStart,
	}}))

	_, err := s.svc.Delete(s.GetContext(), s.request())

	s.Error(err)
	s.True(ierr.IsAlreadyExists(err))
	s.assertNothingDeleted()
	s.Len(s.stateRows(), 1, "the loser must not add a row")
}

func (s *EventDeletionServiceSuite) TestDelete_AllowsAfterAPreviousDeletionCompleted() {
	s.seedCustomer("cust_1", "ext_1")
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_done",
		RequestID:          "req_done",
		ExternalCustomerID: "ext_1",
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          s.periodStart,
	}}))
	s.NoError(s.requestRepo.MarkTerminal(s.GetContext(), []string{"evtdel_done"}, types.EventDeletionStatusCompleted, ""))

	_, err := s.svc.Delete(s.GetContext(), s.request())

	s.NoError(err)
	s.Len(s.stateRows(), 2)
}

// ---------- execution ----------

func (s *EventDeletionServiceSuite) TestDelete_DeletesMeteredAndUnmeteredEvents() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5), Properties: `{"a":1}`},
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_b", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(7), Properties: `{"a":1}`},
	)
	s.logRepo.SeedEvents(testutil.SeededEvent{
		EventID: "evt_2", ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, Properties: `{"b":2}`,
	})
	// Out of scope: outside the period, and a different customer.
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_late", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.periodEnd, QtyTotal: decimal.NewFromInt(1)})
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_other", MeterID: "meter_a",
		ExternalCustomerID: "ext_2", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(1)})

	resp, err := s.svc.Delete(s.GetContext(), s.request())
	s.NoError(err)

	s.Equal(2, resp.DeletedEvents, "evt_1 and evt_2")
	s.Equal(uint64(2), resp.DeletedMeterUsageRows, "evt_1 produced two meter rows, evt_2 none")
	s.assertStateRow(types.EventDeletionStatusCompleted, "")

	// Exactly one mutation per table per request, however many events.
	s.Equal(1, s.logRepo.MeterUsageMutations)
	s.Equal(1, s.logRepo.EventMutations)
	s.Equal(1, s.logRepo.RecordCalls)

	remainingUsage := lo.Map(s.logRepo.RemainingUsage(), func(u testutil.SeededUsage, _ int) string { return u.EventID })
	s.ElementsMatch([]string{"evt_late", "evt_other"}, remainingUsage)
	remainingEvents := lo.Map(s.logRepo.RemainingEvents(), func(e testutil.SeededEvent, _ int) string { return e.EventID })
	s.ElementsMatch([]string{"evt_late", "evt_other"}, remainingEvents)
}

func (s *EventDeletionServiceSuite) TestDelete_RecordsOneRowPerEventAndMeter() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5), Properties: `{"a":1}`},
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_b", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(7), Properties: `{"a":1}`},
	)
	s.logRepo.SeedEvents(testutil.SeededEvent{
		EventID: "evt_2", ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, Properties: `{"b":2}`,
	})

	_, err := s.svc.Delete(s.GetContext(), s.request())
	s.NoError(err)

	logs := s.logRepo.Logs()
	s.Len(logs, 3)

	byKey := lo.SliceToMap(logs, func(l testutil.DeletedEventLog) (string, testutil.DeletedEventLog) {
		return l.EventID + "/" + l.MeterID, l
	})

	s.Equal(decimal.NewFromInt(5), byKey["evt_1/meter_a"].QtyTotal)
	s.Equal(`{"a":1}`, byKey["evt_1/meter_a"].Properties)
	s.Equal(decimal.NewFromInt(7), byKey["evt_1/meter_b"].QtyTotal)

	// An event with no meter usage is recorded, not silently deleted.
	unmetered := byKey["evt_2/"]
	s.Equal("evt_2", unmetered.EventID)
	s.Empty(unmetered.MeterID)
	s.True(unmetered.QtyTotal.IsZero())

	// One deleted_at for the whole request, and the actor is stamped.
	deletedAt := logs[0].DeletedAt
	for _, l := range logs {
		s.Equal(deletedAt, l.DeletedAt)
		s.Equal(types.DefaultUserID, l.DeletedBy)
		s.NotEmpty(l.RequestID)
	}
}

func (s *EventDeletionServiceSuite) TestDelete_SkipPayloadStoresNoProperties() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(testutil.SeededUsage{
		EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
		Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5), Properties: `{"secret":"x"}`,
	})

	req := s.request()
	req.SkipPayload = true

	_, err := s.svc.Delete(s.GetContext(), req)
	s.NoError(err)

	logs := s.logRepo.Logs()
	s.Len(logs, 1)
	s.Empty(logs[0].Properties)
	s.Equal(decimal.NewFromInt(5), logs[0].QtyTotal, "everything but the payload is still recorded")

	rows := s.stateRows()
	s.Len(rows, 1)
	s.True(rows[0].SkipPayload, "the flag is recorded on the request, not on every audit row")
}

func (s *EventDeletionServiceSuite) TestDelete_EventIDsNarrowsToThoseIDs() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(1)},
		testutil.SeededUsage{EventID: "evt_2", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(2)},
	)

	req := s.request()
	req.EventIDs = []string{"evt_1"}

	resp, err := s.svc.Delete(s.GetContext(), req)
	s.NoError(err)

	s.Equal(1, resp.DeletedEvents)
	remaining := lo.Map(s.logRepo.RemainingUsage(), func(u testutil.SeededUsage, _ int) string { return u.EventID })
	s.Equal([]string{"evt_2"}, remaining)
}

func (s *EventDeletionServiceSuite) TestDelete_MatchingNothingCompletesWithZeroCounts() {
	s.seedCustomer("cust_1", "ext_1")

	resp, err := s.svc.Delete(s.GetContext(), s.request())
	s.NoError(err)

	s.Zero(resp.DeletedEvents)
	s.Zero(resp.DeletedMeterUsageRows)
	s.assertStateRow(types.EventDeletionStatusCompleted, "")
	s.assertNothingDeleted()
	s.Zero(s.logRepo.RecordCalls, "nothing to record means no insert is issued")
}

// ---------- multi-meter guard ----------

func (s *EventDeletionServiceSuite) TestDelete_RefusesWhenAnEventFeedsAnUnnamedMeter() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")
	s.seedSubscription("subs_a", "cust_1", "meter_a")

	// One event feeding two meters; the request names only one of them.
	s.logRepo.SeedUsage(
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)},
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_other", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(9)},
	)

	req := s.request()
	req.FeatureIDs = []string{"feat_a"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err)
	s.True(ierr.IsInvalidOperation(err))
	s.assertStateRow(types.EventDeletionStatusFailed, types.EventDeletionReasonMultipleMetersPerEvent)
	s.assertNothingDeleted()
	s.Len(s.logRepo.RemainingUsage(), 2, "neither meter row may be touched")
}

func (s *EventDeletionServiceSuite) TestDelete_AllowsWhenEveryMeterOfTheEventIsNamed() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")
	s.seedMeteredFeature("feat_b", "meter_b", "event_a")
	s.seedSubscription("subs_a", "cust_1", "meter_a", "meter_b")

	s.logRepo.SeedUsage(
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)},
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_b", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(9)},
	)

	req := s.request()
	req.FeatureIDs = []string{"feat_a", "feat_b"}

	resp, err := s.svc.Delete(s.GetContext(), req)

	s.NoError(err, "naming every feature the event feeds is the most correct request a tenant can send")
	s.Equal(1, resp.DeletedEvents)
	s.Equal(uint64(2), resp.DeletedMeterUsageRows)
	s.Empty(s.logRepo.RemainingUsage())
}

// A feature-narrowed request never matches an event that produced no meter usage.
func (s *EventDeletionServiceSuite) TestDelete_FeatureNarrowedIgnoresUnmeteredEvents() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")
	s.seedSubscription("subs_a", "cust_1", "meter_a")

	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)})
	s.logRepo.SeedEvents(testutil.SeededEvent{EventID: "evt_unmetered",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod})

	req := s.request()
	req.FeatureIDs = []string{"feat_a"}

	resp, err := s.svc.Delete(s.GetContext(), req)
	s.NoError(err)

	s.Equal(1, resp.DeletedEvents)
	remaining := lo.Map(s.logRepo.RemainingEvents(), func(e testutil.SeededEvent, _ int) string { return e.EventID })
	s.Equal([]string{"evt_unmetered"}, remaining)
}

// ---------- failure paths ----------

func (s *EventDeletionServiceSuite) TestDelete_MarksFailedWhenAMutationTimesOut() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)})
	s.logRepo.FailOn = "delete_meter_usage"
	s.logRepo.FailWith = types.ErrMutationTimeout

	_, err := s.svc.Delete(s.GetContext(), s.request())

	s.Error(err)
	s.assertStateRow(types.EventDeletionStatusFailed, types.EventDeletionReasonMutationTimeout)
	s.Equal(1, s.logRepo.RecordCalls, "the record is written before the delete is attempted")
	s.Zero(s.logRepo.EventMutations, "events is never touched once meter_usage failed")
}

func (s *EventDeletionServiceSuite) TestDelete_MarksFailedWhenAMutationErrors() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)})
	s.logRepo.FailOn = "delete_events"
	s.logRepo.FailWith = ierr.NewError("clickhouse said no").Mark(ierr.ErrDatabase)

	_, err := s.svc.Delete(s.GetContext(), s.request())

	s.Error(err)
	s.assertStateRow(types.EventDeletionStatusFailed, types.EventDeletionReasonMutationFailed)
	s.Equal(1, s.logRepo.MeterUsageMutations, "meter_usage went first and succeeded")
}

func (s *EventDeletionServiceSuite) TestDelete_MarksFailedWhenTheResolveFails() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.FailOn = "resolve"
	s.logRepo.FailWith = ierr.NewError("clickhouse unreachable").Mark(ierr.ErrDatabase)

	_, err := s.svc.Delete(s.GetContext(), s.request())

	s.Error(err)
	s.assertStateRow(types.EventDeletionStatusFailed, types.EventDeletionReasonMutationFailed)
	s.assertNothingDeleted()
}

// A dropped client connection must never leave invoicing blocked.
func (s *EventDeletionServiceSuite) TestDelete_MarksConnectionLostOnCancelledContext() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)})

	// A non-zero wait, so cancelling the request lands inside it.
	s.svc.suppressionWait = time.Minute

	ctx, cancel := context.WithCancel(s.GetContext())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := s.svc.Delete(ctx, s.request())

	s.Error(err)
	s.assertStateRow(types.EventDeletionStatusFailed, types.EventDeletionReasonConnectionLost)
}

// ---------- dedup locks ----------

func (s *EventDeletionServiceSuite) TestDelete_ReleasesDedupLocksForDeletedEvents() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)})

	ctx := s.GetContext()
	key := cache.GenerateKey(ctx, cache.PrefixEvent, "evt_1")

	held, err := s.GetLocker().AcquireLock(ctx, key, time.Hour)
	s.NoError(err)
	s.True(held.AcquiredSuccessfully())

	_, err = s.svc.Delete(ctx, s.request())
	s.NoError(err)

	// A re-ingest under the same id must be able to take the lock again.
	reacquired, err := s.GetLocker().AcquireLock(ctx, key, time.Hour)
	s.NoError(err)
	s.True(reacquired.AcquiredSuccessfully(), "the dedup lock must be released, or the re-ingest is silently skipped")
}

// ---------- multi-customer ----------

func (s *EventDeletionServiceSuite) TestDelete_WritesOneStateRowPerCustomer() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedCustomer("cust_2", "ext_2")
	s.logRepo.SeedUsage(
		testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a", ExternalCustomerID: "ext_1",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(1)},
		testutil.SeededUsage{EventID: "evt_2", MeterID: "meter_a", ExternalCustomerID: "ext_2",
			Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(2)},
	)

	req := s.request()
	req.ExternalCustomerIDs = []string{"ext_1", "ext_2"}

	resp, err := s.svc.Delete(s.GetContext(), req)
	s.NoError(err)

	s.Equal(2, resp.DeletedEvents)
	rows := s.stateRows()
	s.Len(rows, 2)

	// One request id shared by both rows, distinct row ids.
	s.Equal(rows[0].RequestID, rows[1].RequestID)
	s.NotEqual(rows[0].ID, rows[1].ID)
	s.ElementsMatch([]string{"ext_1", "ext_2"},
		lo.Map(rows, func(r *events.DeletionRequest, _ int) string { return r.ExternalCustomerID }))
	for _, row := range rows {
		s.Equal(types.EventDeletionStatusCompleted, row.DeletionStatus)
	}
}

// One customer already under deletion refuses the whole request.
func (s *EventDeletionServiceSuite) TestDelete_RefusesWholeRequestWhenOneCustomerIsBusy() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedCustomer("cust_2", "ext_2")
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_existing",
		RequestID:          "req_existing",
		ExternalCustomerID: "ext_2",
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          s.periodStart,
	}}))

	req := s.request()
	req.ExternalCustomerIDs = []string{"ext_1", "ext_2"}

	_, err := s.svc.Delete(s.GetContext(), req)

	s.Error(err)
	s.True(ierr.IsAlreadyExists(err))
	s.Len(s.stateRows(), 1, "nothing is started for any customer in the request")
}

// The request id is the HTTP request id, so an audit row points at the call.
func (s *EventDeletionServiceSuite) TestDelete_StampsTheHTTPRequestID() {
	s.seedCustomer("cust_1", "ext_1")
	s.logRepo.SeedUsage(testutil.SeededUsage{EventID: "evt_1", MeterID: "meter_a",
		ExternalCustomerID: "ext_1", Timestamp: s.insidePeriod, QtyTotal: decimal.NewFromInt(5)})

	ctx := context.WithValue(s.GetContext(), types.CtxRequestID, "req-abc-123")

	_, err := s.svc.Delete(ctx, s.request())
	s.NoError(err)

	rows, err := s.requestRepo.ListAll(ctx, types.NewNoLimitEventDeletionRequestFilter())
	s.NoError(err)
	s.Len(rows, 1)
	s.Equal("req-abc-123", rows[0].RequestID)
	s.True(lo.EveryBy(s.logRepo.Logs(), func(l testutil.DeletedEventLog) bool {
		return l.RequestID == "req-abc-123"
	}))
}

// A customer pulled in by the parent hop is guarded, so it must also be fenced:
// otherwise its invoice can finalize while the child's usage is being deleted.
func (s *EventDeletionServiceSuite) TestDelete_FencesSubscriptionOwnersFromTheParentHop() {
	s.seedCustomer("cust_child", "ext_child")
	s.seedCustomer("cust_parent", "ext_parent")
	s.seedMeteredFeature("feat_a", "meter_a", "event_a")

	parent := s.seedSubscription("subs_parent", "cust_parent", "meter_a")
	child := s.seedSubscription("subs_child", "cust_child")
	child.ParentSubscriptionID = lo.ToPtr(parent.ID)
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), child))

	req := s.request()
	req.ExternalCustomerIDs = []string{"ext_child"}

	_, err := s.svc.Delete(s.GetContext(), req)
	s.NoError(err)

	s.ElementsMatch([]string{"ext_child", "ext_parent"},
		lo.Map(s.stateRows(), func(r *events.DeletionRequest, _ int) string { return r.ExternalCustomerID }),
		"the guard checks the parent's customer, so the fence must cover it too")
}

// request_id is audit correlation and may repeat across calls, so closing one
// deletion must never close another's rows.
func (s *EventDeletionServiceSuite) TestMarkTerminal_ClosesOnlyTheGivenRows() {
	ctx := s.GetContext()
	open := func(id, externalCustomerID string) {
		s.NoError(s.requestRepo.CreateBulk(ctx, []*events.DeletionRequest{{
			ID:                 id,
			RequestID:          "shared-http-request-id",
			ExternalCustomerID: externalCustomerID,
			PeriodStart:        s.periodStart,
			PeriodEnd:          s.periodEnd,
			DeletionStatus:     types.EventDeletionStatusDeleting,
			StartedAt:          s.periodStart,
		}}))
	}
	open("evtdel_a", "ext_a")
	open("evtdel_b", "ext_b")

	s.NoError(s.requestRepo.MarkTerminal(ctx, []string{"evtdel_a"}, types.EventDeletionStatusCompleted, ""))

	byID := lo.SliceToMap(s.stateRows(), func(r *events.DeletionRequest) (string, *events.DeletionRequest) {
		return r.ID, r
	})
	s.Equal(types.EventDeletionStatusCompleted, byID["evtdel_a"].DeletionStatus)
	s.Equal(types.EventDeletionStatusDeleting, byID["evtdel_b"].DeletionStatus,
		"a shared request id must not let one deletion close another's row")
}
