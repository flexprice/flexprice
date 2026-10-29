package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type commitmentCreateTerms struct {
	core.Service
	params          ServiceParams
	state           core.State
	initializeCalls int
	err             error
}

func (s *commitmentCreateTerms) Initialize(ctx context.Context, request core.InitializeRequest) (core.State, error) {
	s.initializeCalls++
	if s.params.DB.TxFromContext(ctx) == nil {
		return nil, errors.New("initialize outside owner transaction")
	}
	owner, err := NewCommitmentTermsOwner(s.params.CommitmentRepo, s.params.SubRepo).Snapshot(ctx, request.Owner)
	if err != nil {
		return nil, err
	}
	plan, err := core.NewPlanner().Plan(ctx, core.PlanRequest{Owner: *owner, Calendar: owner.Calendar, Rows: request.Rows})
	if err != nil {
		return nil, err
	}
	state := &billingAPIState{owner: owner, rules: plan.Rules(), items: plan.Items(), summary: domain.CollectionSummary{Currency: owner.Currency, TargetAmount: owner.CollectionTarget, ScheduledAmount: plan.Budget()}}
	for i, rule := range state.rules {
		rule.ID = fmt.Sprintf("rule_%d", i)
	}
	for i, item := range state.items {
		item.ID = fmt.Sprintf("item_%d", i)
		item.BaseModel = types.GetDefaultBaseModel(ctx)
	}
	s.state = state
	return state, s.err
}
func (s *commitmentCreateTerms) Get(context.Context, domain.OwnerRef) (core.State, error) {
	return s.state, nil
}

type commitmentBillingCreateFixture struct {
	base     *testutil.BaseServiceTestSuite
	params   ServiceParams
	service  *commitmentService
	engine   *commitmentCreateTerms
	fixtures commitmentFixtures
	events   *[]string
}

func newCommitmentBillingCreateFixture(t *testing.T) *commitmentBillingCreateFixture {
	t.Helper()
	base := &testutil.BaseServiceTestSuite{}
	base.SetT(t)
	base.SetupSuite()
	base.SetupTest()
	base.WithEnvironment("env_create_terms")
	t.Cleanup(base.TearDownTest)
	fixtures := seedCommitmentFixtures(t, base.GetContext(), base.GetStores())
	fixtures.sub.BillingCadence = types.BILLING_CADENCE_RECURRING
	fixtures.sub.PauseStatus = types.PauseStatusNone
	params := fullServiceParams(base)
	events := []string{}
	params.DB = &recordingDB{IClient: &lifecycleTestDB{IClient: base.GetDB()}, events: &events}
	params.BillingTermsStore = &lifecycleTestStore{order: &events}
	params.BillingScheduleProvisioner = &billingScheduleTestProvisioner{}
	engine := &commitmentCreateTerms{}
	params.BillingTerms = engine
	engine.params = params
	return &commitmentBillingCreateFixture{base: base, params: params, service: newCommitmentService(params), engine: engine, fixtures: fixtures, events: &events}
}
func (f *commitmentBillingCreateFixture) request() dto.CreateCommitmentRequest {
	req := validReq(f.fixtures.sub.ID, f.fixtures.priceAPI.ID)
	req.Type = types.COMMITMENT_TYPE_AMOUNT
	req.CommittedPrice = nil
	req.Value = decimal.NewFromInt(100)
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	req.BillingTerms = &dto.BillingTermsInput{Rows: []dto.BillingTermInput{
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Percent: lo.ToPtr(decimal.NewFromInt(25))},
		{Kind: types.BillingTermRecurring, ValueType: types.BillingTermRemainder, BillAt: lo.ToPtr(types.BillingTermArrear), Split: lo.ToPtr(types.BillingTermEven)},
	}}
	return req
}

