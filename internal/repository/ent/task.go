package ent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/task"
	domainTask "github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type taskRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts TaskQueryOptions
}

func NewTaskRepository(client postgres.IClient, logger *logger.Logger) domainTask.Repository {
	return &taskRepository{
		client:    client,
		logger:    logger,
		queryOpts: TaskQueryOptions{},
	}
}

func (r *taskRepository) Create(ctx context.Context, t *domainTask.Task) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "task", "create", map[string]interface{}{
		"task_id":     t.ID,
		"task_type":   t.TaskType,
		"entity_type": t.EntityType,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	// Set environment ID from context if not already set
	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	r.logger.Info(ctx, "saving task to database",
		"task_id", t.ID,
		"created_by", t.CreatedBy,
		"updated_by", t.UpdatedBy,
		"tenant_id", t.TenantID)

	task, err := client.Task.Create().
		SetID(t.ID).
		SetTenantID(t.TenantID).
		SetTaskType(string(t.TaskType)).
		SetEntityType(string(t.EntityType)).
		SetNillableScheduledTaskID(&t.ScheduledTaskID).
		SetNillableWorkflowID(t.WorkflowID).
		SetFileURL(t.FileURL).
		SetNillableFileName(t.FileName).
		SetFileType(string(t.FileType)).
		SetTaskStatus(string(t.TaskStatus)).
		SetNillableTotalRecords(t.TotalRecords).
		SetProcessedRecords(t.ProcessedRecords).
		SetSuccessfulRecords(t.SuccessfulRecords).
		SetFailedRecords(t.FailedRecords).
		SetNillableErrorSummary(t.ErrorSummary).
		SetMetadata(t.Metadata).
		SetNillableStartedAt(t.StartedAt).
		SetNillableCompletedAt(t.CompletedAt).
		SetNillableFailedAt(t.FailedAt).
		SetStatus(string(t.Status)).
		SetCreatedAt(t.CreatedAt).
		SetUpdatedAt(t.UpdatedAt).
		SetCreatedBy(t.CreatedBy).
		SetUpdatedBy(t.UpdatedBy).
		SetEnvironmentID(t.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.logger.Error(ctx, "failed to create task", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to create task").
			WithReportableDetails(map[string]interface{}{
				"task_id":     t.ID,
				"task_type":   t.TaskType,
				"entity_type": t.EntityType,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)

	r.logger.Info(ctx, "task saved to database successfully",
		"task_id", task.ID,
		"created_by", task.CreatedBy,
		"updated_by", task.UpdatedBy,
		"tenant_id", task.TenantID)

	*t = *domainTask.FromEnt(task)
	return nil
}

func (r *taskRepository) Get(ctx context.Context, id string) (*domainTask.Task, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "task", "get", map[string]interface{}{
		"task_id": id,
	})
	defer FinishSpan(span)

	task, err := r.client.Reader(ctx).Task.Query().
		Where(
			task.ID(id),
			task.TenantID(types.GetTenantID(ctx)),
			task.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Task not found").
				WithReportableDetails(map[string]interface{}{
					"task_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve task").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainTask.FromEnt(task), nil
}

func (r *taskRepository) List(ctx context.Context, filter *types.TaskFilter) ([]*domainTask.Task, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "task", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).Task.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	tasks, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tasks").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainTask.FromEntList(tasks), nil
}

func (r *taskRepository) Count(ctx context.Context, filter *types.TaskFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "task", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).Task.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count tasks").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *taskRepository) Update(ctx context.Context, t *domainTask.Task) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "task", "update", map[string]interface{}{
		"task_id": t.ID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	// Use predicate-based update for optimistic locking
	query := client.Task.Update().
		Where(
			task.ID(t.ID),
			task.TenantID(types.GetTenantID(ctx)),
			task.EnvironmentID(types.GetEnvironmentID(ctx)),
			task.Status(string(types.StatusPublished)),
		)

	// Set all fields
	query.
		SetTaskType(string(t.TaskType)).
		SetEntityType(string(t.EntityType)).
		SetNillableScheduledTaskID(&t.ScheduledTaskID).
		SetFileURL(t.FileURL).
		SetNillableFileName(t.FileName).
		SetFileType(string(t.FileType)).
		SetTaskStatus(string(t.TaskStatus)).
		SetNillableTotalRecords(t.TotalRecords).
		SetProcessedRecords(t.ProcessedRecords).
		SetSuccessfulRecords(t.SuccessfulRecords).
		SetFailedRecords(t.FailedRecords).
		SetNillableErrorSummary(t.ErrorSummary).
		SetMetadata(t.Metadata).
		SetNillableStartedAt(t.StartedAt).
		SetNillableCompletedAt(t.CompletedAt).
		SetNillableFailedAt(t.FailedAt).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update task").
			WithReportableDetails(map[string]interface{}{
				"task_id": t.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	if n == 0 {
		err := ierr.NewError("task not found").
			WithHint("The task may not exist or may have been deleted").
			WithReportableDetails(map[string]interface{}{
				"task_id": t.ID,
			}).
			Mark(ierr.ErrNotFound)
		SetSpanError(span, err)
		return err
	}

	SetSpanSuccess(span)
	return nil
}

func (r *taskRepository) SetTaskMetadataKey(ctx context.Context, id, key string, value any) error {
	span := StartRepositorySpan(ctx, "task", "set_metadata_key", map[string]interface{}{
		"task_id": id,
	})
	defer FinishSpan(span)

	encoded, err := json.Marshal(value)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to encode task metadata").
			Mark(ierr.ErrValidation)
	}

	const query = `
		UPDATE tasks
		SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), ARRAY[$1]::text[], $2::jsonb, true),
		    updated_at = $3,
		    updated_by = $4
		WHERE id = $5 AND tenant_id = $6 AND environment_id = $7 AND status = $8`

	result, err := r.client.Writer(ctx).ExecContext(ctx, query,
		key, string(encoded), clock.Now(), types.GetUserID(ctx), id,
		types.GetTenantID(ctx), types.GetEnvironmentID(ctx), string(types.StatusPublished))
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update task metadata").
			WithReportableDetails(map[string]interface{}{"task_id": id}).
			Mark(ierr.ErrDatabase)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		err := ierr.NewError("task not found").
			WithHint("The task may not exist or may have been deleted").
			WithReportableDetails(map[string]interface{}{"task_id": id}).
			Mark(ierr.ErrNotFound)
		SetSpanError(span, err)
		return err
	}

	SetSpanSuccess(span)
	return nil
}

// System job: deliberately not scoped to the tenant or environment in ctx.
func (r *taskRepository) FailStaleExportTasks(ctx context.Context, olderThan time.Time, reason string, limit int) (int, error) {
	span := StartRepositorySpan(ctx, "task", "fail_stale_export", nil)
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	ids, err := client.Task.Query().
		Where(
			task.TaskType(string(types.TaskTypeExport)),
			task.TaskStatusIn(string(types.TaskStatusProcessing), string(types.TaskStatusPending)),
			task.Status(string(types.StatusPublished)),
			task.CreatedAtLT(olderThan),
		).
		Limit(limit).
		IDs(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to list stale export tasks").
			Mark(ierr.ErrDatabase)
	}
	if len(ids) == 0 {
		SetSpanSuccess(span)
		return 0, nil
	}

	now := clock.Now()
	n, err := client.Task.Update().
		Where(
			task.IDIn(ids...),
			task.TaskStatusIn(string(types.TaskStatusProcessing), string(types.TaskStatusPending)),
		).
		SetTaskStatus(string(types.TaskStatusFailed)).
		SetErrorSummary(reason).
		SetFailedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to fail stale export tasks").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return n, nil
}

func (r *taskRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "task", "delete", map[string]interface{}{
		"task_id": id,
	})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).Task.Update().
		Where(
			task.ID(id),
			task.TenantID(types.GetTenantID(ctx)),
			task.EnvironmentID(types.GetEnvironmentID(ctx)),
			task.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(clock.Now()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete task").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// TaskQuery type alias for better readability
type TaskQuery = *ent.TaskQuery

// TaskQueryOptions implements BaseQueryOptions for task queries
type TaskQueryOptions struct{}

func (o TaskQueryOptions) ApplyTenantFilter(ctx context.Context, query TaskQuery) TaskQuery {
	return query.Where(task.TenantID(types.GetTenantID(ctx)))
}

func (o TaskQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query TaskQuery) TaskQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(task.EnvironmentID(environmentID))
	}
	return query
}

func (o TaskQueryOptions) ApplyStatusFilter(query TaskQuery, status string) TaskQuery {
	if status == "" {
		return query.Where(task.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(task.Status(status))
}

func (o TaskQueryOptions) ApplySortFilter(query TaskQuery, field string, order string) TaskQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o TaskQueryOptions) ApplyPaginationFilter(query TaskQuery, limit int, offset int) TaskQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the ent field name for task; delegates to ent's ValidColumn so new schema fields are supported automatically.
func (o TaskQueryOptions) GetFieldName(field string) string {
	if task.ValidColumn(field) {
		return field
	}
	return ""
}

func (o TaskQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.TaskFilter, query *ent.TaskQuery) *ent.TaskQuery {
	if f == nil {
		return query
	}

	// Apply entity-specific filters
	if f.TaskType != nil {
		query = query.Where(task.TaskType(string(*f.TaskType)))
	}
	if f.EntityType != nil {
		query = query.Where(task.EntityType(string(*f.EntityType)))
	}
	if f.TaskStatus != nil {
		query = query.Where(task.TaskStatus(string(*f.TaskStatus)))
	}
	if f.ScheduledTaskID != "" {
		query = query.Where(task.ScheduledTaskID(f.ScheduledTaskID))
	}
	if f.CreatedBy != "" {
		query = query.Where(task.CreatedBy(f.CreatedBy))
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(task.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(task.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}
