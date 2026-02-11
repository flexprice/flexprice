package interceptor

import (
	"reflect"
	"strings"
	"unicode"

	"github.com/flexprice/flexprice/internal/temporal/tracking"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

// WorkflowTrackingInterceptor provides workflow execution tracking
// It automatically tracks workflow start and end status in the database
type WorkflowTrackingInterceptor struct {
	interceptor.InterceptorBase
}

// NewWorkflowTrackingInterceptor creates a new workflow tracking interceptor
func NewWorkflowTrackingInterceptor() *WorkflowTrackingInterceptor {
	return &WorkflowTrackingInterceptor{}
}

// InterceptWorkflow creates a workflow inbound interceptor
func (w *WorkflowTrackingInterceptor) InterceptWorkflow(
	ctx workflow.Context,
	next interceptor.WorkflowInboundInterceptor,
) interceptor.WorkflowInboundInterceptor {
	return &workflowTrackingInboundInterceptor{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{
			Next: next,
		},
	}
}

// workflowTrackingInboundInterceptor intercepts workflow executions to track status
type workflowTrackingInboundInterceptor struct {
	interceptor.WorkflowInboundInterceptorBase
}

// ExecuteWorkflow intercepts workflow execution to track completion status and timing.
// Workflows in types.WorkflowTypesExcludedFromTracking are not saved or tracked.
func (w *workflowTrackingInboundInterceptor) ExecuteWorkflow(
	ctx workflow.Context,
	in *interceptor.ExecuteWorkflowInput,
) (interface{}, error) {
	workflowInfo := workflow.GetInfo(ctx)

	// Skip all tracking for excluded workflow types (no DB save, no start/end tracking)
	if !tracking.ShouldTrackWorkflow(workflowInfo.WorkflowType.Name) {
		return w.Next.ExecuteWorkflow(ctx, in)
	}

	logger := workflow.GetLogger(ctx)

	// Track workflow start automatically
	trackWorkflowStart(ctx, *workflowInfo, in)

	// Capture start time from workflow info
	startTime := workflowInfo.WorkflowStartTime

	logger.Debug("Workflow tracking interceptor: workflow starting",
		"workflow_type", workflowInfo.WorkflowType.Name,
		"workflow_id", workflowInfo.WorkflowExecution.ID,
		"run_id", workflowInfo.WorkflowExecution.RunID,
		"start_time", startTime,
	)

	// Execute the workflow
	result, err := w.Next.ExecuteWorkflow(ctx, in)

	// Capture end time and calculate duration
	endTime := workflow.Now(ctx)
	durationMs := endTime.Sub(startTime).Milliseconds()

	// After workflow completes (success or failure), update status
	status := types.WorkflowExecutionStatusCompleted
	errorMsg := ""

	if err != nil {
		status = types.WorkflowExecutionStatusFailed
		errorMsg = err.Error()
		logger.Info("Workflow tracking interceptor: workflow failed, tracking status",
			"workflow_type", workflowInfo.WorkflowType.Name,
			"workflow_id", workflowInfo.WorkflowExecution.ID,
			"status", status,
			"duration_ms", durationMs,
		)
	} else {
		logger.Info("Workflow tracking interceptor: workflow completed, tracking status",
			"workflow_type", workflowInfo.WorkflowType.Name,
			"workflow_id", workflowInfo.WorkflowExecution.ID,
			"status", status,
			"duration_ms", durationMs,
		)
	}

	// Execute local activity to update workflow status in database with timing
	// This is non-blocking and won't fail the workflow if tracking fails
	tracking.ExecuteTrackWorkflowEnd(ctx, tracking.TrackWorkflowEndInput{
		WorkflowStatus: status,
		Error:          errorMsg,
		EndTime:        endTime,
		DurationMs:     durationMs,
	})

	return result, err

}

// trackWorkflowStart automatically tracks workflow start by extracting metadata from input
// This function is panic-safe and will never crash the workflow
func trackWorkflowStart(ctx workflow.Context, info workflow.Info, in *interceptor.ExecuteWorkflowInput) {
	logger := workflow.GetLogger(ctx)

	// CRITICAL: Panic recovery to ensure tracking failures never crash workflows
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic recovered in trackWorkflowStart - workflow will continue",
				"panic", r,
				"workflow_id", info.WorkflowExecution.ID,
				"workflow_type", info.WorkflowType.Name)
			// Workflow continues despite panic
		}
	}()

	// Extract tenant, environment, entity, entity_id, and metadata from workflow input
	tenantID, environmentID, entity, entityID, metadata := extractWorkflowContext(in.Args)

	logger.Debug("Tracking workflow start",
		"workflow_type", info.WorkflowType.Name,
		"workflow_id", info.WorkflowExecution.ID,
		"task_queue", info.TaskQueueName,
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"entity", entity,
		"entity_id", entityID,
	)

	tracking.ExecuteTrackWorkflowStart(ctx, tracking.TrackWorkflowStartInput{
		WorkflowType:  info.WorkflowType.Name,
		TaskQueue:     info.TaskQueueName,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		UserID:        "",
		Entity:        entity,
		EntityID:      entityID,
		Metadata:      metadata,
	})
}

