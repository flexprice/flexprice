package service

import (
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"testing"
)

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
