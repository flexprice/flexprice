package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *CommitmentServiceSuite) TestSameOverage() {
	ctx := s.GetContext()
	store := func(id string, model types.BillingModel, amount string) *string {
		p := *s.fx.priceAPI
		p.ID, p.BillingModel, p.Amount = id, model, dec(amount)
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &p))
		return lo.ToPtr(id)
	}
	flat7, flat7b, flat8 := store("ovg_flat_7", types.BILLING_MODEL_FLAT_FEE, "0.07"), store("ovg_flat_7b", types.BILLING_MODEL_FLAT_FEE, "0.07"), store("ovg_flat_8", types.BILLING_MODEL_FLAT_FEE, "0.08")
	tiered, tieredB := store("ovg_tiered", types.BILLING_MODEL_TIERED, "0"), store("ovg_tiered_b", types.BILLING_MODEL_TIERED, "0")

	tests := []struct {
		name string
		a, b *string
		want bool
	}{
		{"both without overage", nil, nil, true},
		{"one without overage", flat7, nil, false},
		{"equal flat amounts", flat7, flat7b, true},
		{"different flat amounts", flat7, flat8, false},
		{"tiered and flat", tiered, flat7, false},
		{"two tiered", tiered, tieredB, false},
	}
	svc := s.svc.(*commitmentService)
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := svc.sameOverage(ctx, &commitment.Commitment{OveragePriceID: tt.a}, &commitment.Commitment{OveragePriceID: tt.b})
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}

func (s *CommitmentServiceSuite) TestAttachPrice_RejectsSecondMeterInQuantityGroupScope() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	subSvc := NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite))
	_, err = subSvc.AddSubscriptionLineItem(
		s.GetContext(),
		s.fx.sub.ID,
		dto.CreateSubscriptionLineItemRequest{PriceID: s.fx.priceOtherMeterInGroup.ID},
	)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_NoopCases() {
	sub, p := s.fx.sub, s.fx.priceOtherMeterInGroup
	at := sub.StartDate
	// No commitments yet.
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, p, at, nil))

	req := validReq(sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	// A price outside the scoped group, and a fixed price, never trigger the check.
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, s.fx.priceStorage, at, nil))
	fixed := *p
	fixed.Type = types.PRICE_TYPE_FIXED
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, &fixed, at, nil))

	// Disabled feature is a no-op.
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, p, at, nil))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsBucketMismatchForMeterScopedWindow() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	req.Measurement = types.CommitmentMeasurementWindow
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	hourly := *s.fx.priceAPI
	hourly.ID, hourly.BucketSize = "price_api_hourly", types.WindowSizeHour
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &hourly, s.fx.sub.StartDate, nil)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), created.ID)

	same := *s.fx.priceAPI
	same.ID = "price_api_same_bucket"
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &same, s.fx.sub.StartDate, nil))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsOverlappingCommitmentsThatDisagree() {
	a := validReq(s.fx.sub.ID, "")
	a.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	a.Type, a.CommittedPrice, a.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	ca, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	// Distinct priorities leave the type and measurement check as the only rejection.
	b := validReq(s.fx.sub.ID, "")
	b.Name, b.Priority = "Storage", 1
	b.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterStorage.ID}
	cb, err := s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, s.fx.sub.StartDate, nil)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), "differ in type or measurement")
	s.Contains(err.Error(), ca.ID)
	s.Contains(err.Error(), cb.ID)
}

type recordingLineItemRepo struct {
	subscription.LineItemRepository
	events *[]string
}

func (r *recordingLineItemRepo) Create(ctx context.Context, li *subscription.SubscriptionLineItem) error {
	*r.events = append(*r.events, "create:"+li.PriceID)
	return r.LineItemRepository.Create(ctx, li)
}

func (r *recordingLineItemRepo) CreateBulk(ctx context.Context, items []*subscription.SubscriptionLineItem) error {
	for _, li := range items {
		*r.events = append(*r.events, "create_bulk:"+li.PriceID)
	}
	return r.LineItemRepository.CreateBulk(ctx, items)
}

