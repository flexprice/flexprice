# Temporal Workflow Monitoring APIs - Setup Guide

## Implementation Complete! ✅

All code has been written for the Temporal Workflow Monitoring feature. Follow the steps below to complete the setup.

---

## Step 1: Generate Ent Code

Run the following command to generate Ent entity code for the new `workflow_executions` table:

```bash
make generate-ent
```

This will generate:
- `ent/workflowexecution.go` - Entity struct
- `ent/workflowexecution_create.go` - Create operations
- `ent/workflowexecution_update.go` - Update operations
- `ent/workflowexecution_query.go` - Query operations
- `ent/workflowexecution/` - Field constants and predicates

---

## Step 2: Create Database Migration

Run the following command to generate the SQL migration:

```bash
make migrate-ent
```

This will create a new migration file in `migrations/` directory with the CREATE TABLE statement for `workflow_executions`.

---

## Step 3: Apply Migration to Database

Run the migration to create the table in PostgreSQL:

```bash
# If using docker-compose
docker-compose exec api make migrate-postgres

# Or run migrations directly
# Your migration script here
```

---

## What Was Implemented

### 1. Database Schema
**File**: `ent/schema/workflow_execution.go`

New table `workflow_executions` with fields:
- `id` (ULID primary key)
- `workflow_id` (user-provided workflow ID)
- `run_id` (Temporal-generated execution UUID)
- `workflow_type` (e.g., "BillingWorkflow")
- `task_queue` (e.g., "subscription")
- `start_time` (execution start time)
- `tenant_id`, `environment_id`, `created_by` (from mixins)
- `metadata` (JSONB for custom attributes)

**Indexes**:
- Unique: `(workflow_id, run_id)`
- Composite: `(tenant_id, environment_id, workflow_type)`
- Composite: `(tenant_id, environment_id, task_queue)`
- Single: `(start_time)`

---

### 2. Temporal SDK Integration
**File**: `internal/temporal/queries/workflow_queries.go`

Helper functions for querying Temporal:
- `DescribeWorkflow()` - Get workflow status and metadata
- `GetWorkflowHistory()` - Get workflow event history
- `ParseActivitiesFromHistory()` - Extract activity executions from events
- `ParseTimelineFromHistory()` - Build timeline visualization data
- `DescribeWorkflowBatch()` - Query multiple workflows in parallel

---

### 3. Service Layer
**File**: `internal/service/workflow_execution.go`

Database operations:
- `CreateWorkflowExecution()` - Insert workflow record
- `ListWorkflowExecutions()` - Query with filters and pagination
- `GetWorkflowExecution()` - Get single workflow by ID
- `DeleteWorkflowExecution()` - Remove workflow record

---

### 4. DTOs
**File**: `internal/api/dto/workflow.go`

Request/response structures:
- `WorkflowExecutionDTO` - Workflow summary
- `WorkflowActivityDTO` - Activity details
- `WorkflowTimelineItemDTO` - Timeline events
- `ListWorkflowsResponse` - List API response
- `WorkflowDetailsResponse` - Details API response
- `WorkflowSummaryResponse` - Summary API response
- `BatchWorkflowsRequest/Response` - Batch API request/response

---

### 5. API Handlers
**File**: `internal/api/v1/workflow.go`

Four REST APIs:

#### API 1: List Workflows
```
GET /v1/workflows?workflow_type=BillingWorkflow&page=1&page_size=50
```
Returns paginated list of workflows (queries database for fast filtering)

#### API 2: Workflow Summary
```
GET /v1/workflows/:workflow_id/:run_id/summary
```
Returns basic status without full activity details (lightweight)

#### API 3: Workflow Details
```
GET /v1/workflows/:workflow_id/:run_id
```
Returns complete details including activities, timeline, and errors (queries Temporal)

#### API 4: Batch Workflows
```
POST /v1/workflows/batch
Body: {
  "workflows": [
    {"workflow_id": "wf-1", "run_id": "run-1"},
    {"workflow_id": "wf-2", "run_id": "run-2"}
  ],
  "include_activities": true
}
```
Returns details for multiple workflows in one request

---

### 6. Workflow Tracking
**File**: `internal/temporal/tracking/workflow_tracking.go`

Automatic tracking:
- `TrackWorkflowStartActivity` - Local activity to save workflow to database
- `ExecuteTrackWorkflowStart()` - Helper function called at workflow start
- All 14 workflows updated to track execution

**Workflows instrumented**:
1. PriceSyncWorkflow
2. QuickBooksPriceSyncWorkflow
3. TaskProcessingWorkflow
4. HubSpotDealSyncWorkflow
5. HubSpotInvoiceSyncWorkflow
6. HubSpotQuoteSyncWorkflow
7. NomodInvoiceSyncWorkflow
8. MoyasarInvoiceSyncWorkflow
9. ExecuteExportWorkflow
10. CustomerOnboardingWorkflow
11. ScheduleSubscriptionBillingWorkflow
12. ProcessSubscriptionBillingWorkflow
13. ProcessInvoiceWorkflow
14. ReprocessEventsWorkflow

