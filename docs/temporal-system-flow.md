# FlexPrice Temporal System Flow

This document provides a comprehensive overview of the FlexPrice Temporal system architecture, workflow execution flow, and developer guide.

## System Architecture Overview

```mermaid
graph TB
    subgraph "🌐 Client Layer"
        API[REST API Endpoints]
        CLI[CLI Commands]
        SDK[External SDKs]
    end

    subgraph "🎯 Handler Layer"
        PlanHandler[Plan Handler]
        TaskHandler[Task Handler]
        OtherHandlers[Other Handlers...]
    end

    subgraph "⚙️ Temporal Service Layer"
        TemporalService[TemporalService]
        WorkerManager[TemporalWorkerManager]
        Validation[Validation Layer]
    end

    subgraph "🔄 Temporal Core"
        TemporalClient[Temporal Client]
        WorkflowEngine[Workflow Engine]
        ActivityEngine[Activity Engine]
        TaskQueues[Task Queues]
    end

    subgraph "🏗️ Business Logic Layer"
        Workflows[Workflow Implementations]
        Activities[Activity Implementations]
        Services[Business Services]
    end

    subgraph "💾 Data Layer"
        Database[(PostgreSQL)]
        Cache[(Redis Cache)]
        Events[(Event Store)]
    end

    API --> PlanHandler
    API --> TaskHandler
    CLI --> PlanHandler
    SDK --> PlanHandler

    PlanHandler --> TemporalService
    TaskHandler --> TemporalService
    OtherHandlers --> TemporalService

    TemporalService --> WorkerManager
    TemporalService --> Validation
    TemporalService --> TemporalClient

    WorkerManager --> TaskQueues
    TemporalClient --> WorkflowEngine
    TemporalClient --> ActivityEngine

    WorkflowEngine --> Workflows
    ActivityEngine --> Activities
    Workflows --> Activities
    Activities --> Services

    Services --> Database
    Services --> Cache
    Services --> Events
```

## Detailed Workflow Execution Flow

```mermaid
sequenceDiagram
    participant Client as 🌐 Client
    participant API as 🎯 API Handler
    participant TS as ⚙️ TemporalService
    participant WM as 🔄 WorkerManager
    participant TC as 🔗 TemporalClient
    participant WE as 🏗️ Workflow Engine
    participant AE as 🏗️ Activity Engine
    participant BS as 💼 Business Service

    Note over Client, BS: Complete Workflow Execution Flow

    %% 1. Initialization Phase
    rect rgb(240, 248, 255)
        Note over TS, WM: 🚀 System Initialization
        TS->>TS: InitTemporalService()
        TS->>WM: NewTemporalWorkerManager()
        WM->>WM: Create Workers for Task Queues
        WM->>TC: Register Workflows & Activities
    end

    %% 2. HTTP Request Phase
    rect rgb(248, 255, 240)
        Note over Client, API: 🌐 HTTP Request Processing
        Client->>API: POST /plans/123/sync/subscriptions
        API->>API: Extract Plan ID & Context
        API->>API: Validate Request
    end

    %% 3. Workflow Execution Phase
    rect rgb(255, 248, 240)
        Note over API, WE: ⚙️ Workflow Execution
        API->>TS: ExecuteWorkflow(PriceSyncWorkflow, "123")
        TS->>TS: validateService()
        TS->>TS: validateNotNil(input)
        TS->>TS: processWorkflowInput()
        TS->>TC: ExecuteWorkflow(options, workflowName, input)
        TC->>WE: Start Workflow Execution
        WE->>WE: Create Workflow Instance
        WE->>WE: Generate Workflow ID
    end

    %% 4. Activity Execution Phase
    rect rgb(255, 240, 248)
        Note over WE, BS: 🏗️ Activity Execution
        WE->>AE: Execute Activity (SyncPlanPrices)
        AE->>AE: Create Activity Instance
        AE->>BS: Call Business Service Method
        BS->>BS: Execute Business Logic
        BS-->>AE: Return Result
        AE-->>WE: Activity Result
        WE-->>TC: Workflow Result
        TC-->>TS: Execution Complete
        TS-->>API: Workflow Started
        API-->>Client: 200 OK + Workflow ID
    end

    %% 5. Result Retrieval Phase
    rect rgb(248, 240, 255)
        Note over Client, BS: 📊 Result Retrieval
        Client->>API: GET /tasks/result?workflow_id=xyz
        API->>TS: GetWorkflowResult(workflowID, result)
        TS->>TC: Get Workflow Result
        TC->>WE: Query Workflow Status
        WE-->>TC: Workflow Result
        TC-->>TS: Result Data
        TS-->>API: Workflow Result
        API-->>Client: JSON Response
    end
```

