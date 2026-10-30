package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type commitmentFixtures struct {
	customer     *customer.Customer
	sub          *subscription.Subscription
	meterAPI     *meter.Meter
	meterStorage *meter.Meter
	priceAPI     *price.Price
	priceStorage *price.Price
	group        *group.Group
	lineAPI      *subscription.SubscriptionLineItem
	lineStorage  *subscription.SubscriptionLineItem

	groupAPIOnly           *group.Group
	priceOtherMeterInGroup *price.Price
}

// seedCommitmentFixtures stores a monthly subscription anchored 1 Jan 2027 with two usage prices in group grp_1,
// and enables commitments_v2.
func seedCommitmentFixtures(t *testing.T, ctx context.Context, stores testutil.Stores) commitmentFixtures {
	t.Helper()
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fx := commitmentFixtures{}

	fx.customer = &customer.Customer{
		ID:         "cust_cmt",
		ExternalID: "ext_cust_cmt",
		Name:       "Commit Co",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.CustomerRepo.Create(ctx, fx.customer))

	fx.meterAPI = &meter.Meter{
		ID:        "meter_api",
		Name:      "GPUs",
		EventName: "gpu",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "gpus",
			BucketSize: types.WindowSizeMinute,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	fx.meterStorage = &meter.Meter{
		ID:          "meter_storage",
		Name:        "Storage",
		EventName:   "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "bytes"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.MeterRepo.CreateMeter(ctx, fx.meterAPI))
	require.NoError(t, stores.MeterRepo.CreateMeter(ctx, fx.meterStorage))

	fx.group = &group.Group{
		ID:            "grp_1",
		Name:          "Compute",
		EntityType:    types.GroupEntityTypePrice,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.GroupRepo.Create(ctx, fx.group))

	usagePrice := func(id, meterID string, amount decimal.Decimal) *price.Price {
		return &price.Price{
			ID:                 id,
			Amount:             amount,
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           "plan_cmt",
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			MeterID:            meterID,
			GroupID:            fx.group.ID,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}

	fx.priceAPI = usagePrice("price_api", fx.meterAPI.ID, dec("0.06"))
	fx.priceStorage = usagePrice("price_storage", fx.meterStorage.ID, dec("0.02"))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceAPI))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceStorage))

	fx.sub = &subscription.Subscription{
		ID:                 "subs_cmt",
		PlanID:             "plan_cmt",
		CustomerID:         fx.customer.ID,
		Currency:           "usd",
		StartDate:          jan,
		BillingAnchor:      jan,
		CurrentPeriodStart: jan,
		CurrentPeriodEnd:   jan.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Timezone:           "UTC",
		SubscriptionStatus: types.SubscriptionStatusActive,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	line := func(id string, p *price.Price) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:             id,
			SubscriptionID: fx.sub.ID,
			CustomerID:     fx.customer.ID,
			EntityID:       "plan_cmt",
			EntityType:     types.SubscriptionLineItemEntityTypePlan,
			PriceID:        p.ID,
			PriceType:      p.Type,
			MeterID:        p.MeterID,
			DisplayName:    p.ID,
			Quantity:       decimal.Zero,
			Currency:       "usd",
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence: types.InvoiceCadenceArrear,
			StartDate:      jan,
			EnvironmentID:  types.GetEnvironmentID(ctx),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
	}

	fx.lineAPI = line("subs_line_api", fx.priceAPI)
	fx.lineStorage = line("subs_line_storage", fx.priceStorage)
	require.NoError(t, stores.SubscriptionRepo.CreateWithLineItems(ctx, fx.sub,
		[]*subscription.SubscriptionLineItem{fx.lineAPI, fx.lineStorage}))

	fx.groupAPIOnly = &group.Group{
		ID:            "grp_api_only",
		Name:          "API only",
		EntityType:    types.GroupEntityTypePrice,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.GroupRepo.Create(ctx, fx.groupAPIOnly))
	// price_api_only moves price_api's twin into the API-only group; the other-meter price joins it unattached.
	apiOnly := usagePrice("price_api_only", fx.meterAPI.ID, dec("0.06"))
	apiOnly.GroupID = fx.groupAPIOnly.ID
	fx.priceOtherMeterInGroup = usagePrice("price_other_meter", fx.meterStorage.ID, dec("0.02"))
	fx.priceOtherMeterInGroup.GroupID = fx.groupAPIOnly.ID
	require.NoError(t, stores.PriceRepo.Create(ctx, apiOnly))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceOtherMeterInGroup))
	apiOnlyLine := line("subs_line_api_only", apiOnly)
	require.NoError(t, stores.SubscriptionLineItemRepo.Create(ctx, apiOnlyLine))

	setCommitmentsV2(t, ctx, stores, true)
	return fx
}

func setCommitmentsV2(t *testing.T, ctx context.Context, stores testutil.Stores, enabled bool) {
	t.Helper()
	if _, err := stores.SettingsRepo.GetByKey(ctx, types.SettingKeyCommitmentsV2); err == nil {
		require.NoError(t, stores.SettingsRepo.DeleteByKey(ctx, types.SettingKeyCommitmentsV2))
	}

	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyCommitmentsV2,
		Value:         map[string]interface{}{"enabled": enabled},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.SettingsRepo.Create(ctx, setting))
}

type CommitmentServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc CommitmentService
	fx  commitmentFixtures
}

func TestCommitmentServiceSuite(t *testing.T) { suite.Run(t, new(CommitmentServiceSuite)) }

func (s *CommitmentServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewCommitmentService(fullServiceParams(&s.BaseServiceTestSuite))
	s.fx = seedCommitmentFixtures(s.T(), s.GetContext(), s.GetStores())
}

func (s *CommitmentServiceSuite) TestCreate_ComputesNoStoredTotalForPerPeriod() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Nil(resp.TotalAmount)
	s.Equal("cmt_", resp.ID[:4])
	s.True(resp.CommitmentValue.Equal(dec("4.608")), "got %s", resp.CommitmentValue) // 8 × 0.048 × 12 periods
	s.Equal(types.CommitmentCollectionPerPeriod, lo.FromPtr(resp.CollectionMethod))
	s.Equal("Base", resp.InvoiceLabel)
	s.Equal(s.fx.customer.ID, resp.CustomerID)
	s.Nil(resp.OveragePriceID)
}