func (r *recordingLineItemRepo) Update(ctx context.Context, li *subscription.SubscriptionLineItem) error {
	*r.events = append(*r.events, "update:"+li.ID)
	return r.LineItemRepository.Update(ctx, li)
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsPriceOnAnotherCadence() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	weekly := *s.fx.priceOtherMeterInGroup
	weekly.ID, weekly.BillingPeriod = "price_other_meter_weekly", types.BILLING_PERIOD_WEEKLY
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &weekly, s.fx.sub.StartDate, nil)
	s.requireValidation(err, "commitments require scope prices billed every subscription period")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsSamePriorityOverlap() {
	a := validReq(s.fx.sub.ID, "")
	a.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	a.Type, a.CommittedPrice, a.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	_, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	// Disjoint scopes, so the same priority is accepted until a price joins both.
	b := a
	b.Name = "Storage"
	b.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterStorage.ID}
	cb, err := s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, s.fx.sub.StartDate, nil)
	s.requireValidation(err, "same priority", cb.ID)
}

func (s *CommitmentServiceSuite) TestAttachPrice_RejectsGroupedBucketedPriceJoiningScope() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, dec("100")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.group.ID}
	_, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	p := s.groupedBucketedPrice()
	_, err = NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite)).AddSubscriptionLineItem(ctx, s.fx.sub.ID, dto.CreateSubscriptionLineItemRequest{PriceID: p.ID})
	s.requireValidation(err, p.ID, "grouped bucketed")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsEntitledMeter() {
	// Subscription-scoped commitment over the API meter; the storage meter gains an
	// entitlement afterwards, so a storage price may not join the scope.
	req := validReq(s.fx.sub.ID, s.fx.sub.ID)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: s.fx.sub.ID}
	req.Type, req.CommittedPrice = types.COMMITMENT_TYPE_AMOUNT, nil
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.entitleMeter(s.fx.meterStorage.ID, true)
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceStorage, s.fx.sub.StartDate, nil)
	s.Require().Error(err)
	s.Require().True(ierr.IsValidation(err))
	s.Require().Contains(err.Error(), "entitlement")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_ReplacedOnlyLineKeepsBucketRule() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.Measurement = types.CommitmentMeasurementWindow
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	// The scope's only line ends where its replacement on an hourly price would start.
	march := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	only, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), "subs_line_api_only")
	s.Require().NoError(err)
	only.EndDate = march
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(s.GetContext(), only))

	hourly := *s.fx.priceAPI
	hourly.ID, hourly.GroupID, hourly.BucketSize = "price_api_only_hourly", s.fx.groupAPIOnly.ID, types.WindowSizeHour
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &hourly, march, nil)
	s.requireValidation(err, "bucket size")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_GuardsOpenEndedCommitmentForever() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.EndDate = nil
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	s.requireValidation(err, "different meter")
}

// An open-ended commitment on a subscription with an end stops guarding price attaches once it ends.
func (s *CommitmentServiceSuite) TestCheckPriceAttach_OpenEndedStopsAtSubscriptionEnd() {
	sub := s.endSubscriptionAt(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC))
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.EndDate = nil
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), sub, s.fx.priceOtherMeterInGroup, time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), nil)
	s.requireValidation(err, "different meter")
	s.Require().NoError(
		s.svc.CheckPriceAttach(s.GetContext(), sub, s.fx.priceOtherMeterInGroup, time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), nil),
	)
}

// A binding open-ended commitment needs its scope covered only until a finite subscription ends.
func (s *CommitmentServiceSuite) TestLineItemDetach_OpenEndedStopsAtSubscriptionEnd() {
	jun := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	subEnd := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	s.endSubscriptionAt(subEnd)
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = nil
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.detachCheck(s.fx.lineAPI.ID, jun)
	s.requireValidation(err, created.ID, "terminate the commitment first")
	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, subEnd))

	// A successor ending with the subscription, as its lines do, covers the rest of the term.
	successor := *s.fx.lineAPI
	successor.ID, successor.StartDate, successor.EndDate = "subs_line_api_successor", jun, subEnd
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &successor))
	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, jun))
}

// A binding open-ended commitment's scope must stay covered for good, not only to some term end.
func (s *CommitmentServiceSuite) TestLineItemDetach_RejectsEndingOpenEndedCommitmentScope() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = nil
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.detachCheck(s.fx.lineAPI.ID, time.Date(2035, 2, 1, 0, 0, 0, 0, time.UTC))
	s.requireValidation(err, created.ID, "terminate the commitment first")
}

