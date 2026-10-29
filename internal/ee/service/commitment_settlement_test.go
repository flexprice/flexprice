package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type commitmentSettlementStore struct{ lifecycleTestStore }

func (s *commitmentSettlementStore) LockOwner(context.Context, domain.OwnerRef) error { return nil }

type commitmentSettlementEngine struct {
	core.Service
	adapter   domain.OwnerAdapter
	assigned  decimal.Decimal
	decisions int
}

func (s *commitmentSettlementEngine) WithSettlement(ctx context.Context, request core.SettlementRequest, apply func(context.Context, core.State) (bool, error)) (core.State, error) {
	owner, err := s.adapter.Snapshot(ctx, request.Owner)
	if err != nil {
		return nil, err
	}
	current := &billingAPIState{owner: owner, summary: domain.CollectionSummary{AssignedAmount: s.assigned}}
	changed, err := apply(ctx, current)
	if err != nil {
		return nil, err
	}
	if changed {
		s.decisions++
	}
	owner, err = s.adapter.Snapshot(ctx, request.Owner)
	if err != nil {
		return nil, err
	}
	return &billingAPIState{owner: owner, summary: domain.CollectionSummary{AssignedAmount: s.assigned, TargetAmount: owner.CollectionTarget}}, nil
}

func newCommitmentSettlementFixture(t *testing.T) (*commitmentTermsFixture, *commitmentService, *commitmentSettlementEngine) {
	t.Helper()
	f := newCommitmentTermsFixture(t)
	f.commitment.StartDate = f.sub.StartDate
	f.commitment.EndDate = *f.sub.EndDate
	f.sub.SubscriptionType = types.SubscriptionTypeStandalone
	base := &testutil.BaseServiceTestSuite{}
	base.SetT(t)
	base.SetupSuite()
	base.SetupTest()
	t.Cleanup(base.TearDownTest)
	params := fullServiceParams(base)
	params.SubRepo, params.CommitmentRepo = f.subs, f.commitments
	params.BillingTermsStore = &commitmentSettlementStore{}
	engine := &commitmentSettlementEngine{adapter: f.adapter}
	params.BillingTerms = engine
	return f, newCommitmentService(params), engine
}

func TestCommitmentSettlementFreezesPrincipal(t *testing.T) {
	for _, assigned := range []string{"0", "250", "500"} {
		t.Run(assigned, func(t *testing.T) {
			f, s, engine := newCommitmentSettlementFixture(t)
			engine.assigned = decimal.RequireFromString(assigned)
			at := f.sub.StartDate.AddDate(0, 3, 0)
			var out *commitment.Commitment
			require.NoError(t, s.DB.WithTx(f.ctx, func(tx context.Context) error {
				var err error
				out, _, err = s.terminate(tx, f.commitment.ID, at, nil)
				return err
			}))
			decision, err := readCommitmentSettlement(out)
			require.NoError(t, err)
			require.Equal(t, "300", decision.Accrued.String())
			require.True(t, decision.Target.Equal(decimal.Max(decimal.NewFromInt(300), engine.assigned)))
			require.Equal(t, types.BillingTermSubscriptionInvoice, decision.Delivery)
			require.Equal(t, "1200", out.TotalAmount.String())
			require.Equal(t, 1, engine.decisions)
			engine.assigned = decimal.Zero
			require.NoError(t, s.DB.WithTx(f.ctx, func(tx context.Context) error { _, _, err := s.terminate(tx, f.commitment.ID, at, nil); return err }))
			require.Equal(t, 1, engine.decisions, "voided invoices must not silently change a frozen target")
			require.NoError(t, s.DB.WithTx(f.ctx, func(tx context.Context) error {
				_, _, err := s.settleCommitment(tx, f.commitment.ID, at.AddDate(0, -1, 0), nil, true)
				return err
			}))
			revised, err := s.CommitmentRepo.Get(f.ctx, out.ID)
			require.NoError(t, err)
			decision, err = readCommitmentSettlement(revised)
			require.NoError(t, err)
			require.Equal(t, "200", decision.Target.String())
			require.Equal(t, types.BillingTermSeparateInvoice, decision.Delivery)
			require.Equal(t, 2, engine.decisions)
		})
	}
}

