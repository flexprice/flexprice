# 🏗️ Temporal Architecture Redesign

## 📋 Current Issues

1. **Mixed Responsibilities**: Everything is in the same package level
2. **Circular Dependencies**: `client` package imports `temporal` package which imports `client`
3. **Poor Separation**: Core client, service, and business logic are mixed
4. **Hard to Test**: Tightly coupled components
5. **Hard to Extend**: Adding new workflows requires touching multiple files

## 🎯 Proposed Architecture

### **Clean Layered Architecture**

```
internal/temporal/
├── core/                       # Core temporal functionality (no business logic)
│   ├── client.go              # Basic temporal client wrapper
│   ├── types.go               # Core types and interfaces
│   └── options.go             # Default options and configurations
├── service/                    # Service layer (orchestration)
│   ├── service.go             # Main temporal service
│   ├── worker_manager.go      # Worker management
│   └── validation.go          # Service validation
├── workflows/                  # Workflow implementations
│   ├── task/                  # Task-related workflows
│   │   └── workflow.go        # TaskProcessingWorkflow
│   ├── plan/                  # Plan-related workflows
│   │   └── workflow.go        # PriceSyncWorkflow
│   └── billing/               # Billing-related workflows (future)
├── activities/                 # Activity implementations
│   ├── task/                  # Task-related activities
│   │   └── activities.go      # TaskActivities
│   ├── plan/                  # Plan-related activities
│   │   └── activities.go      # PlanActivities
│   └── billing/               # Billing-related activities (future)
├── models/                     # Shared data models (no dependencies)
│   ├── task_models.go         # Task workflow and activity models
│   ├── plan_models.go         # Plan workflow and activity models
│   └── common.go              # Common models
├── registry/                   # Registration and discovery
│   ├── registry.go            # Workflow/activity registry
│   └── factory.go             # Factory for creating instances
└── README.md                  # Documentation
```

## 🔄 Dependency Flow

### **Layer 1: Core (No Dependencies)**

- `core/client.go` - Basic temporal client
- `core/types.go` - Core interfaces and types
- `core/options.go` - Configuration options

### **Layer 2: Service (Depends on Core)**

- `service/service.go` - Main service (imports core)
- `service/worker_manager.go` - Worker management (imports core)
- `service/validation.go` - Validation helpers

### **Layer 3: Workflows/Activities (Depends on Core + Service)**

- `workflows/*/workflow.go` - Workflow implementations (imports core + service)
- `activities/*/activities.go` - Activity implementations (imports core + service)

### **Layer 4: Registry (Depends on All)**

- `registry/registry.go` - Registration logic (imports all layers)

## 🚫 Avoiding Circular Dependencies

### **Key Principles:**

1. **Core Layer**: No imports from temporal package
2. **Service Layer**: Only imports core
3. **Workflow/Activity Layer**: Only imports core + service
4. **Registry Layer**: Imports all but is only used by main.go

### **Interface-Based Design:**

```go
// core/types.go - Define interfaces
type WorkflowExecutor interface {
    ExecuteWorkflow(ctx context.Context, workflowName string, input interface{}) (WorkflowRun, error)
}

type ActivityExecutor interface {
    ExecuteActivity(ctx context.Context, activityName string, input interface{}) (interface{}, error)
}

// service/service.go - Implement interfaces
type TemporalService struct {
    executor WorkflowExecutor
    // ...
}
```

## 📁 Detailed File Structure

### **Core Layer**

#### `internal/temporal/core/client.go`

```go
package core

import (
    "context"
    "crypto/tls"
    "github.com/flexprice/flexprice/internal/config"
    "github.com/flexprice/flexprice/internal/logger"
    "go.temporal.io/sdk/client"
)

// TemporalClient wraps the Temporal SDK client
type TemporalClient struct {
    Client client.Client
}

// NewTemporalClient creates a new Temporal client
func NewTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (*TemporalClient, error) {
    // Implementation
}
```

