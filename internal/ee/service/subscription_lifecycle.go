package service

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	domainBillingTerms "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type subscriptionLifecycle struct {
	ctx      context.Context
	current  *subscription.Subscription
	children []*subscription.Subscription
}

func (s *subscriptionService) validateSubscriptionLifecycle(ctx context.Context, id string) error {
	if ctx == nil || (reflect.ValueOf(ctx).Kind() == reflect.Pointer && reflect.ValueOf(ctx).IsNil()) {
		return ierr.Validation("subscription lifecycle requires a context")
	}
	if strings.TrimSpace(id) == "" || len(id) > 50 || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return ierr.Validation("subscription lifecycle requires a subscription, tenant and environment")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ierr.NewError("subscription lifecycle is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	for _, dependency := range []any{s.DB, s.SubRepo, s.InvoiceRepo, s.BillingTermsStore} {
		value := reflect.ValueOf(dependency)
		if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
			return ierr.NewError("subscription lifecycle is unavailable").Mark(ierr.ErrServiceUnavailable)
		}
	}
	return nil
}

func (s *subscriptionService) lifecycleSubscription(ctx context.Context, id string) (*subscription.Subscription, error) {
	rows, err := s.SubRepo.List(types.WithForceWriter(ctx), &types.SubscriptionFilter{
		QueryFilter:        &types.QueryFilter{Limit: lo.ToPtr(2), Status: lo.ToPtr(types.StatusPublished)},
		SubscriptionIDs:    []string{id},
		SubscriptionStatus: types.AllSubscriptionStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ierr.NewError("subscription not found").Mark(ierr.ErrNotFound)
	}
	if len(rows) != 1 || !subscriptionUpdateScoped(ctx, rows[0], id) {
		return nil, ierr.NewError("subscription lifecycle returned invalid scope").Mark(ierr.ErrDatabase)
	}
	return rows[0], nil
}

func (s *subscriptionService) lockSubscriptionLifecycle(ctx context.Context, id string, nonblocking bool) (*subscriptionLifecycle, error) {
	discovered, err := s.lifecycleSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	participantIDs := []string{id}
	if parentID := lo.FromPtr(discovered.ParentSubscriptionID); parentID != "" {
		participantIDs = append(participantIDs, parentID)
	}
	var childIDs []string
	if discovered.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.lifecycleChildren(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			childIDs = append(childIDs, child.ID)
			participantIDs = append(participantIDs, child.ID)
		}
	}
	slices.Sort(participantIDs)
	participantIDs = slices.Compact(participantIDs)
	if len(participantIDs) > domainBillingTerms.MaxEligibilityScopes {
		return nil, ierr.Validation("subscription lifecycle supports at most 500 participants")
	}
	locked := make(map[string]*subscription.Subscription, len(participantIDs))
	scopes := make([]domainBillingTerms.EligibilityScope, 0, len(participantIDs))
	for _, participantID := range participantIDs {
		if strings.TrimSpace(participantID) == "" || len(participantID) > 50 {
			return nil, ierr.Validation("subscription lifecycle requires valid subscription IDs")
		}
	}
	for _, participantID := range participantIDs {
		participant, err := s.SubRepo.GetForUpdateNowait(ctx, participantID)
		if err != nil {
			return nil, err
		}
		if !subscriptionUpdateScoped(ctx, participant, participantID) {
			return nil, ierr.NewError("subscription lifecycle lock returned invalid scope").Mark(ierr.ErrDatabase)
		}
		locked[participantID] = participant
		scopes = append(scopes, domainBillingTerms.EligibilityScope{Type: "subscription", ID: participantID})
	}
	current := locked[id]
	if current.SubscriptionType != discovered.SubscriptionType || lo.FromPtr(current.ParentSubscriptionID) != lo.FromPtr(discovered.ParentSubscriptionID) {
		return nil, lifecycleMembershipChanged()
	}
	start := current.CurrentPeriodStart.Truncate(time.Minute)
	invoices, err := s.InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter:    &types.QueryFilter{Limit: lo.ToPtr(domainBillingTerms.MaxEligibilityScopes + 1), Status: lo.ToPtr(types.StatusPublished)},
		SubscriptionID: id,
		InvoiceStatus:  []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusSkipped},
		PeriodStartGTE: &start,
		SkipLineItems:  true,
	})
	if err != nil {
		return nil, err
	}
	if len(invoices) > domainBillingTerms.MaxEligibilityScopes {
		return nil, ierr.Validation("subscription lifecycle supports at most 500 open invoice candidates")
	}
	invoiceIDs := make([]string, 0, len(invoices))
	for _, inv := range invoices {
		if inv == nil || strings.TrimSpace(inv.ID) == "" || len(inv.ID) > 50 || inv.TenantID != types.GetTenantID(ctx) || inv.EnvironmentID != types.GetEnvironmentID(ctx) ||
			inv.Status != types.StatusPublished || inv.SubscriptionID == nil || *inv.SubscriptionID != id {
			return nil, ierr.NewError("subscription lifecycle invoice returned invalid scope").Mark(ierr.ErrDatabase)
		}
		invoiceIDs = append(invoiceIDs, inv.ID)
	}
	slices.Sort(invoiceIDs)
	invoiceIDs = slices.Compact(invoiceIDs)
	for _, invoiceID := range invoiceIDs {
		inv, err := s.InvoiceRepo.GetForUpdateNowait(ctx, invoiceID)
		if err != nil {
			return nil, err
		}
		if inv == nil || inv.ID != invoiceID || inv.TenantID != types.GetTenantID(ctx) || inv.EnvironmentID != types.GetEnvironmentID(ctx) || inv.SubscriptionID == nil || *inv.SubscriptionID != id {
			return nil, ierr.NewError("subscription lifecycle invoice lock returned invalid scope").Mark(ierr.ErrDatabase)
		}
	}
	if nonblocking {
		store, ok := s.BillingTermsStore.(domainBillingTerms.NonblockingEligibilityStore)
		if !ok {
			return nil, ierr.NewError("nested subscription lifecycle requires nonblocking eligibility locks").Mark(ierr.ErrServiceUnavailable)
		}
		if err := store.TryLockEligibility(ctx, scopes); err != nil {
			return nil, err
		}
	} else if err := s.BillingTermsStore.LockEligibility(ctx, scopes); err != nil {
		return nil, err
	}
	if current.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.lifecycleChildren(ctx, id)
		if err != nil {
			return nil, err
		}
		freshIDs := make([]string, 0, len(children))
		for _, child := range children {
			freshIDs = append(freshIDs, child.ID)
		}
		slices.Sort(childIDs)
		slices.Sort(freshIDs)
		if !slices.Equal(childIDs, freshIDs) {
			return nil, lifecycleMembershipChanged()
		}
	}
	admitted, err := types.WithInvoiceLockAdmission(ctx, invoiceIDs)
	if err != nil {
		return nil, err
	}
	children := make([]*subscription.Subscription, 0, len(childIDs))
	for _, childID := range childIDs {
		if locked[childID].SubscriptionType == types.SubscriptionTypeInherited {
			children = append(children, locked[childID])
		}
	}
	return &subscriptionLifecycle{ctx: admitted, current: current, children: children}, nil
}

