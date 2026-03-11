package subscription

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
)

const (
	WorkflowBuildSandboxCleanupList            = "BuildSandboxCleanupListActivity"
	WorkflowTerminateSandboxSubscriptionsBatch = "TerminateSandboxSubscriptionsBatchActivity"
)

// SandboxSubscriptionCleanupActivities holds activities for sandbox subscription cleanup: build cleanup list and terminate batches.
// When registered with Temporal, methods are invoked as SandboxSubscriptionCleanupActivities.BuildSandboxCleanupListActivity etc.
type SandboxSubscriptionCleanupActivities struct {
	subscriptionService service.SubscriptionService
	settingsService     service.SettingsService
	params              service.ServiceParams
}

// NewSandboxSubscriptionCleanupActivities creates a new SandboxSubscriptionCleanupActivities instance.
func NewSandboxSubscriptionCleanupActivities(subscriptionService service.SubscriptionService, settingsService service.SettingsService, params service.ServiceParams) *SandboxSubscriptionCleanupActivities {
	return &SandboxSubscriptionCleanupActivities{
		subscriptionService: subscriptionService,
		settingsService:     settingsService,
		params:              params,
	}
}

// BuildSandboxCleanupListActivity fetches one page of active sandbox subs (offset/limit), filters to past cleanup window,
// returns that page of SubsToTerminate and HasMore. Workflow calls this in a loop then runs TerminateBatch per page.
func (s *SandboxSubscriptionCleanupActivities) BuildSandboxCleanupListActivity(ctx context.Context, input subscriptionModels.BuildSandboxCleanupListInput) (*subscriptionModels.BuildSandboxCleanupListPageResult, error) {
	logger := activity.GetLogger(ctx)
	result := &subscriptionModels.BuildSandboxCleanupListPageResult{Items: nil, HasMore: false}

	tenants, err := s.params.TenantRepo.List(ctx)
	if err != nil {
		s.params.Logger.Errorw("failed to list tenants for sandbox cleanup", "error", err)
		return result, ierr.WithError(err).
			WithHint("Failed to list tenants").
			Mark(ierr.ErrDatabase)
	}
	var sandboxEnvIDs []string
	tenantExpiryDays := make(map[string]int)

	for _, t := range tenants {
		ctxTenant := context.WithValue(ctx, types.CtxTenantID, t.ID)
		envs, err := s.params.EnvironmentRepo.List(ctxTenant, types.Filter{
			Type:   types.EnvironmentDevelopment,
			Status: types.StatusPublished,
		})
		if err != nil {
			s.params.Logger.Errorw("sandbox cleanup: fetching environments for tenant failed, skipping tenant", "tenant_id", t.ID, "error", err)
			continue
		}
		for _, env := range envs {
			sandboxEnvIDs = append(sandboxEnvIDs, env.ID)
		}

		tenantConfig, err := s.settingsService.GetTenantConfig(ctxTenant)
		if err != nil {
			s.params.Logger.Errorw("sandbox cleanup: fetching tenant config failed, skipping tenant", "tenant_id", t.ID, "error", err)
			continue
		}
		tenantExpiryDays[t.ID] = tenantConfig.SandboxSubscriptionExpiryDays
	}

	if len(sandboxEnvIDs) == 0 {
		logger.Info("No sandbox environments found")
		return result, nil
	}

	pageSize := input.Limit
	if pageSize <= 0 {
		pageSize = 500
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	filter := &types.SubscriptionFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(pageSize),
			Offset: lo.ToPtr(offset),
			Sort:   lo.ToPtr("created_at"),
			Order:  lo.ToPtr("asc"),
			Status: lo.ToPtr(types.StatusPublished),
		},
		EnvironmentIDs:     sandboxEnvIDs,
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
	}
	subs, err := s.subscriptionService.ListAllTenantSubscriptions(ctx, filter)
	if err != nil {
		s.params.Logger.Errorw("failed to list subscriptions in dev envs for sandbox cleanup", "error", err)
		return result, ierr.WithError(err).
			WithHint("Failed to list subscriptions in development environments").
			Mark(ierr.ErrDatabase)
	}

	now := time.Now().UTC()
	for _, item := range subs.Items {
		sub := item.Subscription
		if sub == nil {
			continue
		}
		expiryDays := tenantExpiryDays[sub.TenantID]
		expiryAt := sub.StartDate.AddDate(0, 0, expiryDays)
		if !now.After(expiryAt) {
			continue
		}
		result.Items = append(result.Items, subscriptionModels.SubToTerminate{
			SubscriptionID: sub.ID,
			TenantID:       sub.TenantID,
			EnvironmentID:  sub.EnvironmentID,
			CreatedBy:      sub.CreatedBy,
		})
	}
	// HasMore = we got a full page from the DB, so there may be another page of raw subs to scan.
	// We paginate the source list (all active sandbox subs); after filtering by expiry, result.Items may be smaller.
	result.HasMore = len(subs.Items) == pageSize

	logger.Info("Sandbox cleanup page fetched", "offset", offset, "limit", pageSize, "items_count", len(result.Items), "has_more", result.HasMore)
	return result, nil
}

// TerminateSandboxSubscriptionsBatchActivity terminates a batch of subscriptions (CancelSubscription per item).
// Workflow calls this in a loop over chunks of sandboxCleanupList so each batch has its own timeout and retry.
func (s *SandboxSubscriptionCleanupActivities) TerminateSandboxSubscriptionsBatchActivity(ctx context.Context, batch []subscriptionModels.SubToTerminate) ([]string, error) {
	logger := activity.GetLogger(ctx)
	var terminated []string

	for _, sub := range batch {
		subCtx := context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
		subCtx = context.WithValue(subCtx, types.CtxEnvironmentID, sub.EnvironmentID)
		subCtx = context.WithValue(subCtx, types.CtxUserID, sub.CreatedBy)
		_, err := s.subscriptionService.CancelSubscription(subCtx, sub.SubscriptionID, &dto.CancelSubscriptionRequest{
			CancellationType: types.CancellationTypeSandboxSubscriptionCleanup,
			Reason:           "Sandbox subscription auto-cancelled after cleanup window",
		})
		if err != nil {
			s.params.Logger.Errorw("failed to terminate sandbox subscription", "subscription_id", sub.SubscriptionID, "tenant_id", sub.TenantID, "environment_id", sub.EnvironmentID, "error", err)
			return nil, ierr.WithError(err).
				WithHint("CancelSubscription failed; failing activity so Temporal can retry the batch").
				Mark(ierr.ErrDatabase)
		}
		terminated = append(terminated, sub.SubscriptionID)
	}

	logger.Info("Sandbox terminate batch completed", "batch_size", len(batch), "terminated_count", len(terminated))
	return terminated, nil
}