// quantityCommitmentOnAPIOnlyGroup creates a quantity commitment scoped to the group holding only the API meter.
func (s *CommitmentServiceSuite) quantityCommitmentOnAPIOnlyGroup() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestAddonAttach_RejectsPriceBreakingCommitment() {
	ctx := s.GetContext()
	s.quantityCommitmentOnAPIOnlyGroup()

	s.Require().NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        "addon_storage",
		LookupKey: "addon_storage",
		Name:      "Storage",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	addonPrice := *s.fx.priceOtherMeterInGroup
	addonPrice.ID, addonPrice.EntityType, addonPrice.EntityID = "price_addon_storage", types.PRICE_ENTITY_TYPE_ADDON, "addon_storage"
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &addonPrice))

	start := s.fx.sub.StartDate
	_, err := NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite)).AddAddonToSubscription(ctx, &dto.AddAddonRequest{
		SubscriptionID: s.fx.sub.ID,
		AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
			AddonID:   "addon_storage",
			Cadence:   types.AddonCadenceRecurring,
			StartDate: &start,
		},
	})
	s.requireValidation(err, "different meter")
}

func (s *CommitmentServiceSuite) TestPlanSync_SkipsPriceBreakingCommitment() {
	ctx := s.GetContext()
	s.quantityCommitmentOnAPIOnlyGroup()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	sub.SubscriptionType = types.SubscriptionTypeStandalone
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{
		ID:            "plan_cmt",
		Name:          "Plan",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	// A second meter would break the single-meter commitment; another API price joins it fine.
	storage := *s.fx.priceOtherMeterInGroup
	storage.ID, storage.Sequence = "price_plan_storage_new", 1
	api := *s.fx.priceAPI
	api.ID, api.GroupID, api.Sequence = "price_plan_api_new", s.fx.groupAPIOnly.ID, 2
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &storage))
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &api))

	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.PlanPriceSyncRepo = s.GetStores().PlanPriceSyncRepo
	_, err = NewPlanService(params).SyncPlanPricesV2(ctx, "plan_cmt")
	s.Require().NoError(err)

	_, items, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	priceIDs := lo.Map(items, func(li *subscription.SubscriptionLineItem, _ int) string { return li.PriceID })
	s.Contains(priceIDs, api.ID)
	s.NotContains(priceIDs, storage.ID)
	s.NotContains(priceIDs, s.fx.priceOtherMeterInGroup.ID)
}

func (s *CommitmentServiceSuite) TestLineItemUpdate_RejectsLegacyCommitmentInScope() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	_, err = NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite)).UpdateSubscriptionLineItem(s.GetContext(), s.fx.lineAPI.ID,
		dto.UpdateSubscriptionLineItemRequest{
			CommitmentAmount:        lo.ToPtr(dec("50")),
			CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
			CommitmentOverageFactor: lo.ToPtr(dec("1.5")),
			EffectiveFrom:           lo.ToPtr(s.fx.sub.StartDate.AddDate(0, 1, 0)),
		})
	s.requireValidation(err, "legacy line-item commitment")

	stored, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), s.fx.lineAPI.ID)
	s.Require().NoError(err)
	s.False(stored.HasAnyCommitment())
}

// plainUpdateOfLegacyLine edits the metadata of an API line that carries legacy commitment fields, with a
// commitment on the given meter, and returns the recorded events.
func (s *CommitmentServiceSuite) plainUpdateOfLegacyLine(meterID string) ([]string, error) {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: meterID}
	_, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	line, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.fx.lineAPI.ID)
	s.Require().NoError(err)
	line.CommitmentAmount, line.CommitmentType = lo.ToPtr(dec("50")), types.COMMITMENT_TYPE_AMOUNT
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, line))

	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	params.SubscriptionLineItemRepo = &recordingLineItemRepo{LineItemRepository: s.GetStores().SubscriptionLineItemRepo, events: &events}
	_, err = NewSubscriptionService(params).UpdateSubscriptionLineItem(ctx, s.fx.lineAPI.ID,
		dto.UpdateSubscriptionLineItemRequest{Metadata: map[string]string{"note": "edited"}})

	return events, err
}

