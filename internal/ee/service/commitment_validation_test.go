package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// ratePrice is a flat committed or overage price at amount.
func ratePrice(amount string) *dto.CommitmentPriceInput {
	return &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec(amount))}
}

// tieredOverage charges 0.07 for the first 100 units and 0.05 after.
func tieredOverage() *dto.CommitmentPriceInput {
	return &dto.CommitmentPriceInput{
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []dto.CreatePriceTier{
			{UpTo: lo.ToPtr(uint64(100)), UnitAmount: dec("0.07")},
			{UnitAmount: dec("0.05")},
		},
	}
}

func validReq(subID, priceID string) dto.CreateCommitmentRequest {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return dto.CreateCommitmentRequest{
		SubscriptionID: subID,
		Name:           "Base",
		Scope:          dto.CommitmentScopeInput{EntityType: types.CommitmentScopePrice, EntityID: priceID},
		Type:           types.COMMITMENT_TYPE_QUANTITY,
		Value:          decimal.NewFromInt(8),
		CommittedPrice: ratePrice("0.048"),
		Measurement:    types.CommitmentMeasurementBillingPeriod,
		StartDate:      jan,
		EndDate:        lo.ToPtr(jan.AddDate(1, 0, 0)),
		Binding:        lo.ToPtr(true),
	}
}

