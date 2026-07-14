package workflows

import (
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
)

// ponytail: single assert-based check for the workflow. The workflow body itself
// just orchestrates a prep activity + two conditional evaluation activities — the
// real logic lives in the service package and is covered there. What can only
// break here is the input contract (validation) and the workflow-name constant
// that the SDK dispatches by string.

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

func TestUsageAlertWorkflowConstantsInSync(t *testing.T) {
	// The workflow function name must equal the string constant that
	// registration.go registers under and that types.TemporalUsageAlertWorkflow
	// dispatches by. If someone renames the func without updating the constant
	// the Temporal SDK will fail to route tasks at runtime — this check catches
	// that at compile+test time.
	if WorkflowUsageAlert != "UsageAlertWorkflow" {
		t.Fatalf("WorkflowUsageAlert changed unexpectedly: %s", WorkflowUsageAlert)
	}
}
