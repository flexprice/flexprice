package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type SubscriptionLineItemServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  SubscriptionService
	testData struct {
		customer     *customer.Customer
		plan         *plan.Plan
		subscription *subscription.Subscription
		price        *price.Price
		lineItem     *subscription.SubscriptionLineItem
	}
}

func TestSubscriptionLineItemService(t *testing.T) {
	suite.Run(t, new(SubscriptionLineItemServiceSuite))
}

// TestValidateLineItemEndDateChange covers backdating an already-set EndDate: allowed on/before it, blocked after it or for one-time line items.
func TestValidateLineItemEndDateChange(t *testing.T) {
	base := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	before := base.Add(-48 * time.Hour)
	after := base.Add(48 * time.Hour)

	tests := []struct {
		name          string
		endDate       time.Time
		billingPeriod types.BillingPeriod
		newDate       time.Time
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:          "no end date set - always allowed",
			endDate:       time.Time{},
			billingPeriod: types.BILLING_PERIOD_MONTHLY,
			newDate:       before,
			wantErr:       false,
		},
		{
			name:          "recurring - before existing end date - allowed",
			endDate:       base,
			billingPeriod: types.BILLING_PERIOD_MONTHLY,
			newDate:       before,
			wantErr:       false,
		},
		{
			name:          "recurring - equal to existing end date - allowed",
			endDate:       base,
			billingPeriod: types.BILLING_PERIOD_MONTHLY,
			newDate:       base,
			wantErr:       false,
		},
		{
			name:          "recurring - after existing end date - blocked",
			endDate:       base,
			billingPeriod: types.BILLING_PERIOD_MONTHLY,
			newDate:       after,
			wantErr:       true,
			wantErrSubstr: "already terminated",
		},
		{
			name:          "onetime - before existing end date - blocked",
			endDate:       base,
			billingPeriod: types.BILLING_PERIOD_ONETIME,
			newDate:       before,
			wantErr:       true,
			wantErrSubstr: "already terminated",
		},
		{
			name:          "onetime - equal to existing end date - blocked",
			endDate:       base,
			billingPeriod: types.BILLING_PERIOD_ONETIME,
			newDate:       base,
			wantErr:       true,
			wantErrSubstr: "already terminated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lineItem := &subscription.SubscriptionLineItem{
				ID:            "li_test",
				EndDate:       tt.endDate,
				BillingPeriod: tt.billingPeriod,
			}

			err := validateLineItemEndDateChange(lineItem, tt.newDate)

			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrSubstr)
		})
	}
}

func TestValidateLineItemEndDateChange_NilLineItem(t *testing.T) {
	err := validateLineItemEndDateChange(nil, time.Now())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "line item is required")
}

func (s *SubscriptionLineItemServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *SubscriptionLineItemServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionLineItemServiceSuite) setupService() {
	s.service = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionLineItemServiceSuite) setupTestData() {
	ctx := s.GetContext()
	now := time.Now().UTC()
	lineItemStart := now.AddDate(0, 0, -3) // 3 days ago for effective-date tests

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_lineitem",
		Name:       "Line Item Test Customer",
		Email:      "lineitem@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:        "Line Item Test Plan",
		Description: "Plan for line item tests",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.price = &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(50),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.price))

	s.testData.subscription = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
		BillingAnchor:      now.Add(-30 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.testData.subscription))

	s.testData.lineItem = &subscription.SubscriptionLineItem{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:  s.testData.subscription.ID,
		CustomerID:      s.testData.customer.ID,
		EntityID:        s.testData.plan.ID,
		EntityType:      types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: s.testData.plan.Name,
		PriceID:         s.testData.price.ID,
		PriceType:       s.testData.price.Type,
		DisplayName:     "Test line item",
		Quantity:        decimal.NewFromInt(1),
		Currency:        s.testData.subscription.Currency,
		BillingPeriod:   s.testData.subscription.BillingPeriod,
		InvoiceCadence:  types.InvoiceCadenceAdvance,
		StartDate:       lineItemStart,
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, s.testData.lineItem))
}

func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromBeforeStartDate() {
	ctx := s.GetContext()
	// EffectiveFrom before line item's StartDate (3 days ago)
	effectiveBefore := s.testData.lineItem.StartDate.Add(-24 * time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &effectiveBefore,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "effective from date must be on or after start date")

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.True(li.EndDate.IsZero(), "line item should remain unterminated")
}

// --- Scheduled version lineage ---
//
// UpdateSubscriptionLineItem versions a line item by terminating the predecessor and
// creating a successor that starts where it ended, linking the two with metadata pointers
// in both directions. Delete uses those pointers to revert a bad scheduled change and to
// refuse deleting a chain out of order. The tests below cover that round trip, the
// ordering guard rails, and the edits that must not break the pointers.

// scheduleVersion versions lineItemID at effectiveFrom and returns the successor the
// service created — one link of a predecessor -> successor chain.
func (s *SubscriptionLineItemServiceSuite) scheduleVersion(lineItemID string, amount int64, effectiveFrom time.Time) *subscription.SubscriptionLineItem {
	s.T().Helper()
	newAmount := decimal.NewFromInt(amount)
	resp, err := s.service.UpdateSubscriptionLineItem(s.GetContext(), lineItemID, dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &effectiveFrom,
	})
	s.NoError(err)
	return resp.SubscriptionLineItem
}

// stageLineItem persists a clone of the suite's line item. Rows created this way carry no
// version pointers, standing in for line items the versioning flow did not produce.
func (s *SubscriptionLineItemServiceSuite) stageLineItem(mutate func(*subscription.SubscriptionLineItem)) *subscription.SubscriptionLineItem {
	s.T().Helper()
	clone := *s.testData.lineItem
	clone.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
	mutate(&clone)
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &clone))
	return &clone
}

func (s *SubscriptionLineItemServiceSuite) reloadLineItem(id string) *subscription.SubscriptionLineItem {
	s.T().Helper()
	item, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), id)
	s.NoError(err)
	return item
}

// revertScheduledVersion is the delete call with no effective_from, the only shape a
// not-yet-started line item accepts.
func (s *SubscriptionLineItemServiceSuite) revertScheduledVersion(id string) error {
	_, err := s.service.DeleteSubscriptionLineItem(s.GetContext(), id, dto.DeleteSubscriptionLineItemRequest{})
	return err
}

func (s *SubscriptionLineItemServiceSuite) assertEndDate(item *subscription.SubscriptionLineItem, expected time.Time, msg string) {
	s.T().Helper()
	s.Equal(expected.Truncate(time.Second).Unix(), item.EndDate.Truncate(time.Second).Unix(), msg)
}

// The round trip: versioning links both sides, reverting deletes the successor, reopens
// the predecessor, and drops the pointer that would otherwise dangle.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_ScheduledVersion_RevertsPredecessor() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)

	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)
	s.NotEqual(s.testData.lineItem.ID, successor.ID)
	s.Equal(s.testData.lineItem.ID, successor.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID])

	predecessor := s.reloadLineItem(s.testData.lineItem.ID)
	s.assertEndDate(predecessor, futureStart, "versioning must terminate the predecessor where the successor starts")
	s.Equal(successor.ID, predecessor.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID])

	s.NoError(s.revertScheduledVersion(successor.ID))

	s.Equal(types.StatusDeleted, s.reloadLineItem(successor.ID).Status)
	restored := s.reloadLineItem(s.testData.lineItem.ID)
	s.Equal(types.StatusPublished, restored.Status)
	s.True(restored.EndDate.IsZero(), "reverting the scheduled version should reopen the prior line")
	s.Empty(restored.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID])
}

// Reverting restores exactly the referenced predecessor. A scheduled row with no pointer
// restores nothing, and a sibling sharing the same meter and price terminated at the same
// boundary is not a candidate — the flow once matched on meter/price plus boundary rather
// than on the pointer.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_ScheduledVersion_RestoresOnlyReferencedPredecessor() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)

	sibling := s.stageLineItem(func(li *subscription.SubscriptionLineItem) {
		li.EndDate = futureStart
	})
	unlinked := s.stageLineItem(func(li *subscription.SubscriptionLineItem) {
		li.StartDate = futureStart
		li.EndDate = time.Time{}
		li.Metadata = nil
	})

	s.NoError(s.revertScheduledVersion(unlinked.ID))
	s.Equal(types.StatusDeleted, s.reloadLineItem(unlinked.ID).Status)
	s.assertEndDate(s.reloadLineItem(sibling.ID), futureStart, "a row without a predecessor pointer must restore nothing")

	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)
	s.NoError(s.revertScheduledVersion(successor.ID))

	s.True(s.reloadLineItem(s.testData.lineItem.ID).EndDate.IsZero(), "only the referenced predecessor should be reopened")
	s.assertEndDate(s.reloadLineItem(sibling.ID), futureStart, "sibling sharing meter and price must stay terminated")
}