func (s *CommitmentServiceSuite) TestCreate_CommittedAndOveragePricesAreSubscriptionScoped() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.OveragePrice = ratePrice("0.07")
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	for id, want := range map[string]decimal.Decimal{
		*resp.CommittedPriceID: dec("0.048"),
		*resp.OveragePriceID:   dec("0.07"),
	} {
		p, err := s.GetStores().PriceRepo.Get(s.GetContext(), id)
		s.Require().NoError(err)
		s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, p.EntityType)
		s.Equal(s.fx.sub.ID, p.EntityID)
		s.Equal(s.fx.meterAPI.ID, p.MeterID)
		s.Equal(types.PRICE_TYPE_USAGE, p.Type)
		s.Equal("usd", p.Currency)
		s.True(p.Amount.Equal(want), "price %s amount %s", id, p.Amount)
	}
}

func (s *CommitmentServiceSuite) TestCreate_TieredOveragePassesItsTiersThrough() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.OveragePrice = tieredOverage()
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	p, err := s.GetStores().PriceRepo.Get(s.GetContext(), *resp.OveragePriceID)
	s.Require().NoError(err)
	s.Equal(types.BILLING_MODEL_TIERED, p.BillingModel)
	s.Equal(types.BILLING_TIER_SLAB, p.TierMode)
	s.Require().Len(p.Tiers, 2)
	s.True(p.Tiers[0].UnitAmount.Equal(dec("0.07")), "got %s", p.Tiers[0].UnitAmount)
	s.True(p.Tiers[1].UnitAmount.Equal(dec("0.05")), "got %s", p.Tiers[1].UnitAmount)
	s.Equal(s.fx.meterAPI.ID, p.MeterID)
}

func (s *CommitmentServiceSuite) TestCreate_TieredCommittedPriceValuesEachPeriodOnItsTiers() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Value, req.CommittedPrice = dec("150"), tieredOverage()
	preview, err := s.svc.Preview(s.GetContext(), req)
	s.Require().NoError(err)

	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	for _, v := range []*decimal.Decimal{preview.CommitmentValue, created.CommitmentValue, got.CommitmentValue} {
		s.True(v.Equal(dec("114")), "got %s", v)
	}
}

func (s *CommitmentServiceSuite) TestPreview_RejectsTieredOverageWithoutTiers() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.OveragePrice = tieredOverage()
	req.OveragePrice.Tiers = nil
	_, err := s.svc.Preview(s.GetContext(), req)
	s.requireValidation(err, "tiers are required")
}

func (s *CommitmentServiceSuite) TestCreate_AmountCommitmentHasNoPrices() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.group.ID}
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Nil(resp.CommittedPriceID)
	s.Nil(resp.OveragePriceID)
	s.True(resp.CommitmentValue.Equal(decimal.NewFromInt(1200)), "got %s", resp.CommitmentValue)
}

func (s *CommitmentServiceSuite) TestCreate_RejectedWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	enabled, err := s.svc.IsEnabled(s.GetContext())
	s.Require().NoError(err)
	s.False(enabled)
}

func (s *CommitmentServiceSuite) TestCreate_IdempotencyKeyReturnsExisting() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("contract-1")
	a, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	b, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Equal(a.ID, b.ID)
	n, err := s.GetStores().CommitmentRepo.Count(s.GetContext(), commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Equal(1, n)
}

// lateKeyRepo misses the first `misses` idempotency lookups, as when a concurrent create commits while this one
// waits for the lock (one miss) or inserts after it (two misses).
type lateKeyRepo struct {
	commitment.Repository
	misses  int
	lookups int
}

func (r *lateKeyRepo) GetByIdempotencyKey(ctx context.Context, key string) (*commitment.Commitment, error) {
	r.lookups++
	if r.lookups <= r.misses {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}

	return r.Repository.GetByIdempotencyKey(ctx, key)
}

func (s *CommitmentServiceSuite) TestCreate_KeyReplayedUnderLockReturnsOriginalWithoutWrites() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("contract-2")
	first, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	pricesBefore, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)

	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	params := fullServiceParams(&s.BaseServiceTestSuite)
	repo := &lateKeyRepo{Repository: params.CommitmentRepo, misses: 1}
	params.CommitmentRepo = repo
	got, err := NewCommitmentService(params).Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Equal(first.ID, got.ID)
	s.Equal(2, repo.lookups)
	pricesAfter, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Equal(pricesBefore, pricesAfter)
	s.Empty(publisher.Events(), "a replayed key must not publish commitment.created again")
}

// Each replay site — before the transaction, under the lock, and after losing the insert race — rejects a key
// that belongs to another subscription instead of returning that subscription's commitment.
func (s *CommitmentServiceSuite) TestCreate_IdempotencyKeyFromAnotherSubscriptionIsRejected() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("contract-shared")
	first, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	other := *s.fx.sub
	other.ID = "subs_cmt_other"
	otherLine := *s.fx.lineAPI
	otherLine.ID, otherLine.SubscriptionID = "subs_line_api_other", other.ID
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, &other,
		[]*subscription.SubscriptionLineItem{&otherLine}))

	tests := []struct {
		name   string
		misses int
	}{
		{"found before the transaction", 0},
		{"found under the lock", 1},
		{"found after the insert race", 2},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			params := fullServiceParams(&s.BaseServiceTestSuite)
			params.CommitmentRepo = &lateKeyRepo{Repository: params.CommitmentRepo, misses: tc.misses}
			otherReq := validReq(other.ID, s.fx.priceAPI.ID)
			otherReq.IdempotencyKey = req.IdempotencyKey
			_, err := NewCommitmentService(params).Create(ctx, otherReq)
			s.requireValidation(err, s.fx.sub.ID, other.ID)
		})
	}

	replay, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)
	s.Equal(first.ID, replay.ID, "the key still replays on its own subscription")
	n, err := s.GetStores().CommitmentRepo.Count(ctx, commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Equal(1, n)
}