// entitleMeter gives the subscription's plan an enabled metered entitlement on the meter.
func (s *CommitmentServiceSuite) entitleMeter(meterID string, isEnabled bool) {
	ctx := s.GetContext()
	f := &feature.Feature{
		ID:        "feat_" + meterID,
		Name:      "Feature " + meterID,
		Type:      types.FeatureTypeMetered,
		MeterID:   meterID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))

	e := &entitlement.Entitlement{
		ID:               "ent_" + meterID,
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         s.fx.sub.PlanID,
		FeatureID:        f.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        isEnabled,
		UsageLimit:       lo.ToPtr(int64(100)),
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, e)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestValidation_RejectsEntitledScopeMeter() {
	s.entitleMeter(s.fx.meterAPI.ID, true)

	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().Error(err)
	s.Require().True(ierr.IsValidation(err))
	s.Require().Contains(err.Error(), "entitlement")

	// A disabled entitlement does not block the commitment.
	s.entitleMeter(s.fx.meterStorage.ID, false)
	storage := validReq(s.fx.sub.ID, s.fx.priceStorage.ID)
	storage.Type, storage.CommittedPrice = types.COMMITMENT_TYPE_AMOUNT, nil
	_, err = s.svc.Create(s.GetContext(), storage)
	s.Require().NoError(err)
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

func TestCreateCommitmentRequest_Validate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(r *dto.CreateCommitmentRequest)
		ok     bool
	}{
		{"valid", func(r *dto.CreateCommitmentRequest) {}, true},
		{"standalone without subscription_id", func(r *dto.CreateCommitmentRequest) { r.SubscriptionID = "" }, false},
		{"scope without entity_id", func(r *dto.CreateCommitmentRequest) { r.Scope.EntityID = "" }, false},
		{"C6 zero value", func(r *dto.CreateCommitmentRequest) { r.Value = decimal.Zero }, false},
		{"C7 quantity without committed price", func(r *dto.CreateCommitmentRequest) { r.CommittedPrice = nil }, false},
		{"C7 amount with committed price", func(r *dto.CreateCommitmentRequest) { r.Type = types.COMMITMENT_TYPE_AMOUNT }, false},
		{"C7 zero committed amount", func(r *dto.CreateCommitmentRequest) { r.CommittedPrice = ratePrice("0") }, false},
		{"C7 tiered committed price", func(r *dto.CreateCommitmentRequest) { r.CommittedPrice = tieredOverage() }, true},
		{"C7 tiered committed price without tiers", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice = tieredOverage()
			r.CommittedPrice.Tiers = nil
		}, false},
		{"C7 flat committed price with tiers", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice.Tiers = tieredOverage().Tiers
		}, false},
		{"C7 slab committed price", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice = tieredOverage()
		}, true},
		{"C7 volume committed price", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice = tieredOverage()
			r.CommittedPrice.TierMode = types.BILLING_TIER_VOLUME
		}, false},
		{"C8 volume overage", func(r *dto.CreateCommitmentRequest) {
			r.OveragePrice = tieredOverage()
			r.OveragePrice.TierMode = types.BILLING_TIER_VOLUME
		}, true},
		{"C7 package committed price", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice.BillingModel = types.BILLING_MODEL_PACKAGE
			r.CommittedPrice.TransformQuantity = &price.TransformQuantity{DivideBy: 1000}
		}, true},
		{"C7 package committed price without transform", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice.BillingModel = types.BILLING_MODEL_PACKAGE
		}, false},
		{"C7 explicit flat committed price", func(r *dto.CreateCommitmentRequest) {
			r.CommittedPrice.BillingModel = types.BILLING_MODEL_FLAT_FEE
		}, true},
		{"C8 overage on amount", func(r *dto.CreateCommitmentRequest) {
			r.Type, r.CommittedPrice, r.OveragePrice = types.COMMITMENT_TYPE_AMOUNT, nil, ratePrice("0.07")
		}, false},
		{"C8 flat overage", func(r *dto.CreateCommitmentRequest) { r.OveragePrice = ratePrice("0.07") }, true},
		{"C8 zero flat overage", func(r *dto.CreateCommitmentRequest) { r.OveragePrice = ratePrice("0") }, true},
		{"C8 negative overage", func(r *dto.CreateCommitmentRequest) { r.OveragePrice = ratePrice("-0.01") }, false},
		{"C8 flat overage without amount", func(r *dto.CreateCommitmentRequest) {
			r.OveragePrice = &dto.CommitmentPriceInput{}
		}, false},
		{"C8 tiered overage", func(r *dto.CreateCommitmentRequest) { r.OveragePrice = tieredOverage() }, true},
		{"C8 tiered overage without tier mode", func(r *dto.CreateCommitmentRequest) {
			r.OveragePrice = tieredOverage()
			r.OveragePrice.TierMode = ""
		}, false},
		{"C8 package overage", func(r *dto.CreateCommitmentRequest) {
			r.OveragePrice = ratePrice("1")
			r.OveragePrice.BillingModel = types.BILLING_MODEL_PACKAGE
			r.OveragePrice.TransformQuantity = &price.TransformQuantity{DivideBy: 1000}
		}, true},
		{"C8 unsupported overage model", func(r *dto.CreateCommitmentRequest) {
			r.OveragePrice = ratePrice("1")
			r.OveragePrice.BillingModel = types.BillingModel("SLAB")
		}, false},
		{"C10 windows with billing_period", func(r *dto.CreateCommitmentRequest) {
			r.ActiveWindows = types.ActiveWindows{{Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 21}}}
		}, false},
		{"C12 negative priority", func(r *dto.CreateCommitmentRequest) { r.Priority = -1 }, false},
		{"C11 bad timezone", func(r *dto.CreateCommitmentRequest) { r.Timezone = "Mars/Base" }, false},
		{"C13 non-binding with collection method", func(r *dto.CreateCommitmentRequest) {
			r.Binding = lo.ToPtr(false)
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
		}, false},
		{"C17 billing_terms not yet supported", func(r *dto.CreateCommitmentRequest) {
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
		}, false},
		{"C21 label too long", func(r *dto.CreateCommitmentRequest) { r.InvoiceLabel = string(make([]byte, 256)) }, false},
		{"C2 end before start", func(r *dto.CreateCommitmentRequest) { r.EndDate = lo.ToPtr(r.StartDate) }, false},
		{"D8 open-ended without end_date", func(r *dto.CreateCommitmentRequest) { r.EndDate = nil }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validReq("subs_1", "price_1")
			tc.mutate(&r)
			r.ApplyDefaults("UTC")
			err := r.Validate()
			if tc.ok {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, ierr.IsValidation(err), "want validation error, got %v", err)
		})
	}
}

func (s *CommitmentServiceSuite) TestValidation_ScheduledSecondMeterBreaksQuantityCommitment() {
	future := *s.fx.lineAPI
	future.ID, future.PriceID, future.MeterID = "subs_line_other_meter", s.fx.priceOtherMeterInGroup.ID, s.fx.meterStorage.ID
	future.StartDate = time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &future))

	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "exactly one meter")
}

func (s *CommitmentServiceSuite) TestValidation_ScheduledLegacyLineItemCommitment() {
	future := *s.fx.lineAPI
	future.ID, future.CommitmentAmount = "subs_line_api_legacy", lo.ToPtr(decimal.NewFromInt(50))
	future.StartDate = time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &future))

	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_ReplacedOnlyLineKeepsBucketRule() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.Measurement = types.CommitmentMeasurementWindow
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	// The scope's only line ends where its replacement on an hourly price would start.
	march := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	only, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), "subs_line_api_only")
	s.Require().NoError(err)
	only.EndDate = march
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(s.GetContext(), only))

	hourly := *s.fx.priceAPI
	hourly.ID, hourly.GroupID, hourly.BucketSize = "price_api_only_hourly", s.fx.groupAPIOnly.ID, types.WindowSizeHour
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &hourly, march, nil)
	s.requireValidation(err, "bucket size")
}