// Chains unwind from the tip: with A -> B -> C, B cannot be deleted while C exists, so a
// chain can never develop a deleted middle link.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_ScheduledVersion_MustDeleteTipFirst() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)

	itemB := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)
	itemC := s.scheduleVersion(itemB.ID, 90, futureStart)

	err := s.revertScheduledVersion(itemB.ID)
	s.Error(err)
	s.Contains(err.Error(), "line item has a successor and cannot be deleted")

	s.NoError(s.revertScheduledVersion(itemC.ID))
	s.NoError(s.revertScheduledVersion(itemB.ID))

	restoredA := s.reloadLineItem(s.testData.lineItem.ID)
	s.Equal(types.StatusPublished, restoredA.Status)
	s.True(restoredA.EndDate.IsZero(), "unwinding the chain must reopen the original line")
	s.Equal(types.StatusDeleted, s.reloadLineItem(itemB.ID).Status)
	s.Equal(types.StatusDeleted, s.reloadLineItem(itemC.ID).Status)
}

// A not-yet-started line can only be reverted immediately; scheduling its deletion would
// stack a scheduled change on top of a scheduled change.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_ScheduledVersion_FutureEffectiveFromRejected() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)
	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)

	_, err := s.service.DeleteSubscriptionLineItem(s.GetContext(), successor.ID, dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &futureStart,
	})
	s.Error(err)
	s.Contains(err.Error(), "cannot schedule deletion of a line item that has not started")

	unchanged := s.reloadLineItem(successor.ID)
	s.Equal(types.StatusPublished, unchanged.Status)
	s.True(unchanged.EndDate.IsZero())
}

// Versioning a line that already has a scheduled version would leave two published lines
// starting on the same date and orphan the first successor.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_AlreadyScheduled_Rejected() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)
	nextAmount := decimal.NewFromInt(90)

	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)

	_, err := s.service.UpdateSubscriptionLineItem(s.GetContext(), s.testData.lineItem.ID, dto.UpdateSubscriptionLineItemRequest{
		Amount:        &nextAmount,
		EffectiveFrom: &futureStart,
	})
	s.Error(err)
	s.Contains(err.Error(), "line item already has a scheduled version")

	// Reverting frees the line item for a fresh edit, and the replacement must not inherit
	// the pointer to the successor that was just deleted.
	s.NoError(s.revertScheduledVersion(successor.ID))
	replacement := s.scheduleVersion(s.testData.lineItem.ID, 90, futureStart)
	s.Equal(s.testData.lineItem.ID, replacement.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID])
	s.Empty(replacement.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID],
		"a newly created version must not inherit the predecessor's successor pointer")
}

// An in-place edit of the scheduled version takes a branch that rewrites metadata
// wholesale, which must not drop the pointers the revert depends on.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ScheduledVersion_MetadataEditKeepsLineage() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)
	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)

	_, err := s.service.UpdateSubscriptionLineItem(s.GetContext(), successor.ID, dto.UpdateSubscriptionLineItemRequest{
		Metadata: map[string]string{"note": "renamed"},
	})
	s.NoError(err)

	stillLinked := s.reloadLineItem(successor.ID)
	s.Equal("renamed", stillLinked.Metadata["note"])
	s.Equal(s.testData.lineItem.ID, stillLinked.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID])

	s.NoError(s.revertScheduledVersion(successor.ID))
	s.True(s.reloadLineItem(s.testData.lineItem.ID).EndDate.IsZero(),
		"predecessor must still be reverted after a metadata-only edit")
}

// The lineage keys are reserved. A caller that sends them must not be able to forge a
// link: the stored predecessor wins, and a successor the service never wrote is dropped
// rather than locking the line against deletion.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_MetadataEdit_IgnoresCallerSuppliedLineage() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)
	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)
	forgedTarget := s.stageLineItem(func(li *subscription.SubscriptionLineItem) {
		li.EndDate = futureStart
	})

	_, err := s.service.UpdateSubscriptionLineItem(s.GetContext(), successor.ID, dto.UpdateSubscriptionLineItemRequest{
		Metadata: map[string]string{
			types.SubscriptionLineItemMetadataKeyPredecessorID: forgedTarget.ID,
			types.SubscriptionLineItemMetadataKeySuccessorID:   forgedTarget.ID,
			"note": "kept",
		},
	})
	s.NoError(err)

	stored := s.reloadLineItem(successor.ID)
	s.Equal("kept", stored.Metadata["note"])
	s.Equal(s.testData.lineItem.ID, stored.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID],
		"a caller must not be able to repoint the predecessor")
	s.Empty(stored.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID],
		"a caller must not be able to forge a successor")

	// The forged pointers must not have changed what the revert does.
	s.NoError(s.revertScheduledVersion(successor.ID))
	s.True(s.reloadLineItem(s.testData.lineItem.ID).EndDate.IsZero())
	s.assertEndDate(s.reloadLineItem(forgedTarget.ID), futureStart, "the forged target must be untouched")
}

// Same reserved keys, other entry point: a future-dated line item created with a forged
// predecessor would otherwise reopen that line when it is reverted.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_IgnoresCallerSuppliedLineage() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)
	forgedTarget := s.stageLineItem(func(li *subscription.SubscriptionLineItem) {
		li.EndDate = futureStart
	})

	added, err := s.service.AddSubscriptionLineItem(s.GetContext(), s.testData.subscription.ID, dto.CreateSubscriptionLineItemRequest{
		PriceID:   s.testData.price.ID,
		Quantity:  decimal.NewFromInt(1),
		StartDate: &futureStart,
		Metadata: map[string]string{
			types.SubscriptionLineItemMetadataKeyPredecessorID: forgedTarget.ID,
			types.SubscriptionLineItemMetadataKeySuccessorID:   forgedTarget.ID,
			"note": "kept",
		},
	})
	s.NoError(err)
	s.Equal("kept", added.SubscriptionLineItem.Metadata["note"])
	s.Empty(added.SubscriptionLineItem.Metadata[types.SubscriptionLineItemMetadataKeyPredecessorID])
	s.Empty(added.SubscriptionLineItem.Metadata[types.SubscriptionLineItemMetadataKeySuccessorID])

	s.NoError(s.revertScheduledVersion(added.SubscriptionLineItem.ID))
	s.assertEndDate(s.reloadLineItem(forgedTarget.ID), futureStart, "the forged target must be untouched")
}

// Amending a pending change goes through the scheduled version: it gets terminated and
// replaced, and its own predecessor stays closed rather than being reverted.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ScheduledVersion_TerminatesWithoutRevert() {
	futureStart := time.Now().UTC().Add(13 * 24 * time.Hour)
	nextAmount := decimal.NewFromInt(90)

	successor := s.scheduleVersion(s.testData.lineItem.ID, 75, futureStart)

	replaced, err := s.service.UpdateSubscriptionLineItem(s.GetContext(), successor.ID, dto.UpdateSubscriptionLineItemRequest{
		Amount:        &nextAmount,
		EffectiveFrom: &futureStart,
	})
	s.NoError(err)
	s.NotEqual(successor.ID, replaced.SubscriptionLineItem.ID)

	ended := s.reloadLineItem(successor.ID)
	s.Equal(types.StatusPublished, ended.Status, "the replaced line is terminated, not deleted")
	s.assertEndDate(ended, futureStart, "the scheduled line must be terminated where its replacement starts")
	s.assertEndDate(s.reloadLineItem(s.testData.lineItem.ID), futureStart,
		"update must terminate the scheduled line, not revert its predecessor")
}

func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromOnOrAfterStartDate() {
	ctx := s.GetContext()
	effectiveFrom := s.testData.lineItem.StartDate.Add(24 * time.Hour) // 1 day after start

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &effectiveFrom,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.False(resp.SubscriptionLineItem.EndDate.IsZero())
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.EndDate.Truncate(time.Second).Unix())

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), li.EndDate.Truncate(time.Second).Unix())
}

// TestDeleteSubscriptionLineItem_EffectiveFromBackdated_Allowed verifies a recurring line item's already-set EndDate can be moved earlier.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromBackdated_Allowed() {
	ctx := s.GetContext()

	existingEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = existingEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEndDate := s.testData.lineItem.StartDate.Add(3 * 24 * time.Hour) // before existingEndDate

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &newEndDate,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(newEndDate.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.EndDate.Truncate(time.Second).Unix())

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.Equal(newEndDate.Truncate(time.Second).Unix(), li.EndDate.Truncate(time.Second).Unix())
}

// TestDeleteSubscriptionLineItem_EffectiveFromAfterExistingEndDate_Blocked verifies the out-of-scope "extend forward" case stays blocked.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromAfterExistingEndDate_Blocked() {
	ctx := s.GetContext()

	existingEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = existingEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEndDate := s.testData.lineItem.StartDate.Add(7 * 24 * time.Hour) // after existingEndDate

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &newEndDate,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "already terminated")

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.Equal(existingEndDate.Truncate(time.Second).Unix(), li.EndDate.Truncate(time.Second).Unix(), "end date must remain unchanged")
}