func (s *CommitmentServiceSuite) TestPreview_RejectedWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	_, err := s.svc.Preview(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestPreview_DoesNotPersist() {
	before, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	resp, err := s.svc.Preview(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	s.True(resp.CommitmentValue.Equal(dec("4.608")), "got %s", resp.CommitmentValue)
	n, err := s.GetStores().CommitmentRepo.Count(s.GetContext(), commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Zero(n)
	prices, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Equal(before, prices)
}

func (s *CommitmentServiceSuite) TestGetAndList() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(created.ID, got.ID)
	s.True(got.CommitmentValue.Equal(dec("4.608")), "got %s", got.CommitmentValue)

	f := commitment.NewDefaultFilter()
	f.SubscriptionIDs = []string{s.fx.sub.ID}
	list, err := s.svc.List(s.GetContext(), f)
	s.Require().NoError(err)
	s.Require().Len(list.Items, 1)
	s.Equal(1, list.Pagination.Total)
	s.True(list.Items[0].CommitmentValue.Equal(dec("4.608")))

	_, err = s.svc.Get(s.GetContext(), "cmt_missing")
	s.True(ierr.IsNotFound(err), "got %v", err)
}

// An open-ended commitment has no finite value: preview, create, get and list return it with none.
func (s *CommitmentServiceSuite) TestOpenEnded_HasNoCommitmentValue() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = nil
	preview, err := s.svc.Preview(s.GetContext(), req)
	s.Require().NoError(err)
	s.Nil(preview.EndDate)
	s.Nil(preview.CommitmentValue)

	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	list, err := s.svc.List(s.GetContext(), commitment.NewDefaultFilter())
	s.Require().NoError(err)
	s.Require().Len(list.Items, 1)
	for _, resp := range []*dto.CommitmentResponse{created, got, list.Items[0]} {
		s.Nil(resp.EndDate)
		s.Nil(resp.CommitmentValue)
	}
}

func (s *CommitmentServiceSuite) TestTerminate_OpenEndedAnyTimeWithoutFee() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = nil
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	at := created.StartDate.AddDate(10, 0, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)
	s.True(resp.TerminatedAt.Equal(at), "got %s", resp.TerminatedAt)
	s.Nil(resp.TerminationInvoiceID)
	s.Nil(resp.EndDate)
}

// With no term end to cover, the fee covers the rest of the billing period holding effective_at, up to a
// subscription end; a fee at or after that end is rejected.
func (s *CommitmentServiceSuite) TestTerminate_OpenEndedFeeCoversThePeriodHoldingEffectiveAt() {
	apr, may := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)
	subEnd := apr.AddDate(0, 0, 20)
	tests := []struct {
		name          string
		at            time.Time
		subEnd        *time.Time
		wantPeriodEnd time.Time
		wantErr       bool
	}{
		{name: "on a period edge", at: apr, wantPeriodEnd: may},
		{name: "mid-period", at: apr.AddDate(0, 0, 9), wantPeriodEnd: may},
		{name: "subscription ending inside the period", at: apr.AddDate(0, 0, 9), subEnd: &subEnd, wantPeriodEnd: subEnd},
		{name: "just before the subscription end", at: subEnd.Add(-time.Second), subEnd: &subEnd, wantPeriodEnd: subEnd},
		{name: "at the subscription end", at: subEnd, subEnd: &subEnd, wantErr: true},
		{name: "after the subscription end", at: subEnd.AddDate(0, 0, 5), subEnd: &subEnd, wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			if tt.subEnd != nil {
				s.endSubscriptionAt(*tt.subEnd)
			}

			req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
			req.EndDate = nil
			created, err := s.svc.Create(s.GetContext(), req)
			s.Require().NoError(err)

			s.requireFeeCovers(created.ID, tt.at, tt.wantPeriodEnd, tt.wantErr)
		})
	}
}

// A subscription end set after the commitment, e.g. a scheduled cancellation, bounds a finite term's fee too.
func (s *CommitmentServiceSuite) TestTerminate_FeeStopsAtAnEarlierSubscriptionEnd() {
	subEnd := time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		at            time.Time
		wantPeriodEnd time.Time
		wantErr       bool
	}{
		{name: "before the subscription end", at: subEnd.AddDate(0, -1, 0), wantPeriodEnd: subEnd},
		{name: "at the subscription end", at: subEnd, wantErr: true},
		{name: "after the subscription end", at: subEnd.AddDate(0, 1, 0), wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
			s.Require().NoError(err)
			s.endSubscriptionAt(subEnd)

			s.requireFeeCovers(created.ID, tt.at, tt.wantPeriodEnd, tt.wantErr)
		})
	}
}

