package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/client"
	models "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	workflows "github.com/flexprice/flexprice/internal/temporal/workflows/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	enumspb "go.temporal.io/api/enums/v1"
)

// EnsureBillingSchedule provisions due delivery for one known scope before accepting billing terms.
// Call before opening the owner transaction; an empty scoped schedule is harmless after rollback.
func EnsureBillingSchedule(ctx context.Context, tc client.TemporalClient) error {
	if ctx == nil || strings.TrimSpace(types.GetTenantID(ctx)) == "" || strings.TrimSpace(types.GetEnvironmentID(ctx)) == "" || len(types.GetTenantID(ctx)) > 50 || len(types.GetEnvironmentID(ctx)) > 50 {
		return ierr.Validation("billing schedule provisioning requires tenant and environment scope")
	}
	if ctx.Value(types.CtxDBTransaction) != nil {
		return ierr.Validation("billing schedule provisioning must precede the owner transaction")
	}
	if tc == nil || tc.GetRawClient() == nil {
		return ierr.NewError("billing schedule delivery is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, queue := range []types.TemporalTaskQueue{types.TemporalTaskQueueCron, types.TemporalTaskQueueInvoice} {
		for _, kind := range []enumspb.TaskQueueType{enumspb.TASK_QUEUE_TYPE_WORKFLOW, enumspb.TASK_QUEUE_TYPE_ACTIVITY} {
			status, err := tc.GetRawClient().DescribeTaskQueue(ctx, queue.String(), kind)
			if err != nil {
				return ierr.WithError(err).WithHint("Billing schedule workers are unavailable").Mark(ierr.ErrServiceUnavailable)
			}
			if status == nil || len(status.Pollers) == 0 {
				return ierr.NewError("billing schedule workers are not polling").WithReportableDetails(map[string]any{"queue": queue.String()}).Mark(ierr.ErrServiceUnavailable)
			}
		}
	}
	input := models.DueInput{TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), BatchSize: 100}
	hash := sha256.Sum256([]byte(input.TenantID + "\x00" + input.EnvironmentID))
	id := types.ScheduleID("billing-schedule-due-" + hex.EncodeToString(hash[:]))
	if err := ensureOneSchedule(ctx, tc, types.ScheduleConfig{ID: id, Interval: 2 * time.Minute, Workflow: workflows.ProcessBillingScheduleItemsDueWorkflow, Input: input, TaskQueue: types.TemporalTaskQueueCron}); err != nil {
		return ierr.WithError(err).WithHint("Billing schedule provisioning failed").Mark(ierr.ErrServiceUnavailable)
	}
	description, err := tc.GetScheduleHandle(ctx, string(id)).Describe(ctx)
	if err != nil {
		return ierr.WithError(err).WithHint("Billing schedule readiness check failed").Mark(ierr.ErrServiceUnavailable)
	}
	if description == nil || description.Schedule.State != nil && description.Schedule.State.Paused {
		return ierr.NewError("billing schedule delivery is paused").Mark(ierr.ErrServiceUnavailable)
	}
	return nil
}