func (s *subscriptionService) lifecycleChildren(ctx context.Context, parentID string) ([]*subscription.Subscription, error) {
	rows, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:           &types.QueryFilter{Limit: lo.ToPtr(domainBillingTerms.MaxEligibilityScopes), Status: lo.ToPtr(types.StatusPublished)},
		ParentSubscriptionIDs: []string{parentID},
		SubscriptionTypes:     []types.SubscriptionType{types.SubscriptionTypeInherited, types.SubscriptionTypeGroupedInvoicing},
		SubscriptionStatus:    types.AllSubscriptionStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(rows) >= domainBillingTerms.MaxEligibilityScopes {
		return nil, ierr.Validation("subscription lifecycle supports at most 500 participants")
	}
	seen := map[string]bool{parentID: true}
	for _, child := range rows {
		if child == nil || seen[child.ID] || strings.TrimSpace(child.ID) == "" || len(child.ID) > 50 || !subscriptionUpdateScoped(ctx, child, child.ID) ||
			lo.FromPtr(child.ParentSubscriptionID) != parentID || (child.SubscriptionType != types.SubscriptionTypeInherited && child.SubscriptionType != types.SubscriptionTypeGroupedInvoicing) {
			return nil, ierr.NewError("subscription lifecycle membership returned invalid scope").Mark(ierr.ErrDatabase)
		}
		seen[child.ID] = true
	}
	return rows, nil
}

func (s *subscriptionService) cascadeLifecycle(ctx context.Context, parent *subscription.Subscription, children []*subscription.Subscription, resume bool) error {
	for _, child := range children {
		if child.SubscriptionStatus != types.SubscriptionStatusActive && child.SubscriptionStatus != types.SubscriptionStatusTrialing &&
			child.SubscriptionStatus != types.SubscriptionStatusDraft && child.SubscriptionStatus != types.SubscriptionStatusPaused {
			continue
		}
		builder := subscription.NewSubscriptionBuilder(child).WithStatus(parent.SubscriptionStatus).WithPauseStatus(parent.PauseStatus).WithActivePauseID(nil)
		if resume {
			builder.WithCurrentPeriodStart(parent.CurrentPeriodStart).WithCurrentPeriodEnd(parent.CurrentPeriodEnd)
		}
		if err := s.SubRepo.Update(ctx, builder.Build()); err != nil {
			return err
		}
	}
	return nil
}

func lifecycleMembershipChanged() error {
	return ierr.NewError("subscription lifecycle participants changed; retry the request").Mark(ierr.ErrVersionConflict)
}

func (s *subscriptionService) cascadeCancellation(ctx context.Context, parent *subscription.Subscription, children []*subscription.Subscription) error {
	for _, child := range children {
		if child.SubscriptionStatus != types.SubscriptionStatusActive && child.SubscriptionStatus != types.SubscriptionStatusTrialing &&
			child.SubscriptionStatus != types.SubscriptionStatusDraft && child.SubscriptionStatus != types.SubscriptionStatusPaused {
			continue
		}
		updated := subscription.NewSubscriptionBuilder(child).WithStatus(parent.SubscriptionStatus).WithCancelledAt(parent.CancelledAt).
			WithCancelAt(parent.CancelAt).WithCancelAtPeriodEnd(parent.CancelAtPeriodEnd).WithEndDate(parent.EndDate).
			WithPauseStatus(parent.PauseStatus).WithActivePauseID(nil).Build()
		if err := s.SubRepo.Update(ctx, updated); err != nil {
			return err
		}
	}
	return nil
}