// requireFeeCovers terminates with a 75 fee at `at` and checks the fee invoice covers [at, wantPeriodEnd), or, with
// wantErr, that it is rejected and the commitment stays active.
func (s *CommitmentServiceSuite) requireFeeCovers(commitmentID string, at, wantPeriodEnd time.Time, wantErr bool) {
	resp, err := s.svc.Terminate(s.GetContext(), commitmentID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &at,
		CancellationFee: lo.ToPtr(dec("75")),
	})
	if wantErr {
		s.True(ierr.IsValidation(err), "got %v", err)
		got, err := s.svc.Get(s.GetContext(), commitmentID)
		s.Require().NoError(err)
		s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
		return
	}

	s.Require().NoError(err)
	s.Require().NotNil(resp.TerminationInvoiceID)

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), *resp.TerminationInvoiceID)
	s.Require().NoError(err)
	s.True(inv.AmountDue.Equal(dec("75")), "got %s", inv.AmountDue)
	s.True(lo.FromPtr(inv.PeriodStart).Equal(at), "period start %v", inv.PeriodStart)
	s.True(lo.FromPtr(inv.PeriodEnd).Equal(wantPeriodEnd), "period end %v", inv.PeriodEnd)
	lines := s.invoiceLines(inv.ID)
	s.Require().Len(lines, 1)
	s.requireLine(lines[0], types.CommitmentLineCancellationFee, "75", commitmentID)
	s.True(lo.FromPtr(lines[0].PeriodStart).Equal(at), "line period start %v", lines[0].PeriodStart)
	s.True(lo.FromPtr(lines[0].PeriodEnd).Equal(wantPeriodEnd), "line period end %v", lines[0].PeriodEnd)
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_TerminatesOpenEnded() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = nil
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	at := created.StartDate.AddDate(3, 0, 0)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at))
	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, got.CommitmentStatus)
	s.True(got.TerminatedAt.Equal(at), "got %s", got.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestUpdate_OnlyLabelsAndMetadata() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	updated, err := s.svc.Update(s.GetContext(), created.ID, dto.UpdateCommitmentRequest{
		InvoiceLabel: lo.ToPtr("8× H100"), Metadata: types.Metadata{"contract": "c-1"},
	})
	s.Require().NoError(err)
	s.Equal("8× H100", updated.InvoiceLabel)
	s.Equal("Base", updated.Name)
	s.Equal("c-1", updated.Metadata["contract"])
	s.True(updated.Value.Equal(created.Value))

	_, err = s.svc.Update(s.GetContext(), created.ID, dto.UpdateCommitmentRequest{Name: lo.ToPtr("")})
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestWrites_PublishCommitmentWebhooks() {
	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("cmt-webhooks")
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	_, err = s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	_, err = s.svc.Update(s.GetContext(), created.ID, dto.UpdateCommitmentRequest{InvoiceLabel: lo.ToPtr("Renamed")})
	s.Require().NoError(err)

	late := created.EndDate.AddDate(0, 1, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &late})
	s.Require().Error(err)

	at := created.StartDate.AddDate(0, 4, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	events := publisher.Events()
	s.Require().Len(events, 3)
	for i, want := range []types.WebhookEventName{
		types.WebhookEventCommitmentCreated,
		types.WebhookEventCommitmentUpdated,
		types.WebhookEventCommitmentTerminated,
	} {
		s.Equal(want, events[i].EventName)
		s.Equal(types.SystemEntityTypeCommitment, events[i].EntityType)
		s.Equal(created.ID, events[i].EntityID)

		var payload webhookDto.InternalCommitmentEvent
		s.Require().NoError(json.Unmarshal(events[i].Payload, &payload))
		s.Equal(created.ID, payload.CommitmentID)
		s.Equal(s.fx.sub.ID, payload.SubscriptionID)
		s.Equal(types.GetTenantID(s.GetContext()), payload.TenantID)
	}
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_PublishesTerminated() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, created.StartDate.AddDate(0, 2, 0)))

	events := publisher.Events()
	s.Require().Len(events, 1)
	s.Equal(types.WebhookEventCommitmentTerminated, events[0].EventName)
	s.Equal(created.ID, events[0].EntityID)
}

func (s *CommitmentServiceSuite) TestTerminate_SetsStatusAndTerminatedAt() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, 4, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)
	s.True(resp.TerminatedAt.Equal(at))
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestTerminate_RejectsEffectiveAtAfterEnd() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.EndDate.AddDate(0, 1, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestTerminate_KeepsRequestedDateBeforeStart() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, -1, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.True(resp.TerminatedAt.Equal(at), "got %s", resp.TerminatedAt)
	s.True(resp.CommitmentValue.IsPositive(), "value is still reported for a commitment terminated before start")
}

func (s *CommitmentServiceSuite) TestTerminate_RejectsInvalidCancellationFee() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	midTerm := created.StartDate.AddDate(0, 3, 0)
	tests := []struct {
		name string
		at   time.Time
		fee  decimal.Decimal
	}{
		{name: "zero fee", at: midTerm, fee: decimal.Zero},
		{name: "negative fee", at: midTerm, fee: decimal.NewFromInt(-5)},
		{name: "terminate at end_date", at: *created.EndDate, fee: decimal.NewFromInt(10)},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
				EffectiveAt:     &tt.at,
				CancellationFee: &tt.fee,
			})
			s.True(ierr.IsValidation(err), "got %v", err)
		})
	}

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
}

// createChargeable creates a binding quantity commitment of 100 units/month at 1.00 for 12 months from 1 Jan 2027,
// so 1,200.00 in total and 100.00 earned per month.
func (s *CommitmentServiceSuite) createChargeable(binding bool) *dto.CommitmentResponse {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Value, req.CommittedPrice, req.Binding = decimal.NewFromInt(100), ratePrice("1"), lo.ToPtr(binding)
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	return created
}

func (s *CommitmentServiceSuite) invoiceLines(invoiceID string) []*invoice.InvoiceLineItem {
	lines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	return lines
}

func (s *CommitmentServiceSuite) requireLine(line *invoice.InvoiceLineItem, kind types.CommitmentLineKind, amount, commitmentID string) {
	s.Equal(string(kind), line.Metadata[types.MetadataKeyCommitmentLineKind])
	s.Equal(commitmentID, line.Metadata[types.MetadataKeyCommitmentID])
	s.Equal(commitmentID, lo.FromPtr(line.CommitmentID))
	s.Equal(commitmentID, lo.FromPtr(line.EntityID))
	s.Equal(string(types.InvoiceLineItemEntityTypeCommitment), lo.FromPtr(line.EntityType))
	s.True(line.Quantity.Equal(decimal.NewFromInt(1)), "quantity %s", line.Quantity)
	s.True(line.Amount.Equal(dec(amount)), "amount %s, want %s", line.Amount, amount)
}

func (s *CommitmentServiceSuite) TestTerminate_FlatFeeBillsOneOffInvoice() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &at,
		CancellationFee: lo.ToPtr(dec("250.5")),
	})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)
	s.Require().NotNil(resp.TerminationInvoiceID)

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), *resp.TerminationInvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal(string(types.InvoiceBillingReasonProration), inv.BillingReason)
	s.Equal(s.fx.customer.ID, inv.CustomerID)
	s.True(inv.AmountDue.Equal(dec("250.5")), "got %s", inv.AmountDue)
	lines := s.invoiceLines(inv.ID)
	s.Require().Len(lines, 1)
	s.requireLine(lines[0], types.CommitmentLineCancellationFee, "250.5", created.ID)
	s.Equal(created.InvoiceLabel+" (cancellation fee)", lo.FromPtr(lines[0].DisplayName))
}