func (s *CommitmentServiceSuite) TestValidation_TerminatedCommitmentOverlap() {
	amountA := func(r *dto.CreateCommitmentRequest) {
		r.Type, r.CommittedPrice, r.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	}
	overageA := func(r *dto.CreateCommitmentRequest) {
		r.OveragePrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.07"))}
	}
	startB := func(start time.Time) func(r *dto.CreateCommitmentRequest) {
		return func(r *dto.CreateCommitmentRequest) { r.StartDate = start }
	}

	// A is terminated mid-way through its May 2027 billing period, which it still bills up to the end of.
	terminatedAt := time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		mutateA func(r *dto.CreateCommitmentRequest)
		mutateB func(r *dto.CreateCommitmentRequest)
		wantErr string
	}{
		{
			name:    "different type in the period A was terminated in",
			mutateA: amountA,
			mutateB: startB(time.Date(2027, 5, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: "different type or measurement",
		},
		{
			name:    "different overage rate in A's final billing period",
			mutateA: overageA,
			mutateB: func(r *dto.CreateCommitmentRequest) {
				r.StartDate = time.Date(2027, 5, 20, 0, 0, 0, 0, time.UTC)
				r.OveragePrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.08"))}
			},
			wantErr: "different overage price",
		},
		{
			name:    "different type in the next billing period",
			mutateA: amountA,
			mutateB: startB(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:    "same type and priority recreated after termination",
			mutateA: func(r *dto.CreateCommitmentRequest) {},
			mutateB: startB(terminatedAt),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			tc.mutateA(&a)
			created, err := s.svc.Create(s.GetContext(), a)
			s.Require().NoError(err)
			_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &terminatedAt})
			s.Require().NoError(err)

			b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			tc.mutateB(&b)
			_, err = s.svc.Create(s.GetContext(), b)
			if tc.wantErr == "" {
				s.Require().NoError(err)
				return
			}

			s.requireValidation(err, tc.wantErr)
		})
	}
}

// An open-ended commitment holds its scope forever: any later overlapping commitment on its scope must share its type
// and measurement and take another priority, however far off it starts, until a termination's period has ended.
func (s *CommitmentServiceSuite) TestValidation_OpenEndedOverlap() {
	years := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	terminatedAt := time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC)
	amountFrom := func(start time.Time, priority int) func(r *dto.CreateCommitmentRequest) {
		return func(r *dto.CreateCommitmentRequest) {
			r.Type, r.CommittedPrice, r.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
			r.StartDate, r.EndDate, r.Priority = start, lo.ToPtr(start.AddDate(1, 0, 0)), priority
		}
	}
	quantityFrom := func(start time.Time, priority int) func(r *dto.CreateCommitmentRequest) {
		return func(r *dto.CreateCommitmentRequest) {
			r.StartDate, r.EndDate, r.Priority = start, lo.ToPtr(start.AddDate(1, 0, 0)), priority
		}
	}

	tests := []struct {
		name      string
		terminate bool
		mutateB   func(r *dto.CreateCommitmentRequest)
		wantErr   string
	}{
		{name: "different type years later", mutateB: amountFrom(years, 1), wantErr: "different type or measurement"},
		{name: "same type and priority years later", mutateB: quantityFrom(years, 0), wantErr: "priority 0"},
		{name: "same type at another priority years later", mutateB: quantityFrom(years, 1)},
		{
			name:    "open-ended of a different type years later",
			mutateB: func(r *dto.CreateCommitmentRequest) { amountFrom(years, 1)(r); r.EndDate = nil },
			wantErr: "different type or measurement",
		},
		{
			name:      "different type inside the terminated commitment's last period",
			terminate: true,
			mutateB:   amountFrom(time.Date(2027, 5, 20, 0, 0, 0, 0, time.UTC), 1),
			wantErr:   "different type or measurement",
		},
		{name: "different type after the terminated commitment's last period", terminate: true, mutateB: amountFrom(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), 1)},
		{name: "same type and priority from the termination", terminate: true, mutateB: quantityFrom(terminatedAt, 0)},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			a.EndDate = nil
			created, err := s.svc.Create(s.GetContext(), a)
			s.Require().NoError(err)
			if tc.terminate {
				_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &terminatedAt})
				s.Require().NoError(err)
			}

			b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			tc.mutateB(&b)
			_, err = s.svc.Create(s.GetContext(), b)
			if tc.wantErr == "" {
				s.Require().NoError(err)
				return
			}

			s.requireValidation(err, tc.wantErr)
		})
	}
}