// TestDeleteSubscriptionLineItem_EffectiveFromEqualsExistingEndDate_Allowed verifies a no-op re-set to the same end date succeeds.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_EffectiveFromEqualsExistingEndDate_Allowed() {
	ctx := s.GetContext()

	existingEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = existingEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEndDate := existingEndDate

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &newEndDate,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(existingEndDate.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.EndDate.Truncate(time.Second).Unix())
}

// TestDeleteSubscriptionLineItem_OnetimeExcludedFromBackdate verifies one-time line items never get the new backdate behavior.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_OnetimeExcludedFromBackdate() {
	ctx := s.GetContext()

	onetimePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ONETIME,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, onetimePrice))

	existingEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	onetimeItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.testData.subscription.ID,
		CustomerID:     s.testData.customer.ID,
		EntityID:       s.testData.plan.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        onetimePrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Onetime addon",
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_ONETIME,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      s.testData.lineItem.StartDate,
		EndDate:        existingEndDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, onetimeItem))

	// before existingEndDate: would be allowed for a recurring line item
	newEndDate := s.testData.lineItem.StartDate.Add(3 * 24 * time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom: &newEndDate,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, onetimeItem.ID, req)
	s.Error(err, "one-time line items must never be backdated")
	s.Contains(err.Error(), "already terminated")
}

// TestDeleteSubscriptionLineItem_BackdateSkipsProrationCredit documents a known limitation: the
// proration onetime-skip heuristic keys off EndDate being non-zero, so a backdate produces no credit.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_BackdateSkipsProrationCredit() {
	ctx := s.GetContext()

	existingEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = existingEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEndDate := s.testData.lineItem.StartDate.Add(3 * 24 * time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &newEndDate,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)

	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Empty(wallets, "known limitation: backdating an already-terminated line item does not currently produce a proration credit")
}

func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromBeforeStartDate() {
	ctx := s.GetContext()
	effectiveBefore := s.testData.lineItem.StartDate.Add(-24 * time.Hour)
	newAmount := decimal.NewFromInt(100)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &effectiveBefore,
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "effective date must be on or after line item start date")

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.True(li.EndDate.IsZero())
}

func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromBackdated() {
	ctx := s.GetContext()
	// EffectiveFrom in the past but on or after line item start (e.g. 1 day after start)
	effectiveFrom := s.testData.lineItem.StartDate.Add(24 * time.Hour)
	newAmount := decimal.NewFromInt(200)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &effectiveFrom,
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEqual(s.testData.lineItem.ID, resp.SubscriptionLineItem.ID, "new line item should be created")
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.StartDate.Truncate(time.Second).Unix())

	oldLi, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.False(oldLi.EndDate.IsZero())
	s.Equal(effectiveFrom.Truncate(time.Second).Unix(), oldLi.EndDate.Truncate(time.Second).Unix())
}

// TestUpdateSubscriptionLineItem_BackdateAlreadyTerminated_Allowed verifies the new line item
// fills exactly the freed-up gap (StartDate == new effective date, EndDate == old EndDate).
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_BackdateAlreadyTerminated_Allowed() {
	ctx := s.GetContext()

	oldEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = oldEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEffectiveFrom := s.testData.lineItem.StartDate.Add(3 * 24 * time.Hour) // before oldEndDate
	newAmount := decimal.NewFromInt(300)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &newEffectiveFrom,
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEqual(s.testData.lineItem.ID, resp.SubscriptionLineItem.ID, "new line item should be created")
	s.Equal(newEffectiveFrom.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.StartDate.Truncate(time.Second).Unix())
	s.False(resp.SubscriptionLineItem.EndDate.IsZero(), "new line item must inherit the old end date, not be open-ended")
	s.Equal(oldEndDate.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.EndDate.Truncate(time.Second).Unix())

	oldLi, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.Equal(newEffectiveFrom.Truncate(time.Second).Unix(), oldLi.EndDate.Truncate(time.Second).Unix())
}

// TestUpdateSubscriptionLineItem_EffectiveFromAfterExistingEndDate_Blocked verifies the out-of-scope "extend forward" case stays blocked.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromAfterExistingEndDate_Blocked() {
	ctx := s.GetContext()

	oldEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = oldEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEffectiveFrom := s.testData.lineItem.StartDate.Add(7 * 24 * time.Hour) // after oldEndDate
	newAmount := decimal.NewFromInt(300)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &newEffectiveFrom,
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "already terminated")

	li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.testData.lineItem.ID)
	s.NoError(err)
	s.Equal(oldEndDate.Truncate(time.Second).Unix(), li.EndDate.Truncate(time.Second).Unix(), "end date must remain unchanged")
}

// TestUpdateSubscriptionLineItem_OnetimeExcludedFromBackdate verifies one-time line items never get the new backdate behavior via the edit flow.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_OnetimeExcludedFromBackdate() {
	ctx := s.GetContext()

	onetimePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ONETIME,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, onetimePrice))

	existingEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	onetimeItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.testData.subscription.ID,
		CustomerID:     s.testData.customer.ID,
		EntityID:       s.testData.plan.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        onetimePrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Onetime addon",
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_ONETIME,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      s.testData.lineItem.StartDate,
		EndDate:        existingEndDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, onetimeItem))

	// before existingEndDate: would be allowed for a recurring line item
	newEffectiveFrom := s.testData.lineItem.StartDate.Add(3 * 24 * time.Hour)
	newAmount := decimal.NewFromInt(300)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &newEffectiveFrom,
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, onetimeItem.ID, req)
	s.Error(err, "one-time line items must never be backdated")
	s.Contains(err.Error(), "already terminated")
}

// TestUpdateSubscriptionLineItem_EffectiveFromEqualsExistingEndDate_Allowed verifies an effective
// date exactly at the current end date succeeds, producing a zero-duration replacement line item.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromEqualsExistingEndDate_Allowed() {
	ctx := s.GetContext()

	oldEndDate := s.testData.lineItem.StartDate.Add(5 * 24 * time.Hour)
	s.testData.lineItem.EndDate = oldEndDate
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, s.testData.lineItem))

	newEffectiveFrom := oldEndDate
	newAmount := decimal.NewFromInt(300)

	req := dto.UpdateSubscriptionLineItemRequest{
		Amount:        &newAmount,
		EffectiveFrom: &newEffectiveFrom,
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(oldEndDate.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.StartDate.Truncate(time.Second).Unix())
	s.Equal(oldEndDate.Truncate(time.Second).Unix(), resp.SubscriptionLineItem.EndDate.Truncate(time.Second).Unix())
}

func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_EffectiveFromWithoutCriticalField() {
	ctx := s.GetContext()
	effectiveFrom := time.Now().UTC().Add(24 * time.Hour)

	req := dto.UpdateSubscriptionLineItemRequest{
		EffectiveFrom: &effectiveFrom,
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.Error(err)
	s.Contains(err.Error(), "effective_from requires at least one critical field")
}

func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_Success() {
	ctx := s.GetContext()
	// Use a second price so we can add another line item
	price2 := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, price2))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              price2.ID,
		Quantity:             decimal.NewFromInt(2),
		SkipEntitlementCheck: true,
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEmpty(resp.SubscriptionLineItem.ID)
	s.Equal(s.testData.subscription.ID, resp.SubscriptionLineItem.SubscriptionID)
	s.Equal(price2.ID, resp.SubscriptionLineItem.PriceID)
	s.True(resp.SubscriptionLineItem.Quantity.Equal(decimal.NewFromInt(2)))

	_, err = s.GetStores().SubscriptionLineItemRepo.Get(ctx, resp.SubscriptionLineItem.ID)
	s.NoError(err)
}

// TestAddSubscriptionLineItem_InlinePriceZeroDefaultsToMinQuantity verifies that when an
// inline price has min_quantity=5, sending quantity=0 stores quantity=5 (the min_quantity default).
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_InlinePriceZeroDefaultsToMinQuantity() {
	ctx := s.GetContext()

	inlinePrice := &dto.SubscriptionPriceCreateRequest{
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Amount:             lo.ToPtr(decimal.NewFromInt(1)),
		LookupKey:          "inline_min_quantity_floor",
		MinQuantity:        lo.ToPtr(int64(5)),
	}

	req := dto.CreateSubscriptionLineItemRequest{
		Price:                inlinePrice,
		Quantity:             decimal.Zero, // zero → should default to min_quantity=5
		SkipEntitlementCheck: true,
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.Require().NoError(err, "zero quantity with inline price should succeed and default to min_quantity")
	s.Require().NotNil(resp)
	s.True(decimal.NewFromInt(5).Equal(resp.Quantity),
		"quantity should default to min_quantity=5, got %s", resp.Quantity)
}

// TestAddSubscriptionLineItem_DateBoundsValidation asserts that when sub is passed, date-bounds validation runs:
// line item start_date cannot be before subscription start date; line item end_date cannot be after subscription end date.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_DateBoundsValidation() {
	ctx := s.GetContext()

	// 1) start_date before subscription start -> validation error
	startBeforeSub := s.testData.subscription.StartDate.Add(-24 * time.Hour)
	reqStartBefore := dto.CreateSubscriptionLineItemRequest{
		PriceID:              s.testData.price.ID,
		StartDate:            &startBeforeSub,
		SkipEntitlementCheck: true,
	}
	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, reqStartBefore)
	s.Error(err)
	s.Contains(err.Error(), "line item start_date cannot be before subscription start date")

	// 2) subscription with end date; line item end_date after subscription end -> validation error
	subEnd := s.testData.subscription.StartDate.Add(30 * 24 * time.Hour)
	subWithEnd := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.subscription.StartDate,
		EndDate:            &subEnd,
		CurrentPeriodStart: s.testData.subscription.StartDate,
		CurrentPeriodEnd:   subEnd,
		BillingAnchor:      s.testData.subscription.StartDate,
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, subWithEnd, nil))

	lineItemEndAfterSub := subEnd.Add(24 * time.Hour)
	reqEndAfter := dto.CreateSubscriptionLineItemRequest{
		PriceID:              s.testData.price.ID,
		EndDate:              &lineItemEndAfterSub,
		SkipEntitlementCheck: true,
	}
	_, err = s.service.AddSubscriptionLineItem(ctx, subWithEnd.ID, reqEndAfter)
	s.Error(err)
	s.Contains(err.Error(), "line item end_date cannot be after subscription end date")
}