func (s *CommitmentServiceSuite) TestTerminate_WithoutFeeCreatesNoInvoice() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.Nil(resp.TerminationInvoiceID)

	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), types.NewNoLimitInvoiceFilter())
	s.Require().NoError(err)
	s.Empty(invoices)
}

func (s *CommitmentServiceSuite) TestTerminate_FeeRequiresBindingCommitment() {
	created := s.createChargeable(false)
	at := created.StartDate.AddDate(0, 3, 0)
	_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &at,
		CancellationFee: lo.ToPtr(decimal.NewFromInt(5)),
	})
	s.requireValidation(err, "binding")

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
}

func (s *CommitmentServiceSuite) TestTerminate_SecondTerminateBillsNothing() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	req := dto.TerminateCommitmentRequest{EffectiveAt: &at, CancellationFee: lo.ToPtr(decimal.NewFromInt(50))}
	_, err := s.svc.Terminate(s.GetContext(), created.ID, req)
	s.Require().NoError(err)

	_, err = s.svc.Terminate(s.GetContext(), created.ID, req)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), types.NewNoLimitInvoiceFilter())
	s.Require().NoError(err)
	s.Len(invoices, 1)
}

func (s *CommitmentServiceSuite) TestTerminate_RejectedWhenDisabled() {
	created := s.createChargeable(true)
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	at := created.StartDate.AddDate(0, 3, 0)
	_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

// failingInvoiceRepo rejects every invoice insert.
type failingInvoiceRepo struct{ invoice.Repository }

func (failingInvoiceRepo) Create(context.Context, *invoice.Invoice) error {
	return ierr.NewError("invoice store unavailable").Mark(ierr.ErrDatabase)
}

func (failingInvoiceRepo) CreateWithLineItems(context.Context, *invoice.Invoice) error {
	return ierr.NewError("invoice store unavailable").Mark(ierr.ErrDatabase)
}

// The in-memory DB has no rollback, so the invoice is created before the status change: a failure leaves the
// commitment active here, and the transaction guarantees the same against a real database.
func (s *CommitmentServiceSuite) TestTerminate_InvoiceFailureLeavesCommitmentActive() {
	created := s.createChargeable(true)
	params := fullServiceParams(&s.BaseServiceTestSuite)
	params.InvoiceRepo = failingInvoiceRepo{params.InvoiceRepo}

	at := created.StartDate.AddDate(0, 3, 0)
	_, err := NewCommitmentService(params).Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &at,
		CancellationFee: lo.ToPtr(decimal.NewFromInt(50)),
	})
	s.Require().Error(err)

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
	s.Nil(got.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_KeepsRequestedDateAndIsIdempotent() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, -1, 0)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at))
	first, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.True(first.TerminatedAt.Equal(at), "got %s", first.TerminatedAt)

	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at.AddDate(0, 2, 0)))
	second, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, second.CommitmentStatus)
	s.True(second.TerminatedAt.Equal(*first.TerminatedAt))
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_TerminatesActiveCommitments() {
	a, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	req := validReq(s.fx.sub.ID, s.fx.priceStorage.ID)
	b, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	at := a.StartDate.AddDate(0, 2, 0)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at))
	for _, id := range []string{a.ID, b.ID} {
		got, err := s.svc.Get(s.GetContext(), id)
		s.Require().NoError(err)
		s.Equal(types.CommitmentStatusTerminated, got.CommitmentStatus)
		s.True(got.TerminatedAt.Equal(at))
	}
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_PullsLaterTerminationBack() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	scheduled := created.StartDate.AddDate(0, 2, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &scheduled})
	s.Require().NoError(err)

	cancelAt := created.StartDate.AddDate(0, 0, 15)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, cancelAt))
	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, got.CommitmentStatus)
	s.True(got.TerminatedAt.Equal(cancelAt), "got %s", got.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestActiveForPeriod_IncludesTerminated() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, 4, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	got, err := s.svc.ActiveForPeriod(s.GetContext(), s.fx.sub.ID, at.AddDate(0, -1, 0), at)
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	s.Equal(created.ID, got[0].ID)

	got, err = s.svc.ActiveForPeriod(s.GetContext(), "subs_other", at.AddDate(0, -1, 0), at)
	s.Require().NoError(err)
	s.Empty(got)
}

// Validation rules that need stored subscription data.

func (s *CommitmentServiceSuite) requireValidation(err error, contains ...string) {
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "want validation error, got %v", err)
	for _, c := range contains {
		s.Contains(err.Error(), c)
	}
}

// A $300 floor ending 16 Apr is worth January to March in full and 15 of April's 30 days.
func (s *CommitmentServiceSuite) TestValidation_V1_AcceptsEndDateMidPeriod() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, dec("300")
	req.EndDate = lo.ToPtr(time.Date(2027, 4, 16, 0, 0, 0, 0, time.UTC))
	preview, err := s.svc.Preview(s.GetContext(), req)
	s.Require().NoError(err)

	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	for _, v := range []*decimal.Decimal{preview.CommitmentValue, created.CommitmentValue, got.CommitmentValue} {
		s.True(v.Equal(dec("1050")), "got %s", v)
	}
}

func (s *CommitmentServiceSuite) TestValidation_V2_QuantityOverGroupWithTwoMeters() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.group.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "exactly one meter")
}

func (s *CommitmentServiceSuite) TestValidation_V3_AmountAndQuantityOverlap() {
	amount := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	amount.Type, amount.CommittedPrice, amount.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	_, err := s.svc.Create(s.GetContext(), amount)
	s.Require().NoError(err)

	qty := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	qty.Priority = 1
	_, err = s.svc.Create(s.GetContext(), qty)
	s.requireValidation(err, "different type or measurement")
}

func (s *CommitmentServiceSuite) TestValidation_V4_SamePriorityOverlap() {
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "priority 0")
}

