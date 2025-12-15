# Workflow Orchestration System - Product Requirements Document

**Version:** 1.0  
**Status:** Draft  
**Last Updated:** 2024-12-19  
**Author:** Engineering Team  
**Reviewers:** Product, Engineering, Security

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Problem Statement](#problem-statement)
3. [Goals & Objectives](#goals--objectives)
4. [Architecture Overview](#architecture-overview)
5. [Data Models](#data-models)
6. [Use Cases](#use-cases)
7. [Error Handling](#error-handling)
8. [API Design](#api-design)
9. [Security & Isolation](#security--isolation)
10. [Implementation Phases](#implementation-phases)

--- 

## Executive Summary

The Workflow Orchestration System enables Flexprice tenants to configure and execute automated workflows triggered by business events (e.g., customer creation, subscription activation). The system provides reliable, scalable automation for multi-step business processes while maintaining strict tenant and environment isolation.

**Key Capabilities:**

- Tenant-configurable workflow definitions
- Event-driven workflow triggers
- Temporal-based reliable execution with automatic retries
- Multi-step orchestration with conditional logic
- Full audit trail and observability

---

## Problem Statement

### Current State

- Manual onboarding requires multiple sequential API calls
- No way to automate multi-step processes
- Each tenant has different requirements but no configuration mechanism
- No reliability guarantees for long-running processes
- Difficult to track and debug complex flows

### Desired State

- Tenants configure workflows via API
- Workflows execute automatically on events
- Reliable execution with automatic retries
- Full visibility into execution status
- Extensible to support future use cases

---

## Goals & Objectives

### Primary Goals

1. **Enable Tenant-Configurable Workflows**: Allow tenants to define custom workflows without code changes
2. **Ensure Reliability**: Guarantee workflow execution with retry logic
3. **Maintain Isolation**: Strict tenant × environment isolation
4. **Provide Observability**: Complete audit trail and monitoring

### Success Metrics

- **Adoption**: 80% of tenants using workflows within 6 months
- **Reliability**: 99.9% workflow completion rate
- **Performance**: <5s workflow trigger latency, <30s average completion

### Non-Goals (Phase 1)

- Visual workflow builder UI
- Workflow versioning and rollback
- Workflow templates marketplace
- Parallel execution of steps

---

## Architecture Overview

### High-Level Architecture

```
API Layer (Gin Router)
    │
    ▼
Workflow Service Layer
    ├─> WorkflowDefinitionService (CRUD)
    ├─> WorkflowExecutionService (Trigger & Monitor)
    └─> WorkflowValidationService
    │        
    ├─> Event System (Webhooks)
    └─> Temporal Workflow Engine
            │
            ▼
    Activity Layer
    ├─> AssignPlanActivity
    ├─> CreateWalletActivity
    ├─> GrantCreditsActivity
    ├─> SendEmailActivity
    └─> CreateEntitlementActivity
            │
            ▼
    Domain Services (Existing)
```

### Execution Flow

1. Event occurs (e.g., `customer.created`)
2. Webhook handler receives event
3. System queries active workflows for tenant × environment
4. For each matching workflow:
   - Validate workflow is enabled
   - Check trigger conditions
   - Start Temporal workflow
5. Temporal workflow executes steps sequentially:
   - Step 1: AssignPlan → SubscriptionService
   - Step 2: CreateWallet → WalletService
   - Step 3: GrantCredits → WalletService
   - Step N: SendEmail → EmailService

---

## Data Models

### Workflow Definition

```go
type WorkflowDefinition struct {
    ID            string    `json:"id"`
    Name          string    `json:"name"`
    Description   string    `json:"description"`
    TenantID      string    `json:"tenant_id"`
    EnvironmentID string    `json:"environment_id"`

    Trigger       WorkflowTrigger `json:"trigger"`
    Steps         []WorkflowStep  `json:"steps"`

    Enabled       bool        `json:"enabled"`
    RetryPolicy   RetryPolicy `json:"retry_policy"`
    Timeout       int         `json:"timeout_seconds"`

    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
    CreatedBy     string    `json:"created_by"`
    Status        types.Status `json:"status"`
}
```

### Workflow Trigger
 
```go
type WorkflowTrigger struct {
    Type        TriggerType `json:"type"` // event, scheduled, manual, api
    EventName   string      `json:"event_name,omitempty"` // e.g., "customer.created"
    Conditions  []Condition `json:"conditions,omitempty"` // Optional filters
    Schedule    *Schedule   `json:"schedule,omitempty"` // Cron expression
}
```

### Workflow Step

```go
type WorkflowStep struct {
    ID          string    `json:"id"`
    Order       int       `json:"order"`
    Type        StepType  `json:"type"` // assign_plan, create_wallet, etc.
    Name        string    `json:"name"`
    Config      map[string]interface{} `json:"config"`
    Condition   *Condition `json:"condition,omitempty"`
    RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`
    DependsOn   []string   `json:"depends_on,omitempty"` // Step IDs
}
```

### Workflow Execution

```go
type WorkflowExecution struct {
    ID              string    `json:"id"`
    WorkflowID      string    `json:"workflow_id"`
    WorkflowName    string    `json:"workflow_name"`
    TenantID        string    `json:"tenant_id"`
    EnvironmentID   string    `json:"environment_id"`

    TriggerType     TriggerType `json:"trigger_type"`
    TriggerEvent    map[string]interface{} `json:"trigger_event"`

    Status          ExecutionStatus `json:"status"`
    StartedAt       *time.Time `json:"started_at"`
    CompletedAt     *time.Time `json:"completed_at"`

    TemporalWorkflowID string `json:"temporal_workflow_id"`
    StepExecutions  []StepExecution `json:"step_executions"`
    Context         map[string]interface{} `json:"context"`
    Error           *ExecutionError `json:"error,omitempty"`
}
```

### Enums

```go
type TriggerType string
const (
    TriggerTypeEvent     TriggerType = "event"
    TriggerTypeScheduled TriggerType = "scheduled"
    TriggerTypeManual    TriggerType = "manual"
    TriggerTypeAPI       TriggerType = "api"
)

type StepType string
const (
    StepTypeAssignPlan      StepType = "assign_plan"
    StepTypeCreateWallet    StepType = "create_wallet"
    StepTypeGrantCredits    StepType = "grant_credits"
    StepTypeSendEmail       StepType = "send_email"
    StepTypeCreateEntitlement StepType = "create_entitlement"
)

type ExecutionStatus string
const (
    ExecutionStatusPending   ExecutionStatus = "pending"
    ExecutionStatusRunning   ExecutionStatus = "running"
    ExecutionStatusCompleted ExecutionStatus = "completed"
    ExecutionStatusFailed    ExecutionStatus = "failed"
    ExecutionStatusCancelled ExecutionStatus = "cancelled"
)

type RetryPolicy struct {
    MaxAttempts       int     `json:"max_attempts"` // Default: 3
    InitialInterval   int     `json:"initial_interval_seconds"` // Default: 1
    BackoffCoefficient float64 `json:"backoff_coefficient"` // Default: 2.0
    MaxInterval       int     `json:"max_interval_seconds"` // Default: 60
}
```

---

## Use Cases

### UC-1: Configure Customer Onboarding Workflow

**Actor**: Tenant Admin

**Flow**:

1. User calls `POST /api/v1/workflows/definitions`
2. Provides workflow configuration:
   ```json
   {
     "name": "Customer Onboarding - Standard",
     "trigger": {
       "type": "event",
       "event_name": "customer.created"
     },
     "steps": [
       {
         "id": "step_1",
         "order": 1,
         "type": "assign_plan",
         "config": {
           "plan_id": "plan_starter",
           "currency": "USD"
         }
       },
       {
         "id": "step_2",
         "order": 2,
         "type": "create_wallet",
         "config": {
           "currency": "USD",
           "conversion_rate": 1.0,
           "initial_credits": 100
         },
         "depends_on": ["step_1"]
       }
     ],
     "enabled": true
   }
   ```
3. System validates workflow definition
4. System creates workflow definition
5. Returns workflow definition with ID

---

### UC-2: Event Triggers Workflow

**Actor**: System

**Flow**:

1. Customer is created via API
2. System publishes `customer.created` webhook event
3. System queries active workflows for tenant × environment
4. For each matching workflow:
   - Validates trigger conditions
   - Creates WorkflowExecution record
   - Starts Temporal workflow
5. Temporal workflow executes steps sequentially
6. Updates execution status on completion/failure

---

### UC-3: Query Workflow Execution Status

**Actor**: Tenant Admin

**Flow**:

1. User calls `GET /api/v1/workflows/executions/{execution_id}`
2. System validates tenant × environment access
3. Returns execution details with step statuses

---

### UC-4: Workflow Step Failure & Retry

**Actor**: System

**Flow**:

1. Activity execution fails
2. Temporal catches error
3. If retry count < max_attempts:
   - Wait for backoff interval
   - Retry activity
4. If retry count >= max_attempts:
   - Mark step as "failed"
   - Mark workflow execution as "failed"

**Error Classification**:

- **Retryable**: Transient errors (network, timeout, rate limit)
- **Non-retryable**: Validation errors, not found, permission denied

---

## Error Handling

### EC-1: Concurrent Workflow Executions

**Handling**: Each event creates a separate execution. Executions run in parallel. Ensure idempotency in activities.

### EC-2: Workflow Definition Deleted During Execution

**Handling**: Prevent deletion if running executions exist. Option to force delete (cancels executions).

### EC-3: Resource Not Found During Execution

**Handling**: Mark as non-retryable error. Fail step immediately. Fail workflow execution.

### EC-4: Partial Workflow Execution Failure

**Handling**: Mark completed steps as "completed", failed step as "failed", remaining steps as "skipped". Mark workflow as "failed". Store partial results.

### EC-5: Temporal Workflow Timeout

**Handling**: Temporal automatically cancels workflow. Mark execution as "failed" with timeout error. Store partial results.

### EC-6: Step Dependency Circular Reference

**Handling**: Validation service detects circular dependencies at definition time. Reject workflow definition with clear error message.

---

## API Design

### Workflow Definitions

#### Create Workflow Definition

```http
POST /api/v1/workflows/definitions
Content-Type: application/json
Authorization: Bearer {token}

{
  "name": "Customer Onboarding - Standard",
  "trigger": {
    "type": "event",
    "event_name": "customer.created"
  },
  "steps": [
    {
      "id": "step_1",
      "order": 1,
      "type": "assign_plan",
      "config": {
        "plan_id": "plan_starter",
        "currency": "USD"
      }
    }
  ],
  "enabled": true,
  "retry_policy": {
    "max_attempts": 3,
    "initial_interval_seconds": 1,
    "backoff_coefficient": 2.0
  }
}
```

**Response:**

```http
HTTP/1.1 201 Created
{
  "id": "wf_123",
  "name": "Customer Onboarding - Standard",
  "created_at": "2024-12-19T10:00:00Z",
  "status": "published"
}
```

#### Get Workflow Definition

```http
GET /api/v1/workflows/definitions/{workflow_id}
```

#### List Workflow Definitions

```http
GET /api/v1/workflows/definitions?enabled=true&limit=50&offset=0
```

#### Update Workflow Definition

```http
PUT /api/v1/workflows/definitions/{workflow_id}
```

#### Delete Workflow Definition

```http
DELETE /api/v1/workflows/definitions/{workflow_id}
```

**Error Cases:**

- `409 Conflict`: Workflow has running executions
- `404 Not Found`: Workflow doesn't exist

#### Enable/Disable Workflow

```http
POST /api/v1/workflows/definitions/{workflow_id}/enable
POST /api/v1/workflows/definitions/{workflow_id}/disable
```

### Workflow Executions

#### Trigger Workflow (Manual)

```http
POST /api/v1/workflows/definitions/{workflow_id}/trigger
Content-Type: application/json

{
  "trigger_data": {
    "customer_id": "cust_123"
  }
}
```

**Response:**

```http
HTTP/1.1 202 Accepted
{
  "execution_id": "exec_789",
  "status": "pending"
}
```

#### Get Workflow Execution

```http
GET /api/v1/workflows/executions/{execution_id}
```

**Response:**

```json
{
  "id": "exec_789",
  "workflow_id": "wf_123",
  "status": "running",
  "step_executions": [
    {
      "step_id": "step_1",
      "status": "completed",
      "output": {
        "subscription_id": "sub_123"
      }
    },
    {
      "step_id": "step_2",
      "status": "running"
    }
  ]
}
```

#### List Workflow Executions

```http
GET /api/v1/workflows/executions?workflow_id=wf_123&status=running&limit=50
```

#### Cancel Workflow Execution

```http
POST /api/v1/workflows/executions/{execution_id}/cancel
```

#### Retry Failed Workflow Execution

```http
POST /api/v1/workflows/executions/{execution_id}/retry
```

---

## Security & Isolation

### Tenant × Environment Isolation

**Principle**: All workflow operations must be scoped to tenant × environment.

**Implementation**:

1. **Database Level**: All tables include `tenant_id` and `environment_id`. All queries filter by both.
2. **API Level**: Middleware extracts tenant_id and environment_id from context. All endpoints validate access.
3. **Service Level**: All service methods require context with tenant_id and environment_id. Services validate context before operations.
4. **Temporal Level**: Workflow input includes tenant_id and environment_id. Activities validate context before execution.

**Isolation Rules**:

- Tenant A cannot see or modify Tenant B's workflows
- Environment Dev cannot see or modify Environment Prod's workflows
- Event triggers only match workflows in same tenant × environment

### Access Control

**RBAC Integration**:

- Use existing RBAC system
- Required permissions:
  - `workflows:read` - View workflows and executions
  - `workflows:write` - Create/update/delete workflows
  - `workflows:execute` - Trigger workflows manually

---

## Implementation Phases

### Phase 1: Core Workflow Engine (MVP)

**Scope**:

- Workflow definition CRUD
- Event-driven triggers
- Basic step types: AssignPlan, CreateWallet, GrantCredits, SendEmail
- Sequential step execution
- Basic retry logic
- Execution status tracking

**Timeline**: 8-10 weeks

### Phase 2: Enhanced Features

**Scope**:

- Conditional step execution
- Scheduled triggers
- Additional step types: CreateEntitlement, UpdateCustomer
- Step dependencies
- Enhanced error handling

**Timeline**: 4-6 weeks

### Phase 3: Advanced Features

**Scope**:

- Workflow templates
- Compensation logic
- Advanced monitoring and analytics
- Workflow versioning

**Timeline**: 6-8 weeks

---