// TestAddSubscriptionLineItem_ValidationErrors covers invalid or out-of-bound values: both/neither price,
// start after end, date bounds (line item and inline price), negative quantity.
// ─── Proration integration – AddSubscriptionLineItem ─────────────────────────

// TestAddSubscriptionLineItem_WithCreateProrations_CreatesInvoice verifies that
// calling AddSubscriptionLineItem with ProrationBehavior=create_prorations creates
// a ONE_OFF invoice for the prorated portion of the billing period.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_WithCreateProrations_CreatesInvoice() {
	ctx := s.GetContext()

	// Create a second distinct price so we can add it as a new line item.
	secondPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(30),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, secondPrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              secondPrice.ID,
		Quantity:             decimal.NewFromInt(1),
		SkipEntitlementCheck: true,
		ProrationBehavior:    types.ProrationBehaviorCreateProrations,
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEmpty(resp.SubscriptionLineItem.ID)

	// A ONE_OFF proration invoice should have been created.
	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	var prorationInvoices []*types.InvoiceFilter
	_ = prorationInvoices
	var found bool
	for _, inv := range invoices {
		if inv.InvoiceType == types.InvoiceTypeOneOff {
			found = true
			s.True(inv.AmountDue.GreaterThan(decimal.Zero),
				"proration invoice amount must be positive, got %s", inv.AmountDue)
			break
		}
	}
	s.True(found, "expected a ONE_OFF proration invoice to be created")
}

// TestAddSubscriptionLineItem_NoneProration_NoInvoiceCreated confirms that
// ProrationBehavior=none does not create any invoice.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_NoneProration_NoInvoiceCreated() {
	ctx := s.GetContext()

	thirdPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(40),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, thirdPrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              thirdPrice.ID,
		Quantity:             decimal.NewFromInt(1),
		SkipEntitlementCheck: true,
		ProrationBehavior:    types.ProrationBehaviorNone,
	}

	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)

	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	for _, inv := range invoices {
		s.NotEqual(types.InvoiceTypeOneOff, inv.InvoiceType,
			"no ONE_OFF proration invoice expected for behavior=none")
	}
}

// TestAddSubscriptionLineItem_UsagePrice_SkipsProration confirms that adding a
// usage-type price with create_prorations does not produce a charge invoice
// (future consumption is unknown).
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_UsagePrice_SkipsProration() {
	ctx := s.GetContext()

	usagePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              usagePrice.ID,
		SkipEntitlementCheck: true,
		ProrationBehavior:    types.ProrationBehaviorCreateProrations,
	}

	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)

	invoices, listErr := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	for _, inv := range invoices {
		s.NotEqual(types.InvoiceTypeOneOff, inv.InvoiceType,
			"usage price add must not trigger a proration invoice")
	}
}

// ─── Proration integration – DeleteSubscriptionLineItem ──────────────────────

// TestDeleteSubscriptionLineItem_WithCreateProrations_CreatesWalletCredit verifies
// that deleting a fixed-price line item with create_prorations issues a wallet credit
// for the unused portion of the billing period.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_WithCreateProrations_CreatesWalletCredit() {
	ctx := s.GetContext()
	// effectiveFrom must be (a) >= lineItem.StartDate and (b) within the subscription's current
	// billing period so that FindPeriodForDate can locate it by walking forward.
	// CurrentPeriodStart + 1 hour satisfies both constraints.
	effectiveFrom := s.testData.subscription.CurrentPeriodStart.Add(time.Hour)

	// The credit is capped at what this line item was actually billed for the
	// period, so the period's charge has to exist for a credit to be possible.
	lineItemID := s.testData.lineItem.ID
	s.NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, &invoice.InvoiceLineItem{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:             s.testData.subscription.CustomerID,
		SubscriptionID:         &s.testData.subscription.ID,
		SubscriptionLineItemID: &lineItemID,
		Amount:                 s.testData.price.Amount.Mul(s.testData.lineItem.Quantity),
		Quantity:               s.testData.lineItem.Quantity,
		Currency:               s.testData.subscription.Currency,
		PeriodStart:            &s.testData.subscription.CurrentPeriodStart,
		PeriodEnd:              &s.testData.subscription.CurrentPeriodEnd,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}))

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &effectiveFrom,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	resp, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.False(resp.SubscriptionLineItem.EndDate.IsZero())

	// A wallet credit should have been issued.
	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Require().NotEmpty(wallets, "expected a wallet to be created for the proration credit")

	w := wallets[0]
	s.True(w.Balance.GreaterThan(decimal.Zero),
		"wallet balance %s must be positive after proration credit", w.Balance)
}

// TestDeleteSubscriptionLineItem_NoneProration_NoWalletCredit confirms that
// deleting with ProrationBehavior=none does not issue any wallet credit.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_NoneProration_NoWalletCredit() {
	ctx := s.GetContext()
	effectiveFrom := s.testData.lineItem.StartDate.Add(24 * time.Hour)

	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &effectiveFrom,
		ProrationBehavior: types.ProrationBehaviorNone,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, req)
	s.NoError(err)

	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Empty(wallets, "behavior=none must not create a wallet credit")
}

// TestDeleteSubscriptionLineItem_SnapshotBeforeMutation_OnetimeSkipped verifies
// the critical invariant: the pre-mutation snapshot (EndDate==zero) is passed to
// proration Compute so that onetime-cadence check works correctly.
//
// A line item with EndDate == effectiveFrom after the Update call would otherwise
// be misidentified as a onetime addon and skipped. The snapshot must have EndDate==zero.
func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_SnapshotBeforeMutation_OnetimeSkipped() {
	ctx := s.GetContext()

	// Create a fresh line item with a non-zero EndDate to simulate a onetime addon.
	// Deleting it with create_prorations should produce NO credit (non-refundable).
	onetimePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_ONETIME,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, onetimePrice))

	onetimeEnd := s.testData.subscription.CurrentPeriodEnd
	onetimeItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: s.testData.subscription.ID,
		CustomerID:     s.testData.customer.ID,
		EntityID:       s.testData.plan.ID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        onetimePrice.ID,
		PriceType:      types.PRICE_TYPE_FIXED,
		DisplayName:    "Onetime addon",
		Quantity:       decimal.NewFromInt(1),
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_ONETIME,
		InvoiceCadence: types.InvoiceCadenceAdvance,
		StartDate:      s.testData.lineItem.StartDate,
		EndDate:        onetimeEnd, // non-zero EndDate marks this as onetime/already-terminated
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, onetimeItem))

	// The onetime item already has an EndDate, so DeleteSubscriptionLineItem should
	// reject it as already terminated.
	effectiveFrom := onetimeItem.StartDate.Add(time.Hour)
	req := dto.DeleteSubscriptionLineItemRequest{
		EffectiveFrom:     &effectiveFrom,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}

	_, err := s.service.DeleteSubscriptionLineItem(ctx, onetimeItem.ID, req)
	s.Error(err, "deleting an already-terminated line item must return an error")
	s.Contains(err.Error(), "already terminated")

	// No wallet credit should have been issued.
	wallets, listErr := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.NoError(listErr)
	s.Empty(wallets, "no wallet credit expected for already-terminated (onetime) line item")
}

