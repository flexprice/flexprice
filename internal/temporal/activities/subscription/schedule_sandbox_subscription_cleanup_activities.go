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

// BuildSandboxCleanupListActivity builds the sandboxCleanupList: lists tenants, builds tenantExpiryDays and sandbox env IDs,
// lists all active subscriptions in those envs, filters to past cleanup window, returns SubsToTerminate. No termination; workflow
// runs TerminateSandboxSubscriptionsBatchActivity over chunks of this list. Uses GetTenantConfig only (no hardcoded defaults).
func (s *SandboxSubscriptionCleanupActivities) BuildSandboxCleanupListActivity(ctx context.Context) (*subscriptionModels.BuildSandboxCleanupListResult, error) {
	logger := activity.GetLogger(ctx)
	result := &subscriptionModels.BuildSandboxCleanupListResult{SubsToTerminate: nil}

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
			s.params.Logger.Errorw("failed to list environments for tenant in sandbox cleanup", "tenant_id", t.ID, "error", err)
			continue
		}
		for _, env := range envs {
			sandboxEnvIDs = append(sandboxEnvIDs, env.ID)
		}

		tenantConfig, err := s.settingsService.GetTenantConfig(ctxTenant)
		if err != nil {
			s.params.Logger.Errorw("failed to get tenant config for sandbox cleanup", "tenant_id", t.ID, "error", err)
			continue
		}
		tenantExpiryDays[t.ID] = tenantConfig.SandboxSubscriptionExpiryDays
	}

	if len(sandboxEnvIDs) == 0 {
		logger.Info("No sandbox environments found")
		return result, nil
	}

	filter := types.NewNoLimitSubscriptionFilter()
	filter.EnvironmentIDs = sandboxEnvIDs
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}
	filter.Status = lo.ToPtr(types.StatusPublished)
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
		if expiryDays < 1 {
			continue
		}
		expiryAt := sub.StartDate.AddDate(0, 0, expiryDays)
		if !now.After(expiryAt) {
			continue
		}
		result.SubsToTerminate = append(result.SubsToTerminate, subscriptionModels.SubToTerminate{
			SubscriptionID: sub.ID,
			TenantID:       sub.TenantID,
			EnvironmentID:  sub.EnvironmentID,
			CreatedBy:      sub.CreatedBy,
		})
	}

	logger.Info("Sandbox cleanup list built", "subs_to_terminate_count", len(result.SubsToTerminate))
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
			Reason:           "Sandbox subscription auto-cancelled after expiry",
		})
		if err != nil {
			s.params.Logger.Errorw("failed to terminate sandbox subscription", "subscription_id", sub.SubscriptionID, "tenant_id", sub.TenantID, "environment_id", sub.EnvironmentID, "error", err)
			continue
		}
		terminated = append(terminated, sub.SubscriptionID)
	}

	logger.Info("Sandbox terminate batch completed", "batch_size", len(batch), "terminated_count", len(terminated))
	return terminated, nil
}
