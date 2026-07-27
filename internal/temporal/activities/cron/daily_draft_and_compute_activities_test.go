package cron

import (
	"context"
	"testing"
	"time"

	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/testsuite"
)

func TestDailyDraftAndComputeWorkflowID(t *testing.T) {
	t.Parallel()

	ref := time.Date(2026, 7, 27, 1, 59, 0, 0, time.UTC)

	id1 := dailyDraftAndComputeWorkflowID("sub_123", ref)
	id2 := dailyDraftAndComputeWorkflowID("sub_123", ref)
	require.Equal(t, id1, id2, "same subscription + same reference time must produce the same ID")
	require.Contains(t, id1, "sub_123")
	require.Contains(t, id1, "20260727")

	retryLater := ref.Add(3 * time.Hour) // still same UTC day
	idRetry := dailyDraftAndComputeWorkflowID("sub_123", retryLater)
	require.Equal(t, id1, idRetry, "later retry with the SAME reference time (not wall-clock time) must produce the same ID")

	nextDayRef := ref.AddDate(0, 0, 1)
	idNextDay := dailyDraftAndComputeWorkflowID("sub_123", nextDayRef)
	require.NotEqual(t, id1, idNextDay, "a genuinely different reference day must produce a different ID")

	idOtherSub := dailyDraftAndComputeWorkflowID("sub_456", ref)
	require.NotEqual(t, id1, idOtherSub, "different subscriptions must never collide")
}

// stubInvoiceService and stubSubscriptionService below implement only the two methods this
// activity actually calls; every other interface method panics if hit (nil embedded interface),
// which is intentional — it would mean the activity started depending on something it shouldn't.

type stubInvoiceService struct {
	service.InvoiceService
	subs []*subscription.Subscription
}

func (s *stubInvoiceService) ListSubscriptionsDueForDailyDraftCompute(ctx context.Context, onTenantEnvScanned func()) ([]*subscription.Subscription, error) {
	if onTenantEnvScanned != nil {
		onTenantEnvScanned()
	}
	return s.subs, nil
}

type stubSubscriptionService struct {
	interfaces.SubscriptionService
	results map[string]error // keyed by subscription ID; nil error = success
}

func (s *stubSubscriptionService) TriggerSubscriptionDraftAndComputeWorkflowWithOptions(
	ctx context.Context, subscriptionID string, opts interfaces.DraftAndComputeOptions,
) (*apidto.TriggerSubscriptionWorkflowResponse, error) {
	if err, ok := s.results[subscriptionID]; ok && err != nil {
		return nil, err
	}
	return &apidto.TriggerSubscriptionWorkflowResponse{WorkflowID: opts.WorkflowID}, nil
}

func TestDailyDraftAndComputeActivity_ClassifiesAlreadyStartedAsSkipped(t *testing.T) {
	baseModel := types.BaseModel{TenantID: "t1"}
	subs := []*subscription.Subscription{
		{ID: "sub_ok", EnvironmentID: "e1", BaseModel: baseModel},
		{ID: "sub_already_started", EnvironmentID: "e1", BaseModel: baseModel},
		{ID: "sub_real_failure", EnvironmentID: "e1", BaseModel: baseModel},
	}

	activities := NewDailyDraftAndComputeActivities(
		&stubInvoiceService{subs: subs},
		&stubSubscriptionService{results: map[string]error{
			"sub_already_started": &serviceerror.WorkflowExecutionAlreadyStarted{},
			"sub_real_failure":    assert.AnError,
		}},
		logger.NewNoopLogger(),
	)

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(activities.DailyDraftAndComputeActivity)
	encoded, err := env.ExecuteActivity(activities.DailyDraftAndComputeActivity, cronModels.DailyDraftAndComputeActivityInput{
		ReferenceTime: time.Now(),
	})
	require.NoError(t, err)

	var result cronModels.DailyDraftAndComputeWorkflowResult
	require.NoError(t, encoded.Get(&result))
	require.Equal(t, 3, result.TotalDueSubscriptions)
	require.Equal(t, 1, result.TriggeredCount)
	require.Equal(t, 1, result.SkippedCount, "WorkflowExecutionAlreadyStarted must count as skipped, not failed")
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, 1, result.TenantEnvsProcessed)
}