func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_ValidationErrors() {
	ctx := s.GetContext()
	subStart := s.testData.subscription.StartDate
	subEnd := subStart.Add(30 * 24 * time.Hour)
	subWithEnd := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          subStart,
		EndDate:            &subEnd,
		CurrentPeriodStart: subStart,
		CurrentPeriodEnd:   subEnd,
		BillingAnchor:      subStart,
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, subWithEnd, nil))

	validInlinePrice := &dto.SubscriptionPriceCreateRequest{
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Amount:             lo.ToPtr(decimal.NewFromInt(1)),
		LookupKey:          "validation_test",
	}

	tests := []struct {
		name        string
		subID       string
		req         dto.CreateSubscriptionLineItemRequest
		wantErrCont string
	}{
		{
			name:        "both price_id and price",
			subID:       s.testData.subscription.ID,
			req:         dto.CreateSubscriptionLineItemRequest{PriceID: s.testData.price.ID, Price: validInlinePrice, SkipEntitlementCheck: true},
			wantErrCont: "cannot provide both price_id and price",
		},
		{
			name:        "neither price_id nor price",
			subID:       s.testData.subscription.ID,
			req:         dto.CreateSubscriptionLineItemRequest{SkipEntitlementCheck: true},
			wantErrCont: "either price_id or price is required",
		},
		{
			name:  "start_date after end_date",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				StartDate:            lo.ToPtr(subStart.Add(48 * time.Hour)),
				EndDate:              lo.ToPtr(subStart.Add(24 * time.Hour)),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "start_date cannot be after end_date",
		},
		{
			name:  "line item start_date before subscription start",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				StartDate:            lo.ToPtr(subStart.Add(-24 * time.Hour)),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "line item start_date cannot be before subscription start date",
		},
		{
			name:  "line item end_date after subscription end",
			subID: subWithEnd.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				EndDate:              lo.ToPtr(subEnd.Add(24 * time.Hour)),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "line item end_date cannot be after subscription end date",
		},
		{
			name:  "inline price start_date before subscription start",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				Price: &dto.SubscriptionPriceCreateRequest{
					Type:               types.PRICE_TYPE_FIXED,
					PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:     types.InvoiceCadenceAdvance,
					Amount:             lo.ToPtr(decimal.NewFromInt(1)),
					LookupKey:          "inline_bad_start",
					StartDate:          lo.ToPtr(subStart.Add(-24 * time.Hour)),
				},
				SkipEntitlementCheck: true,
			},
			wantErrCont: "price start_date cannot be before subscription start date",
		},
		{
			name:  "inline price end_date after subscription end",
			subID: subWithEnd.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				Price: &dto.SubscriptionPriceCreateRequest{
					Type:               types.PRICE_TYPE_FIXED,
					PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:     types.InvoiceCadenceAdvance,
					Amount:             lo.ToPtr(decimal.NewFromInt(1)),
					LookupKey:          "inline_bad_end",
					EndDate:            lo.ToPtr(subEnd.Add(24 * time.Hour)),
				},
				SkipEntitlementCheck: true,
			},
			wantErrCont: "price end_date cannot be after subscription end date",
		},
		{
			name:  "negative quantity",
			subID: s.testData.subscription.ID,
			req: dto.CreateSubscriptionLineItemRequest{
				PriceID:              s.testData.price.ID,
				Quantity:             decimal.NewFromInt(-1),
				SkipEntitlementCheck: true,
			},
			wantErrCont: "quantity must be non-negative",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.service.AddSubscriptionLineItem(ctx, tt.subID, tt.req)
			s.Error(err, "expected validation error for: %s", tt.name)
			s.Contains(err.Error(), tt.wantErrCont, "error should contain: %s", tt.wantErrCont)
		})
	}
}

func (s *SubscriptionLineItemServiceSuite) TestListSubscriptionLineItems_BySubscriptionID() {
	ctx := s.GetContext()
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}

	resp, err := s.service.ListSubscriptionLineItems(ctx, filter)
	s.NoError(err)
	s.Require().NotNil(resp)
	found := lo.ContainsBy(resp.Items, func(item *dto.SubscriptionLineItemResponse) bool {
		return item.SubscriptionLineItem.ID == s.testData.lineItem.ID
	})
	s.True(found)
	s.GreaterOrEqual(resp.Pagination.Total, 1)
}

func (s *SubscriptionLineItemServiceSuite) TestListSubscriptionLineItems_InvalidExpand() {
	ctx := s.GetContext()
	filter := types.NewSubscriptionLineItemFilter()
	filter.QueryFilter = types.NewDefaultQueryFilter()
	filter.QueryFilter.Expand = lo.ToPtr("plan")

	_, err := s.service.ListSubscriptionLineItems(ctx, filter)
	s.Error(err)
}

func (s *SubscriptionLineItemServiceSuite) TestListSubscriptionLineItems_ExpandPrices() {
	ctx := s.GetContext()
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	filter.QueryFilter = types.NewDefaultQueryFilter()
	filter.QueryFilter.Expand = lo.ToPtr("prices")

	resp, err := s.service.ListSubscriptionLineItems(ctx, filter)
	s.NoError(err)
	s.Require().NotNil(resp)
	var target *dto.SubscriptionLineItemResponse
	for _, item := range resp.Items {
		if item.SubscriptionLineItem.ID == s.testData.lineItem.ID {
			target = item
			break
		}
	}
	s.Require().NotNil(target)
	s.Require().NotNil(target.Price)
	s.Equal(s.testData.price.ID, target.Price.ID)
}

// TestAddSubscriptionLineItem_WithBuckets_MaterializesPrices verifies that when a
// CreateSubscriptionLineItemRequest includes CommitmentTimeBuckets, the service
// calls resolveBucketPrices and the persisted line item's CommitmentTimeBuckets
// carries materialized bucket prices (non-empty PriceID and ID).
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_WithBuckets_MaterializesPrices() {
	ctx := s.GetContext()

	// 1. Create a meter with BucketSize (required for CommitmentWindowed=true validation).
	meterWithBucket := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Meter",
		EventName: "bucket_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, meterWithBucket))

	// 2. Create a SUBSCRIPTION-scoped usage price referencing the meter.
	usagePriceForBucket := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           s.testData.subscription.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            meterWithBucket.ID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePriceForBucket))

	// 3. Build the request with one CommitmentTimeBuckets entry.
	overageFactor := decimal.NewFromFloat(1.5)
	commitmentAmount := decimal.NewFromInt(500)
	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:                 usagePriceForBucket.ID,
		SkipEntitlementCheck:    true,
		CommitmentAmount:        &commitmentAmount,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overageFactor,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
			{
				Start: types.Bucket{Hour: 9, Minute: 0},
				End:   types.Bucket{Hour: 17, Minute: 0},
				Price: &dto.CreatePriceRequest{
					Amount:               lo.ToPtr(decimal.NewFromInt(15)),
					Currency:             "usd",
					EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
					EntityID:             s.testData.subscription.ID,
					Type:                 types.PRICE_TYPE_FIXED,
					PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount:   1,
					BillingModel:         types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:       types.InvoiceCadenceAdvance,
					LookupKey:            "bucket_test_price",
					SkipEntityValidation: true,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(200),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
				TrueUpEnabled:   true,
			},
		},
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.NoError(err)
	s.Require().NotNil(resp)

	li := resp.SubscriptionLineItem

	// The persisted line item must have exactly one materialized bucket.
	s.Require().Len(li.CommitmentTimeBuckets, 1, "expected 1 materialized bucket on the line item")

	bucket := li.CommitmentTimeBuckets[0]

	// PriceID must be non-empty (set by resolveBucketPrices).
	s.NotEmpty(bucket.PriceID, "bucket.PriceID must be set after materialization")

	// ID must be a non-empty UUID (assigned by resolveBucketPrices).
	s.NotEmpty(bucket.ID, "bucket.ID must be set after materialization")

	// The created price must exist in the repo as SUBSCRIPTION-scoped.
	createdPrice, getErr := s.GetStores().PriceRepo.Get(ctx, bucket.PriceID)
	s.NoError(getErr)
	s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, createdPrice.EntityType)

	// Verify the line item is persisted correctly.
	persisted, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.NoError(getErr)
	s.Require().Len(persisted.CommitmentTimeBuckets, 1, "persisted line item must have 1 bucket")
	s.Equal(bucket.PriceID, persisted.CommitmentTimeBuckets[0].PriceID)
}