func (s *CommitmentServiceSuite) TestLineItemUpdate_PlainUpdateRunsGuardInTx() {
	events, err := s.plainUpdateOfLegacyLine(s.fx.meterStorage.ID)
	s.Require().NoError(err)
	s.Equal([]string{"tx", "lock:commitments:" + s.fx.sub.ID, "update:" + s.fx.lineAPI.ID, "tx_end"}, events)
}

func (s *CommitmentServiceSuite) TestLineItemUpdate_PlainUpdateRejectsCoveringCommitment() {
	_, err := s.plainUpdateOfLegacyLine(s.fx.meterAPI.ID)
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) TestLineItemAdd_RejectsLegacyCommitmentInScope() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: s.fx.sub.ID}
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, dec("100")
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	_, err = NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite)).AddSubscriptionLineItem(s.GetContext(), s.fx.sub.ID,
		dto.CreateSubscriptionLineItemRequest{
			PriceID:          s.fx.priceOtherMeterInGroup.ID,
			CommitmentAmount: lo.ToPtr(dec("50")),
			CommitmentType:   types.COMMITMENT_TYPE_AMOUNT,
		})
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_AllowsLineEndingBeforeCommitmentStart() {
	march := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.StartDate = march
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	// A second meter would break the single-meter commitment, but this line is gone before it starts.
	start := s.fx.sub.StartDate
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, start, &march))

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, start, lo.ToPtr(march.AddDate(0, 0, 1)))
	s.requireValidation(err, "different meter")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_GuardsTerminatedCommitmentUntilCutoff() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	cutoff := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, s.fx.sub.StartDate, nil)
	s.requireValidation(err, "different meter")

	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, cutoff, nil))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_GuardsTerminatedCommitmentThroughPeriodEnd() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	cutoff := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, cutoff.AddDate(0, 0, 5), nil)
	s.requireValidation(err, "different meter")

	periodEnd := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, periodEnd, nil))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_GuardsMidPeriodEndThroughPeriodEnd() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.EndDate = lo.ToPtr(time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC))
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, req.EndDate.AddDate(0, 0, 5), nil)
	s.requireValidation(err, "different meter")

	periodEnd := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, periodEnd, nil))
}

func (s *CommitmentServiceSuite) TestLineItemUpdate_LegacyGuardCoversTerminatedCommitmentThroughPeriodEnd() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)
	cutoff := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(ctx, created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	// A line starting in the cutoff's billing period overlaps the extended span; one starting at its end does not.
	line := *s.fx.lineAPI
	line.CommitmentAmount, line.CommitmentType = lo.ToPtr(dec("50")), types.COMMITMENT_TYPE_AMOUNT
	line.StartDate = cutoff.AddDate(0, 0, 5)
	s.requireValidation(checkLegacyLineFields(ctx, fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &line), "legacy line-item commitment")

	line.StartDate = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(checkLegacyLineFields(ctx, fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &line))
}

// A line joining through an addon or a price change runs the attach guard, which also refuses legacy commitment
// fields inside a commitment's scope.
func (s *CommitmentServiceSuite) TestLineItemAttach_RejectsLegacyCommitmentInScope() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	_, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	line := *s.fx.lineAPI
	line.CommitmentAmount, line.CommitmentType = lo.ToPtr(dec("50")), types.COMMITMENT_TYPE_AMOUNT
	err = checkLineAttach(ctx, fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &line)
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) TestPlanSync_InsertsUsageLineUnderCommitmentsLock() {
	ctx := s.GetContext()
	s.quantityCommitmentOnAPIOnlyGroup()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	sub.SubscriptionType = types.SubscriptionTypeStandalone
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{
		ID:            "plan_cmt",
		Name:          "Plan",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
	api := *s.fx.priceAPI
	api.ID, api.GroupID, api.Sequence = "price_plan_api_new", s.fx.groupAPIOnly.ID, 1
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &api))

	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.PlanPriceSyncRepo = s.GetStores().PlanPriceSyncRepo
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	params.SubscriptionLineItemRepo = &recordingLineItemRepo{LineItemRepository: s.GetStores().SubscriptionLineItemRepo, events: &events}
	_, err = NewPlanService(params).SyncPlanPricesV2(ctx, "plan_cmt")
	s.Require().NoError(err)

	s.requireInsertUnderLock(events, "create:"+api.ID)
}