func (s *CommitmentServiceSuite) TestValidation_V5_LegacyLineItemCommitment() {
	li := s.fx.lineAPI
	li.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(50))
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(s.GetContext(), li))
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) TestValidation_V18_OverlapsMustShareOneFlatOverage() {
	a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	a.OveragePrice = ratePrice("0.07")
	_, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	b.Priority = 1
	for name, overage := range map[string]*dto.CommitmentPriceInput{
		"different flat amount": ratePrice("0.08"),
		"no overage":            nil,
		"tiered overage":        tieredOverage(),
	} {
		s.Run(name, func() {
			b.OveragePrice = overage
			_, err := s.svc.Create(s.GetContext(), b)
			s.requireValidation(err, "different overage price")
		})
	}

	b.OveragePrice = ratePrice("0.07")
	_, err = s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestValidation_V18_TieredOverageNeverCombines() {
	a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	a.OveragePrice = tieredOverage()
	_, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	b.Priority, b.OveragePrice = 1, tieredOverage()
	_, err = s.svc.Create(s.GetContext(), b)
	s.requireValidation(err, "only equal flat overage amounts")
}

func (s *CommitmentServiceSuite) TestSameCommitmentOverage() {
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
			got, err := svc.sameCommitmentOverage(ctx, &commitment.Commitment{OveragePriceID: tt.a}, &commitment.Commitment{OveragePriceID: tt.b})
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}

func (s *CommitmentServiceSuite) TestValidation_C16_SubscriptionScopeWithLegacyCommitment() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(500))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedPrice, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: s.fx.sub.ID}
	_, err = s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "legacy subscription-level commitment")
}

// fullServiceParams wires every in-memory store the commitment, subscription and billing flows touch.
func fullServiceParams(s *testutil.BaseServiceTestSuite) ServiceParams {
	st := s.GetStores()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		EventRepo:                    st.EventRepo,
		SubRepo:                      st.SubscriptionRepo,
		SubscriptionLineItemRepo:     st.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        st.SubscriptionPhaseRepo,
		SubScheduleRepo:              st.SubscriptionScheduleRepo,
		PlanRepo:                     st.PlanRepo,
		PlanPriceSyncRepo:            st.PlanPriceSyncRepo,
		PriceRepo:                    st.PriceRepo,
		PriceUnitRepo:                st.PriceUnitRepo,
		MeterRepo:                    st.MeterRepo,
		CustomerRepo:                 st.CustomerRepo,
		TenantRepo:                   st.TenantRepo,
		EnvironmentRepo:              st.EnvironmentRepo,
		InvoiceRepo:                  st.InvoiceRepo,
		InvoiceLineItemRepo:          st.InvoiceLineItemRepo,
		HostedInvoiceTokenRepo:       st.HostedInvoiceTokenRepo,
		PaymentRepo:                  st.PaymentRepo,
		EntitlementRepo:              st.EntitlementRepo,
		EntitlementGrantRepo:         st.EntitlementGrantRepo,
		FeatureRepo:                  st.FeatureRepo,
		AddonRepo:                    st.AddonRepo,
		AddonAssociationRepo:         st.AddonAssociationRepo,
		CreditGrantRepo:              st.CreditGrantRepo,
		CreditGrantApplicationRepo:   st.CreditGrantApplicationRepo,
		GroupRepo:                    st.GroupRepo,
		SettingsRepo:                 st.SettingsRepo,
		AddonPriceSyncRepo:           st.AddonPriceSyncRepo,
		CommitmentRepo:               st.CommitmentRepo,
		CheckoutSessionRepo:          st.CheckoutSessionRepo,
		TaxAssociationRepo:           st.TaxAssociationRepo,
		TaxRateRepo:                  st.TaxRateRepo,
		TaxAppliedRepo:               st.TaxAppliedRepo,
		FXRateRepo:                   st.FXRateRepo,
		WalletRepo:                   st.WalletRepo,
		CouponRepo:                   st.CouponRepo,
		CouponAssociationRepo:        st.CouponAssociationRepo,
		CouponApplicationRepo:        st.CouponApplicationRepo,
		ConnectionRepo:               st.ConnectionRepo,
		EntityIntegrationMappingRepo: st.EntityIntegrationMappingRepo,
		MeterUsageRepo:               st.MeterUsageRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
}

func (s *CommitmentServiceSuite) TestCancelSubscription_TerminatesCommitments() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	subSvc := NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite))
	at := created.StartDate.AddDate(0, 2, 0)
	s.Require().NoError(subSvc.TerminateSubscriptionResources(s.GetContext(), dto.TerminateSubscriptionResourcesRequest{
		SubscriptionID: s.fx.sub.ID, EffectiveDate: at,
	}))
	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, got.CommitmentStatus)
}

func (s *CommitmentServiceSuite) TestTerminateResources_NoopWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	subSvc := NewSubscriptionService(fullServiceParams(&s.BaseServiceTestSuite))
	s.Require().NoError(subSvc.TerminateSubscriptionResources(s.GetContext(), dto.TerminateSubscriptionResourcesRequest{
		SubscriptionID: s.fx.sub.ID, EffectiveDate: s.fx.sub.StartDate.AddDate(0, 2, 0),
	}))
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

	b := validReq(s.fx.sub.ID, "")
	b.Name = "Storage"
	b.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterStorage.ID}
	cb, err := s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, s.fx.sub.StartDate, nil)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), ca.ID)
	s.Contains(err.Error(), cb.ID)
}

// recordingDB and recordingCommitmentRepo log the order of transactions, locks and reads.
type recordingDB struct {
	postgres.IClient
	events *[]string
}

func (d *recordingDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	*d.events = append(*d.events, "tx")
	err := d.IClient.WithTx(ctx, fn)
	*d.events = append(*d.events, "tx_end")
	return err
}

func (d *recordingDB) LockWithWait(ctx context.Context, req postgres.LockRequest) error {
	*d.events = append(*d.events, "lock:"+req.Key)
	return d.IClient.LockWithWait(ctx, req)
}

type recordingCommitmentRepo struct {
	commitment.Repository
	events *[]string
}

