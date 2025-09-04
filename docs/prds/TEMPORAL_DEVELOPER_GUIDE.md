# 🚀 Temporal Workflow & Activity Implementation Guide

## 📋 Table of Contents

1. [Overview](#overview)
2. [Step-by-Step Implementation Guide](#step-by-step-implementation-guide)
3. [Execution Patterns](#execution-patterns)
4. [Best Practices](#best-practices)
5. [Troubleshooting](#troubleshooting)
6. [Examples](#examples)

## 🎯 Overview

This guide will walk you through implementing new workflows and activities in our Temporal-based system. We'll cover everything from business logic to execution patterns.

> **📚 Reference Implementation**: Throughout this guide, we'll reference the existing **Plan Price Sync** workflow as a concrete example. You can find the complete implementation in:
>
> - **Workflow**: `internal/temporal/workflows/price_sync_workflow.go`
> - **Activity**: `internal/temporal/activities/plan_activities.go`
> - **Service**: `internal/temporal/service.go` (StartPlanPriceSync method)
> - **Handler**: `internal/api/v1/plan.go` (SyncPlanPrices method)
> - **Registration**: `internal/temporal/registration.go`
> - **Flow Diagram**: `temporal_flow_diagram.md`

This will help you understand the patterns by seeing them in action!

## 🛠️ Step-by-Step Implementation Guide

Let's implement a **User Data Export** workflow as our example - this is a common use case where you need to export user data asynchronously.

### Step 1: Implement Business Logic in Service Layer

**Start here** - Implement your core business logic in the service layer first.

> **📖 Reference**: See `internal/service/plan.go` → `SyncPlanPrices` method for a real example.

```go
// internal/service/user_service.go
package service

import (
    "context"
    "github.com/flexprice/flexprice/internal/api/dto"
)

type UserService struct {
    userRepo    UserRepository
    exportRepo  ExportRepository
    s3Service   S3Service
}

func (s *UserService) ExportUserData(ctx context.Context, input ExportUserDataInput) (*ExportUserDataOutput, error) {
    // 1. Validate input
    if err := input.Validate(); err != nil {
        return nil, err
    }

    // 2. Extract context values (tenant, environment)
    tenantID := types.GetTenantID(ctx)
    environmentID := types.GetEnvironmentID(ctx)

    // 3. Implement your business logic
    result, err := s.performUserDataExport(ctx, input)
    if err != nil {
        return nil, err
    }

    // 4. Return structured result
    return &ExportUserDataOutput{
        ExportID:    result.ExportID,
        FileURL:     result.FileURL,
        RecordCount: result.RecordCount,
        Status:      "completed",
    }, nil
}

func (s *UserService) performUserDataExport(ctx context.Context, input ExportUserDataInput) (*ExportResult, error) {
    // 1. Fetch user data from database
    users, err := s.userRepo.GetUsersByTenant(ctx, input.TenantID, input.Filters)
    if err != nil {
        return nil, err
    }

    // 2. Generate CSV/JSON export
    exportData, err := s.generateExportFile(users, input.Format)
    if err != nil {
        return nil, err
    }

    // 3. Upload to S3
    fileURL, err := s.s3Service.UploadExport(ctx, exportData, input.TenantID)
    if err != nil {
        return nil, err
    }

    // 4. Save export record
    exportID, err := s.exportRepo.SaveExportRecord(ctx, &ExportRecord{
        TenantID:    input.TenantID,
        FileURL:     fileURL,
        RecordCount: len(users),
        Status:      "completed",
    })
    if err != nil {
        return nil, err
    }

    return &ExportResult{
        ExportID:    exportID,
        FileURL:     fileURL,
        RecordCount: len(users),
    }, nil
}
```

### Step 2: Define Types in Types Directory

**Create your workflow and activity types** in the types directory.

> **📖 Reference**: See `internal/types/workflow.go` for existing workflow types like `PriceSyncWorkflow`.

```go
// internal/types/workflow.go (add to existing file)
const (
    // Add your new workflow type
    UserDataExportWorkflow WorkflowType = "UserDataExportWorkflow"
)

// internal/types/user_export_types.go (create new file)
package types

// ExportUserDataInput represents input for user data export
type ExportUserDataInput struct {
    TenantID      string                 `json:"tenant_id" validate:"required"`
    EnvironmentID string                 `json:"environment_id" validate:"required"`
    UserFilters   map[string]interface{} `json:"user_filters"`
    Format        string                 `json:"format" validate:"required,oneof=csv json"`
    EmailNotify   bool                   `json:"email_notify"`
}

// Validate validates the export input
func (e *ExportUserDataInput) Validate() error {
    if e.TenantID == "" {
        return ierr.NewError("tenant ID is required").Mark(ierr.ErrValidation)
    }
    if e.EnvironmentID == "" {
        return ierr.NewError("environment ID is required").Mark(ierr.ErrValidation)
    }
    if e.Format == "" {
        return ierr.NewError("format is required").Mark(ierr.ErrValidation)
    }
    return nil
}

// ExportUserDataOutput represents output from user data export
type ExportUserDataOutput struct {
    ExportID    string `json:"export_id"`
    FileURL     string `json:"file_url"`
    RecordCount int    `json:"record_count"`
    Status      string `json:"status"`
}
```

### Step 3: Create Activity Implementation

**Implement your activity** - this is where your business logic gets called.

> **📖 Reference**: See `internal/temporal/activities/plan_activities.go` → `SyncPlanPrices` method for a real example.

```go
// internal/temporal/activities/user_export_activities.go
package activities

import (
    "context"
    "github.com/flexprice/flexprice/internal/service"
    "github.com/flexprice/flexprice/internal/types"
    ierr "github.com/flexprice/flexprice/internal/errors"
)

// UserExportActivities contains all user export activity methods
type UserExportActivities struct {
    userService service.UserService
}

// NewUserExportActivities creates a new UserExportActivities instance
func NewUserExportActivities(userService service.UserService) *UserExportActivities {
    return &UserExportActivities{
        userService: userService,
    }
}

// ExportUserDataInput represents the input for the ExportUserData activity
type ExportUserDataInput struct {
    TenantID      string                 `json:"tenant_id"`
    EnvironmentID string                 `json:"environment_id"`
    UserFilters   map[string]interface{} `json:"user_filters"`
    Format        string                 `json:"format"`
    EmailNotify   bool                   `json:"email_notify"`
}

// ExportUserData executes user data export business logic
// This method will be registered as "ExportUserData" in Temporal
func (a *UserExportActivities) ExportUserData(ctx context.Context, input ExportUserDataInput) (*types.ExportUserDataOutput, error) {
    // 1. Validate input parameters
    if input.TenantID == "" {
        return nil, ierr.NewError("tenant ID is required").
            WithHint("Tenant ID is required").
            Mark(ierr.ErrValidation)
    }

    if input.EnvironmentID == "" {
        return nil, ierr.NewError("environment ID is required").
            WithHint("Environment ID is required").
            Mark(ierr.ErrValidation)
    }

    if input.Format == "" {
        return nil, ierr.NewError("format is required").
            WithHint("Export format (csv/json) is required").
            Mark(ierr.ErrValidation)
    }

    // 2. Set up context with tenant/environment info
    ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
    ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)

    // 3. Call your business logic
    result, err := a.userService.ExportUserData(ctx, service.ExportUserDataInput{
        TenantID:    input.TenantID,
        UserFilters: input.UserFilters,
        Format:      input.Format,
        EmailNotify: input.EmailNotify,
    })
    if err != nil {
        return nil, err
    }

    // 4. Return result
    return &types.ExportUserDataOutput{
        ExportID:    result.ExportID,
        FileURL:     result.FileURL,
        RecordCount: result.RecordCount,
        Status:      result.Status,
    }, nil
}
```

### Step 4: Create Workflow Implementation

**Implement your workflow** - this orchestrates the activity execution.

> **📖 Reference**: See `internal/temporal/workflows/price_sync_workflow.go` → `PriceSyncWorkflow` function for a real example.

```go
// internal/temporal/workflows/user_export_workflow.go
package workflows

import (
    "time"
    "github.com/flexprice/flexprice/internal/temporal/models"
    "github.com/flexprice/flexprice/internal/types"
    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"
)

const (
    // Workflow name - must match the function name
    WorkflowUserDataExport = "UserDataExportWorkflow"
    // Activity name - must match the registered method name
    ActivityExportUserData = "ExportUserData"
)

// UserDataExportWorkflow orchestrates user data export process
func UserDataExportWorkflow(ctx workflow.Context, input models.UserDataExportWorkflowInput) (*types.ExportUserDataOutput, error) {
    // 1. Validate input
    if err := input.Validate(); err != nil {
        return nil, err
    }

    // 2. Create activity input with context
    activityInput := struct {
        TenantID      string                 `json:"tenant_id"`
        EnvironmentID string                 `json:"environment_id"`
        UserFilters   map[string]interface{} `json:"user_filters"`
        Format        string                 `json:"format"`
        EmailNotify   bool                   `json:"email_notify"`
    }{
        TenantID:      input.TenantID,
        EnvironmentID: input.EnvironmentID,
        UserFilters:   input.UserFilters,
        Format:        input.Format,
        EmailNotify:   input.EmailNotify,
    }

    // 3. Configure activity options (timeouts, retries, etc.)
    ao := workflow.ActivityOptions{
        StartToCloseTimeout: 10 * time.Minute, // Export can take longer
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval:    time.Second,
            BackoffCoefficient: 2.0,
            MaximumInterval:    2 * time.Minute,
            MaximumAttempts:    3,
        },
    }
    ctx = workflow.WithActivityOptions(ctx, ao)

    // 4. Execute the activity
    var out types.ExportUserDataOutput
    if err := workflow.ExecuteActivity(ctx, ActivityExportUserData, activityInput).Get(ctx, &out); err != nil {
        return nil, err
    }

    // 5. Return result
    return &out, nil
}
```

### Step 5: Add Models for Temporal

**Add your workflow input model** to the Temporal models.

> **📖 Reference**: See `internal/temporal/models/types.go` → `PriceSyncWorkflowInput` for a real example.

```go
// internal/temporal/models/types.go (add to existing file)
// UserDataExportWorkflowInput represents input for user data export workflow
type UserDataExportWorkflowInput struct {
    TenantID      string                 `json:"tenant_id"`
    EnvironmentID string                 `json:"environment_id"`
    UserFilters   map[string]interface{} `json:"user_filters"`
    Format        string                 `json:"format"`
    EmailNotify   bool                   `json:"email_notify"`
}

// Validate validates the workflow input
func (w *UserDataExportWorkflowInput) Validate() error {
    if w.TenantID == "" {
        return ierr.NewError("tenant ID is required").Mark(ierr.ErrValidation)
    }
    if w.EnvironmentID == "" {
        return ierr.NewError("environment ID is required").Mark(ierr.ErrValidation)
    }
    if w.Format == "" {
        return ierr.NewError("format is required").Mark(ierr.ErrValidation)
    }
    return nil
}
```

### Step 6: Register Workflow and Activity

**Register your new workflow and activity** in the registration file.

> **📖 Reference**: See `internal/temporal/registration.go` for how `PriceSyncWorkflow` and `SyncPlanPrices` are registered.

```go
// internal/temporal/registration.go (add to existing function)
func RegisterWorkflowsAndActivities(w worker.Worker, params service.ServiceParams) {
    // Existing registrations...

    // Add your new workflow
    w.RegisterWorkflow(workflows.UserDataExportWorkflow) // "UserDataExportWorkflow"

    // Create your service and activities
    userService := service.NewUserService(params) // You need to implement this
    userExportActivities := activities.NewUserExportActivities(userService)

    // Register your activity
    w.RegisterActivity(userExportActivities.ExportUserData) // "ExportUserData"
}
```

### Step 7: Add Service Method to Temporal Service

**Add method to start your workflow** in the Temporal service.

> **📖 Reference**: See `internal/temporal/service.go` → `StartPlanPriceSync` method for a real example.

```go
// internal/temporal/service.go (add to existing file)
// StartUserDataExport starts user data export workflow
func (s *Service) StartUserDataExport(ctx context.Context, input types.ExportUserDataInput) (*types.ExportUserDataOutput, error) {
    // 1. Extract tenant and environment from context
    tenantID := types.GetTenantID(ctx)
    environmentID := types.GetEnvironmentID(ctx)

    // 2. Generate unique workflow ID
    workflowID := fmt.Sprintf("user-export-%s-%d", tenantID, time.Now().Unix())

    // 3. Configure workflow options
    workflowOptions := client.StartWorkflowOptions{
        ID:        workflowID,
        TaskQueue: s.cfg.TaskQueue,
    }

    // 4. Start workflow
    we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(types.UserDataExportWorkflow), models.UserDataExportWorkflowInput{
        TenantID:      tenantID,
        EnvironmentID: environmentID,
        UserFilters:   input.UserFilters,
        Format:        input.Format,
        EmailNotify:   input.EmailNotify,
    })
    if err != nil {
        return nil, err
    }

    // 5. Wait for completion (for synchronous execution)
    var result types.ExportUserDataOutput
    if err := we.Get(ctx, &result); err != nil {
        return nil, err
    }

    return &result, nil
}
```

## 🚀 Execution Patterns

### Pattern 1: HTTP Handler Trigger (Synchronous)

**Trigger from HTTP endpoint** - waits for completion.

> **📖 Reference**: See `internal/api/v1/plan.go` → `SyncPlanPrices` method for a real example.

```go
// internal/api/v1/user_handler.go
func (h *UserHandler) ExportUserData(c *gin.Context) {
    var req dto.ExportUserDataRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(ierr.WithError(err).
            WithHint("Invalid request format").
            Mark(ierr.ErrValidation))
        return
    }

    // Start workflow and wait for completion
    result, err := h.temporalService.StartUserDataExport(c.Request.Context(), types.ExportUserDataInput{
        UserFilters: req.Filters,
        Format:      req.Format,
        EmailNotify: req.EmailNotify,
    })
    if err != nil {
        c.Error(err)
        return
    }

    c.JSON(http.StatusOK, result)
}
```

### Pattern 2: HTTP Handler Trigger (Asynchronous)

**Trigger from HTTP endpoint** - returns immediately, workflow runs in background.

```go
// internal/temporal/service.go
// StartUserDataExportAsync starts user data export workflow asynchronously
func (s *Service) StartUserDataExportAsync(ctx context.Context, input types.ExportUserDataInput) (*models.UserExportWorkflowResult, error) {
    tenantID := types.GetTenantID(ctx)
    environmentID := types.GetEnvironmentID(ctx)

    workflowID := fmt.Sprintf("user-export-async-%s-%d", tenantID, time.Now().Unix())

    workflowOptions := client.StartWorkflowOptions{
        ID:        workflowID,
        TaskQueue: s.cfg.TaskQueue,
    }

    we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(types.UserDataExportWorkflow), models.UserDataExportWorkflowInput{
        TenantID:      tenantID,
        EnvironmentID: environmentID,
        UserFilters:   input.UserFilters,
        Format:        input.Format,
        EmailNotify:   input.EmailNotify,
    })
    if err != nil {
        return nil, err
    }

    // Return immediately without waiting
    return &models.UserExportWorkflowResult{
        WorkflowID: workflowID,
        RunID:      we.GetRunID(),
        Status:     "started",
    }, nil
}

// internal/api/v1/user_handler.go
func (h *UserHandler) ExportUserDataAsync(c *gin.Context) {
    var req dto.ExportUserDataRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(ierr.WithError(err).
            WithHint("Invalid request format").
            Mark(ierr.ErrValidation))
        return
    }

    // Start workflow asynchronously
    result, err := h.temporalService.StartUserDataExportAsync(c.Request.Context(), types.ExportUserDataInput{
        UserFilters: req.Filters,
        Format:      req.Format,
        EmailNotify: req.EmailNotify,
    })
    if err != nil {
        c.Error(err)
        return
    }

    c.JSON(http.StatusAccepted, result)
}
```

### Pattern 3: Cron/Scheduled Execution

**Run workflow on a schedule** using Temporal's cron feature.

```go
// internal/temporal/service.go
// StartUserDataExportCron starts user data export workflow on a cron schedule
func (s *Service) StartUserDataExportCron(ctx context.Context, cronSchedule string) (*models.UserExportWorkflowResult, error) {
    tenantID := types.GetTenantID(ctx)
    environmentID := types.GetEnvironmentID(ctx)

    workflowID := fmt.Sprintf("user-export-cron-%s", tenantID)

    workflowOptions := client.StartWorkflowOptions{
        ID:           workflowID,
        TaskQueue:    s.cfg.TaskQueue,
        CronSchedule: cronSchedule, // e.g., "0 2 * * *" for daily at 2 AM
    }

    we, err := s.client.Client.ExecuteWorkflow(ctx, workflowOptions, string(types.UserDataExportWorkflow), models.UserDataExportWorkflowInput{
        TenantID:      tenantID,
        EnvironmentID: environmentID,
        UserFilters:   map[string]interface{}{"status": "active"}, // Default filters
        Format:        "csv", // Default format
        EmailNotify:   true,  // Always notify for scheduled exports
    })
    if err != nil {
        return nil, err
    }

    return &models.UserExportWorkflowResult{
        WorkflowID: workflowID,
        RunID:      we.GetRunID(),
        Status:     "scheduled",
    }, nil
}
```

### Pattern 4: Event-Driven Execution

**Trigger workflow from events** (Kafka, webhooks, etc.).

```go
// internal/event/user_event_handler.go
type UserEventHandler struct {
    temporalService *temporal.Service
}

func (h *UserEventHandler) HandleUserDataRequestEvent(ctx context.Context, event UserDataRequestEvent) error {
    // Extract relevant data from event
    tenantID := event.TenantID
    userFilters := event.Filters
    format := event.Format

    // Start workflow
    _, err := h.temporalService.StartUserDataExportAsync(ctx, types.ExportUserDataInput{
        UserFilters: userFilters,
        Format:      format,
        EmailNotify: true, // Always notify for event-driven exports
    })
    if err != nil {
        return err
    }

    return nil
}
```

### Pattern 5: Batch Processing

**Process multiple items** in a single workflow.

```go
// internal/temporal/workflows/user_batch_export_workflow.go
func UserBatchExportWorkflow(ctx workflow.Context, input models.UserBatchExportWorkflowInput) (*types.UserBatchExportOutput, error) {
    var results []types.ExportUserDataOutput

    // Process each tenant's export
    for _, tenantID := range input.TenantIDs {
        activityInput := struct {
            TenantID      string                 `json:"tenant_id"`
            EnvironmentID string                 `json:"environment_id"`
            UserFilters   map[string]interface{} `json:"user_filters"`
            Format        string                 `json:"format"`
            EmailNotify   bool                   `json:"email_notify"`
        }{
            TenantID:      tenantID,
            EnvironmentID: input.EnvironmentID,
            UserFilters:   input.UserFilters,
            Format:        input.Format,
            EmailNotify:   input.EmailNotify,
        }

        var result types.ExportUserDataOutput
        if err := workflow.ExecuteActivity(ctx, ActivityExportUserData, activityInput).Get(ctx, &result); err != nil {
            // Handle individual failures - log but continue
            workflow.GetLogger(ctx).Error("Failed to export user data for tenant", "tenantID", tenantID, "error", err)
            continue
        }

        results = append(results, result)
    }

    return &types.UserBatchExportOutput{
        Results: results,
        Total:   len(results),
        Failed:  len(input.TenantIDs) - len(results),
    }, nil
}
```

## 🎯 Best Practices

### 1. Error Handling

```go
// Always validate inputs
if err := input.Validate(); err != nil {
    return nil, err
}

// Use structured errors
return nil, ierr.NewError("operation failed").
    WithHint("Check your input parameters").
    Mark(ierr.ErrValidation)
```

### 2. Context Management

```go
// Always set tenant and environment context
ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
```

### 3. Timeout Configuration

```go
// Set appropriate timeouts based on operation complexity
ao := workflow.ActivityOptions{
    StartToCloseTimeout: 5 * time.Minute, // For quick operations
    // StartToCloseTimeout: 30 * time.Minute, // For long operations
}
```

### 4. Retry Policies

```go
RetryPolicy: &temporal.RetryPolicy{
    InitialInterval:    time.Second,
    BackoffCoefficient: 2.0,
    MaximumInterval:    time.Minute,
    MaximumAttempts:    3, // Adjust based on operation criticality
}
```

### 5. Workflow ID Generation

```go
// Use descriptive, unique IDs
workflowID := fmt.Sprintf("your-operation-%s-%d", identifier, time.Now().Unix())
```

## 🔧 Troubleshooting

### Common Issues

1. **Activity Not Found**

   - Check registration in `registration.go`
   - Ensure activity name matches exactly

2. **Workflow Not Found**

   - Check workflow registration
   - Verify workflow name in service call

3. **Context Issues**

   - Ensure tenant/environment are set in context
   - Check context extraction in service layer

4. **Timeout Issues**
   - Adjust `StartToCloseTimeout` based on operation complexity
   - Consider breaking large operations into smaller activities

### Debugging Tips

1. **Check Temporal UI** for workflow execution details
2. **Add logging** at each step
3. **Use workflow.GetLogger()** for workflow logging
4. **Test activities independently** before integrating

## 📚 Examples

### Example 1: User Data Export (Synchronous)

```go
// Export user data and wait for completion
func (h *UserHandler) ExportUserDataSync(c *gin.Context) {
    var req dto.ExportUserDataRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(ierr.WithError(err).Mark(ierr.ErrValidation))
        return
    }

    result, err := h.temporalService.StartUserDataExport(c.Request.Context(), types.ExportUserDataInput{
        UserFilters: req.Filters,
        Format:      req.Format,
        EmailNotify: req.EmailNotify,
    })
    if err != nil {
        c.Error(err)
        return
    }
    c.JSON(http.StatusOK, result)
}
```

### Example 2: Scheduled User Data Backup

```go
// Generate user data backups every day at 2 AM
func (h *UserHandler) ScheduleDailyUserBackup(c *gin.Context) {
    result, err := h.temporalService.StartUserDataExportCron(
        c.Request.Context(),
        "0 2 * * *", // Daily at 2 AM
    )
    if err != nil {
        c.Error(err)
        return
    }
    c.JSON(http.StatusOK, result)
}
```

### Example 3: Event-Driven User Data Export

```go
// Process user data export requests from events
func (h *UserEventHandler) HandleUserDataExportRequest(ctx context.Context, event UserDataExportRequestEvent) error {
    return h.temporalService.StartUserDataExportAsync(ctx, types.ExportUserDataInput{
        UserFilters: event.Filters,
        Format:      event.Format,
        EmailNotify: true,
    })
}
```

## 🚀 Scaling Considerations

### 1. Task Queues

- Use different task queues for different types of work
- Configure worker pools based on workload

### 2. Workflow Limits

- Keep workflows deterministic
- Avoid external calls in workflows
- Use activities for all external operations

### 3. Activity Limits

- Design activities to be idempotent
- Handle partial failures gracefully
- Use appropriate timeouts and retries

### 4. Monitoring

- Set up alerts for workflow failures
- Monitor activity execution times
- Track workflow completion rates

## 📚 Complete Reference: Plan Price Sync Example

To help you understand the complete flow, here's how the existing **Plan Price Sync** workflow is implemented across all layers:

### 🔍 **File-by-File Breakdown**

| Layer              | File                                                 | Key Components                       |
| ------------------ | ---------------------------------------------------- | ------------------------------------ |
| **Business Logic** | `internal/service/plan.go`                           | `SyncPlanPrices(ctx, planID)` method |
| **Types**          | `internal/types/workflow.go`                         | `PriceSyncWorkflow` constant         |
| **Models**         | `internal/temporal/models/types.go`                  | `PriceSyncWorkflowInput` struct      |
| **Activity**       | `internal/temporal/activities/plan_activities.go`    | `SyncPlanPrices` method              |
| **Workflow**       | `internal/temporal/workflows/price_sync_workflow.go` | `PriceSyncWorkflow` function         |
| **Service**        | `internal/temporal/service.go`                       | `StartPlanPriceSync` method          |
| **Handler**        | `internal/api/v1/plan.go`                            | `SyncPlanPrices` HTTP handler        |
| **Registration**   | `internal/temporal/registration.go`                  | Workflow & activity registration     |

### 🔄 **Complete Flow Trace**

1. **HTTP Request**: `POST /plans/123/sync/subscriptions`
2. **Handler**: `PlanHandler.SyncPlanPrices()` extracts plan ID "123"
3. **Temporal Service**: `StartPlanPriceSync()` extracts tenant/env, creates workflow ID
4. **Workflow Execution**: `PriceSyncWorkflow()` validates input, sets activity options
5. **Activity Execution**: `PlanActivities.SyncPlanPrices()` sets context, calls business logic
6. **Business Logic**: `PlanService.SyncPlanPrices()` does actual work
7. **Result Flow**: Returns through activity → workflow → service → handler → HTTP response

### 🎯 **Key Registration Names**

```go
// Workflows
"PriceSyncWorkflow" → workflows.PriceSyncWorkflow

// Activities
"SyncPlanPrices" → planActivities.SyncPlanPrices
```

### 📊 **Visual Flow**

See the complete flow diagram in `temporal_flow_diagram.md` for a detailed visual representation of every step.

---

This guide should give you everything you need to implement new workflows and activities in our Temporal-based system! 🎉

**Next Steps:**

1. Study the Plan Price Sync implementation
2. Follow the step-by-step guide above
3. Use the flow diagram as a reference
4. Start with a simple workflow and build up complexity