---

### 7. Router Registration
**Files**: 
- `internal/api/router.go` - Routes added
- `cmd/server/main.go` - Handler wired into DI

Routes registered:
```go
GET  /v1/workflows
POST /v1/workflows/batch
GET  /v1/workflows/:workflow_id/:run_id/summary
GET  /v1/workflows/:workflow_id/:run_id/timeline
GET  /v1/workflows/:workflow_id/:run_id
```

---

## Testing

Basic unit tests created in `internal/service/workflow_execution_test.go`

To run tests:
```bash
go test ./internal/service/workflow_execution_test.go -v
```

---

## How It Works

### When a Workflow Starts:
1. Workflow function executes `ExecuteTrackWorkflowStart()`
2. Local activity saves workflow metadata to database
3. Record includes: workflow_id, run_id, type, tenant, environment, start time

### When User Queries Workflows:

**List Page** (many workflows):
```
GET /v1/workflows?page=1&page_size=50
```
- Queries database (fast)
- Returns basic info for 50 workflows
- Supports filtering by type, task queue, etc.

**Details Page** (one workflow):
```
GET /v1/workflows/billing-001/run-123
```
- Queries Temporal SDK directly (real-time)
- Returns current status, all activities, timeline, errors
- Shows which step failed, retry attempts, durations

---

## Example API Responses

### List API Response:
```json
{
  "workflows": [
    {
      "workflow_id": "billing-2026-01-001",
      "run_id": "uuid-123",
      "workflow_type": "ProcessSubscriptionBillingWorkflow",
      "task_queue": "subscription",
      "start_time": "2026-01-29T10:00:00Z",
      "created_by": "user@example.com"
    }
  ],
  "total": 125,
  "page": 1,
  "page_size": 50,
  "total_pages": 3
}
```

### Details API Response:
```json
{
  "workflow_id": "billing-2026-01-001",
  "run_id": "uuid-123",
  "workflow_type": "ProcessSubscriptionBillingWorkflow",
  "status": "FAILED",
  "start_time": "2026-01-29T10:00:00Z",
  "close_time": "2026-01-29T10:15:30Z",
  "duration_ms": 930000,
  "total_duration": "15m 30s",
  "activities": [
    {
      "activity_id": "1",
      "activity_type": "CheckDraftSubscriptionActivity",
      "status": "COMPLETED",
      "start_time": "2026-01-29T10:00:01Z",
      "close_time": "2026-01-29T10:00:03Z",
      "duration_ms": 2000,
      "retry_attempt": 0
    },
    {
      "activity_id": "2",
      "activity_type": "CreateDraftInvoicesActivity",
      "status": "FAILED",
      "start_time": "2026-01-29T10:00:05Z",
      "close_time": "2026-01-29T10:15:30Z",
      "duration_ms": 925000,
      "retry_attempt": 3,
      "error": {
        "message": "Database connection timeout",
        "type": "ApplicationError"
      }
    }
  ],
  "timeline": [...]
}
```

---

## Next Steps

1. ✅ Run `make generate-ent` 
2. ✅ Run `make migrate-ent`
3. ✅ Apply database migration
4. ✅ Restart your application
5. ✅ Test the APIs using curl or Postman
6. ✅ Build UI to display workflow data

---

## Notes

- **No background sync** needed - status is queried from Temporal in real-time
- **Tracking is automatic** - all workflows now save to database on start
- **Local activities** are used for tracking (fast, non-intrusive)
- **Error handling** - tracking failures won't break workflows
- **Multi-tenant** - All queries filter by tenant_id and environment_id

---

## Troubleshooting

If you encounter issues:

1. **Ent generation fails**: Make sure all imports are correct
2. **Migration fails**: Check if table already exists
3. **Tracking not working**: Check Temporal worker logs for local activity execution
4. **API returns empty**: Verify workflows are running and being tracked

---

## Files Created/Modified

### New Files:
- `ent/schema/workflow_execution.go`
- `internal/temporal/queries/workflow_queries.go`
- `internal/service/workflow_execution.go`
- `internal/service/workflow_execution_test.go`
- `internal/api/dto/workflow.go`
- `internal/api/v1/workflow.go`
- `internal/temporal/tracking/workflow_tracking.go`

### Modified Files:
- All 14 workflow files (tracking added)
- `internal/temporal/registration.go`
- `internal/api/router.go`
- `cmd/server/main.go`

---

**Ready to test!** Run the commands above and start using the APIs.
