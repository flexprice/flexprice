package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func validReq(subID, priceID string) dto.CreateCommitmentRequest {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return dto.CreateCommitmentRequest{
		SubscriptionID: subID,
		Name:           "Base",
		Scope:          dto.CommitmentScopeInput{EntityType: types.CommitmentScopePrice, EntityID: priceID},
		Type:           types.COMMITMENT_TYPE_QUANTITY,
		Value:          decimal.NewFromInt(8),
		CommittedRate:  lo.ToPtr(dec("0.048")),
		Measurement:    types.CommitmentMeasurementBillingPeriod,
		StartDate:      jan,
		EndDate:        jan.AddDate(1, 0, 0),
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
	storage.Type, storage.CommittedRate = types.COMMITMENT_TYPE_AMOUNT, nil
	_, err = s.svc.Create(s.GetContext(), storage)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsEntitledMeter() {
	// Subscription-scoped commitment over the API meter; the storage meter gains an
	// entitlement afterwards, so a storage price may not join the scope.
	req := validReq(s.fx.sub.ID, s.fx.sub.ID)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: s.fx.sub.ID}
	req.Type, req.CommittedRate = types.COMMITMENT_TYPE_AMOUNT, nil
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	s.entitleMeter(s.fx.meterStorage.ID, true)
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceStorage, s.fx.sub.StartDate)
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
		{"C6 zero value", func(r *dto.CreateCommitmentRequest) { r.Value = decimal.Zero }, false},
		{"C7 quantity without rate", func(r *dto.CreateCommitmentRequest) { r.CommittedRate = nil }, false},
		{"C7 amount with rate", func(r *dto.CreateCommitmentRequest) { r.Type = types.COMMITMENT_TYPE_AMOUNT }, false},
		{"C8 overage on amount", func(r *dto.CreateCommitmentRequest) {
			r.Type, r.CommittedRate, r.OverageRate = types.COMMITMENT_TYPE_AMOUNT, nil, lo.ToPtr(dec("0.07"))
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
		{"C2 end before start", func(r *dto.CreateCommitmentRequest) { r.EndDate = r.StartDate }, false},
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

func TestValidateBoundary(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &subscription.Subscription{
		StartDate:          start,
		BillingAnchor:      start,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Timezone:           "UTC",
	}

	require.NoError(t, validateBoundary(sub, time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)))

	err := validateBoundary(sub, time.Date(2027, 4, 23, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err))
	require.Contains(t, err.Error(), "1 Apr 2027")
	require.Contains(t, err.Error(), "1 May 2027")
}