func (s *CommitmentServiceSuite) TestAddonSync_SkipsPriceBreakingCommitment() {
	ctx := s.GetContext()
	s.quantityCommitmentOnAPIOnlyGroup()
	s.Require().NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        "addon_sync",
		LookupKey: "addon_sync",
		Name:      "Sync",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	// A second meter would break the single-meter commitment; another API price joins it fine.
	storage := *s.fx.priceOtherMeterInGroup
	storage.ID, storage.EntityType, storage.EntityID, storage.Sequence = "price_addon_sync_storage", types.PRICE_ENTITY_TYPE_ADDON, "addon_sync", 1
	api := *s.fx.priceAPI
	api.ID, api.EntityType, api.EntityID, api.Sequence = "price_addon_sync_api", types.PRICE_ENTITY_TYPE_ADDON, "addon_sync", 2
	api.GroupID = s.fx.groupAPIOnly.ID
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &storage))
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &api))
	s.Require().NoError(s.GetStores().AddonAssociationRepo.Create(ctx, &addonassociation.AddonAssociation{
		ID:          "addon_assoc_sync",
		AddonID:     "addon_sync",
		EntityID:    s.fx.sub.ID,
		EntityType:  types.AddonAssociationEntityTypeSubscription,
		AddonStatus: types.AddonStatusActive,
		StartDate:   lo.ToPtr(s.fx.sub.StartDate),
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}))

	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	params.SubscriptionLineItemRepo = &recordingLineItemRepo{LineItemRepository: s.GetStores().SubscriptionLineItemRepo, events: &events}
	resp, err := NewAddonService(params).SyncAddonPrices(ctx, "addon_sync")
	s.Require().NoError(err)
	s.Equal(2, resp.Summary.LineItemsFoundForCreation)
	s.Equal(1, resp.Summary.LineItemsCreated)

	_, items, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	priceIDs := lo.Map(items, func(li *subscription.SubscriptionLineItem, _ int) string { return li.PriceID })
	s.Contains(priceIDs, api.ID)
	s.NotContains(priceIDs, storage.ID)
	s.requireInsertUnderLock(events, "create:"+api.ID)
}

func (s *CommitmentServiceSuite) TestLegacyFields_AllowedWhenLineEndsBeforeCommitmentStarts() {
	march := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.StartDate = march
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	gone := *s.fx.lineAPI
	gone.ID, gone.EndDate = "subs_line_api_gone", time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	gone.CommitmentAmount = lo.ToPtr(dec("50"))
	s.Require().NoError(checkLegacyLineFields(s.GetContext(), fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &gone))

	// The same legacy line overlapping the commitment term is still rejected.
	overlapping := gone
	overlapping.ID, overlapping.EndDate = "subs_line_api_overlap", time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	err = checkLegacyLineFields(s.GetContext(), fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &overlapping)
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) detachCheck(lineItemID string, endDate time.Time) error {
	li, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), lineItemID)
	s.Require().NoError(err)
	return checkLineDetach(s.GetContext(), fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, li, endDate)
}

func (s *CommitmentServiceSuite) TestLineItemDetach_RejectsEmptyingBindingCommitmentScope() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	err = s.detachCheck(s.fx.lineAPI.ID, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC))
	s.requireValidation(err, created.ID, "terminate the commitment first")
}

func (s *CommitmentServiceSuite) TestLineItemDetach_AllowsWhenAnotherScopedLineRemains() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)))
}

func (s *CommitmentServiceSuite) TestLineItemDetach_AllowsEndDateWithSuccessorOnSamePrice() {
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	feb := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	successor := *s.fx.lineAPI
	successor.ID, successor.StartDate = "subs_line_api_successor", feb
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &successor))

	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, feb))
}

func (s *CommitmentServiceSuite) TestLineItemDetach_IgnoresNonBindingCommitment() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Binding = lo.ToPtr(false)
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)))
}

