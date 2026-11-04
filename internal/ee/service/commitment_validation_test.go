package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
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
	jan := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
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
		{"C17 billing_terms without terms", func(r *dto.CreateCommitmentRequest) {
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
		}, false},
		{"C17 billing_terms with terms", func(r *dto.CreateCommitmentRequest) {
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
			r.BillingTerms = upfrontPlusRemainder()
		}, true},
		{"C21 label too long", func(r *dto.CreateCommitmentRequest) { r.InvoiceLabel = string(make([]byte, 256)) }, false},
		{"C2 end before start", func(r *dto.CreateCommitmentRequest) { r.EndDate = lo.ToPtr(r.StartDate) }, false},
		{"D8 open-ended without end_date", func(r *dto.CreateCommitmentRequest) { r.EndDate = nil }, true},
		{"start_date with a fraction of a second", func(r *dto.CreateCommitmentRequest) {
			r.StartDate = r.StartDate.Add(750 * time.Millisecond)
		}, false},
		{"end_date with a fraction of a second", func(r *dto.CreateCommitmentRequest) {
			r.EndDate = lo.ToPtr(r.EndDate.Add(time.Microsecond))
		}, false},
		{"billing term bill_on with a fraction of a second", func(r *dto.CreateCommitmentRequest) {
			r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
			r.BillingTerms = upfrontPlusRemainder()
			r.BillingTerms[0].BillOn = lo.ToPtr(r.StartDate.Add(500 * time.Millisecond))
		}, false},
	}

	t.Run("billing_terms default to the subscription invoice", func(t *testing.T) {
		r := validReq("subs_1", "price_1")
		r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
		r.ApplyDefaults("UTC")
		require.Equal(t, types.InstalmentDeliverySubscriptionInvoice, lo.FromPtr(r.InstalmentInvoicing))

		r.InstalmentInvoicing = lo.ToPtr(types.InstalmentDeliverySeparateInvoice)
		r.ApplyDefaults("UTC")
		require.Equal(t, types.InstalmentDeliverySeparateInvoice, lo.FromPtr(r.InstalmentInvoicing), "a chosen delivery is kept")
	})

	t.Run("per_period leaves instalment_invoicing unset", func(t *testing.T) {
		r := validReq("subs_1", "price_1")
		r.ApplyDefaults("UTC")
		require.Nil(t, r.InstalmentInvoicing)
	})

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
	future.StartDate = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &future))

	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "exactly one meter")
}

func (s *CommitmentServiceSuite) TestValidation_ScheduledLegacyLineItemCommitment() {
	future := *s.fx.lineAPI
	future.ID, future.CommitmentAmount = "subs_line_api_legacy", lo.ToPtr(decimal.NewFromInt(50))
	future.StartDate = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &future))

	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "legacy line-item commitment")
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

	// A is terminated mid-way through its May 2025 billing period, which it still bills up to the end of.
	terminatedAt := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		mutateA func(r *dto.CreateCommitmentRequest)
		mutateB func(r *dto.CreateCommitmentRequest)
		wantErr string
	}{
		{
			name:    "different type in the period A was terminated in",
			mutateA: amountA,
			mutateB: startB(time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: "different type or measurement",
		},
		{
			name:    "different overage rate in A's final billing period",
			mutateA: overageA,
			mutateB: func(r *dto.CreateCommitmentRequest) {
				r.StartDate = time.Date(2025, 5, 20, 0, 0, 0, 0, time.UTC)
				r.OveragePrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.08"))}
			},
			wantErr: "different overage price",
		},
		{
			name:    "different type in the next billing period",
			mutateA: amountA,
			mutateB: startB(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
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
	terminatedAt := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
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
			mutateB:   amountFrom(time.Date(2025, 5, 20, 0, 0, 0, 0, time.UTC), 1),
			wantErr:   "different type or measurement",
		},
		{name: "different type after the terminated commitment's last period", terminate: true, mutateB: amountFrom(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), 1)},
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

// A subscription starting mid-second, as one created without a start_date does, accepts a commitment starting at the
// whole second holding its start.
func (s *CommitmentServiceSuite) TestValidation_AcceptsStartMatchingAMidSecondSubscriptionStart() {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	sub.StartDate = sub.StartDate.Add(500 * time.Millisecond)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	// The usage lines inherit the mid-second start, as subscription creation sets them.
	line, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.fx.lineAPI.ID)
	s.Require().NoError(err)
	line = subscription.NewSubscriptionLineItemBuilder(line).WithStartDate(sub.StartDate).Build()
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, line))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.StartDate = sub.StartDate.Truncate(time.Second)
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)
	s.True(created.StartDate.Equal(sub.StartDate.Truncate(time.Second)))
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

// endSubscriptionAt schedules the fixture subscription's end.
func (s *CommitmentServiceSuite) endSubscriptionAt(end time.Time) *subscription.Subscription {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.EndDate = lo.ToPtr(end)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	s.fx.sub.EndDate = lo.ToPtr(end)
	return sub
}

func (s *CommitmentServiceSuite) TestValidation_MidPeriodEndOverlap() {
	// A ends on 10 May 2025, mid-way through a billing period whose invoice still bills its floor.
	endsMidMay := func(r *dto.CreateCommitmentRequest) {
		r.EndDate = lo.ToPtr(time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC))
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
			mutateSecond: amountFrom(time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:      "different type or measurement",
		},
		{
			name:         "A ending before a different type starts in the same billing period",
			mutateFirst:  amountFrom(time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)),
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
				r.StartDate = time.Date(2025, 5, 20, 0, 0, 0, 0, time.UTC)
				r.OveragePrice = ratePrice("0.08")
			},
			wantErr: "different overage price",
		},
		{
			name:         "different type from the next billing period",
			mutateFirst:  endsMidMay,
			mutateSecond: amountFrom(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:        "same type and priority after A's end",
			mutateFirst: endsMidMay,
			mutateSecond: func(r *dto.CreateCommitmentRequest) {
				r.StartDate = time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)
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

func (s *CommitmentServiceSuite) TestCreate_RejectsSamePriorityOverFutureCutoff() {
	a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	created, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)
	cutoff := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &cutoff})
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	b.StartDate = time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	_, err = s.svc.Create(s.GetContext(), b)
	s.requireValidation(err, "priority")
}