#### `internal/temporal/core/types.go`

```go
package core

import (
    "context"
    "time"
    "go.temporal.io/sdk/client"
)

// WorkflowExecutor defines the interface for executing workflows
type WorkflowExecutor interface {
    ExecuteWorkflow(ctx context.Context, workflowName string, input interface{}) (WorkflowRun, error)
}

// ActivityExecutor defines the interface for executing activities
type ActivityExecutor interface {
    ExecuteActivity(ctx context.Context, activityName string, input interface{}) (interface{}, error)
}

// WorkflowRun represents a running workflow
type WorkflowRun interface {
    GetID() string
    GetRunID() string
    Get(ctx context.Context, valuePtr interface{}) error
}

// WorkerManager manages workers for different task queues
type WorkerManager interface {
    StartWorker(taskQueue string) error
    StopWorker(taskQueue string) error
    RegisterWorkflow(taskQueue string, workflow interface{}) error
    RegisterActivity(taskQueue string, activity interface{}) error
}
```

#### `internal/temporal/core/options.go`

```go
package core

import "time"

// WorkflowOptions contains options for workflow execution
type WorkflowOptions struct {
    TaskQueue        string
    ExecutionTimeout time.Duration
    WorkflowID       string
    RetryPolicy      *RetryPolicy
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
    InitialInterval        time.Duration
    BackoffCoefficient     float64
    MaximumInterval        time.Duration
    MaximumAttempts        int32
    NonRetryableErrorTypes []string
}

// DefaultWorkflowOptions returns default options
func DefaultWorkflowOptions() *WorkflowOptions {
    return &WorkflowOptions{
        ExecutionTimeout: time.Hour,
        RetryPolicy:      DefaultRetryPolicy(),
    }
}
```

### **Service Layer**

#### `internal/temporal/service/service.go`

```go
package service

import (
    "context"
    "github.com/flexprice/flexprice/internal/temporal/core"
    "github.com/flexprice/flexprice/internal/types"
)

// TemporalService provides a centralized interface for all Temporal operations
type TemporalService struct {
    client        *core.TemporalClient
    workerManager core.WorkerManager
    logger        *logger.Logger
}

// NewTemporalService creates a new temporal service
func NewTemporalService(client *core.TemporalClient, workerManager core.WorkerManager, logger *logger.Logger) *TemporalService {
    return &TemporalService{
        client:        client,
        workerManager: workerManager,
        logger:        logger,
    }
}

// ExecuteWorkflow executes a workflow
func (s *TemporalService) ExecuteWorkflow(ctx context.Context, workflowName types.TemporalWorkflowType, input interface{}) (core.WorkflowRun, error) {
    // Implementation
}
```

### **Workflow Layer**

#### `internal/temporal/workflows/task/workflow.go`

```go
package task

import (
    "time"
    "github.com/flexprice/flexprice/internal/temporal/core"
    "go.temporal.io/sdk/workflow"
)

// TaskProcessingWorkflow processes a task asynchronously
func TaskProcessingWorkflow(ctx workflow.Context, input TaskProcessingWorkflowInput) (*TaskProcessingWorkflowResult, error) {
    // Implementation
}
```

#### `internal/temporal/workflows/task/models.go`

```go
package task

import (
    "time"
    ierr "github.com/flexprice/flexprice/internal/errors"
)

// TaskProcessingWorkflowInput represents the input for task processing workflow
type TaskProcessingWorkflowInput struct {
    TaskID        string `json:"task_id"`
    TenantID      string `json:"tenant_id"`
    EnvironmentID string `json:"environment_id"`
}

// Validate validates the input
func (i *TaskProcessingWorkflowInput) Validate() error {
    if i.TaskID == "" {
        return ierr.NewError("task_id is required").
            WithHint("Task ID is required").
            Mark(ierr.ErrValidation)
    }
    // More validations
    return nil
}
```