## Workflow Types and Examples

```mermaid
graph TD
    subgraph "📋 Available Workflow Types"
        W1[TemporalTaskProcessingWorkflow<br/>📝 Process file uploads & data imports]
        W2[TemporalPriceSyncWorkflow<br/>💰 Sync plan prices across subscriptions]
        W3[TemporalBillingWorkflow<br/>💳 Execute billing cycles]
        W4[TemporalCalculationWorkflow<br/>🧮 Calculate charges and fees]
        W5[TemporalSubscriptionChangeWorkflow<br/>🔄 Handle subscription changes]
        W6[TemporalSubscriptionCreationWorkflow<br/>✨ Create new subscriptions]
        W7[TemporalTaskProcessingWithProgressWorkflow<br/>📊 Process tasks with progress updates]
    end

    subgraph "🎯 Task Queues"
        TQ1[TaskProcessingWorkflowTaskQueue]
        TQ2[PriceSyncWorkflowTaskQueue]
        TQ3[CronBillingWorkflowTaskQueue]
        TQ4[CalculateChargesWorkflowTaskQueue]
        TQ5[SubscriptionChangeWorkflowTaskQueue]
        TQ6[SubscriptionCreationWorkflowTaskQueue]
        TQ7[TaskProcessingWithProgressWorkflowTaskQueue]
    end

    W1 --> TQ1
    W2 --> TQ2
    W3 --> TQ3
    W4 --> TQ4
    W5 --> TQ5
    W6 --> TQ6
    W7 --> TQ7
```

## Detailed Code Examples

### 1. Starting a Workflow (API Handler)

```go
// Example: Plan Handler - Sync Plan Prices
func (h *PlanHandler) SyncPlanPrices(c *gin.Context) {
    id := c.Param("id")
    if id == "" {
        c.Error(ierr.NewError("plan ID is required").Mark(ierr.ErrValidation))
        return
    }

    // Execute the temporal workflow
    _, err := h.temporalService.ExecuteWorkflow(
        c.Request.Context(),
        types.TemporalPriceSyncWorkflow,
        id
    )
    if err != nil {
        c.Error(err)
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "price sync workflow started successfully"})
}
```

### 2. Workflow Implementation

```go
// Example: Price Sync Workflow
func PriceSyncWorkflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
    // Validate input
    if err := in.Validate(); err != nil {
        return nil, err
    }

    // Set up activity options with retry policy
    ao := workflow.ActivityOptions{
        StartToCloseTimeout: time.Minute * 30,
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval:    time.Second,
            BackoffCoefficient: 2.0,
            MaximumInterval:    time.Minute * 5,
            MaximumAttempts:    3,
        },
    }
    ctx = workflow.WithActivityOptions(ctx, ao)

    // Execute the activity
    var result dto.SyncPlanPricesResponse
    err := workflow.ExecuteActivity(ctx, "SyncPlanPrices", activities.SyncPlanPricesInput{
        PlanID:        in.PlanID,
        TenantID:      in.TenantID,
        EnvironmentID: in.EnvironmentID,
    }).Get(ctx, &result)

    if err != nil {
        return nil, err
    }

    return &result, nil
}
```