// extractWorkflowContext extracts TenantID, EnvironmentID, entity, entity_id, and metadata from workflow input.
// Entity and entity_id are derived from known ID fields (PlanID->plan, InvoiceID->invoice, etc.) for efficient DB filtering.
// This function is panic-safe and will return empty values if extraction fails.
func extractWorkflowContext(args []interface{}) (tenantID, environmentID, entity, entityID string, metadata map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			tenantID = ""
			environmentID = ""
			entity = ""
			entityID = ""
			metadata = nil
		}
	}()

	if len(args) == 0 {
		return "", "", "", "", nil
	}

	metadata = make(map[string]interface{})
	input := args[0]

	val := reflect.ValueOf(input)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return "", "", "", "", nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		if !fieldVal.CanInterface() {
			continue
		}

		if field.Name == "TenantID" && fieldVal.Kind() == reflect.String {
			tenantID = fieldVal.String()
			continue
		}

		if (field.Name == "EnvironmentID" || field.Name == "EnvID") && fieldVal.Kind() == reflect.String {
			environmentID = fieldVal.String()
			continue
		}

		// Set entity and entity_id from known ID fields (for efficient column-based filtering)
		if fieldVal.Kind() == reflect.String && !isZeroValue(fieldVal) {
			idVal := fieldVal.String()
			switch field.Name {
			case "PlanID":
				entity, entityID = "plan", idVal
				continue
			case "PriceID":
				entity, entityID = "price", idVal
				continue
			case "InvoiceID":
				entity, entityID = "invoice", idVal
				continue
			case "SubscriptionID":
				entity, entityID = "subscription", idVal
				continue
			case "CustomerID":
				entity, entityID = "customer", idVal
				continue
			case "ScheduledTaskID":
				entity, entityID = "scheduled_task", idVal
				continue
			case "TaskID":
				entity, entityID = "task", idVal
				continue
			}
		}

		if !isZeroValue(fieldVal) {
			metadata[toSnakeCase(field.Name)] = fieldVal.Interface()
		}
	}

	return tenantID, environmentID, entity, entityID, metadata
}

// isZeroValue checks if a reflect.Value is the zero value for its type
func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:
		return v.IsNil()
	default:
		return false
	}
}

// toSnakeCase converts a string from CamelCase to snake_case
// Handles acronyms correctly: UserID -> user_id, ScheduledTaskID -> scheduled_task_id
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			// Don't add underscore at the start
			if i > 0 {
				// Check if previous char was lowercase or if next char is lowercase
				// This handles acronyms: UserID -> user_id (not user_i_d)
				prevIsLower := i > 0 && unicode.IsLower(rune(s[i-1]))
				nextIsLower := i+1 < len(s) && unicode.IsLower(rune(s[i+1]))

				// Add underscore if:
				// - Previous char was lowercase (camelCase boundary)
				// - OR current is uppercase but next is lowercase (end of acronym: HTMLParser -> html_parser)
				if prevIsLower || (i > 0 && nextIsLower && unicode.IsUpper(rune(s[i-1]))) {
					result.WriteRune('_')
				}
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