func TestCommitmentSettlementBeforeStartAndConflictingRetry(t *testing.T) {
	f, s, engine := newCommitmentSettlementFixture(t)
	at := f.commitment.StartDate.Add(-time.Hour)
	require.NoError(t, s.DB.WithTx(f.ctx, func(tx context.Context) error { _, _, err := s.terminate(tx, f.commitment.ID, at, nil); return err }))
	c, err := s.CommitmentRepo.Get(f.ctx, f.commitment.ID)
	require.NoError(t, err)
	decision, err := readCommitmentSettlement(c)
	require.NoError(t, err)
	require.True(t, decision.Target.IsZero())
	err = s.DB.WithTx(f.ctx, func(tx context.Context) error { _, _, err := s.terminate(tx, c.ID, at.Add(time.Hour), nil); return err })
	require.True(t, ierr.IsVersionConflict(err), "%v", err)
	require.Equal(t, 1, engine.decisions)
}

func TestCommitmentSettlementMetadataProtection(t *testing.T) {
	f, s, _ := newCommitmentSettlementFixture(t)
	at := f.sub.StartDate.AddDate(0, 1, 0)
	require.NoError(t, s.DB.WithTx(f.ctx, func(tx context.Context) error { _, _, err := s.terminate(tx, f.commitment.ID, at, nil); return err }))
	c, err := s.CommitmentRepo.Get(f.ctx, f.commitment.ID)
	require.NoError(t, err)
	protected := c.Metadata[commitmentSettlementKey]
	_, err = s.Update(f.ctx, c.ID, dto.UpdateCommitmentRequest{Metadata: types.Metadata{"user": "label"}})
	require.NoError(t, err)
	c, err = s.CommitmentRepo.Get(f.ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, protected, c.Metadata[commitmentSettlementKey])
	require.Equal(t, "label", c.Metadata["user"])
	_, err = s.Update(f.ctx, c.ID, dto.UpdateCommitmentRequest{Metadata: types.Metadata{commitmentSettlementKey: protected}})
	require.True(t, ierr.IsValidation(err))
	for _, raw := range []string{"{}", protected + " {}", strings.Repeat("x", 4097), strings.Replace(protected, `"target_amount":"100"`, `"target_amount":"1e2147483647"`, 1)} {
		c.Metadata[commitmentSettlementKey] = raw
		_, err := readCommitmentSettlement(c)
		require.Error(t, err)
	}
}

func TestCommitmentTermsOwnerGroupedDestination(t *testing.T) {
	f := newCommitmentTermsFixture(t)
	parent := subscription.NewSubscriptionBuilder(f.sub).WithSubscriptionType(types.SubscriptionTypeParent).Build()
	parent.ID = "parent_terms"
	parent.CustomerID = "parent_customer"
	parent.InvoicingCustomerID = lo.ToPtr("parent_recipient")
	require.NoError(t, f.subs.Create(f.ctx, parent))
	f.sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
	f.sub.ParentSubscriptionID = &parent.ID
	owner, err := f.adapter.Snapshot(f.ctx, f.owner)
	require.NoError(t, err)
	require.Equal(t, "parent_recipient", owner.CustomerID)
	require.Equal(t, f.sub.ID, *owner.SubscriptionID)
	require.Equal(t, []domain.EligibilityScope{{Type: "subscription", ID: parent.ID}, {Type: "subscription", ID: f.sub.ID}}, owner.EligibilityScopes)
	require.Equal(t, f.sub.BillingAnchor, owner.CalendarAnchor)
	parent.SubscriptionStatus = types.SubscriptionStatusPaused
	owner, err = f.adapter.Snapshot(f.ctx, f.owner)
	require.NoError(t, err)
	require.Equal(t, "subscription_paused", owner.DeferredReason)
	parent.SubscriptionStatus = types.SubscriptionStatusCancelled
	owner, err = f.adapter.Snapshot(f.ctx, f.owner)
	require.NoError(t, err)
	require.False(t, owner.CanCollect)
	require.Equal(t, "subscription_destination_unavailable", owner.DeferredReason)
	require.Equal(t, types.CommitmentStatusActive, f.commitment.CommitmentStatus)
}

