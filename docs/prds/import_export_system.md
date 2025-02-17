# Import/Export System Design

## Overview
This document outlines the design and implementation of FlexPrice's import/export system, which enables bulk operations through file-based imports (and future exports) of core entities. The system is designed to be scalable, maintainable, and support both synchronous (Phase 1) and asynchronous (Phase 2) processing.

## Goals
- Enable bulk imports through CSV/JSON files
- Support client-side validations using @CSVBox
- Provide task tracking and history
- Design for easy transition from sync to async processing
- Maintain proper error handling and reporting
- Support multiple entity types (events, prices, etc.)

## System Components

### Core Entities

#### 1. Task
A unified table for both import and export tasks, following the pattern of other entities in the system.

```sql
CREATE TABLE tasks (
    id VARCHAR(50) PRIMARY KEY,
    tenant_id UUID NOT NULL,
    task_type VARCHAR(50) NOT NULL, -- IMPORT, EXPORT
    entity_type VARCHAR(50) NOT NULL, -- EVENTS, PRICES, etc.
    file_url VARCHAR(255) NOT NULL,
    file_type VARCHAR(10) NOT NULL, -- CSV, JSON
    task_status VARCHAR(50) NOT NULL, -- PENDING, PROCESSING, COMPLETED, FAILED
    total_records INTEGER,
    processed_records INTEGER DEFAULT 0,
    successful_records INTEGER DEFAULT 0,
    failed_records INTEGER DEFAULT 0,
    error_summary TEXT, -- High-level error summary
    metadata JSONB, -- Store task-specific configuration
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    failed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_by VARCHAR(50) NOT NULL,
    updated_by VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'published'
);

CREATE INDEX idx_tasks_tenant ON tasks(tenant_id, task_type, entity_type, status);
CREATE INDEX idx_tasks_user ON tasks(tenant_id, created_by);
```

For Phase 1, we'll keep error tracking simple by storing only critical errors and summary information in the task metadata. This avoids the complexity of managing a separate error table initially.

### Domain Structure

```go
// domain/task/model.go
package task

import (
    "time"
    "github.com/flexprice/flexprice/internal/types"
)

type Task struct {
    ID               string
    TaskType         TaskType
    EntityType       EntityType
    FileURL          string
    FileType         FileType
    TaskStatus       TaskStatus
    TotalRecords     *int
    ProcessedRecords int
    SuccessfulRecords int
    FailedRecords    int
    ErrorSummary     string
    Metadata         map[string]interface{}
    StartedAt       *time.Time
    CompletedAt     *time.Time
    FailedAt        *time.Time
    BaseModel        types.BaseModel
}

type TaskType string
const (
    TaskTypeImport TaskType = "IMPORT"
    TaskTypeExport TaskType = "EXPORT"
)

type EntityType string
const (
    EntityTypeEvents EntityType = "EVENTS"
    EntityTypePrices EntityType = "PRICES"
)

type FileType string
const (
    FileTypeCSV  FileType = "CSV"
    FileTypeJSON FileType = "JSON"
)

type TaskStatus string
const (
    TaskStatusPending    TaskStatus = "PENDING"
    TaskStatusProcessing TaskStatus = "PROCESSING"
    TaskStatusCompleted  TaskStatus = "COMPLETED"
    TaskStatusFailed    TaskStatus = "FAILED"
)

// FromEnt converts ent.Task to domain Task
func FromEnt(e *ent.Task) *Task {
    if e == nil {
        return nil
    }
    
    return &Task{
        ID:               e.ID,
        TaskType:         TaskType(e.TaskType),
        EntityType:       EntityType(e.EntityType),
        FileURL:          e.FileURL,
        FileType:         FileType(e.FileType),
        TaskStatus:       TaskStatus(e.Status),
        TotalRecords:     e.TotalRecords,
        ProcessedRecords: e.ProcessedRecords,
        SuccessfulRecords:   e.SuccessfulRecords,
        FailedRecords:    e.FailedRecords,
        ErrorSummary:     e.ErrorSummary,
        Metadata:         e.Metadata,
        StartedAt:        e.StartedAt,
        CompletedAt:      e.CompletedAt,
        FailedAt:         e.FailedAt,
        BaseModel: types.BaseModel{
            TenantID: e.TenantID,
            Status:    types.Status(e.Status),
            CreatedBy: e.CreatedBy,
            UpdatedBy: e.UpdatedBy,
            CreatedAt: e.CreatedAt,
            UpdatedAt: e.UpdatedAt,
        },
    }
}

// Repository interface
type Repository interface {
    Create(ctx context.Context, task *Task) error
    Get(ctx context.Context, id string) (*Task, error)
    List(ctx context.Context, filter *TaskFilter) ([]*Task, error)
    Count(ctx context.Context, filter *TaskFilter) (int, error)
    Update(ctx context.Context, task *Task) error
    UpdateProgress(ctx context.Context, id string, processed, success, failed int, errorSummary string) error
    Delete(ctx context.Context, id string) error
}
```