func (s *CommitmentServiceSuite) TestLineItemDetach_RequiresContinuousCoverage() {
	feb := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		nextStart *time.Time // a further API-meter line, open-ended from here
		wantErr   bool
	}{
		{name: "the remaining line ends mid-term", wantErr: true},
		{name: "a second line takes over before the first ends", nextStart: lo.ToPtr(time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC))},
		{name: "a second line takes over on the day the first ends", nextStart: lo.ToPtr(jun)},
		{name: "a second line leaves a gap", nextStart: lo.ToPtr(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)), wantErr: true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			req := validReq(s.fx.sub.ID, "")
			req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
			_, err := s.svc.Create(ctx, req)
			s.Require().NoError(err)

			other, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, "subs_line_api_only")
			s.Require().NoError(err)
			other.EndDate = jun
			s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, other))
			if tt.nextStart != nil {
				next := *other
				next.ID, next.StartDate, next.EndDate = "subs_line_api_next", *tt.nextStart, time.Time{}
				s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &next))
			}

			err = s.detachCheck(s.fx.lineAPI.ID, feb)
			if tt.wantErr {
				s.requireValidation(err, "terminate the commitment first")
				return
			}

			s.Require().NoError(err)
		})
	}
}

func (s *CommitmentServiceSuite) TestLineItemDelete_EndsUsageLineUnderCommitmentsLock() {
	ctx := s.GetContext()
	past := *s.fx.lineStorage
	past.ID, past.StartDate = "subs_line_storage_past", time.Now().UTC().AddDate(0, -1, 0)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &past))

	var events []string
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.DB = &recordingDB{IClient: s.GetDB(), events: &events}
	params.SubscriptionLineItemRepo = &recordingLineItemRepo{LineItemRepository: s.GetStores().SubscriptionLineItemRepo, events: &events}
	_, err := NewSubscriptionService(params).DeleteSubscriptionLineItem(ctx, past.ID, dto.DeleteSubscriptionLineItemRequest{})
	s.Require().NoError(err)

	s.requireInsertUnderLock(events, "update:"+past.ID)
}

func (s *CommitmentServiceSuite) TestLineItemDelete_RejectsCancellingTheOnlyScheduledScopedLine() {
	ctx := s.GetContext()
	start := clock.Now().AddDate(0, 1, 0).Truncate(24 * time.Hour)
	line, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.fx.lineAPI.ID)
	s.Require().NoError(err)
	line.StartDate = start
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, line))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.StartDate, req.EndDate = start, lo.ToPtr(start.AddDate(1, 0, 0))
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	_, err = NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite)).DeleteSubscriptionLineItem(ctx, s.fx.lineAPI.ID, dto.DeleteSubscriptionLineItemRequest{})
	s.requireValidation(err, created.ID, "terminate the commitment first")
}

// seedUsageAddon stores an addon with one usage price on m.
func (s *CommitmentServiceSuite) seedUsageAddon(id string, meterID string) {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        id,
		LookupKey: id,
		Name:      id,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	p := *s.fx.priceAPI
	p.ID, p.EntityType, p.EntityID, p.MeterID, p.GroupID = "price_"+id, types.PRICE_ENTITY_TYPE_ADDON, id, meterID, ""
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &p))
}

func (s *CommitmentServiceSuite) addonAdd(id string, at time.Time) AddonAdd {
	return AddonAdd{Request: &dto.AddAddonToSubscriptionRequest{
		AddonID:           id,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(at),
		ProrationBehavior: types.ProrationBehaviorNone,
	}}
}