### **Activity Layer**

#### `internal/temporal/activities/task/activities.go`

```go
package task

import (
    "context"
    "github.com/flexprice/flexprice/internal/service"
    ierr "github.com/flexprice/flexprice/internal/errors"
)

// TaskActivities contains all task-related activities
type TaskActivities struct {
    taskService service.TaskService
}

// NewTaskActivities creates a new TaskActivities instance
func NewTaskActivities(taskService service.TaskService) *TaskActivities {
    return &TaskActivities{
        taskService: taskService,
    }
}

// ProcessTask processes a task asynchronously
func (a *TaskActivities) ProcessTask(ctx context.Context, input ProcessTaskActivityInput) (*ProcessTaskActivityResult, error) {
    // Implementation
}
```

### **Registry Layer**

#### `internal/temporal/registry/registry.go`

```go
package registry

import (
    "github.com/flexprice/flexprice/internal/service"
    "github.com/flexprice/flexprice/internal/temporal/core"
    "github.com/flexprice/flexprice/internal/temporal/workflows/task"
    "github.com/flexprice/flexprice/internal/temporal/activities/task"
)

// Registry manages workflow and activity registration
type Registry struct {
    service       *core.TemporalService
    workerManager core.WorkerManager
}

// NewRegistry creates a new registry
func NewRegistry(service *core.TemporalService, workerManager core.WorkerManager) *Registry {
    return &Registry{
        service:       service,
        workerManager: workerManager,
    }
}

// RegisterAll registers all workflows and activities
func (r *Registry) RegisterAll(params service.ServiceParams) error {
    // Register task workflows and activities
    if err := r.registerTaskWorkflows(params); err != nil {
        return err
    }

    // Register plan workflows and activities
    if err := r.registerPlanWorkflows(params); err != nil {
        return err
    }

    return nil
}

func (r *Registry) registerTaskWorkflows(params service.ServiceParams) error {
    // Create task service and activities
    taskService := service.NewTaskService(params)
    taskActivities := task.NewTaskActivities(taskService)

    // Register workflow
    if err := r.workerManager.RegisterWorkflow(
        "TaskProcessingWorkflowTaskQueue",
        task.TaskProcessingWorkflow,
    ); err != nil {
        return err
    }

    // Register activity
    if err := r.workerManager.RegisterActivity(
        "TaskProcessingWorkflowTaskQueue",
        taskActivities.ProcessTask,
    ); err != nil {
        return err
    }

    return nil
}
```

## 🚀 Migration Strategy

### **Phase 1: Create New Structure**

1. Create new directory structure
2. Move core functionality to `core/` package
3. Move service functionality to `service/` package

### **Phase 2: Move Workflows and Activities**

1. Create `workflows/task/` and `workflows/plan/` packages
2. Move existing workflows to appropriate packages
3. Create `activities/task/` and `activities/plan/` packages
4. Move existing activities to appropriate packages

### **Phase 3: Create Registry**

1. Create `registry/` package
2. Move registration logic from `registration.go`
3. Update main.go to use new registry

### **Phase 4: Update Imports**

1. Update all import statements
2. Remove old files
3. Test the new structure

## ✅ Benefits

1. **No Circular Dependencies**: Clear dependency flow
2. **Easy to Test**: Each layer can be tested independently
3. **Easy to Extend**: Add new workflows by creating new packages
4. **Clear Separation**: Each package has a single responsibility
5. **Better Organization**: Related code is grouped together
6. **Maintainable**: Easy to understand and modify

## 🎯 Implementation Plan

1. **Create new directory structure**
2. **Move core files first** (no dependencies)
3. **Move service files** (depends on core)
4. **Move workflow/activity files** (depends on core + service)
5. **Create registry** (depends on all)
6. **Update main.go** to use new structure
7. **Remove old files**
8. **Test everything works**

This architecture provides a clean, maintainable, and extensible structure for the temporal system! 🚀
