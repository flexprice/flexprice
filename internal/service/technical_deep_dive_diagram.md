# Temporal Architecture Redesign

## Current Issues

- Tight coupling between services and temporal implementation
- Repetitive code for tenant/environment validation
- Direct temporal dependencies in multiple layers
- Complex workflow registration process
- Lack of clear separation between workflow and business logic

## Proposed Architecture

### Directory Structure

```
internal/temporal/
├── client/
│   ├── interface.go         # Core temporal client interface
│   ├── client.go           # Implementation of temporal client
│   ├── worker.go           # Temporal worker implementation
│   └── options.go          # Common workflow/activity options
├── models/
│   ├── common.go           # Shared models/types
│   ├── workflow_inputs.go  # Workflow input DTOs
│   └── workflow_outputs.go # Workflow output DTOs
├── activities/
│   ├── interfaces/         # Activity interfaces
│   │   └── activity_interfaces.go
│   ├── base_activity.go   # Common activity functionality
│   └── implementations/   # Concrete activity implementations
│       ├── plan_activities.go
│       ├── billing_activities.go
│       └── task_activities.go
├── workflows/
│   ├── interfaces/        # Workflow interfaces
│   │   └── workflow_interfaces.go
│   ├── base_workflow.go  # Common workflow functionality
│   └── implementations/  # Concrete workflow implementations
│       ├── plan_workflow.go
│       ├── billing_workflow.go
│       └── task_workflow.go
└── registration/
    ├── registry.go       # Workflow/Activity registration logic
    └── worker_init.go    # Worker initialization and setup
```

### Core Interfaces

```go
// client/interface.go
type TemporalClient interface {
    // Core execution methods
    ExecuteWorkflow(ctx context.Context, workflowName string, input interface{}) (WorkflowRun, error)
    ExecuteWorkflowWithOptions(ctx context.Context, workflowName string, input interface{}, options WorkflowOptions) (WorkflowRun, error)

    // Registration methods
    RegisterWorkflow(workflow interface{})
    RegisterActivity(activity interface{})

    // Helper methods
    WithTenantContext(ctx context.Context) context.Context
    ValidateTenantContext(ctx context.Context) error
}

type WorkflowRun interface {
    GetID() string
    Get(ctx context.Context, valuePtr interface{}) error
    GetWithTimeout(ctx context.Context, timeout time.Duration, valuePtr interface{}) error
}
```

### Key Improvements

1. **Dependency Injection**

```go
// Base activity with common functionality
type BaseActivity struct {
    tenantValidator TenantValidator
    logger         Logger
    metrics        MetricsCollector
}

// Activity implementations focus on business logic
type PlanActivity struct {
    *BaseActivity
    planService service.PlanService
}
```

2. **Context Management**

```go
// client/client.go
func (c *temporalClient) WithTenantContext(ctx context.Context) context.Context {
    tenantID := types.GetTenantID(ctx)
    envID := types.GetEnvironmentID(ctx)
    return workflow.WithValue(ctx, "tenant_context", TenantContext{
        TenantID: tenantID,
        EnvID: envID,
    })
}
```

3. **Workflow Registration**

```go
// registration/registry.go
type WorkflowRegistry struct {
    workflows map[string]interface{}
    activities map[string]interface{}
}

func (r *WorkflowRegistry) RegisterWorkflow(name string, workflow interface{}) {
    r.workflows[name] = workflow
}

func (r *WorkflowRegistry) InitializeWorker(worker worker.Worker) {
    for name, wf := range r.workflows {
        worker.RegisterWorkflow(wf)
    }
    // ... activity registration
}
```

### Usage Example

```go
// service/plan_service.go
type planService struct {
    temporal TemporalClient
    // ... other dependencies
}

func (s *planService) SyncPlanPrices(ctx context.Context, planID string) error {
    input := models.PlanSyncInput{
        PlanID: planID,
    }

    run, err := s.temporal.ExecuteWorkflow(ctx, "PlanSyncWorkflow", input)
    if err != nil {
        return err
    }

    var result models.PlanSyncOutput
    return run.Get(ctx, &result)
}
```

## Benefits

1. **Reduced Coupling**: Services only depend on the TemporalClient interface
2. **Centralized Context Management**: Common tenant/environment validation
3. **Simplified Testing**: Easy to mock TemporalClient interface
4. **Clear Separation of Concerns**:
   - Models: Data structures
   - Activities: Business logic
   - Workflows: Orchestration
   - Client: Temporal interaction
5. **Reusable Components**: Base implementations for common functionality
6. **Type Safety**: Strong typing for workflow inputs/outputs

## Migration Strategy

1. Create new directory structure and interfaces
2. Implement new TemporalClient
3. Create base activity/workflow implementations
4. Gradually migrate existing workflows to new structure
5. Update services to use new TemporalClient interface
6. Remove old temporal dependencies

## Monitoring & Observability

The new architecture makes it easier to add:

- Centralized error handling
- Metrics collection
- Logging
- Tracing
- Tenant context validation

## Security Considerations

- Tenant isolation is enforced at client level
- Environment validation is standardized
- Workflow execution permissions can be centrally managed
- Activity execution context is validated

## Future Improvements

1. Add workflow versioning support
2. Implement workflow retry policies
3. Add workflow timeout configurations
4. Create workflow testing utilities
5. Add workflow monitoring dashboards