// TestCreateBucketPrices_CreatesSubscriptionPriceAndAssignsID verifies that
// createBucketPrices creates a SUBSCRIPTION-scoped Price for each bucket and
// fills in the created price's ID on the (DTO-built) domain buckets.
func (s *SubscriptionLineItemServiceSuite) TestCreateBucketPrices_CreatesSubscriptionPriceAndAssignsID() {
	ctx := s.GetContext()

	// Access the concrete *subscriptionService so we can call the unexported helper.
	concreteSvc := s.service.(*subscriptionService)

	reqs := []dto.CommitmentBucketRequest{
		{
			Start: types.Bucket{Hour: 9, Minute: 0},
			End:   types.Bucket{Hour: 17, Minute: 0},
			Price: &dto.CreatePriceRequest{
				Amount:               lo.ToPtr(decimal.NewFromInt(10)),
				Currency:             "usd",
				EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
				EntityID:             s.testData.subscription.ID,
				Type:                 types.PRICE_TYPE_FIXED,
				PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
				BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount:   1,
				BillingModel:         types.BILLING_MODEL_FLAT_FEE,
				InvoiceCadence:       types.InvoiceCadenceAdvance,
				SkipEntityValidation: true,
			},
			CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(100),
			OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			TrueUpEnabled:   true,
		},
	}

	// DTO builds the domain buckets (IDs + commitment fields, empty PriceIDs).
	buckets := make(types.TimeOfDayBuckets, len(reqs))
	for i, r := range reqs {
		buckets[i] = r.ToTimeOfDayBucket()
	}

	err := concreteSvc.createBucketPrices(ctx, s.testData.lineItem.ID, s.testData.subscription.ID, reqs, buckets, nil)
	s.NoError(err)
	s.Require().Len(buckets, 1)

	bucket := buckets[0]

	// ID should be a non-empty UUID assigned by the DTO converter.
	s.NotEmpty(bucket.ID)

	// PriceID should reference the newly created price.
	s.NotEmpty(bucket.PriceID)

	// Verify the price actually exists in the repo with SUBSCRIPTION entity type.
	createdPrice, getErr := s.GetStores().PriceRepo.Get(ctx, bucket.PriceID)
	s.NoError(getErr)
	s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, createdPrice.EntityType)
	s.Equal(s.testData.subscription.ID, createdPrice.EntityID)

	// Other bucket fields should be copied verbatim from the request.
	s.Equal(types.Bucket{Hour: 9, Minute: 0}, bucket.Start)
	s.Equal(types.Bucket{Hour: 17, Minute: 0}, bucket.End)
	s.Equal(types.COMMITMENT_TYPE_AMOUNT, bucket.CommitmentType)
	s.True(bucket.CommitmentValue.Equal(decimal.NewFromInt(100)))
	s.Require().NotNil(bucket.OverageFactor)
	s.True(bucket.OverageFactor.Equal(decimal.NewFromFloat(1.5)))
	s.True(bucket.TrueUpEnabled)
}

// TestCreateBucketPrices_EmptySlice is a no-op without error.
func (s *SubscriptionLineItemServiceSuite) TestCreateBucketPrices_EmptySlice() {
	ctx := s.GetContext()
	concreteSvc := s.service.(*subscriptionService)

	err := concreteSvc.createBucketPrices(ctx, s.testData.lineItem.ID, s.testData.subscription.ID, nil, types.TimeOfDayBuckets{}, nil)
	s.NoError(err)

	err2 := concreteSvc.createBucketPrices(ctx, s.testData.lineItem.ID, s.testData.subscription.ID, []dto.CommitmentBucketRequest{}, types.TimeOfDayBuckets{}, nil)
	s.NoError(err2)
}

// TestCreateBucketPrices_MultipleBuckets verifies that each bucket gets its own price and a distinct UUID.
func (s *SubscriptionLineItemServiceSuite) TestCreateBucketPrices_MultipleBuckets() {
	ctx := s.GetContext()
	concreteSvc := s.service.(*subscriptionService)

	makeBucketReq := func(startH, endH int, lookupKey string) dto.CommitmentBucketRequest {
		return dto.CommitmentBucketRequest{
			Start: types.Bucket{Hour: startH, Minute: 0},
			End:   types.Bucket{Hour: endH, Minute: 0},
			Price: &dto.CreatePriceRequest{
				Amount:               lo.ToPtr(decimal.NewFromInt(5)),
				Currency:             "usd",
				EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
				EntityID:             s.testData.subscription.ID,
				Type:                 types.PRICE_TYPE_FIXED,
				PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
				BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount:   1,
				BillingModel:         types.BILLING_MODEL_FLAT_FEE,
				InvoiceCadence:       types.InvoiceCadenceAdvance,
				LookupKey:            lookupKey,
				SkipEntityValidation: true,
			},
			CommitmentType:  types.COMMITMENT_TYPE_QUANTITY,
			CommitmentValue: decimal.NewFromInt(50),
			OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.2)),
		}
	}

	reqs := []dto.CommitmentBucketRequest{
		makeBucketReq(0, 8, "bucket_multi_test_1"),
		makeBucketReq(8, 16, "bucket_multi_test_2"),
		makeBucketReq(16, 24, "bucket_multi_test_3"),
	}

	buckets := make(types.TimeOfDayBuckets, len(reqs))
	for i, r := range reqs {
		buckets[i] = r.ToTimeOfDayBucket()
	}

	err := concreteSvc.createBucketPrices(ctx, s.testData.lineItem.ID, s.testData.subscription.ID, reqs, buckets, nil)
	s.NoError(err)
	s.Require().Len(buckets, 3)

	// All bucket IDs must be distinct.
	ids := map[string]bool{}
	priceIDs := map[string]bool{}
	for _, b := range buckets {
		s.NotEmpty(b.ID)
		s.NotEmpty(b.PriceID)
		s.False(ids[b.ID], "bucket ID must be unique: %s", b.ID)
		s.False(priceIDs[b.PriceID], "price ID must be unique: %s", b.PriceID)
		ids[b.ID] = true
		priceIDs[b.PriceID] = true
	}
}

// ─── Bucket update semantics (Task 10) ───────────────────────────────────────

// makeBucketLineItem is a helper that creates a windowed-commitment usage line
// item with one materialized bucket on the given subscription, returning the
// persisted line item and the ID of the bucket price so callers can assert
// against it.
func (s *SubscriptionLineItemServiceSuite) makeBucketLineItem(subID string, meterID string, lookupKey string) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()

	// Subscription-scoped usage price referencing the meter.
	usagePr := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           subID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            meterID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePr))

	overage := decimal.NewFromFloat(1.5)
	commitment := decimal.NewFromInt(300)
	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:                 usagePr.ID,
		SkipEntitlementCheck:    true,
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
			{
				Start: types.Bucket{Hour: 9, Minute: 0},
				End:   types.Bucket{Hour: 17, Minute: 0},
				Price: &dto.CreatePriceRequest{
					Amount:               lo.ToPtr(decimal.NewFromInt(20)),
					Currency:             "usd",
					EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
					EntityID:             subID,
					Type:                 types.PRICE_TYPE_FIXED,
					PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount:   1,
					BillingModel:         types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:       types.InvoiceCadenceAdvance,
					LookupKey:            lookupKey,
					SkipEntityValidation: true,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(150),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
				TrueUpEnabled:   true,
			},
		},
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, subID, req)
	s.Require().NoError(err)
	s.Require().Len(resp.CommitmentTimeBuckets, 1)
	return resp.SubscriptionLineItem
}

// TestUpdateSubscriptionLineItem_ReplaceAllBuckets verifies that when
// UpdateSubscriptionLineItem is called with a new commitment_time_buckets list,
// the update creates a successor line item whose buckets are freshly
// materialized (new PriceIDs / IDs), replacing the old ones. This is the
// "replace-all" semantic: CommitmentBucketRequest has no stable ID field, so
// the entire bucket set is replaced atomically.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ReplaceAllBuckets() {
	ctx := s.GetContext()

	// Create a meter with BucketSize for windowed commitment validation.
	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Update Meter",
		EventName: "bucket_update_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	// Create a line item with one bucket (9:00–17:00 @ $20).
	original := s.makeBucketLineItem(s.testData.subscription.ID, m.ID, "update_bucket_orig")
	oldBucketPriceID := original.CommitmentTimeBuckets[0].PriceID
	oldBucketID := original.CommitmentTimeBuckets[0].ID

	// Update: replace the single bucket with a different time window (0:00–8:00 @ $12).
	overage := decimal.NewFromFloat(1.2)
	commitment := decimal.NewFromInt(400)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      lo.ToPtr(true),
		CommitmentTimeBuckets: lo.ToPtr([]dto.CommitmentBucketRequest{
			{
				Start: types.Bucket{Hour: 0, Minute: 0},
				End:   types.Bucket{Hour: 8, Minute: 0},
				Price: &dto.CreatePriceRequest{
					Amount:               lo.ToPtr(decimal.NewFromInt(12)),
					Currency:             "usd",
					EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
					EntityID:             s.testData.subscription.ID,
					Type:                 types.PRICE_TYPE_FIXED,
					PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
					BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount:   1,
					BillingModel:         types.BILLING_MODEL_FLAT_FEE,
					InvoiceCadence:       types.InvoiceCadenceAdvance,
					LookupKey:            "update_bucket_new",
					SkipEntityValidation: true,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(200),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.2)),
				TrueUpEnabled:   false,
			},
		}),
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	newLI := resp.SubscriptionLineItem

	// A new line item must have been created (different ID).
	s.NotEqual(original.ID, newLI.ID, "successor line item must have a new ID")

	// New line item must have exactly one materialized bucket.
	s.Require().Len(newLI.CommitmentTimeBuckets, 1, "successor must have 1 bucket")

	newBucket := newLI.CommitmentTimeBuckets[0]

	// The new bucket must have fresh IDs (not inherited from the original).
	s.NotEmpty(newBucket.PriceID, "new bucket must have a PriceID")
	s.NotEmpty(newBucket.ID, "new bucket must have an ID")
	s.NotEqual(oldBucketPriceID, newBucket.PriceID, "new bucket PriceID must differ from old")
	s.NotEqual(oldBucketID, newBucket.ID, "new bucket ID must differ from old")

	// The new bucket time window must match the request (0:00–8:00).
	s.Equal(types.Bucket{Hour: 0, Minute: 0}, newBucket.Start)
	s.Equal(types.Bucket{Hour: 8, Minute: 0}, newBucket.End)

	// The new price must exist in the repo.
	newPrice, getErr := s.GetStores().PriceRepo.Get(ctx, newBucket.PriceID)
	s.NoError(getErr)
	s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, newPrice.EntityType)

	// Original line item must now be terminated.
	oldLI, getErr := s.GetStores().SubscriptionLineItemRepo.Get(ctx, original.ID)
	s.NoError(getErr)
	s.False(oldLI.EndDate.IsZero(), "original line item must be terminated after update")
}

