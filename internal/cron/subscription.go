package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type SubscriptionCron struct {
	subscriptionRepo subscription.Repository
	logger           *logger.Logger
}

func NewSubscriptionCron(
	subscriptionRepo subscription.Repository,
	logger *logger.Logger,
) *SubscriptionCron {
	return &SubscriptionCron{
		subscriptionRepo: subscriptionRepo,
		logger:           logger,
	}
}

// UpdateBillingPeriods updates the current billing periods for all active subscriptions
// This should be run every 15 minutes to ensure billing periods are up to date
func (s *SubscriptionCron) UpdateBillingPeriods(ctx context.Context) error {
	const batchSize = 100
	now := time.Now().UTC()

	s.logger.Infow("starting billing period updates",
		"current_time", now)

	offset := 0
	for {
		filter := &types.SubscriptionFilter{
			Filter: types.Filter{
				Limit:  batchSize,
				Offset: offset,
			},
			SubscriptionStatus:     types.SubscriptionStatusActive,
			Status:                 types.StatusPublished,
			CurrentPeriodEndBefore: &now,
		}

		subs, err := s.subscriptionRepo.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("failed to list subscriptions: %w", err)
		}

		s.logger.Infow("processing subscription batch",
			"batch_size", len(subs),
			"offset", offset)

		if len(subs) == 0 {
			break // No more subscriptions to process
		}

		// Process each subscription in the batch
		for _, sub := range subs {
			if err := s.processSubscriptionPeriod(ctx, sub, now); err != nil {
				s.logger.Errorw("failed to process subscription period",
					"subscription_id", sub.ID,
					"error", err)
				// Continue processing other subscriptions
				continue
			}
		}

		offset += len(subs)
		if len(subs) < batchSize {
			break // No more subscriptions to fetch
		}
	}

	return nil
}

// processSubscriptionPeriod handles the period transitions for a single subscription
func (s *SubscriptionCron) processSubscriptionPeriod(ctx context.Context, sub *subscription.Subscription, now time.Time) error {
	originalStart := sub.CurrentPeriodStart
	originalEnd := sub.CurrentPeriodEnd

	currentStart := sub.CurrentPeriodStart
	currentEnd := sub.CurrentPeriodEnd
	var transitions []struct {
		start time.Time
		end   time.Time
	}

	// Calculate all transitions up to the next hour boundary
	// This ensures we have a stable window for processing regardless of when the cron runs
	for currentEnd.Before(now) {
		nextStart := currentEnd
		nextEnd, err := types.NextBillingDate(nextStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod)
		if err != nil {
			s.logger.Errorw("failed to calculate next billing date",
				"subscription_id", sub.ID,
				"current_start", currentStart,
				"current_end", currentEnd,
				"process_up_to", now,
				"error", err)
			return err
		}

		transitions = append(transitions, struct {
			start time.Time
			end   time.Time
		}{
			start: nextStart,
			end:   nextEnd,
		})

		currentStart = nextStart
		currentEnd = nextEnd
	}

	if len(transitions) == 0 {
		s.logger.Debugw("no transitions needed for subscription",
			"subscription_id", sub.ID,
			"current_period_start", sub.CurrentPeriodStart,
			"current_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now)
		return nil
	}

	// Update to the latest period
	lastTransition := transitions[len(transitions)-1]
	sub.CurrentPeriodStart = lastTransition.start
	sub.CurrentPeriodEnd = lastTransition.end

	// Handle subscription cancellation at period end
	if sub.CancelAtPeriodEnd && sub.CancelAt != nil {
		for _, t := range transitions {
			if !sub.CancelAt.After(t.end) {
				sub.SubscriptionStatus = types.SubscriptionStatusCancelled
				sub.CancelledAt = sub.CancelAt
				break
			}
		}
	}

	if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	s.logger.Infow("updated subscription billing period",
		"subscription_id", sub.ID,
		"previous_period_start", originalStart,
		"previous_period_end", originalEnd,
		"new_period_start", sub.CurrentPeriodStart,
		"new_period_end", sub.CurrentPeriodEnd,
		"process_up_to", now,
		"transitions_count", len(transitions))

	return nil
}