func (s *CommitmentServiceSuite) TestAddonChange_SwapKeepsBindingCommitmentScopeCovered() {
	feb := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		swapIn  bool
		wantErr bool
	}{
		{name: "swapping the only scoped addon for one on the same meter passes", swapIn: true},
		{name: "removing it alone is rejected", wantErr: true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			addonMeter := *s.fx.meterStorage
			addonMeter.ID, addonMeter.EventName = "meter_addon", "addon_usage"
			s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, &addonMeter))
			s.seedUsageAddon("addon_out", addonMeter.ID)
			s.seedUsageAddon("addon_in", addonMeter.ID)

			changes := NewAddonChangeService(fullServiceParams(&s.BaseServiceTestSuite))
			_, _, err := changes.Execute(ctx, AddonChangeRequest{
				Subscription: s.fx.sub,
				Adds:         []AddonAdd{s.addonAdd("addon_out", s.fx.sub.StartDate)},
			})
			s.Require().NoError(err)

			req := validReq(s.fx.sub.ID, "")
			req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: addonMeter.ID}
			created, err := s.svc.Create(ctx, req)
			s.Require().NoError(err)

			assocs, err := s.GetStores().AddonAssociationRepo.List(ctx, &types.AddonAssociationFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				AddonIDs:    []string{"addon_out"},
			})
			s.Require().NoError(err)
			s.Require().Len(assocs, 1)

			change := AddonChangeRequest{
				Subscription: s.fx.sub,
				Removes: []*dto.RemoveAddonRequest{{
					AddonAssociationID: assocs[0].ID,
					ProrationBehavior:  types.ProrationBehaviorNone,
					EffectiveDate:      lo.ToPtr(feb),
				}},
			}
			if tt.swapIn {
				change.Adds = []AddonAdd{s.addonAdd("addon_in", feb)}
			}

			_, _, err = changes.Execute(ctx, change)
			if tt.wantErr {
				s.requireValidation(err, created.ID, "terminate the commitment first")
				return
			}

			s.Require().NoError(err)
			_, items, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, s.fx.sub.ID)
			s.Require().NoError(err)
			incoming, ok := lo.Find(items, func(li *subscription.SubscriptionLineItem) bool { return li.PriceID == "price_addon_in" })
			s.Require().True(ok, "the incoming addon line exists")
			s.True(incoming.StartDate.Equal(feb))
		})
	}
}

func (s *CommitmentServiceSuite) TestLineItemUpdate_RejectsPriceOverrideEmptyingBindingCommitmentScope() {
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	_, err = NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite)).UpdateSubscriptionLineItem(s.GetContext(), s.fx.lineAPI.ID,
		dto.UpdateSubscriptionLineItemRequest{
			Amount:        lo.ToPtr(dec("0.07")),
			EffectiveFrom: lo.ToPtr(s.fx.sub.StartDate.AddDate(0, 1, 0)),
		})
	s.requireValidation(err, "terminate the commitment first")
}

// microsecondLineItemRepo stores line item dates the way production writes them: the ent hook truncates
// every time field to whole microseconds, leaving lib/pq's half-up rounding nothing to round.
type microsecondLineItemRepo struct {
	subscription.LineItemRepository
}

func (r *microsecondLineItemRepo) Create(ctx context.Context, li *subscription.SubscriptionLineItem) error {
	stored := *li
	stored.StartDate, stored.EndDate = stored.StartDate.Truncate(time.Microsecond), stored.EndDate.Truncate(time.Microsecond)
	return r.LineItemRepository.Create(ctx, &stored)
}

func (r *microsecondLineItemRepo) Update(ctx context.Context, li *subscription.SubscriptionLineItem) error {
	stored := *li
	stored.StartDate, stored.EndDate = stored.StartDate.Truncate(time.Microsecond), stored.EndDate.Truncate(time.Microsecond)
	return r.LineItemRepository.Update(ctx, &stored)
}

// An end instant that reaches the service with stray nanoseconds (bypassing the API binding) must still
// not open a sub-microsecond gap in the binding commitment's coverage: storage truncation is the backstop.
func (s *CommitmentServiceSuite) TestLineItemUpdate_PriceEditKeepsCoverageAcrossMicrosecondRounding() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	_, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	// The edited line is the scope's only cover after January.
	only, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, "subs_line_api_only")
	s.Require().NoError(err)
	only.EndDate = s.fx.sub.StartDate.AddDate(0, 1, 0)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, only))

	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.SubscriptionLineItemRepo = &microsecondLineItemRepo{LineItemRepository: s.GetStores().SubscriptionLineItemRepo}
	effectiveFrom := time.Date(2025, 2, 1, 0, 0, 0, 123456789, time.UTC)
	resp, err := NewSubscriptionService(params).UpdateSubscriptionLineItem(ctx, s.fx.lineAPI.ID,
		dto.UpdateSubscriptionLineItemRequest{
			Amount:        lo.ToPtr(dec("0.07")),
			EffectiveFrom: lo.ToPtr(effectiveFrom),
		})
	s.Require().NoError(err)

	ended, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.fx.lineAPI.ID)
	s.Require().NoError(err)
	successor, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	s.True(successor.StartDate.Equal(ended.EndDate), "successor starts %s, predecessor ends %s", successor.StartDate, ended.EndDate)
}