func (r *recordingCommitmentRepo) Get(ctx context.Context, id string) (*commitment.Commitment, error) {
	*r.events = append(*r.events, "get")
	return r.Repository.Get(ctx, id)
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

// requireInsertUnderLock asserts the insert ran inside the transaction that took the commitments lock.
func (s *CommitmentServiceSuite) requireInsertUnderLock(events []string, insert string) {
	at := lo.IndexOf(events, insert)
	s.Require().GreaterOrEqual(at, 0, "events %v", events)
	lock := lo.LastIndexOf(events[:at], "lock:commitments:"+s.fx.sub.ID)
	s.Require().GreaterOrEqual(lock, 0, "events %v", events)
	depth := 0
	for _, e := range events[lock:at] {
		switch e {
		case "tx":
			depth++
		case "tx_end":
			depth--
		}
		s.Require().GreaterOrEqual(depth, 0, "lock released before %s: %v", insert, events)
	}
}

func (s *CommitmentServiceSuite) TestUpdate_ReadsUnderCommitmentLockInTx() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	var events []string
	stores := s.GetStores()
	params := ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       &recordingDB{IClient: s.GetDB(), events: &events},
		CommitmentRepo:           &recordingCommitmentRepo{Repository: stores.CommitmentRepo, events: &events},
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		PriceRepo:                stores.PriceRepo,
		MeterRepo:                stores.MeterRepo,
		GroupRepo:                stores.GroupRepo,
		SettingsRepo:             stores.SettingsRepo,
		WebhookPublisher:         s.GetWebhookPublisher(),
	}

	_, err = NewCommitmentService(params).Update(
		s.GetContext(),
		created.ID,
		dto.UpdateCommitmentRequest{Name: lo.ToPtr("Renamed")},
	)
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(len(events), 3, "events %v", events)
	s.Equal([]string{"tx", "lock:commitment:" + created.ID, "get"}, events[:3])
}

func (s *CommitmentServiceSuite) TestUpdate_AfterTerminateKeepsTerminated() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, 4, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	updated, err := s.svc.Update(
		s.GetContext(),
		created.ID,
		dto.UpdateCommitmentRequest{InvoiceLabel: lo.ToPtr("Renamed")},
	)
	s.Require().NoError(err)
	s.Equal("Renamed", updated.InvoiceLabel)
	s.Equal(types.CommitmentStatusTerminated, updated.CommitmentStatus)
	s.Require().NotNil(updated.TerminatedAt)
	s.True(updated.TerminatedAt.Equal(at), "got %s", updated.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestValidation_EndDateAfterSubscriptionEnd() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.EndDate = lo.ToPtr(sub.StartDate.AddDate(0, 6, 0))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID) // ends 12 months in
	_, err = s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "subscription end")

	req.EndDate = lo.ToPtr(sub.EndDate.Add(time.Second))
	_, err = s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "subscription end")

	req.EndDate = lo.ToPtr(*sub.EndDate)
	_, err = s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestValidation_C16_AnyScopeWithLegacyCommitment() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(500))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "legacy subscription-level commitment")
}

func (s *CommitmentServiceSuite) TestValidation_ScopePriceMustBillEverySubscriptionPeriod() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.BillingPeriod = types.BILLING_PERIOD_QUARTER
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "commitments require scope prices billed every subscription period")
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

func (s *CommitmentServiceSuite) TestValidation_FutureScopeLineOnAnotherCadence() {
	future := *s.fx.lineAPI
	future.ID, future.BillingPeriod = "subs_line_api_weekly", types.BILLING_PERIOD_WEEKLY
	future.StartDate = time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &future))

	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "commitments require scope prices billed every subscription period")
}

func (s *CommitmentServiceSuite) TestCreate_RejectsIncompleteSubscription() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "must be active or trialing")
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

func (s *CommitmentServiceSuite) TestResponse_ResolvesWindowBucketAfterScopeLineEndsBeforeCurrentPeriod() {
	ctx := s.GetContext()
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.Measurement = types.CommitmentMeasurementWindow
	created, err := s.svc.Create(ctx, req)
	s.Require().NoError(err)

	// The scope's only line is versioned on 1 Feb, then the subscription moves on to March.
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	only, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, "subs_line_api_only")
	s.Require().NoError(err)
	successor := *only
	successor.ID, successor.StartDate, successor.PredecessorLineItemID = "subs_line_api_only_v2", feb, lo.ToPtr(only.ID)
	only.EndDate, only.SuccessorLineItemID = feb, lo.ToPtr(successor.ID)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, only))
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &successor))

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = feb.AddDate(0, 1, 0), feb.AddDate(0, 2, 0)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	svc := s.svc.(*commitmentService)
	cc, err := svc.loadCommitmentContext(ctx, s.fx.sub.ID)
	s.Require().NoError(err)
	s.Equal(types.WindowSizeMinute, bucketFor(created.Commitment, cc))

	got, err := s.svc.Get(ctx, created.ID)
	s.Require().NoError(err)
	s.True(created.CommitmentValue.Equal(*got.CommitmentValue), "want %s, got %s", created.CommitmentValue, got.CommitmentValue)
}

// listCountingLineItemRepo counts line-item list reads.
type listCountingLineItemRepo struct {
	subscription.LineItemRepository
	lists int
}

func (r *listCountingLineItemRepo) List(ctx context.Context, f *types.SubscriptionLineItemFilter) ([]*subscription.SubscriptionLineItem, error) {
	r.lists++
	return r.LineItemRepository.List(ctx, f)
}

func (s *CommitmentServiceSuite) TestResponse_LoadsScopeOnlyForWindowMeasurement() {
	perPeriod, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceStorage.ID))
	s.Require().NoError(err)

	window := validReq(s.fx.sub.ID, "")
	window.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	window.Measurement = types.CommitmentMeasurementWindow
	windowed, err := s.svc.Create(s.GetContext(), window)
	s.Require().NoError(err)

	tests := []struct {
		name      string
		created   *dto.CommitmentResponse
		wantLists int
	}{
		{"billing period reads only the subscription", perPeriod, 0},
		{"window loads the scope lines", windowed, 1},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			params := fullServiceParams(&s.BaseServiceTestSuite)
			repo := &listCountingLineItemRepo{LineItemRepository: params.SubscriptionLineItemRepo}
			params.SubscriptionLineItemRepo = repo
			got, err := NewCommitmentService(params).Get(s.GetContext(), tt.created.ID)
			s.Require().NoError(err)
			s.True(got.CommitmentValue.Equal(*tt.created.CommitmentValue), "want %s, got %s", tt.created.CommitmentValue, got.CommitmentValue)
			s.Equal(tt.wantLists, repo.lists)
		})
	}
}

