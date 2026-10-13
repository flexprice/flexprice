package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
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
}

// seedCommitmentFixtures stores a monthly subscription anchored 1 Jan 2027 with two usage prices in group grp_1,
// and enables commitments_v2.
func seedCommitmentFixtures(t *testing.T, ctx context.Context, stores testutil.Stores) commitmentFixtures {
	t.Helper()
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fx := commitmentFixtures{}

	fx.customer = &customer.Customer{ID: "cust_cmt", ExternalID: "ext_cust_cmt", Name: "Commit Co", BaseModel: types.GetDefaultBaseModel(ctx)}
	require.NoError(t, stores.CustomerRepo.Create(ctx, fx.customer))

	fx.meterAPI = &meter.Meter{
		ID: "meter_api", Name: "GPUs", EventName: "gpu",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeMinute},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	fx.meterStorage = &meter.Meter{
		ID: "meter_storage", Name: "Storage", EventName: "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "bytes"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, stores.MeterRepo.CreateMeter(ctx, fx.meterAPI))
	require.NoError(t, stores.MeterRepo.CreateMeter(ctx, fx.meterStorage))

	fx.group = &group.Group{ID: "grp_1", Name: "Compute", EntityType: types.GroupEntityTypePrice,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	require.NoError(t, stores.GroupRepo.Create(ctx, fx.group))

	usagePrice := func(id, meterID string, amount decimal.Decimal) *price.Price {
		return &price.Price{
			ID: id, Amount: amount, Currency: "usd", EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_cmt",
			Type: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
			BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING,
			InvoiceCadence: types.InvoiceCadenceArrear, MeterID: meterID, GroupID: fx.group.ID,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	fx.priceAPI = usagePrice("price_api", fx.meterAPI.ID, dec("0.06"))
	fx.priceStorage = usagePrice("price_storage", fx.meterStorage.ID, dec("0.02"))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceAPI))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceStorage))

	fx.sub = &subscription.Subscription{
		ID: "subs_cmt", PlanID: "plan_cmt", CustomerID: fx.customer.ID, Currency: "usd",
		StartDate: jan, BillingAnchor: jan, CurrentPeriodStart: jan, CurrentPeriodEnd: jan.AddDate(0, 1, 0),
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, Timezone: "UTC",
		SubscriptionStatus: types.SubscriptionStatusActive, EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	line := func(id string, p *price.Price) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID: id, SubscriptionID: fx.sub.ID, CustomerID: fx.customer.ID, EntityID: "plan_cmt",
			EntityType: types.SubscriptionLineItemEntityTypePlan, PriceID: p.ID, PriceType: p.Type, MeterID: p.MeterID,
			DisplayName: p.ID, Quantity: decimal.Zero, Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence: types.InvoiceCadenceArrear, StartDate: jan, EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	fx.lineAPI = line("subs_line_api", fx.priceAPI)
	fx.lineStorage = line("subs_line_storage", fx.priceStorage)
	require.NoError(t, stores.SubscriptionRepo.CreateWithLineItems(ctx, fx.sub,
		[]*subscription.SubscriptionLineItem{fx.lineAPI, fx.lineStorage}))

	setCommitmentsV2(t, ctx, stores, true)
	return fx
}

func setCommitmentsV2(t *testing.T, ctx context.Context, stores testutil.Stores, enabled bool) {
	t.Helper()
	if _, err := stores.SettingsRepo.GetByKey(ctx, types.SettingKeyCommitmentsV2); err == nil {
		require.NoError(t, stores.SettingsRepo.DeleteByKey(ctx, types.SettingKeyCommitmentsV2))
	}
	setting := &settings.Setting{
		ID: types.GenerateUUIDWithPrefix("setting"), Key: types.SettingKeyCommitmentsV2,
		Value: map[string]interface{}{"enabled": enabled}, EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.GetDefaultBaseModel(ctx),
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
	stores := s.GetStores()
	params := ServiceParams{
		Logger: s.GetLogger(), Config: s.GetConfig(), DB: s.GetDB(),
		CommitmentRepo: stores.CommitmentRepo, SubRepo: stores.SubscriptionRepo, PriceRepo: stores.PriceRepo,
		MeterRepo: stores.MeterRepo, GroupRepo: stores.GroupRepo, SettingsRepo: stores.SettingsRepo,
		PlanRepo: stores.PlanRepo, AddonRepo: stores.AddonRepo, PriceUnitRepo: stores.PriceUnitRepo,
	}
	s.svc = NewCommitmentService(params)
	s.fx = seedCommitmentFixtures(s.T(), s.GetContext(), stores)
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
	req.OverageRate = lo.ToPtr(dec("0.07"))
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	for id, want := range map[string]decimal.Decimal{*resp.CommittedPriceID: dec("0.048"), *resp.OveragePriceID: dec("0.07")} {
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

func (s *CommitmentServiceSuite) TestCreate_AmountCommitmentHasNoPrices() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedRate, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
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

func (s *CommitmentServiceSuite) TestCreateLocked_ReplayedKeyReturnsOriginalWithoutWrites() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("contract-2")
	first, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	pricesBefore, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)

	got, err := s.svc.(*commitmentService).createLocked(s.GetContext(), &req)
	s.Require().NoError(err)
	s.Equal(first.ID, got.ID)
	pricesAfter, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Equal(pricesBefore, pricesAfter)
}

func (s *CommitmentServiceSuite) TestPreview_RejectedWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	_, err := s.svc.Preview(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestPreview_DoesNotPersist() {
	resp, err := s.svc.Preview(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	s.True(resp.CommitmentValue.Equal(dec("4.608")), "got %s", resp.CommitmentValue)
	n, err := s.GetStores().CommitmentRepo.Count(s.GetContext(), commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Zero(n)
	prices, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Equal(2, prices)
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

func (s *CommitmentServiceSuite) TestTerminate_ClampsTerminatedAtToStartDate() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, -1, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.True(resp.TerminatedAt.Equal(created.StartDate), "got %s", resp.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_ClampsAndIsIdempotent() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, -1, 0)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at))
	first, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.True(first.TerminatedAt.Equal(created.StartDate), "got %s", first.TerminatedAt)

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

// Validation rules that need stored subscription data (ERD §6.6).

func (s *CommitmentServiceSuite) requireValidation(err error, contains ...string) {
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "want validation error, got %v", err)
	for _, c := range contains {
		s.Contains(err.Error(), c)
	}
}

func (s *CommitmentServiceSuite) TestValidation_V1_EndDateNotOnBillingBoundary() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = time.Date(2027, 4, 23, 0, 0, 0, 0, time.UTC)
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "1 Apr 2027", "1 May 2027")
}

func (s *CommitmentServiceSuite) TestValidation_V2_QuantityOverGroupWithTwoMeters() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.group.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "exactly one meter")
}

func (s *CommitmentServiceSuite) TestValidation_V3_AmountAndQuantityOverlap() {
	amount := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	amount.Type, amount.CommittedRate, amount.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
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

func (s *CommitmentServiceSuite) TestValidation_V18_DifferentOverageRates() {
	a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	a.OverageRate = lo.ToPtr(dec("0.07"))
	_, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	b.Priority, b.OverageRate = 1, lo.ToPtr(dec("0.08"))
	_, err = s.svc.Create(s.GetContext(), b)
	s.requireValidation(err, "different overage rate")

	b.OverageRate = lo.ToPtr(dec("0.07"))
	_, err = s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestValidation_C16_SubscriptionScopeWithLegacyCommitment() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(500))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedRate, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: s.fx.sub.ID}
	_, err = s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "legacy subscription-level commitment")
}