func TestCommitmentTermsOwnerTerminalReadOnly(t *testing.T) {
	for _, status := range []types.CommitmentStatus{types.CommitmentStatusTerminated, types.CommitmentStatusActive} {
		f := newCommitmentTermsFixture(t)
		if status == types.CommitmentStatusTerminated {
			f.commitment.CommitmentStatus = status
			f.commitment.TerminatedAt = lo.ToPtr(f.commitment.StartDate)
		} else {
			f.sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		}
		owner, err := f.adapter.Snapshot(f.ctx, f.owner)
		require.NoError(t, err)
		require.False(t, owner.CanEdit)
		require.False(t, owner.CanCollect)
		require.Equal(t, "settlement_required", owner.DeferredReason)
	}
}

func TestCommitmentSettlementCancellationIncludesInheritedOnly(t *testing.T) {
	f, s, engine := newCommitmentSettlementFixture(t)
	f.sub.SubscriptionType = types.SubscriptionTypeParent
	for _, kind := range []types.SubscriptionType{types.SubscriptionTypeInherited, types.SubscriptionTypeGroupedInvoicing} {
		child := subscription.NewSubscriptionBuilder(f.sub).WithSubscriptionType(kind).WithParentSubscriptionID(&f.sub.ID).Build()
		child.ID = string(kind)
		require.NoError(t, f.subs.Create(f.ctx, child))
		owner := commitment.NewCommitmentBuilder(f.commitment).Build()
		owner.ID = "owner_" + child.ID
		owner.SubscriptionID = child.ID
		_, err := f.commitments.Create(f.ctx, owner)
		require.NoError(t, err)
	}
	at := f.sub.StartDate.AddDate(0, 2, 0)
	require.NoError(t, s.TerminateForSubscription(f.ctx, f.sub.ID, at))
	require.Equal(t, 2, engine.decisions)
	inherited, err := f.commitments.Get(f.ctx, "owner_"+string(types.SubscriptionTypeInherited))
	require.NoError(t, err)
	grouped, err := f.commitments.Get(f.ctx, "owner_"+string(types.SubscriptionTypeGroupedInvoicing))
	require.NoError(t, err)
	require.Equal(t, types.CommitmentStatusTerminated, inherited.CommitmentStatus)
	require.Equal(t, types.CommitmentStatusActive, grouped.CommitmentStatus)
}

func TestCommitmentSettlementCancellationChangesOnlyDelivery(t *testing.T) {
	f, s, engine := newCommitmentSettlementFixture(t)
	engine.assigned = decimal.NewFromInt(500)
	at := f.sub.StartDate.AddDate(0, 2, 0)
	require.NoError(t, s.DB.WithTx(f.ctx, func(tx context.Context) error { _, _, err := s.terminate(tx, f.commitment.ID, at, nil); return err }))
	engine.assigned = decimal.Zero
	require.NoError(t, s.TerminateForSubscription(f.ctx, f.sub.ID, at.Add(time.Hour)))
	c, err := f.commitments.Get(f.ctx, f.commitment.ID)
	require.NoError(t, err)
	decision, err := readCommitmentSettlement(c)
	require.NoError(t, err)
	require.Equal(t, "500", decision.Target.String())
	require.Equal(t, at, decision.EffectiveAt)
	require.Equal(t, types.BillingTermSeparateInvoice, decision.Delivery)
	require.Equal(t, 2, engine.decisions)
	require.NoError(t, s.TerminateForSubscription(f.ctx, f.sub.ID, at.Add(2*time.Hour)))
	require.Equal(t, 2, engine.decisions)
}
