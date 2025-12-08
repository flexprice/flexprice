# Workflow Implementation Using Settings System - Product Requirements Document

**Version:** 1.0  
**Status:** Draft  
**Last Updated:** 2024-12-19  
**Author:** Engineering Team  
**Reviewers:** Product, Engineering

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Architecture Overview](#architecture-overview)
3. [Workflow Definition Structure](#workflow-definition-structure)
4. [Key Design Decisions](#key-design-decisions)
5. [Workflow Steps](#workflow-steps)
6. [Triggering Workflows](#triggering-workflows)
7. [API Design](#api-design)
8. [Implementation Details](#implementation-details)
9. [Migration Path](#migration-path)

---

## Executive Summary

This PRD outlines how to implement the Workflow Orchestration System by leveraging the existing **type-safe Settings System**. Each workflow definition is stored as a setting with a unique key (e.g., `customer_onboarding_workflow`), automatically inheriting tenant × environment isolation, caching, and audit trails.

**Key Benefits:**

- ✅ Automatic tenant × environment isolation (built into settings)
- ✅ Type-safe workflow definitions
- ✅ Automatic caching and audit trails
- ✅ No new database tables required
- ✅ Leverages existing settings infrastructure

---

## Architecture Overview

### High-Level Design

```
┌─────────────────────────────────────────────────────────────┐
│                    Settings System                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Key: "customer_onboarding_workflow"                │   │
│  │  Value: {                                            │   │
│  │    "name": "Customer Onboarding",                    │   │
│  │    "steps": [...],                                  │   │
│  │    "enabled": true                                  │   │
│  │  }                                                   │   │
│  │  TenantID: <auto>                                    │   │
│  │  EnvironmentID: <auto>                               │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│              Workflow Execution Service                     │
│  - Query workflows by key prefix                            │
│  - Match triggers to events                                 │
│  - Execute via Temporal                                     │
└─────────────────────────────────────────────────────────────┘
```

### Key Insight

**Workflow Definition = Setting with Descriptive Key**

- **Setting Key**: `customer_onboarding_workflow`, `subscription_activation_workflow`, etc.
- **Setting Value**: Workflow definition JSON (steps, config, retry policy)
- **Tenant × Environment**: Automatically handled by settings system
- **Trigger Identification**: Can be derived from key or stored in value

---

## Workflow Definition Structure

### Setting Key Pattern

```
workflow_{workflow_name}_workflow

Examples:
- workflow_customer_onboarding_workflow
- workflow_subscription_activation_workflow
- workflow_trial_expiration_workflow
```

### Setting Value Structure

```json
{
  "id": "wf_123",
  "name": "Customer Onboarding",
  "description": "Automated onboarding for new customers",
  "enabled": true,
  "trigger": {
    "type": "event",
    "event_name": "customer.created"
  },
  "steps": [
    {
      "id": "step_1",
      "order": 1,
      "type": "assign_plan",
      "name": "Assign Starter Plan",
      "config": {
        "plan_id": "plan_starter",
        "currency": "USD"
      }
    },
    {
      "id": "step_2",
      "order": 2,
      "type": "create_wallet",
      "name": "Create Wallet",
      "config": {
        "currency": "USD",
        "initial_credits": 100
      },
      "depends_on": ["step_1"]
    }
  ],
  "retry_policy": {
    "max_attempts": 3,
    "initial_interval_seconds": 1,
    "backoff_coefficient": 2.0,
    "max_interval_seconds": 60
  },
  "timeout_seconds": 300
}
```

---

## Key Design Decisions

### 1. Tenant × Environment Isolation

**Decision**: Handled automatically by settings system

- Settings repository automatically filters by `tenant_id` and `environment_id` from context
- No additional isolation logic needed
- Each tenant × environment has independent workflow definitions

### 2. Trigger Identification

**Decision**: Store trigger info in setting value, query by key prefix

**Option A**: Trigger in setting value (Recommended)

```json
{
  "trigger": {
    "type": "event",
    "event_name": "customer.created"
  }
}
```

**Option B**: Derive from key (Alternative)

- Key pattern: `workflow_customer_created_workflow` → event `customer.created`
- Less flexible, harder to maintain

**Recommendation**: Use Option A for flexibility

### 3. Workflow Discovery

**Decision**: Query settings by key prefix `workflow_`

```go
// Repository method
ListSettingsByKeyPrefix(ctx, "workflow_") []*Setting
```

This returns all workflow definitions for the current tenant × environment.

### 4. Workflow Execution Tracking

**Decision**: Separate entity (not in settings)

- Workflow executions are separate entities (temporal workflows, execution logs)
- Settings only store definitions, not execution state
- Execution tracking in separate `workflow_executions` table

---

## Workflow Steps

### Step Types (Phase 1)

1. **assign_plan**: Assign a plan to a customer
2. **create_wallet**: Create a wallet for a customer
3. **grant_credits**: Grant credits to a wallet
4. **send_email**: Send an email notification
5. **create_entitlement**: Create an entitlement for a customer

### Step Structure

```json
{
  "id": "step_1",
  "order": 1,
  "type": "assign_plan",
  "name": "Assign Starter Plan",
  "config": {
    "plan_id": "plan_starter",
    "currency": "USD",
    "billing_cycle": "monthly"
  },
  "condition": {
    "field": "customer.tier",
    "operator": "eq",
    "value": "premium"
  },
  "retry_policy": {
    "max_attempts": 3,
    "initial_interval_seconds": 1
  },
  "depends_on": []
}
```

### Step Configuration by Type

#### assign_plan

```json
{
  "plan_id": "plan_starter",
  "currency": "USD",
  "billing_cycle": "monthly",
  "start_date": "immediate" | "next_billing_cycle"
}
```

#### create_wallet

```json
{
  "currency": "USD",
  "conversion_rate": 1.0,
  "initial_credits": 100
}
```

#### grant_credits

```json
{
  "amount": 100,
  "currency": "USD",
  "reason": "welcome_bonus",
  "expires_at": "2024-12-31T23:59:59Z"
}
```

#### send_email

```json
{
  "template_id": "welcome_email",
  "to": "{{customer.email}}",
  "subject": "Welcome to FlexPrice",
  "variables": {
    "customer_name": "{{customer.name}}"
  }
}
```

#### create_entitlement

```json
{
  "feature_id": "feature_premium_support",
  "entity_type": "customer",
  "entity_id": "{{customer.id}}",
  "granted": true
}
```

### Step Dependencies

- Steps can depend on other steps using `depends_on` array
- Steps execute in order, respecting dependencies
- Circular dependencies are invalid (validation required)

### Step Conditions

- Steps can have optional conditions
- If condition fails, step is skipped
- Conditions use field path notation: `customer.tier`, `subscription.status`

---

## Triggering Workflows

### Event-Based Triggers

**Flow:**

1. Event occurs (e.g., `customer.created` webhook)
2. Workflow service queries all workflows with key prefix `workflow_`
3. For each workflow:
   - Check if `enabled: true`
   - Check if `trigger.type === "event"` and `trigger.event_name` matches
   - If match, start Temporal workflow execution

**Example:**

```go
// On customer.created event
workflows := workflowService.ListWorkflows(ctx)
for _, workflow := range workflows {
    if workflow.Enabled &&
       workflow.Trigger.Type == "event" &&
       workflow.Trigger.EventName == "customer.created" {
        // Start execution
        workflowService.Execute(ctx, workflow, eventData)
    }
}
```

### Manual Triggers

**Flow:**

1. User calls `POST /api/v1/workflows/{workflow_key}/trigger`
2. System looks up workflow by key
3. Validates tenant × environment access
4. Starts Temporal workflow execution

### Scheduled Triggers

**Flow:**

1. Cron job runs periodically
2. Queries all workflows with `trigger.type === "scheduled"`
3. Checks if schedule matches current time
4. Starts Temporal workflow execution

---

## API Design

### Create Workflow Definition

```http
POST /api/v1/workflows/definitions
Content-Type: application/json

{
  "key": "customer_onboarding_workflow",
  "name": "Customer Onboarding",
  "description": "Automated onboarding flow",
  "enabled": true,
  "trigger": {
    "type": "event",
    "event_name": "customer.created"
  },
  "steps": [
    {
      "id": "step_1",
      "order": 1,
      "type": "assign_plan",
      "name": "Assign Plan",
      "config": {
        "plan_id": "plan_starter"
      }
    }
  ]
}
```

**Implementation**: Creates a setting with key `workflow_customer_onboarding_workflow`

### Get Workflow Definition

```http
GET /api/v1/workflows/definitions/{workflow_key}
```

**Implementation**: Gets setting by key `workflow_{workflow_key}_workflow`

### List Workflow Definitions

```http
GET /api/v1/workflows/definitions?enabled=true
```

**Implementation**:

1. Query settings by key prefix `workflow_`
2. Filter by `enabled` if specified
3. Convert setting values to workflow definitions

### Update Workflow Definition

```http
PUT /api/v1/workflows/definitions/{workflow_key}
Content-Type: application/json

{
  "steps": [...],
  "enabled": false
}
```

**Implementation**: Updates setting value (partial update supported)

### Delete Workflow Definition

```http
DELETE /api/v1/workflows/definitions/{workflow_key}
```

**Implementation**: Deletes setting (archives it)

### Trigger Workflow (Manual)

```http
POST /api/v1/workflows/definitions/{workflow_key}/trigger
Content-Type: application/json

{
  "trigger_data": {
    "customer_id": "cust_123"
  }
}
```

---

## Implementation Details

### 1. Type Definitions

**File**: `internal/types/workflow.go`

```go
package types

// WorkflowDefinition represents a workflow stored as a setting value
type WorkflowDefinition struct {
    ID          string         `json:"id"`
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Enabled     bool           `json:"enabled"`
    Trigger     WorkflowTrigger `json:"trigger"`
    Steps       []WorkflowStep  `json:"steps"`
    RetryPolicy *RetryPolicy   `json:"retry_policy,omitempty"`
    Timeout     int            `json:"timeout_seconds,omitempty"`
}

// WorkflowTrigger defines when workflow executes
type WorkflowTrigger struct {
    Type       TriggerType   `json:"type"`
    EventName  string        `json:"event_name,omitempty"`
    Conditions []Condition   `json:"conditions,omitempty"`
    Schedule   *Schedule     `json:"schedule,omitempty"`
}

// WorkflowStep defines a single step
type WorkflowStep struct {
    ID          string                 `json:"id"`
    Order       int                    `json:"order"`
    Type        StepType               `json:"type"`
    Name        string                 `json:"name"`
    Config      map[string]interface{} `json:"config"`
    Condition   *Condition             `json:"condition,omitempty"`
    RetryPolicy *RetryPolicy           `json:"retry_policy,omitempty"`
    DependsOn   []string               `json:"depends_on,omitempty"`
}

// Enums
type TriggerType string
type StepType string
type Condition struct { ... }
type RetryPolicy struct { ... }
type Schedule struct { ... }
```

### 2. Repository Extensions

**File**: `internal/repository/ent/settings.go`

```go
// ListSettingsByKeyPrefix returns all settings matching a key prefix
func (r *settingsRepository) ListSettingsByKeyPrefix(
    ctx context.Context,
    prefix string,
) ([]*domainSettings.Setting, error) {
    client := r.client.Reader(ctx)

    settings, err := client.Settings.Query().
        Where(
            settings.KeyHasPrefix(prefix),
            settings.TenantID(types.GetTenantID(ctx)),
            settings.EnvironmentID(types.GetEnvironmentID(ctx)),
            settings.Status(string(types.StatusPublished)),
        ).
        All(ctx)

    if err != nil {
        return nil, ierr.WithError(err).
            WithHintf("Failed to list settings with prefix %s", prefix).
            Mark(ierr.ErrDatabase)
    }

    return domainSettings.FromEntList(settings), nil
}
```

**File**: `internal/domain/settings/repository.go`

```go
type Repository interface {
    // ... existing methods ...

    // List settings by key prefix
    ListSettingsByKeyPrefix(ctx context.Context, prefix string) ([]*Setting, error)
}
```

### 3. Workflow Service

**File**: `internal/service/workflow.go`

```go
package service

type WorkflowService interface {
    // CRUD operations
    CreateWorkflow(ctx context.Context, req *dto.CreateWorkflowRequest) (*dto.WorkflowResponse, error)
    GetWorkflow(ctx context.Context, key string) (*dto.WorkflowResponse, error)
    ListWorkflows(ctx context.Context, filter *dto.WorkflowFilter) ([]*dto.WorkflowResponse, error)
    UpdateWorkflow(ctx context.Context, key string, req *dto.UpdateWorkflowRequest) (*dto.WorkflowResponse, error)
    DeleteWorkflow(ctx context.Context, key string) error

    // Execution
    TriggerWorkflow(ctx context.Context, key string, triggerData map[string]interface{}) (*dto.WorkflowExecutionResponse, error)
    GetExecution(ctx context.Context, executionID string) (*dto.WorkflowExecutionResponse, error)

    // Event handling
    HandleEvent(ctx context.Context, eventName string, eventData map[string]interface{}) error
}

type workflowService struct {
    ServiceParams
    settingsService SettingsService
}

func NewWorkflowService(params ServiceParams) WorkflowService {
    return &workflowService{
        ServiceParams:   params,
        settingsService: NewSettingsService(params),
    }
}

// ListWorkflows queries settings by prefix "workflow_"
func (s *workflowService) ListWorkflows(ctx context.Context, filter *dto.WorkflowFilter) ([]*dto.WorkflowResponse, error) {
    // Get all workflow settings
    settings, err := s.SettingsRepo.ListSettingsByKeyPrefix(ctx, "workflow_")
    if err != nil {
        return nil, err
    }

    // Convert to workflow definitions
    workflows := make([]*dto.WorkflowResponse, 0)
    for _, setting := range settings {
        var def types.WorkflowDefinition
        if err := json.Unmarshal(setting.Value, &def); err != nil {
            continue // Skip invalid workflows
        }

        // Apply filters
        if filter != nil {
            if filter.Enabled != nil && def.Enabled != *filter.Enabled {
                continue
            }
            if filter.TriggerType != "" && def.Trigger.Type != types.TriggerType(filter.TriggerType) {
                continue
            }
        }

        workflows = append(workflows, &dto.WorkflowResponse{
            Key:     setting.Key,
            Setting: setting,
            Definition: def,
        })
    }

    return workflows, nil
}

// HandleEvent processes events and triggers matching workflows
func (s *workflowService) HandleEvent(ctx context.Context, eventName string, eventData map[string]interface{}) error {
    // Get all enabled workflows
    workflows, err := s.ListWorkflows(ctx, &dto.WorkflowFilter{Enabled: boolPtr(true)})
    if err != nil {
        return err
    }

    // Find matching workflows
    for _, workflow := range workflows {
        if workflow.Definition.Trigger.Type == types.TriggerTypeEvent &&
           workflow.Definition.Trigger.EventName == eventName {
            // Trigger execution
            _, err := s.TriggerWorkflow(ctx, workflow.Key, eventData)
            if err != nil {
                // Log error but continue processing other workflows
                s.Log.Errorw("Failed to trigger workflow", "error", err, "workflow", workflow.Key)
            }
        }
    }

    return nil
}
```

### 4. Key Generation Helper

**File**: `internal/types/workflow.go`

```go
package types

const WorkflowKeyPrefix = "workflow_"
const WorkflowKeySuffix = "_workflow"

// GenerateWorkflowKey creates a setting key from workflow name
// Example: "Customer Onboarding" -> "workflow_customer_onboarding_workflow"
func GenerateWorkflowKey(name string) string {
    // Normalize: lowercase, replace spaces/hyphens with underscores
    normalized := strings.ToLower(strings.TrimSpace(name))
    normalized = strings.ReplaceAll(normalized, " ", "_")
    normalized = strings.ReplaceAll(normalized, "-", "_")

    // Remove non-alphanumeric except underscores
    var result strings.Builder
    for _, r := range normalized {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
            result.WriteRune(r)
        }
    }

    key := result.String()
    if !strings.HasPrefix(key, WorkflowKeyPrefix) {
        key = WorkflowKeyPrefix + key
    }
    if !strings.HasSuffix(key, WorkflowKeySuffix) {
        key = key + WorkflowKeySuffix
    }

    return key
}

// IsWorkflowKey checks if a key is a workflow key
func IsWorkflowKey(key string) bool {
    return strings.HasPrefix(key, WorkflowKeyPrefix) &&
           strings.HasSuffix(key, WorkflowKeySuffix)
}
```

### 5. Validation

**File**: `internal/types/workflow.go`

```go
// ValidateWorkflowDefinition validates a workflow definition
func ValidateWorkflowDefinition(def *WorkflowDefinition) error {
    if def.Name == "" {
        return errors.New("workflow name is required")
    }

    if len(def.Steps) == 0 {
        return errors.New("workflow must have at least one step")
    }

    // Validate trigger
    if err := validateTrigger(def.Trigger); err != nil {
        return err
    }

    // Validate steps
    stepIDs := make(map[string]bool)
    for i, step := range def.Steps {
        if err := validateStep(step, i, stepIDs); err != nil {
            return err
        }
        stepIDs[step.ID] = true
    }

    // Check for circular dependencies
    if err := checkCircularDependencies(def.Steps); err != nil {
        return err
    }

    return nil
}

func validateTrigger(trigger WorkflowTrigger) error {
    switch trigger.Type {
    case TriggerTypeEvent:
        if trigger.EventName == "" {
            return errors.New("event_name is required for event triggers")
        }
    case TriggerTypeScheduled:
        if trigger.Schedule == nil || trigger.Schedule.CronExpression == "" {
            return errors.New("schedule is required for scheduled triggers")
        }
    }
    return nil
}

func validateStep(step WorkflowStep, index int, stepIDs map[string]bool) error {
    if step.ID == "" {
        return fmt.Errorf("step %d: id is required", index)
    }

    if stepIDs[step.ID] {
        return fmt.Errorf("step %d: duplicate step id '%s'", index, step.ID)
    }

    if step.Type == "" {
        return fmt.Errorf("step %d: type is required", index)
    }

    if step.Order < 1 {
        return fmt.Errorf("step %d: order must be >= 1", index)
    }

    // Validate step type
    validTypes := []StepType{
        StepTypeAssignPlan,
        StepTypeCreateWallet,
        StepTypeGrantCredits,
        StepTypeSendEmail,
        StepTypeCreateEntitlement,
    }

    valid := false
    for _, validType := range validTypes {
        if step.Type == validType {
            valid = true
            break
        }
    }

    if !valid {
        return fmt.Errorf("step %d: invalid step type '%s'", index, step.Type)
    }

    return nil
}

func checkCircularDependencies(steps []WorkflowStep) error {
    // Build dependency graph
    graph := make(map[string][]string)
    for _, step := range steps {
        graph[step.ID] = step.DependsOn
    }

    // Check for cycles using DFS
    visited := make(map[string]bool)
    recStack := make(map[string]bool)

    var dfs func(string) bool
    dfs = func(node string) bool {
        visited[node] = true
        recStack[node] = true

        for _, dep := range graph[node] {
            if !visited[dep] {
                if dfs(dep) {
                    return true
                }
            } else if recStack[dep] {
                return true // Cycle detected
            }
        }

        recStack[node] = false
        return false
    }

    for _, step := range steps {
        if !visited[step.ID] {
            if dfs(step.ID) {
                return errors.New("circular dependency detected in workflow steps")
            }
        }
    }

    return nil
}
```

---

## Migration Path

### Phase 1: Foundation (Week 1-2)

1. Add workflow type definitions
2. Extend repository with `ListSettingsByKeyPrefix`
3. Add key generation helpers
4. Add validation functions

### Phase 2: Service Layer (Week 3-4)

1. Implement `WorkflowService`
2. Implement CRUD operations
3. Implement event handling
4. Add DTOs and API handlers

### Phase 3: Temporal Integration (Week 5-6)

1. Create Temporal workflow definitions
2. Implement activity functions
3. Add execution tracking
4. Implement retry logic

### Phase 4: Testing & Documentation (Week 7-8)

1. Unit tests
2. Integration tests
3. API documentation
4. User guide

---

## Summary

This PRD outlines how to implement workflows using the existing settings system:

1. **Workflow = Setting**: Each workflow is a setting with key `workflow_{name}_workflow`
2. **Automatic Isolation**: Tenant × environment handled by settings
3. **Trigger in Value**: Trigger info stored in setting value
4. **Query by Prefix**: List workflows using `ListSettingsByKeyPrefix("workflow_")`
5. **Type Safety**: Workflow definitions are validated and type-safe

This approach maximizes code reuse and minimizes new infrastructure while providing a robust workflow system.
