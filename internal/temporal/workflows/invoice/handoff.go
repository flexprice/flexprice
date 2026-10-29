package invoice

import (
	"crypto/sha256"
	"encoding/hex"

	models "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/types"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// StartInvoiceProcessing waits for durable start, allowing the parent to finish before payment.
func StartInvoiceProcessing(ctx workflow.Context, input models.ProcessInvoiceWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(input.TenantID + "\x00" + input.EnvironmentID + "\x00" + input.InvoiceID))
	ctx = workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: "wf_ProcessInvoiceWorkflow_" + hex.EncodeToString(hash[:]), TaskQueue: types.TemporalTaskQueueInvoice.String(),
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON, WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	})
	child := workflow.ExecuteChildWorkflow(ctx, types.TemporalProcessInvoiceWorkflow.String(), input)
	var execution workflow.Execution
	err := child.GetChildWorkflowExecution().Get(ctx, &execution)
	if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return nil
	}
	return err
}
