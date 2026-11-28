package subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/testsuite"
)

// Global stub outlives tests; unarmed calls fail.
var errStubUnarmed = errors.New("temporal stub called without useStartErr")

var startErr = errStubUnarmed

type stubClient struct {
	temporalclient.TemporalClient
}

func (stubClient) StartWorkflow(context.Context, models.StartWorkflowOptions, interface{}, ...interface{}) (models.WorkflowRun, error) {
	return nil, startErr
}

func useStartErr(t *testing.T, err error) {
	temporalService.InitializeGlobalTemporalService(stubClient{}, nil, logger.NewNoopLogger(), nil, &config.TemporalConfig{})
	startErr = err
	t.Cleanup(func() { startErr = errStubUnarmed })
}

var errAlreadyStarted = serviceerror.NewWorkflowExecutionAlreadyStarted("running", "", "run_1")

func TestTriggerInvoiceWorkflowActivityAlreadyStarted(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantFailed int
	}{
		{"already started is skipped", errAlreadyStarted, 0},
		{"other error fails", errors.New("boom"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useStartErr(t, tt.err)
			a := &BillingActivities{logger: logger.NewNoopLogger()}

			out, err := a.TriggerInvoiceWorkflowActivity(context.Background(), invoiceModels.TriggerInvoiceWorkflowActivityInput{
				InvoiceIDs:    []string{"inv_1"},
				TenantID:      "tenant_1",
				EnvironmentID: "env_1",
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantFailed, out.FailedCount)
			require.Equal(t, 0, out.TriggeredCount)
		})
	}
}

type stubSubscriptionService struct {
	interfaces.SubscriptionService
}

func (stubSubscriptionService) GetSubscriptionsForBillingPeriodUpdate(_ context.Context, f *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error) {
	if f.QueryFilter.Offset != nil && *f.QueryFilter.Offset > 0 {
		return &dto.ListSubscriptionsResponse{}, nil
	}
	return &dto.ListSubscriptionsResponse{Items: []*dto.SubscriptionResponse{
		{Subscription: &subscription.Subscription{ID: "sub_1", EnvironmentID: "env_1", CurrentPeriodStart: time.Now(), CurrentPeriodEnd: time.Now().Add(time.Hour), BaseModel: types.BaseModel{TenantID: "tenant_1"}}},
	}}, nil
}

func TestScheduleBillingActivityAlreadyStarted(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantIDs []string
	}{
		{"already started is skipped", errAlreadyStarted, nil},
		{"other error is not counted", errors.New("boom"), nil},
		{"success is counted", nil, []string{"sub_1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useStartErr(t, tt.err)
			a := &SubscriptionActivities{subscriptionService: stubSubscriptionService{}}

			var ts testsuite.WorkflowTestSuite
			env := ts.NewTestActivityEnvironment()
			env.RegisterActivity(a.ScheduleBillingActivity)

			val, err := env.ExecuteActivity(a.ScheduleBillingActivity, subscriptionModels.ScheduleSubscriptionBillingWorkflowInput{BatchSize: 10})
			require.NoError(t, err)
			var res subscriptionModels.ScheduleSubscriptionBillingWorkflowResult
			require.NoError(t, val.Get(&res))
			require.ElementsMatch(t, tt.wantIDs, res.SubscriptionIDs)
		})
	}
}