func TestCommitmentBillingCreate_PreviewAndPersistence(t *testing.T) {
	f := newCommitmentBillingCreateFixture(t)
	ctx := f.base.GetContext()
	req := f.request()
	preview, err := f.service.Preview(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "1200", preview.TotalAmount.String())
	require.Len(t, preview.BillingTerms.Rows, 2)
	require.Len(t, preview.BillingScheduleItems.Items, 13)
	require.Equal(t, "300", preview.BillingTerms.Rows[0].Value.String())
	require.Empty(t, preview.BillingScheduleItems.Items[0].ID)
	require.Zero(t, f.engine.initializeCalls)
	owners, err := f.params.CommitmentRepo.List(ctx, commitment.NewNoLimitFilter())
	require.NoError(t, err)
	require.Empty(t, owners)
	created, err := f.service.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "1200", created.TotalAmount.String())
	require.Equal(t, string(types.BillingTermSubscriptionInvoice), *created.InstalmentInvoicing)
	require.Len(t, created.BillingScheduleItems.Items, 13)
	require.NotEmpty(t, created.BillingScheduleItems.Items[0].ID)
	require.Equal(t, 1, f.engine.initializeCalls)
	require.Equal(t, []string{"tx", "eligibility", "lock:commitments:" + f.fixtures.sub.ID, "tx_end"}, *f.events)
}

func TestCommitmentBillingCreate_InvalidPlanBeforeRateWrites(t *testing.T) {
	f := newCommitmentBillingCreateFixture(t)
	ctx := f.base.GetContext()
	req := f.request()
	req.Type = types.COMMITMENT_TYPE_QUANTITY
	req.CommittedPrice = ratePrice("1")
	req.BillingTerms.Rows[1].Interval = lo.ToPtr(types.BILLING_PERIOD_WEEKLY)
	_, err := f.service.Create(ctx, req)
	require.Error(t, err)
	owners, err := f.params.CommitmentRepo.List(ctx, commitment.NewNoLimitFilter())
	require.NoError(t, err)
	require.Empty(t, owners)
	require.Zero(t, f.engine.initializeCalls)
	require.Empty(t, f.base.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

func TestCommitmentBillingCreate_IdempotencyPreservesExistingArrangement(t *testing.T) {
	f := newCommitmentBillingCreateFixture(t)
	ctx := f.base.GetContext()
	req := f.request()
	req.IdempotencyKey = lo.ToPtr("original-contract")
	created, err := f.service.Create(ctx, req)
	require.NoError(t, err)
	req.BillingTerms = &dto.BillingTermsInput{Rows: []dto.BillingTermInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}}
	replayed, err := f.service.Create(ctx, req)
	require.NoError(t, err)
	require.Equal(t, created.ID, replayed.ID)
	require.Len(t, replayed.BillingScheduleItems.Items, 13)
	require.Equal(t, 1, f.engine.initializeCalls)
}

func TestCommitmentBillingCreate_InitializationFailurePublishesNothing(t *testing.T) {
	f := newCommitmentBillingCreateFixture(t)
	f.engine.err = errors.New("schedule persistence failed")
	_, err := f.service.Create(f.base.GetContext(), f.request())
	require.ErrorContains(t, err, "schedule persistence failed")
	require.Equal(t, 1, f.engine.initializeCalls)
	require.Empty(t, f.base.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

func TestCommitmentBillingCreate_PendingInlineOwnerUsesNestedGuard(t *testing.T) {
	f := newCommitmentBillingCreateFixture(t)
	ctx, err := prepareBillingSchedule(f.base.GetContext(), f.params.BillingScheduleProvisioner)
	require.NoError(t, err)
	req := f.request()
	f.fixtures.sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	req.InvoiceDelivery = lo.ToPtr(types.BillingTermSeparateInvoice)
	var created *dto.CommitmentResponse
	err = f.params.DB.WithTx(ctx, func(tx context.Context) error {
		var err error
		created, err = f.service.create(tx, req, commitmentCreateOptions{allowPendingSubscription: true})
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, string(types.BillingTermSeparateInvoice), *created.InstalmentInvoicing)
	require.Equal(t, 1, f.params.BillingTermsStore.(*lifecycleTestStore).tries)
	require.False(t, f.engine.state.Owner().CanCollect)
	require.Equal(t, "subscription_pending", f.engine.state.Owner().DeferredReason)
}