// An open-ended commitment needs no end inside the subscription's: it stops when the subscription does.
func (s *CommitmentServiceSuite) TestValidation_OpenEndedOnSubscriptionWithEndDate() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.EndDate = lo.ToPtr(sub.StartDate.AddDate(0, 6, 0))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = nil
	_, err = s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
}

// A term starting at or after a scheduled subscription end has no billable period, whatever its own end.
func (s *CommitmentServiceSuite) TestValidation_StartAtOrAfterSubscriptionEnd() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	subEnd := sub.StartDate.AddDate(0, 6, 0)
	sub.EndDate = lo.ToPtr(subEnd)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	tests := []struct {
		name    string
		start   time.Time
		end     *time.Time
		wantErr string
	}{
		{"open-ended at the subscription end", subEnd, nil, "start_date must be before the subscription end"},
		{"open-ended after the subscription end", subEnd.AddDate(0, 1, 0), nil, "start_date must be before the subscription end"},
		{"finite at the subscription end", subEnd, lo.ToPtr(subEnd.AddDate(0, 1, 0)), "start_date must be before the subscription end"},
		{"open-ended an instant before the subscription end", subEnd.Add(-time.Second), nil, ""},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			req.StartDate, req.EndDate = tt.start, tt.end
			_, err := s.svc.Create(s.GetContext(), req)
			if tt.wantErr == "" {
				s.Require().NoError(err)
				return
			}

			s.requireValidation(err, tt.wantErr)
		})
	}
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

// endSubscriptionAt schedules the fixture subscription's end.
func (s *CommitmentServiceSuite) endSubscriptionAt(end time.Time) *subscription.Subscription {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.EndDate = lo.ToPtr(end)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	s.fx.sub.EndDate = lo.ToPtr(end)
	return sub
}

// An open-ended commitment on a subscription with an end stops guarding price attaches once it ends.
func (s *CommitmentServiceSuite) TestCheckPriceAttach_OpenEndedStopsAtSubscriptionEnd() {
	sub := s.endSubscriptionAt(time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC))
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.EndDate = nil
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), sub, s.fx.priceOtherMeterInGroup, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), nil)
	s.requireValidation(err, "different meter")
	s.Require().NoError(
		s.svc.CheckPriceAttach(s.GetContext(), sub, s.fx.priceOtherMeterInGroup, time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC), nil),
	)
}

// A binding open-ended commitment needs its scope covered only until a finite subscription ends.
func (s *CommitmentServiceSuite) TestLineItemDetach_OpenEndedStopsAtSubscriptionEnd() {
	jun := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	subEnd := time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC)
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

func (s *CommitmentServiceSuite) TestValidation_MidPeriodEndOverlap() {
	// A ends on 10 May 2027, mid-way through a billing period whose invoice still bills its floor.
	endsMidMay := func(r *dto.CreateCommitmentRequest) {
		r.EndDate = lo.ToPtr(time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC))
	}
	amountFrom := func(start time.Time) func(r *dto.CreateCommitmentRequest) {
		return func(r *dto.CreateCommitmentRequest) {
			r.Type, r.CommittedPrice, r.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
			r.StartDate = start
		}
	}
	tests := []struct {
		name         string
		mutateFirst  func(r *dto.CreateCommitmentRequest)
		mutateSecond func(r *dto.CreateCommitmentRequest)
		wantErr      string
	}{
		{
			name:         "different type after A's end in its final billing period",
			mutateFirst:  endsMidMay,
			mutateSecond: amountFrom(time.Date(2027, 5, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:      "different type or measurement",
		},
		{
			name:         "A ending before a different type starts in the same billing period",
			mutateFirst:  amountFrom(time.Date(2027, 5, 15, 0, 0, 0, 0, time.UTC)),
			mutateSecond: endsMidMay,
			wantErr:      "different type or measurement",
		},
		{
			name: "different overage rate after A's end in its final billing period",
			mutateFirst: func(r *dto.CreateCommitmentRequest) {
				endsMidMay(r)
				r.OveragePrice = ratePrice("0.07")
			},
			mutateSecond: func(r *dto.CreateCommitmentRequest) {
				r.StartDate = time.Date(2027, 5, 20, 0, 0, 0, 0, time.UTC)
				r.OveragePrice = ratePrice("0.08")
			},
			wantErr: "different overage price",
		},
		{
			name:         "different type from the next billing period",
			mutateFirst:  endsMidMay,
			mutateSecond: amountFrom(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:        "same type and priority after A's end",
			mutateFirst: endsMidMay,
			mutateSecond: func(r *dto.CreateCommitmentRequest) {
				r.StartDate = time.Date(2027, 5, 15, 0, 0, 0, 0, time.UTC)
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			first := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			tc.mutateFirst(&first)
			_, err := s.svc.Create(s.GetContext(), first)
			s.Require().NoError(err)

			second := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			tc.mutateSecond(&second)
			_, err = s.svc.Create(s.GetContext(), second)
			if tc.wantErr == "" {
				s.Require().NoError(err)
				return
			}

			s.requireValidation(err, tc.wantErr)
		})
	}
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
	march := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
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
	cutoff := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
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
	cutoff := time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, cutoff.AddDate(0, 0, 5), nil)
	s.requireValidation(err, "different meter")

	periodEnd := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, periodEnd, nil))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_GuardsMidPeriodEndThroughPeriodEnd() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.EndDate = lo.ToPtr(time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC))
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, req.EndDate.AddDate(0, 0, 5), nil)
	s.requireValidation(err, "different meter")

	periodEnd := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, periodEnd, nil))
}

