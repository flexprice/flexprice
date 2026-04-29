package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	cronWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/cron"
	"github.com/flexprice/flexprice/internal/types"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
)

// AllTemporalScheduleConfigs returns the configuration for every Temporal server schedule
// (not HTTP-only cron entrypoints; keep in sync with types.AllTemporalServerScheduleIDs).
func AllTemporalScheduleConfigs() []types.ScheduleConfig {
	return []types.ScheduleConfig{
		{
			ID:        types.ScheduleIDCreditGrantProcessing,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.CreditGrantProcessingWorkflow,
			Input:     models.CreditGrantProcessingWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionAutoCancellation,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionAutoCancellationWorkflow,
			Input:     models.SubscriptionAutoCancellationWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDWalletCreditExpiry,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.WalletCreditExpiryWorkflow,
			Input:     models.WalletCreditExpiryWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionBillingPeriods,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionBillingPeriodsWorkflow,
			Input:     models.SubscriptionBillingPeriodsWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionRenewalAlerts,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionRenewalDueAlertsWorkflow,
			Input:     models.SubscriptionRenewalDueAlertsWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDSubscriptionTrialEndDue,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.SubscriptionTrialEndDueWorkflow,
			Input:     models.SubscriptionTrialEndDueWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
		{
			ID:        types.ScheduleIDOutboundWebhookStaleRetry,
			Interval:  15 * time.Minute,
			Workflow:  cronWorkflows.OutboundWebhookStaleRetryWorkflow,
			Input:     models.OutboundWebhookStaleRetryWorkflowInput{},
			TaskQueue: types.TemporalTaskQueueCron,
		},
	}
}

// EnsureSchedules idempotently creates or updates every configured Temporal server schedule.
// It returns the first error encountered; per-schedule outcomes are logged only.
func EnsureSchedules(ctx context.Context, tc client.TemporalClient, log *logger.Logger) error {
	for _, cfg := range AllTemporalScheduleConfigs() {
		if err := ensureOneSchedule(ctx, tc, cfg); err != nil {
			return err
		}
		log.Infow("schedule ensured", "id", cfg.ID)
	}
	return nil
}

func ensureOneSchedule(ctx context.Context, tc client.TemporalClient, cfg types.ScheduleConfig) error {
	id := string(cfg.ID)
	handle := tc.GetScheduleHandle(ctx, id)

	spec := sdkclient.ScheduleSpec{
		Intervals: []sdkclient.ScheduleIntervalSpec{
			{Every: cfg.Interval},
		},
	}

	_, err := handle.Describe(ctx)
	if err == nil {
		updateErr := handle.Update(ctx, sdkclient.ScheduleUpdateOptions{
			DoUpdate: func(in sdkclient.ScheduleUpdateInput) (*sdkclient.ScheduleUpdate, error) {
				in.Description.Schedule.Spec = &spec
				in.Description.Schedule.Action = &sdkclient.ScheduleWorkflowAction{
					Workflow:  cfg.Workflow,
					TaskQueue: cfg.TaskQueue.String(),
					Args:      []interface{}{cfg.Input},
				}
				in.Description.Schedule.Policy = &sdkclient.SchedulePolicies{
					Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
				}
				return &sdkclient.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
			},
		})
		if updateErr != nil {
			return fmt.Errorf("update temporal schedule %q: %w", id, updateErr)
		}
		return nil
	}

	var notFound *serviceerror.NotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("describe temporal schedule %q: %w", id, err)
	}

	_, createErr := tc.CreateSchedule(ctx, models.CreateScheduleOptions{
		ID:      id,
		Spec:    spec,
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		Action: &sdkclient.ScheduleWorkflowAction{
			Workflow:  cfg.Workflow,
			TaskQueue: cfg.TaskQueue.String(),
			Args:      []interface{}{cfg.Input},
		},
	})
	if createErr != nil {
		return fmt.Errorf("create temporal schedule %q: %w", id, createErr)
	}
	return nil
}
