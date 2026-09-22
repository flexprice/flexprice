//go:build ee

package alerts

import (
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestUsageAlertWorkflow_ValidatesInput(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(UsageAlertWorkflow, models.UsageAlertWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestUsageAlertWorkflowInput_Validate(t *testing.T) {
	cases := []struct {
		name    string
		input   models.UsageAlertWorkflowInput
		wantErr bool
	}{
		{"missing tenant", models.UsageAlertWorkflowInput{EnvironmentID: "env_1", CustomerID: "cust_1"}, true},
		{"missing environment", models.UsageAlertWorkflowInput{TenantID: "tnt_1", CustomerID: "cust_1"}, true},
		{"missing customer", models.UsageAlertWorkflowInput{TenantID: "tnt_1", EnvironmentID: "env_1"}, true},
		{"all present", models.UsageAlertWorkflowInput{TenantID: "tnt_1", EnvironmentID: "env_1", CustomerID: "cust_1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

// The workflow function name must equal the string constant registered under
// (registration.go / ee/alerts register.go). Renaming the func without
// updating the constant breaks Temporal task routing at runtime.
func TestUsageAlertWorkflowConstantsInSync(t *testing.T) {
	if WorkflowUsageAlert != "UsageAlertWorkflow" {
		t.Fatalf("WorkflowUsageAlert changed unexpectedly: %s", WorkflowUsageAlert)
	}
}