### Implementation Details

#### 1. Progress Tracking Strategy
For efficient progress tracking with large datasets:

1. **Batch Updates**: Instead of updating on every record, accumulate progress and update in batches (e.g., every 1000 records or every 30 seconds).
```go
type progressTracker struct {
    taskID           string
    processed        int
    successful      int
    failed          int
    errors          []string
    lastUpdateTime  time.Time
    batchSize       int
    updateInterval  time.Duration
}

func (t *progressTracker) increment(success bool, err error) {
    t.processed++
    if success {
        t.successful++
    } else {
        t.failed++
        if err != nil && len(t.errors) < 10 { // Keep only first 10 errors
            t.errors = append(t.errors, err.Error())
        }
    }
    
    if t.shouldUpdate() {
        t.flush()
    }
}
```

2. **Error Handling**: Store only critical errors and maintain counts for different error types in metadata:
```go
type taskMetadata struct {
    ValidationErrors   int      `json:"validation_errors"`
    ProcessingErrors   int      `json:"processing_errors"`
    LastErrors        []string `json:"last_errors"` // Keep last N errors
}
```

### Service Layer

```go
// service/task.go
type TaskService interface {
    CreateTask(ctx context.Context, req *dto.CreateTaskRequest) (*dto.TaskResponse, error)
    GetTask(ctx context.Context, id string) (*dto.TaskResponse, error)
    ListTasks(ctx context.Context, filter *types.TaskFilter) (*dto.ListTasksResponse, error)
    ProcessTask(ctx context.Context, id string) error
}

type taskService struct {
    taskRepo  task.Repository
    eventSvc  EventService
    priceSvc  PriceService
    logger    *logger.Logger
}

func (s *taskService) ProcessTask(ctx context.Context, id string) error {
    task, err := s.taskRepo.Get(ctx, id)
    if err != nil {
        return err
    }
    
    // Create progress tracker
    tracker := newProgressTracker(task.ID, 1000, 30*time.Second)
    
    // Process file based on type
    switch task.EntityType {
    case task.EntityTypeEvents:
        return s.processEvents(ctx, task, tracker)
    case task.EntityTypePrices:
        return s.processPrices(ctx, task, tracker)
    default:
        return fmt.Errorf("unsupported entity type: %s", task.EntityType)
    }
}
```

### API Layer

```go
// api/v1/task.go
type TaskHandler struct {
    taskService TaskService
    logger      *logger.Logger
}

func (h *TaskHandler) CreateTask(c *gin.Context) {
    var req dto.CreateTaskRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
        return
    }
    
    task, err := h.taskService.CreateTask(c.Request.Context(), &req)
    if err != nil {
        NewErrorResponse(c, http.StatusInternalServerError, "failed to create task", err)
        return
    }
    
    c.JSON(http.StatusCreated, task)
}
```

### Phase 2 Enhancements

1. **Async Processing**:
   - Add message queue integration
   - Implement worker pool for processing
   - Add task status webhooks

2. **Error Management**:
   - If needed, add separate error tracking table
   - Implement error aggregation and analysis
   - Add error export functionality

### API Endpoints

1. **Create Import Task**
```http
POST /v1/tasks
{
    "task_type": "IMPORT",
    "entity_type": "EVENTS",
    "file_url": "https://csvbox.io/exports/123.csv",
    "file_type": "CSV"
}
```

2. **Get Task**
```http
GET /v1/tasks/{id}
```

3. **List Tasks**
```http
GET /v1/tasks?task_type=IMPORT&entity_type=EVENTS&status=COMPLETED
```

### Implementation Plan

#### Phase 1 (Sync):
1. Implement core task entity and repository
2. Add basic file processing with batch progress updates
3. Implement synchronous processing flow
4. Add basic error tracking in task metadata

#### Phase 2 (Async):
1. Add message queue integration
2. Implement worker pool
3. Add webhook notifications
4. Enhance error tracking if needed

The design prioritizes simplicity and ease of implementation while maintaining scalability for future enhancements. By using a single task table and batch updates, we avoid unnecessary complexity while still providing robust progress tracking and error reporting. 