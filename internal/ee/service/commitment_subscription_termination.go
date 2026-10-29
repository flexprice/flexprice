package service

import (
	"context"
	"slices"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *commitmentService) terminateSubscriptionCommitments(ctx context.Context, subscriptionID string, effectiveAt time.Time, termsOnly bool) error {
	if !commitmentTermsDate(effectiveAt) {
		return ierr.Validation("commitment termination requires a finite date")
	}
	adapter := &commitmentTermsOwner{subscriptions: s.SubRepo}
	parent, err := adapter.subscription(types.WithForceWriter(ctx), subscriptionID)
	if err != nil {
		return err
	}
	subscriptionIDs := []string{subscriptionID}
	if parent.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
			QueryFilter:           &types.QueryFilter{Limit: lo.ToPtr(domain.MaxEligibilityScopes), Status: lo.ToPtr(types.StatusPublished)},
			ParentSubscriptionIDs: []string{subscriptionID}, SubscriptionTypes: []types.SubscriptionType{types.SubscriptionTypeInherited}, SubscriptionStatus: types.AllSubscriptionStatuses(),
		})
		if err != nil {
			return err
		}
		if len(children) >= domain.MaxEligibilityScopes {
			return ierr.Validation("commitment cancellation supports at most 500 subscription participants")
		}
		for _, child := range children {
			if child == nil || !subscriptionUpdateScoped(ctx, child, child.ID) || child.SubscriptionType != types.SubscriptionTypeInherited || lo.FromPtr(child.ParentSubscriptionID) != subscriptionID {
				return ierr.Validation("invalid inherited commitment cancellation membership")
			}
			if child.SubscriptionStatus == types.SubscriptionStatusActive || child.SubscriptionStatus == types.SubscriptionStatusTrialing || child.SubscriptionStatus == types.SubscriptionStatusDraft || child.SubscriptionStatus == types.SubscriptionStatusPaused {
				subscriptionIDs = append(subscriptionIDs, child.ID)
			}
		}
	}
	slices.Sort(subscriptionIDs)
	subscriptionIDs = slices.Compact(subscriptionIDs)
	scopes := make([]domain.EligibilityScope, 0, len(subscriptionIDs))
	for _, id := range subscriptionIDs {
		scopes = append(scopes, domain.EligibilityScope{Type: "subscription", ID: id})
	}
	store, ok := s.BillingTermsStore.(domain.NonblockingEligibilityStore)
	if !ok {
		return ierr.NewError("commitment cancellation requires nonblocking eligibility guards").Mark(ierr.ErrServiceUnavailable)
	}
	if err := store.TryLockEligibility(ctx, scopes); err != nil {
		return err
	}
	for _, id := range subscriptionIDs {
		ctx, err = s.lockCollectionOwner(ctx, id)
		if err != nil {
			return err
		}
		if err := s.lockCommitmentKey(ctx, "commitments:"+id); err != nil {
			return err
		}
	}
	filter := &commitment.Filter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(501), Status: lo.ToPtr(types.StatusPublished)}, SubscriptionIDs: subscriptionIDs,
		Statuses: []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated, types.CommitmentStatusEnded}}
	listed, err := s.CommitmentRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	if len(listed) > 500 {
		return ierr.Validation("subscription cancellation supports at most 500 commitments")
	}
	slices.SortFunc(listed, func(a, b *commitment.Commitment) int {
		if a == nil || b == nil {
			return 0
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, c := range listed {
		if c == nil || !commitmentTermsScope(ctx, c.TenantID, c.EnvironmentID, c.Status) || !slices.Contains(subscriptionIDs, c.SubscriptionID) {
			return ierr.Validation("invalid subscription commitment cancellation scope")
		}
		if commitmentUsesTerms(c) {
			if err := s.lockCommitment(ctx, c.ID); err != nil {
				return err
			}
			out, _, err := s.settleCommitment(ctx, c.ID, effectiveAt, nil, true)
			if err != nil {
				return err
			}
			if c.Metadata[commitmentSettlementKey] != out.Metadata[commitmentSettlementKey] {
				s.publishSystemEvent(ctx, types.WebhookEventCommitmentTerminated, out)
			}
			continue
		}
		if termsOnly || c.CommitmentStatus == types.CommitmentStatusEnded {
			continue
		}
		if c.CommitmentStatus == types.CommitmentStatusTerminated {
			if c.TerminatedAt == nil || !c.TerminatedAt.After(effectiveAt) {
				continue
			}
			if err := s.lockCommitment(ctx, c.ID); err != nil {
				return err
			}
			latest, err := s.CommitmentRepo.Get(ctx, c.ID)
			if err != nil {
				return err
			}
			if _, err := s.CommitmentRepo.Update(ctx, commitment.NewCommitmentBuilder(latest).WithTerminatedAt(effectiveAt).Build()); err != nil {
				return err
			}
			continue
		}
		if !effectiveAt.Before(c.EndDate) {
			continue
		}
		out, _, err := s.terminate(ctx, c.ID, effectiveAt, nil)
		if err != nil {
			return err
		}
		s.publishSystemEvent(ctx, types.WebhookEventCommitmentTerminated, out)
	}
	return nil
}

func (s *subscriptionService) settleTerminalBillingTerms(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil || sub.SubscriptionStatus != types.SubscriptionStatusCancelled {
		return nil
	}
	effectiveAt := sub.CancelledAt
	if effectiveAt == nil {
		effectiveAt = sub.EndDate
	}
	if effectiveAt == nil || !commitmentTermsDate(*effectiveAt) {
		return ierr.Validation("terminal billing collection requires its effective date")
	}
	return newCommitmentService(s.ServiceParams).terminateSubscriptionCommitments(ctx, sub.ID, *effectiveAt, true)
}
