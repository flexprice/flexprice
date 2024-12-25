package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscription"
	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionRepository struct {
	client *postgres.Client
	logger *logger.Logger
}

func NewSubscriptionRepository(client *postgres.Client, logger *logger.Logger) domainSub.Repository {
	return &subscriptionRepository{
		client: client,
		logger: logger,
	}
}

func (r *subscriptionRepository) Create(ctx context.Context, sub *domainSub.Subscription) error {
	client := r.client.Querier(ctx)
	subscription, err := client.Subscription.Create().
		SetID(sub.ID).
		SetTenantID(sub.TenantID).
		SetLookupKey(sub.LookupKey).
		SetCustomerID(sub.CustomerID).
		SetPlanID(sub.PlanID).
		SetSubscriptionStatus(string(sub.SubscriptionStatus)).
		SetCurrency(sub.Currency).
		SetBillingAnchor(sub.BillingAnchor).
		SetStartDate(sub.StartDate).
		SetNillableEndDate(sub.EndDate).
		SetCurrentPeriodStart(sub.CurrentPeriodStart).
		SetCurrentPeriodEnd(sub.CurrentPeriodEnd).
		SetNillableCancelledAt(sub.CancelledAt).
		SetNillableCancelAt(sub.CancelAt).
		SetCancelAtPeriodEnd(sub.CancelAtPeriodEnd).
		SetNillableTrialStart(sub.TrialStart).
		SetNillableTrialEnd(sub.TrialEnd).
		SetInvoiceCadence(string(sub.InvoiceCadence)).
		SetBillingCadence(string(sub.BillingCadence)).
		SetBillingPeriod(string(sub.BillingPeriod)).
		SetBillingPeriodCount(sub.BillingPeriodCount).
		SetStatus(string(sub.Status)).
		SetCreatedBy(sub.CreatedBy).
		SetUpdatedBy(sub.UpdatedBy).
		SetVersion(1).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Update the input subscription with created data
	*sub = *toDomainSubscription(subscription)
	return nil
}

func (r *subscriptionRepository) Get(ctx context.Context, id string) (*domainSub.Subscription, error) {
	client := r.client.Querier(ctx)
	sub, err := client.Subscription.Query().
		Where(
			subscription.ID(id),
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, domainSub.NewNotFoundError(id)
		}
		return nil, errors.WithOp(err, "repository.subscription.Get")
	}

	return toDomainSubscription(sub), nil
}

func (r *subscriptionRepository) Update(ctx context.Context, sub *domainSub.Subscription) error {
	client := r.client.Querier(ctx)
	now := time.Now().UTC()

	_, err := client.Subscription.UpdateOneID(sub.ID).
		Where(
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
			subscription.Version(sub.Version), // Add optimistic locking
		).
		SetLookupKey(sub.LookupKey).
		SetSubscriptionStatus(string(sub.SubscriptionStatus)).
		SetCurrentPeriodStart(sub.CurrentPeriodStart).
		SetCurrentPeriodEnd(sub.CurrentPeriodEnd).
		SetNillableCancelledAt(sub.CancelledAt).
		SetNillableCancelAt(sub.CancelAt).
		SetCancelAtPeriodEnd(sub.CancelAtPeriodEnd).
		SetUpdatedAt(now).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetVersion(sub.Version + 1). // Increment version for optimistic locking
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return domainSub.NewNotFoundError(sub.ID)
		}
		if ent.IsConstraintError(err) {
			return domainSub.NewVersionConflictError(sub.ID, sub.Version, sub.Version+1)
		}
		return errors.WithOp(err, "repository.subscription.Update")
	}

	return nil
}

func (r *subscriptionRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)
	err := client.Subscription.UpdateOneID(id).
		Where(
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Exec(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return domainSub.NewNotFoundError(id)
		}
		return errors.WithOp(err, "repository.subscription.Delete")
	}

	return nil
}

func (r *subscriptionRepository) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*domainSub.Subscription, error) {
	client := r.client.Querier(ctx)
	query := client.Subscription.Query().
		Where(
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		)

	if filter.CustomerID != "" {
		query = query.Where(subscription.CustomerID(filter.CustomerID))
	}

	if filter.SubscriptionStatus != "" {
		query = query.Where(subscription.SubscriptionStatus(string(filter.SubscriptionStatus)))
	}

	if filter.PlanID != "" {
		query = query.Where(subscription.PlanID(filter.PlanID))
	}

	if filter.CurrentPeriodEndBefore != nil {
		query = query.Where(subscription.CurrentPeriodEndLTE(*filter.CurrentPeriodEndBefore))
	}

	// Apply pagination
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	// Execute query
	subs, err := query.All(ctx)
	if err != nil {
		return nil, errors.WithOp(err, "repository.subscription.List")
	}

	// Convert to domain subscriptions
	result := make([]*domainSub.Subscription, len(subs))
	for i, sub := range subs {
		result[i] = toDomainSubscription(sub)
	}

	return result, nil
}

func toDomainSubscription(sub *ent.Subscription) *domainSub.Subscription {
	return &domainSub.Subscription{
		ID:                 sub.ID,
		LookupKey:          sub.LookupKey,
		CustomerID:         sub.CustomerID,
		PlanID:             sub.PlanID,
		SubscriptionStatus: types.SubscriptionStatus(sub.SubscriptionStatus),
		Currency:           sub.Currency,
		BillingAnchor:      sub.BillingAnchor,
		StartDate:          sub.StartDate,
		EndDate:            sub.EndDate,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd,
		CancelledAt:        sub.CancelledAt,
		CancelAt:           sub.CancelAt,
		CancelAtPeriodEnd:  sub.CancelAtPeriodEnd,
		TrialStart:         sub.TrialStart,
		TrialEnd:           sub.TrialEnd,
		InvoiceCadence:     types.InvoiceCadence(sub.InvoiceCadence),
		BillingCadence:     types.BillingCadence(sub.BillingCadence),
		BillingPeriod:      types.BillingPeriod(sub.BillingPeriod),
		BillingPeriodCount: sub.BillingPeriodCount,
		Version:            sub.Version,
		BaseModel: types.BaseModel{
			TenantID:  sub.TenantID,
			Status:    types.Status(sub.Status),
			CreatedAt: sub.CreatedAt,
			CreatedBy: sub.CreatedBy,
			UpdatedAt: sub.UpdatedAt,
			UpdatedBy: sub.UpdatedBy,
		},
	}
}
