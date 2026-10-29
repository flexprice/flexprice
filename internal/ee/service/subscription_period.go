package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionService) AdvanceBillingPeriod(ctx context.Context, id string, period dto.Period) ([]*subscription.Subscription, error) {
	if !commitmentTermsDate(period.Start) || !commitmentTermsDate(period.End) || period.End.Before(period.Start) {
		return nil, ierr.Validation("billing period requires finite ordered dates")
	}
	if err := s.validateSubscriptionLifecycle(ctx, id); err != nil {
		return nil, err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	var updated []*subscription.Subscription
	err := s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		lifecycle, err := s.lockSubscriptionLifecycle(tx, id, nested)
		if err != nil {
			return err
		}
		tx = lifecycle.ctx
		current := lifecycle.current
		if current.SubscriptionStatus == types.SubscriptionStatusCancelled || period.Start.Before(current.CurrentPeriodStart) || period.End.Before(current.CurrentPeriodEnd) {
			updated = []*subscription.Subscription{current}
			return nil
		}
		participants := []*subscription.Subscription{current}
		if current.SubscriptionType == types.SubscriptionTypeParent {
			children, err := s.lifecycleChildren(tx, id)
			if err != nil {
				return err
			}
			for _, child := range children {
				if child.SubscriptionType == types.SubscriptionTypeGroupedInvoicing && (child.SubscriptionStatus == types.SubscriptionStatusActive || child.SubscriptionStatus == types.SubscriptionStatusTrialing) {
					participants = append(participants, child)
				}
			}
		}
		for _, sub := range participants {
			if period.Start.Before(sub.CurrentPeriodStart) || period.End.Before(sub.CurrentPeriodEnd) {
				continue
			}
			next := subscription.NewSubscriptionBuilder(sub).WithCurrentPeriodStart(period.Start).WithCurrentPeriodEnd(period.End).Build()
			if err := s.SubRepo.Update(tx, next); err != nil {
				return err
			}
			updated = append(updated, next)
		}
		return nil
	})
	return updated, err
}

func (s *subscriptionService) processSubscriptionPeriod(ctx context.Context, sub *subscription.Subscription, now time.Time) error {
	if sub == nil {
		return ierr.Validation("subscription is required")
	}
	if err := s.validateSubscriptionLifecycle(ctx, sub.ID); err != nil {
		return err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	return s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		lifecycle, err := s.lockSubscriptionLifecycle(tx, sub.ID, nested)
		if err != nil {
			return err
		}
		return s.processLockedSubscriptionPeriod(lifecycle.ctx, lifecycle.current, now)
	})
}