### 3. Activity Implementation

```go
// Example: Plan Activities
func (a *PlanActivities) SyncPlanPrices(ctx context.Context, input SyncPlanPricesInput) (*dto.SyncPlanPricesResponse, error) {
    // Validate input
    if input.PlanID == "" {
        return nil, ierr.NewError("plan ID is required").Mark(ierr.ErrValidation)
    }

    // Set context values
    ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
    ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)

    // Call business service
    result, err := a.planService.SyncPlanPrices(ctx, input.PlanID)
    if err != nil {
        return nil, err
    }

    return result, nil
}
```

## Developer Guide: How to Add New Workflows

### Step 1: Define Workflow Type

```go
// In internal/types/temporal.go
const (
    // Add your new workflow type
    TemporalMyNewWorkflow TemporalWorkflowType = "MyNewWorkflow"
)

// Add to allowed workflows list
func (w TemporalWorkflowType) Validate() error {
    allowedWorkflows := []TemporalWorkflowType{
        // ... existing workflows
        TemporalMyNewWorkflow, // Add your workflow here
    }
    // ... rest of validation
}
```

### Step 2: Create Workflow Implementation

```go
// In internal/temporal/workflows/my_new_workflow.go
package workflows

import (
    "time"
    "go.temporal.io/sdk/workflow"
    "go.temporal.io/sdk/temporal"
)

const (
    WorkflowMyNew = "MyNewWorkflow"
    ActivityMyNew = "MyNewActivity"
)

func MyNewWorkflow(ctx workflow.Context, input MyNewWorkflowInput) (*MyNewWorkflowResult, error) {
    // Set up activity options
    ao := workflow.ActivityOptions{
        StartToCloseTimeout: time.Minute * 10,
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval:    time.Second * 5,
            BackoffCoefficient: 2.0,
            MaximumInterval:    time.Minute * 2,
            MaximumAttempts:    3,
        },
    }
    ctx = workflow.WithActivityOptions(ctx, ao)

    // Execute activities
    var result MyNewWorkflowResult
    err := workflow.ExecuteActivity(ctx, ActivityMyNew, input).Get(ctx, &result)
    if err != nil {
        return nil, err
    }

    return &result, nil
}
```

### Step 3: Create Activity Implementation

```go
// In internal/temporal/activities/my_new_activities.go
package activities

import (
    "context"
    "github.com/flexprice/flexprice/internal/service"
)

type MyNewActivities struct {
    myService service.MyService
}

func NewMyNewActivities(myService service.MyService) *MyNewActivities {
    return &MyNewActivities{myService: myService}
}

func (a *MyNewActivities) MyNewActivity(ctx context.Context, input MyNewActivityInput) (*MyNewActivityResult, error) {
    // Implement your business logic here
    result, err := a.myService.DoSomething(ctx, input)
    if err != nil {
        return nil, err
    }

    return &MyNewActivityResult{
        // ... populate result fields
    }, nil
}
```

### Step 4: Register Workflow and Activity

```go
// In internal/temporal/registration.go
func RegisterWorkflowsAndActivities(w worker.Worker, params service.ServiceParams) {
    // Create your activity instance
    myService := service.NewMyService(params)
    myActivities := activities.NewMyNewActivities(myService)

    // Register workflow
    w.RegisterWorkflow(workflows.MyNewWorkflow)

    // Register activity
    w.RegisterActivity(myActivities.MyNewActivity)
}
```

### Step 5: Add API Handler

```go
// In internal/api/v1/my_handler.go
func (h *MyHandler) ExecuteMyWorkflow(c *gin.Context) {
    id := c.Param("id")
    if id == "" {
        c.Error(ierr.NewError("ID is required").Mark(ierr.ErrValidation))
        return
    }

    // Execute the workflow
    _, err := h.temporalService.ExecuteWorkflow(
        c.Request.Context(),
        types.TemporalMyNewWorkflow,
        id,
    )
    if err != nil {
        c.Error(err)
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "workflow started successfully"})
}
```