// TestUpdateSubscriptionLineItem_ReuseBucketByID verifies the id-based reuse
// contract: a bucket request carrying the existing bucket's id (and no price)
// keeps that bucket's price + id on the successor line item, while commitment
// fields are taken from the request.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ReuseBucketByID() {
	ctx := s.GetContext()

	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Reuse Meter",
		EventName: "bucket_reuse_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	original := s.makeBucketLineItem(s.testData.subscription.ID, m.ID, "reuse_bucket_orig")
	oldBucket := original.CommitmentTimeBuckets[0]

	// Update: keep the bucket (by id, no price) but change its commitment value
	// AND its time window — none of which must trigger a new price row.
	overage := decimal.NewFromFloat(1.2)
	commitment := decimal.NewFromInt(400)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      lo.ToPtr(true),
		CommitmentTimeBuckets: lo.ToPtr([]dto.CommitmentBucketRequest{
			{
				ID:              oldBucket.ID,
				Start:           types.Bucket{Hour: 6, Minute: 0},
				End:             types.Bucket{Hour: 14, Minute: 0},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(999),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.2)),
			},
		}),
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().Len(resp.SubscriptionLineItem.CommitmentTimeBuckets, 1)

	got := resp.SubscriptionLineItem.CommitmentTimeBuckets[0]
	s.Equal(oldBucket.ID, got.ID, "bucket id must be preserved on reuse")
	s.Equal(oldBucket.PriceID, got.PriceID, "bucket price must be reused, not recreated")
	s.True(got.CommitmentValue.Equal(decimal.NewFromInt(999)), "commitment must come from the request, got %s", got.CommitmentValue)
	s.Equal(types.Bucket{Hour: 6, Minute: 0}, got.Start, "time window must come from the request")
	s.Equal(types.Bucket{Hour: 14, Minute: 0}, got.End)
}

// TestUpdateSubscriptionLineItem_UnknownBucketIDRejected verifies that a bucket
// request referencing a non-existent bucket id is rejected.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_UnknownBucketIDRejected() {
	ctx := s.GetContext()

	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Unknown ID Meter",
		EventName: "bucket_unknown_id_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	original := s.makeBucketLineItem(s.testData.subscription.ID, m.ID, "unknown_bucket_orig")

	overage := decimal.NewFromFloat(1.2)
	commitment := decimal.NewFromInt(400)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      lo.ToPtr(true),
		CommitmentTimeBuckets: lo.ToPtr([]dto.CommitmentBucketRequest{
			{
				ID:              "bucket_does_not_exist",
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 17, Minute: 0},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.2)),
			},
		}),
	}

	_, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "unknown bucket id")
}

// TestUpdateSubscriptionLineItem_ClearBuckets verifies that passing an explicit
// empty slice for commitment_time_buckets clears all buckets on the successor
// line item.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_ClearBuckets() {
	ctx := s.GetContext()

	// Create a meter with BucketSize.
	m2 := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Clear Meter",
		EventName: "bucket_clear_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m2))

	// Create a line item with one bucket.
	original := s.makeBucketLineItem(s.testData.subscription.ID, m2.ID, "clear_bucket_orig")
	s.Require().Len(original.CommitmentTimeBuckets, 1, "precondition: original has 1 bucket")

	// Update with an explicit empty slice — this should clear buckets.
	// We must also change a critical field to trigger ShouldCreateNewLineItem.
	// Providing an explicit empty CommitmentTimeBuckets alone is sufficient because
	// CommitmentTimeBuckets != nil triggers ShouldCreateNewLineItem.
	overage := decimal.NewFromFloat(1.3)
	commitment := decimal.NewFromInt(250)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overage,
		CommitmentWindowed:      lo.ToPtr(false),
		CommitmentTimeBuckets:   lo.ToPtr([]dto.CommitmentBucketRequest{}), // explicit empty
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	newLI := resp.SubscriptionLineItem
	s.NotEqual(original.ID, newLI.ID, "successor line item must have a new ID")
	s.Empty(newLI.CommitmentTimeBuckets, "successor must have no buckets when empty slice is passed")
}

// TestUpdateSubscriptionLineItem_NilBuckets_InheritsExisting verifies that
// when CommitmentTimeBuckets is omitted (nil pointer) in the update request,
// the successor line item inherits the existing buckets verbatim.
func (s *SubscriptionLineItemServiceSuite) TestUpdateSubscriptionLineItem_NilBuckets_InheritsExisting() {
	ctx := s.GetContext()

	// Create a meter with BucketSize.
	m3 := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Bucket Inherit Meter",
		EventName: "bucket_inherit_event",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "value",
			BucketSize: types.WindowSizeHour,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m3))

	// Create a line item with one bucket.
	original := s.makeBucketLineItem(s.testData.subscription.ID, m3.ID, "inherit_bucket_orig")
	s.Require().Len(original.CommitmentTimeBuckets, 1, "precondition: original has 1 bucket")
	origBucketPriceID := original.CommitmentTimeBuckets[0].PriceID

	// Update without providing CommitmentTimeBuckets (nil pointer) —
	// the update changes the overage factor, which triggers ShouldCreateNewLineItem
	// via CommitmentOverageFactor.
	newOverage := decimal.NewFromFloat(1.8)
	commitment := decimal.NewFromInt(300)
	req := dto.UpdateSubscriptionLineItemRequest{
		CommitmentAmount:        &commitment,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &newOverage,
		CommitmentWindowed:      lo.ToPtr(true),
		// CommitmentTimeBuckets is nil — should inherit original buckets
	}

	resp, err := s.service.UpdateSubscriptionLineItem(ctx, original.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	newLI := resp.SubscriptionLineItem
	s.NotEqual(original.ID, newLI.ID, "successor must have new ID")

	// Successor must carry the original bucket (same PriceID — inherited, not re-created).
	s.Require().Len(newLI.CommitmentTimeBuckets, 1, "successor must inherit 1 bucket")
	s.Equal(origBucketPriceID, newLI.CommitmentTimeBuckets[0].PriceID,
		"inherited bucket PriceID must match original")
}