func (s *CommitmentServiceSuite) TestTerminate_FeeBeforeCurrentPeriodIsRejected() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CurrentPeriodStart = sub.CurrentPeriodStart.AddDate(0, 2, 0)
	sub.CurrentPeriodEnd = sub.CurrentPeriodStart.AddDate(0, 1, 0)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	before := sub.CurrentPeriodStart.AddDate(0, -1, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &before,
		CancellationFee: lo.ToPtr(dec("25")),
	})
	s.requireValidation(err, "current billing period")

	// Without a fee the same retroactive date terminates fine.
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &before})
	s.Require().NoError(err)
}

// splitAPILinesAt ends the API-meter lines at at and starts one on a new price with the given bucket size.
func (s *CommitmentServiceSuite) splitAPILinesAt(at time.Time, bucket types.WindowSize) {
	ctx := s.GetContext()
	for _, id := range []string{s.fx.lineAPI.ID, "subs_line_api_only"} {
		li, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, id)
		s.Require().NoError(err)
		li.EndDate = at
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))
	}

	next := *s.fx.priceAPI
	next.ID, next.GroupID, next.BucketSize = "price_api_succ", "", bucket
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &next))

	line := *s.fx.lineAPI
	line.ID, line.PriceID, line.StartDate, line.EndDate = "subs_line_api_succ", next.ID, at, time.Time{}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &line))
}

func (s *CommitmentServiceSuite) windowMeterCommitment(priority int, start time.Time) dto.CreateCommitmentRequest {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	req.Measurement = types.CommitmentMeasurementWindow
	req.Priority, req.StartDate = priority, start
	return req
}

func (s *CommitmentServiceSuite) TestValidation_OverlapRejectsBucketSizeMismatch() {
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	first, err := s.svc.Create(s.GetContext(), s.windowMeterCommitment(0, s.fx.sub.StartDate))
	s.Require().NoError(err)

	s.splitAPILinesAt(feb, types.WindowSizeHour)
	_, err = s.svc.Create(s.GetContext(), s.windowMeterCommitment(1, feb))
	s.requireValidation(err, first.ID, string(types.WindowSizeHour), string(types.WindowSizeMinute))
}

// groupedBucketedPrice stores a bucketed MAX price in grp_1 whose meter groups usage by region.
func (s *CommitmentServiceSuite) groupedBucketedPrice() *price.Price {
	ctx := s.GetContext()
	m := *s.fx.meterAPI
	m.ID, m.EventName, m.Aggregation.GroupBy = "meter_grouped", "grouped", "region"
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, &m))
	p := *s.fx.priceAPI
	p.ID, p.MeterID = "price_grouped", m.ID
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &p))
	return &p
}

func (s *CommitmentServiceSuite) TestValidation_RejectsCalendarWindowBuckets() {
	tests := []struct {
		size types.WindowSize
		want []string
	}{
		{size: types.WindowSizeWeek, want: []string{"weekly buckets", "Sunday"}},
		{size: types.WindowSizeMonth, want: []string{"monthly buckets", "UTC calendar date"}},
	}

	for _, tt := range tests {
		s.Run(string(tt.size), func() {
			s.SetupTest()
			ctx := s.GetContext()
			calendar := *s.fx.priceAPI
			calendar.ID, calendar.BucketSize = "price_api_calendar", tt.size
			s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &calendar))
			line := *s.fx.lineAPI
			line.ID, line.PriceID = "subs_line_api_calendar", calendar.ID
			s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &line))

			req := validReq(s.fx.sub.ID, calendar.ID)
			req.Measurement = types.CommitmentMeasurementWindow
			_, err := s.svc.Create(ctx, req)
			s.requireValidation(err, tt.want...)

			// Measured per billing period, the same price is fine.
			req.Measurement = types.CommitmentMeasurementBillingPeriod
			_, err = s.svc.Create(ctx, req)
			s.Require().NoError(err)
		})
	}
}

func (s *CommitmentServiceSuite) TestValidation_RejectsGroupedBucketedPriceInScope() {
	ctx := s.GetContext()
	p := s.groupedBucketedPrice()
	line := *s.fx.lineAPI
	line.ID, line.PriceID, line.MeterID = "subs_line_grouped", p.ID, p.MeterID
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &line))

	_, err := s.svc.Create(ctx, validReq(s.fx.sub.ID, p.ID))
	s.requireValidation(err, p.ID, "region", "grouped bucketed")
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

func (s *CommitmentServiceSuite) TestValidation_OverlapChecksEveryLineOfTheOtherScope() {
	ctx := s.GetContext()
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	first, err := s.svc.Create(ctx, s.windowMeterCommitment(0, s.fx.sub.StartDate))
	s.Require().NoError(err)

	// An hourly line joins the first commitment's scope beside the minute lines it started with.
	hourly := *s.fx.priceAPI
	hourly.ID, hourly.GroupID, hourly.BucketSize = "price_api_hourly", "", types.WindowSizeHour
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &hourly))
	line := *s.fx.lineAPI
	line.ID, line.PriceID, line.StartDate = "subs_line_api_hourly", hourly.ID, feb
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &line))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Measurement, req.Priority, req.StartDate = types.CommitmentMeasurementWindow, 1, feb
	_, err = s.svc.Create(ctx, req)
	s.requireValidation(err, first.ID, string(types.WindowSizeHour), string(types.WindowSizeMinute))
}

func (s *CommitmentServiceSuite) TestValidation_OverlapAcceptsMatchingBucketSize() {
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.svc.Create(s.GetContext(), s.windowMeterCommitment(0, s.fx.sub.StartDate))
	s.Require().NoError(err)

	s.splitAPILinesAt(feb, "")
	_, err = s.svc.Create(s.GetContext(), s.windowMeterCommitment(1, feb))
	s.Require().NoError(err)
}