## Error Handling and Retry Policies

```mermaid
graph TD
    subgraph "🔄 Retry Policy Configuration"
        RP[Retry Policy]
        RP --> II[Initial Interval: 5s]
        RP --> BC[Backoff Coefficient: 2.0]
        RP --> MI[Maximum Interval: 5m]
        RP --> MA[Maximum Attempts: 3]
    end

    subgraph "❌ Error Types"
        RE[Retryable Errors]
        NRE[Non-Retryable Errors]
        RE --> DB[Database Connection Issues]
        RE --> NET[Network Timeouts]
        RE --> TEMP[Temporary Service Unavailable]
        NRE --> VAL[Validation Errors]
        NRE --> AUTH[Authentication Errors]
        NRE --> NOTF[Resource Not Found]
    end

    subgraph "🛡️ Error Handling Flow"
        E1[Error Occurs] --> E2{Is Retryable?}
        E2 -->|Yes| E3[Wait & Retry]
        E2 -->|No| E4[Fail Immediately]
        E3 --> E5{Max Attempts Reached?}
        E5 -->|No| E6[Execute Activity Again]
        E5 -->|Yes| E4
        E6 --> E1
    end
```

## Monitoring and Observability

```mermaid
graph LR
    subgraph "📊 Monitoring Stack"
        TS[Temporal Service] --> LOG[Structured Logging]
        TS --> METRICS[Prometheus Metrics]
        TS --> TRACES[Distributed Tracing]

        LOG --> ELK[ELK Stack]
        METRICS --> PROM[Prometheus]
        TRACES --> JAEGER[Jaeger]
    end

    subgraph "🔍 Key Metrics"
        WM[Workflow Metrics]
        AM[Activity Metrics]
        QM[Queue Metrics]

        WM --> WSUCCESS[Workflow Success Rate]
        WM --> WFAIL[Workflow Failure Rate]
        WM --> WDURATION[Workflow Duration]

        AM --> ASUCCESS[Activity Success Rate]
        AM --> AFAIL[Activity Failure Rate]
        AM --> ADURATION[Activity Duration]

        QM --> QSIZE[Queue Size]
        QM --> QLATENCY[Queue Latency]
    end
```

## Best Practices

### 1. Workflow Design

- Keep workflows deterministic (no random numbers, current time, etc.)
- Use activities for all external operations
- Design for idempotency
- Handle failures gracefully

### 2. Activity Design

- Keep activities short and focused
- Use appropriate timeouts
- Implement proper error handling
- Make activities idempotent

### 3. Error Handling

- Use retry policies appropriately
- Distinguish between retryable and non-retryable errors
- Log errors with sufficient context
- Implement circuit breakers for external services

### 4. Performance

- Use appropriate task queue partitioning
- Monitor workflow and activity performance
- Implement proper backpressure handling
- Use batch operations where possible

## Troubleshooting Guide

### Common Issues

1. **Workflow Not Starting**

   - Check if Temporal service is initialized
   - Verify workflow is registered
   - Check task queue configuration

2. **Activity Failures**

   - Check activity registration
   - Verify input validation
   - Review retry policy configuration

3. **Context Issues**

   - Ensure tenant and environment IDs are set
   - Check context propagation
   - Verify authentication

4. **Performance Issues**
   - Monitor task queue sizes
   - Check activity execution times
   - Review retry policies

### Debug Commands

```bash
# Check workflow status
temporal workflow describe --workflow-id <workflow-id>

# List running workflows
temporal workflow list

# Check task queue status
temporal task-queue describe --task-queue <queue-name>

# View workflow history
temporal workflow show --workflow-id <workflow-id>
```

This comprehensive guide should help developers understand and work with the FlexPrice Temporal system effectively!
