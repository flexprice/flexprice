package recognition

import (
	"context"
	"testing"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	tmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeTemporal records started runs, refuses a second start of the same workflow ID, and describes runs as given.
type fakeTemporal struct {
	temporalservice.TemporalService
	started   map[string]tmodels.RevenueRecognitionInput
	described *workflowservice.DescribeWorkflowExecutionResponse
}

type fakeRun struct{ tmodels.WorkflowRun }

func (fakeRun) GetRunID() string { return "run_1" }

func (f *fakeTemporal) StartWorkflow(_ context.Context, options tmodels.StartWorkflowOptions, _ types.TemporalWorkflowType, args ...interface{}) (tmodels.WorkflowRun, error) {
	if _, ok := f.started[options.ID]; ok {
		return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
	}
	f.started[options.ID] = args[0].(tmodels.RevenueRecognitionInput)
	return fakeRun{}, nil
}

func (f *fakeTemporal) DescribeWorkflowExecution(context.Context, string, string) (interface{}, error) {
	if f.described == nil {
		return nil, serviceerror.NewNotFound("workflow not found")
	}
	return f.described, nil
}

func newRunFixture(t *testing.T, enabled bool) (*runService, *fakeTemporal) {
	ctx := testutil.SetupContext()
	contracts := testutil.NewInMemoryContractStore()
	require.NoError(t, contracts.Create(ctx, domainContract.NewBuilder(nil).WithID("contract_1").WithContractStatus(types.ContractStatusActive).
		WithStatus(types.StatusPublished).Build()))
	customers := testutil.NewInMemoryCustomerStore()
	require.NoError(t, customers.Create(ctx, &customer.Customer{ID: "cust_1", BaseModel: types.GetDefaultBaseModel(ctx)}))
	settingsRepo := testutil.NewInMemorySettingsStore()
	require.NoError(t, settingsRepo.Create(ctx, &settings.Setting{ID: "setting_revrec", Key: types.SettingKeyRevenueRecognitionConfig,
		Value: map[string]interface{}{"enabled": enabled}, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}))

	temporal := &fakeTemporal{started: map[string]tmodels.RevenueRecognitionInput{}}
	return NewRunService(temporal, contracts, customers, settingsRepo).(*runService), temporal
}

func TestStartRun(t *testing.T) {
	ctx := testutil.SetupContext()
	env := types.GetEnvironmentID(ctx)
	tests := []struct {
		name       string
		req        dto.StartRevenueRecognitionRunRequest
		disabled   bool
		wantID     string
		wantInput  tmodels.RevenueRecognitionInput
		wantErr    func(error) bool
		startTwice bool
	}{
		{name: "the environment", wantID: "revenue-recognition-run-" + env,
			wantInput: tmodels.RevenueRecognitionInput{TenantID: types.GetTenantID(ctx), EnvironmentID: env}},
		{name: "one contract", req: dto.StartRevenueRecognitionRunRequest{ContractID: "contract_1"}, wantID: "revenue-recognition-run-" + env + "-contract-contract_1",
			wantInput: tmodels.RevenueRecognitionInput{TenantID: types.GetTenantID(ctx), EnvironmentID: env, ContractID: "contract_1"}},
		{name: "one customer", req: dto.StartRevenueRecognitionRunRequest{CustomerID: "cust_1"}, wantID: "revenue-recognition-run-" + env + "-customer-cust_1",
			wantInput: tmodels.RevenueRecognitionInput{TenantID: types.GetTenantID(ctx), EnvironmentID: env, CustomerID: "cust_1"}},
		{name: "both a contract and a customer", req: dto.StartRevenueRecognitionRunRequest{ContractID: "contract_1", CustomerID: "cust_1"}, wantErr: ierr.IsValidation},
		{name: "revenue recognition off", disabled: true, wantErr: ierr.IsValidation},
		{name: "a contract of another environment", req: dto.StartRevenueRecognitionRunRequest{ContractID: "contract_other"}, wantErr: ierr.IsNotFound},
		{name: "a run already going for the scope", startTwice: true, wantErr: ierr.IsAlreadyExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, temporal := newRunFixture(t, !tt.disabled)
			if tt.startTwice {
				_, err := svc.StartRun(ctx, &tt.req)
				require.NoError(t, err)
			}

			resp, err := svc.StartRun(ctx, &tt.req)
			if tt.wantErr != nil {
				require.True(t, tt.wantErr(err), "got %v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantID, resp.WorkflowID)
			require.Equal(t, dto.RevenueRecognitionRunStatusQueued, resp.Status)
			require.Equal(t, tt.wantInput, temporal.started[tt.wantID])
		})
	}
}

func TestGetRun(t *testing.T) {
	ctx := testutil.SetupContext()
	own := "revenue-recognition-run-" + types.GetEnvironmentID(ctx) + "-contract-contract_1"
	describe := func(status enums.WorkflowExecutionStatus, activities ...*workflow.PendingActivityInfo) *workflowservice.DescribeWorkflowExecutionResponse {
		return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflow.WorkflowExecutionInfo{Status: status}, PendingActivities: activities}
	}
	tests := []struct {
		name       string
		workflowID string
		described  *workflowservice.DescribeWorkflowExecutionResponse
		want       dto.RevenueRecognitionRunStatus
		wantErr    func(error) bool
	}{
		{name: "running, its activity not yet picked up", workflowID: own, described: describe(enums.WORKFLOW_EXECUTION_STATUS_RUNNING), want: dto.RevenueRecognitionRunStatusQueued},
		{name: "running", workflowID: own, described: describe(enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			&workflow.PendingActivityInfo{State: enums.PENDING_ACTIVITY_STATE_STARTED}), want: dto.RevenueRecognitionRunStatusRunning},
		{name: "completed", workflowID: own, described: describe(enums.WORKFLOW_EXECUTION_STATUS_COMPLETED), want: dto.RevenueRecognitionRunStatusCompleted},
		{name: "failed", workflowID: own, described: describe(enums.WORKFLOW_EXECUTION_STATUS_FAILED), want: dto.RevenueRecognitionRunStatusFailed},
		{name: "another environment's run", workflowID: "revenue-recognition-run-env_other", described: describe(enums.WORKFLOW_EXECUTION_STATUS_COMPLETED), wantErr: ierr.IsNotFound},
		{name: "no such run", workflowID: own, wantErr: ierr.IsNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, temporal := newRunFixture(t, true)
			temporal.described = tt.described

			resp, err := svc.GetRun(ctx, tt.workflowID)
			if tt.wantErr != nil {
				require.True(t, tt.wantErr(err), "got %v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, resp.Status)
		})
	}
}