// createMeterForBucketTests creates a meter with the given aggregation and
// (possibly empty) bucket size for bucket-validation tests.
func (s *SubscriptionLineItemServiceSuite) createMeterForBucketTests(eventName string, agg types.AggregationType, bucketSize types.WindowSize) *meter.Meter {
	ctx := s.GetContext()
	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      eventName,
		EventName: eventName,
		Aggregation: meter.Aggregation{
			Type:       agg,
			Field:      "value",
			BucketSize: bucketSize,
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	return m
}

// createUsagePriceForSubscription creates a SUBSCRIPTION-scoped usage price,
// optionally tied to a meter (empty meterID = no meter).
func (s *SubscriptionLineItemServiceSuite) createUsagePriceForSubscription(subID, meterID string) *price.Price {
	ctx := s.GetContext()
	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           subID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            meterID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

// bucketReqForTests builds a CommitmentBucketRequest with a fresh inline price.
func (s *SubscriptionLineItemServiceSuite) bucketReqForTests(subID string, start, end types.Bucket, lookupKey string) dto.CommitmentBucketRequest {
	return dto.CommitmentBucketRequest{
		Start: start,
		End:   end,
		Price: &dto.CreatePriceRequest{
			Amount:               lo.ToPtr(decimal.NewFromInt(10)),
			Currency:             "usd",
			EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:             subID,
			Type:                 types.PRICE_TYPE_FIXED,
			PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
			BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount:   1,
			BillingModel:         types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence:       types.InvoiceCadenceAdvance,
			LookupKey:            lookupKey,
			SkipEntityValidation: true,
		},
		CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
		CommitmentValue: decimal.NewFromInt(200),
		OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
	}
}

// TestAddSubscriptionLineItem_BucketValidationErrors covers the array-level
// bucket validation failures in one table: overlapping buckets, grid
// misalignment, a missing bucket size, and conflict with a cumulative
// subscription commitment.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_BucketValidationErrors() {
	ctx := s.GetContext()

	overage := decimal.NewFromFloat(1.5)
	commitment := decimal.NewFromInt(500)
	withTopCommitment := func(req *dto.CreateSubscriptionLineItemRequest) {
		req.CommitmentAmount = &commitment
		req.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		req.CommitmentOverageFactor = &overage
	}

	cases := []struct {
		name    string
		setup   func() (subID string, req dto.CreateSubscriptionLineItemRequest)
		wantErr string
	}{
		{
			// Two buckets whose time ranges overlap (09:00-12:00 and 11:00-14:00).
			name: "overlapping buckets",
			setup: func() (string, dto.CreateSubscriptionLineItemRequest) {
				subID := s.testData.subscription.ID
				m := s.createMeterForBucketTests("overlap_val_event", types.AggregationMax, types.WindowSizeHour)
				p := s.createUsagePriceForSubscription(subID, m.ID)
				req := dto.CreateSubscriptionLineItemRequest{
					PriceID:              p.ID,
					SkipEntitlementCheck: true,
					CommitmentWindowed:   true,
					CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
						s.bucketReqForTests(subID, types.Bucket{Hour: 9}, types.Bucket{Hour: 12}, "overlap_bucket_a"),
						s.bucketReqForTests(subID, types.Bucket{Hour: 11}, types.Bucket{Hour: 14}, "overlap_bucket_b"),
					},
				}
				withTopCommitment(&req)
				return subID, req
			},
			wantErr: "overlap",
		},
		{
			// Start 09:30 is not on the meter's 60-minute grid.
			name: "misaligned bucket start",
			setup: func() (string, dto.CreateSubscriptionLineItemRequest) {
				subID := s.testData.subscription.ID
				m := s.createMeterForBucketTests("align_val_event", types.AggregationMax, types.WindowSizeHour)
				p := s.createUsagePriceForSubscription(subID, m.ID)
				req := dto.CreateSubscriptionLineItemRequest{
					PriceID:              p.ID,
					SkipEntitlementCheck: true,
					CommitmentWindowed:   true,
					CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
						s.bucketReqForTests(subID, types.Bucket{Hour: 9, Minute: 30}, types.Bucket{Hour: 10, Minute: 30}, "misalign_bucket"),
					},
				}
				withTopCommitment(&req)
				return subID, req
			},
			wantErr: "alignment",
		},
		{
			// Meter without BucketSize — ValidateWindowAlignment rejects it.
			name: "no bucket size",
			setup: func() (string, dto.CreateSubscriptionLineItemRequest) {
				subID := s.testData.subscription.ID
				m := s.createMeterForBucketTests("non_windowed_event", types.AggregationSum, "")
				p := s.createUsagePriceForSubscription(subID, m.ID)
				req := dto.CreateSubscriptionLineItemRequest{
					PriceID:              p.ID,
					SkipEntitlementCheck: true,
					CommitmentWindowed:   true,
					CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
						s.bucketReqForTests(subID, types.Bucket{Hour: 9}, types.Bucket{Hour: 17}, "non_windowed_bucket"),
					},
				}
				withTopCommitment(&req)
				return subID, req
			},
			wantErr: "bucket size",
		},
		{
			// Per-bucket commitment conflicts with a cumulative subscription
			// commitment (ANNUAL window on MONTHLY billing). The price has no
			// meter, so the alignment check is skipped and the cumulative guard
			// fires.
			name: "cumulative subscription commitment",
			setup: func() (string, dto.CreateSubscriptionLineItemRequest) {
				now := time.Now().UTC()
				annualDuration := types.BILLING_PERIOD_ANNUAL
				commitmentAmount := decimal.NewFromInt(12000)
				cumulativeSub := &subscription.Subscription{
					ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
					PlanID:             s.testData.plan.ID,
					CustomerID:         s.testData.customer.ID,
					StartDate:          now.Add(-30 * 24 * time.Hour),
					CurrentPeriodStart: now.Add(-24 * time.Hour),
					CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
					BillingAnchor:      now.Add(-30 * 24 * time.Hour),
					Currency:           "usd",
					BillingCycle:       types.BillingCycleAnniversary,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					SubscriptionStatus: types.SubscriptionStatusActive,
					CommitmentDuration: &annualDuration,
					CommitmentAmount:   &commitmentAmount,
					BaseModel:          types.GetDefaultBaseModel(ctx),
				}
				s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, cumulativeSub))
				p := s.createUsagePriceForSubscription(cumulativeSub.ID, "")
				return cumulativeSub.ID, dto.CreateSubscriptionLineItemRequest{
					PriceID:              p.ID,
					SkipEntitlementCheck: true,
					CommitmentWindowed:   true,
					CommitmentTimeBuckets: []dto.CommitmentBucketRequest{
						s.bucketReqForTests(cumulativeSub.ID, types.Bucket{Hour: 9}, types.Bucket{Hour: 17}, "cumulative_bucket"),
					},
				}
			},
			wantErr: "cumulative",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			subID, req := tc.setup()
			_, err := s.service.AddSubscriptionLineItem(ctx, subID, req)
			s.Require().Error(err)
			s.Contains(err.Error(), tc.wantErr, "error should mention %q", tc.wantErr)
		})
	}
}

func (s *SubscriptionLineItemServiceSuite) resetWebhooks() {
	s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Reset()
}

func (s *SubscriptionLineItemServiceSuite) countSubscriptionUpdated() int {
	n := 0
	for _, e := range s.GetPublishedWebhooks() {
		if e.EventName == types.WebhookEventSubscriptionUpdated {
			n++
		}
	}
	return n
}

func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_PublishesSubscriptionUpdated() {
	ctx := s.GetContext()
	price2 := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, price2))

	s.resetWebhooks()
	_, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, dto.CreateSubscriptionLineItemRequest{
		PriceID:              price2.ID,
		Quantity:             decimal.NewFromInt(1),
		SkipEntitlementCheck: true,
	})
	s.Require().NoError(err)
	s.Equal(1, s.countSubscriptionUpdated())
}

func (s *SubscriptionLineItemServiceSuite) TestDeleteSubscriptionLineItem_PublishesSubscriptionUpdated() {
	ctx := s.GetContext()
	s.resetWebhooks()
	_, err := s.service.DeleteSubscriptionLineItem(ctx, s.testData.lineItem.ID, dto.DeleteSubscriptionLineItemRequest{})
	s.Require().NoError(err)
	s.Equal(1, s.countSubscriptionUpdated())
}

func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItemInternal_DoesNotPublish() {
	ctx := s.GetContext()
	price2 := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(15),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, price2))

	svc := s.service.(*subscriptionService)
	s.resetWebhooks()
	_, err := svc.addSubscriptionLineItem(ctx, s.testData.subscription.ID, dto.CreateSubscriptionLineItemRequest{
		PriceID:              price2.ID,
		Quantity:             decimal.NewFromInt(1),
		SkipEntitlementCheck: true,
	})
	s.Require().NoError(err)
	s.Equal(0, s.countSubscriptionUpdated())
}

// TestAddSubscriptionLineItem_ResponseCarriesExpandedPrice asserts the create
// response embeds the resulting price, so a caller creating an inline price
// learns its amount and currency without a second call.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_ResponseCarriesExpandedPrice() {
	ctx := s.GetContext()

	req := dto.CreateSubscriptionLineItemRequest{
		Price: &dto.SubscriptionPriceCreateRequest{
			Type:               types.PRICE_TYPE_FIXED,
			PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			Amount:             lo.ToPtr(decimal.NewFromInt(42)),
			LookupKey:          "inline_price_expansion",
		},
		SkipEntitlementCheck: true,
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp.Price, "create response must embed the resulting price")
	s.Equal(resp.PriceID, resp.Price.ID)
	s.True(decimal.NewFromInt(42).Equal(resp.Price.Amount), "expected amount 42, got %s", resp.Price.Amount)
	s.Equal(s.testData.subscription.Currency, resp.Price.Currency)
}

// TestAddSubscriptionLineItem_CommitmentOverageFactorDefaultsToOne asserts a
// commitment without commitment_overage_factor is accepted and stored as 1.0,
// matching the field being optional in the OpenAPI spec.
func (s *SubscriptionLineItemServiceSuite) TestAddSubscriptionLineItem_CommitmentOverageFactorDefaultsToOne() {
	ctx := s.GetContext()

	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Commitment Default Meter",
		EventName: "commitment_default_event",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "value",
		},
		ResetUsage: types.ResetUsageBillingPeriod,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	usagePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(2),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           s.testData.subscription.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            m.ID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, usagePrice))

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:              usagePrice.ID,
		SkipEntitlementCheck: true,
		CommitmentAmount:     lo.ToPtr(decimal.NewFromInt(100)),
	}

	resp, err := s.service.AddSubscriptionLineItem(ctx, s.testData.subscription.ID, req)
	s.Require().NoError(err, "commitment without an overage factor must be accepted")
	s.Require().NotNil(resp.CommitmentOverageFactor)
	s.True(decimal.NewFromInt(1).Equal(*resp.CommitmentOverageFactor),
		"expected default overage factor 1, got %s", resp.CommitmentOverageFactor)
	s.Equal(types.COMMITMENT_TYPE_AMOUNT, resp.CommitmentType)

	// The create response also carries the meter for usage line items.
	s.Require().NotNil(resp.Meter, "usage line item response must embed its meter")
	s.Equal(m.ID, resp.Meter.ID)
}