func (s *CommitmentServiceSuite) TestLineItemUpdate_LegacyGuardCoversTerminatedCommitmentThroughPeriodEnd() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)
	cutoff := time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(ctx, created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	// A line starting in the cutoff's billing period overlaps the extended span; one starting at its end does not.
	line := *s.fx.lineAPI
	line.CommitmentAmount, line.CommitmentType = lo.ToPtr(dec("50")), types.COMMITMENT_TYPE_AMOUNT
	line.StartDate = cutoff.AddDate(0, 0, 5)
	s.requireValidation(rejectLegacyCommitmentInScope(ctx, fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &line), "legacy line-item commitment")

	line.StartDate = time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(rejectLegacyCommitmentInScope(ctx, fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &line))
}

func (s *CommitmentServiceSuite) TestCreate_RejectsSamePriorityOverFutureCutoff() {
	a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	created, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)
	cutoff := time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	b.StartDate = time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Create(s.GetContext(), b)
	s.requireValidation(err, "priority")
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
	march := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.StartDate = march
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	gone := *s.fx.lineAPI
	gone.ID, gone.EndDate = "subs_line_api_gone", time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	gone.CommitmentAmount = lo.ToPtr(dec("50"))
	s.Require().NoError(rejectLegacyCommitmentInScope(s.GetContext(), fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &gone))

	// The same legacy line overlapping the commitment term is still rejected.
	overlapping := gone
	overlapping.ID, overlapping.EndDate = "subs_line_api_overlap", time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	err = rejectLegacyCommitmentInScope(s.GetContext(), fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, &overlapping)
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) detachCheck(lineItemID string, endDate time.Time) error {
	li, err := s.GetStores().SubscriptionLineItemRepo.Get(s.GetContext(), lineItemID)
	s.Require().NoError(err)
	return checkLineItemCommitmentDetach(s.GetContext(), fullServiceParams(&s.BaseServiceTestSuite), s.fx.sub, li, endDate)
}

func (s *CommitmentServiceSuite) TestLineItemDetach_RejectsEmptyingBindingCommitmentScope() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	err = s.detachCheck(s.fx.lineAPI.ID, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC))
	s.requireValidation(err, created.ID, "terminate the commitment first")
}

func (s *CommitmentServiceSuite) TestLineItemDetach_AllowsWhenAnotherScopedLineRemains() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)))
}

func (s *CommitmentServiceSuite) TestLineItemDetach_AllowsEndDateWithSuccessorOnSamePrice() {
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
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

	s.Require().NoError(s.detachCheck(s.fx.lineAPI.ID, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)))
}

func (s *CommitmentServiceSuite) TestLineItemDetach_RequiresContinuousCoverage() {
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		nextStart *time.Time // a further API-meter line, open-ended from here
		wantErr   bool
	}{
		{name: "the remaining line ends mid-term", wantErr: true},
		{name: "a second line takes over before the first ends", nextStart: lo.ToPtr(time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC))},
		{name: "a second line takes over on the day the first ends", nextStart: lo.ToPtr(jun)},
		{name: "a second line leaves a gap", nextStart: lo.ToPtr(time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC)), wantErr: true},
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
	s.Require().True(s.fx.lineAPI.StartDate.After(time.Now()), "the fixture line must not have started")
	created, err := s.svc.Create(ctx, validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
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
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
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
