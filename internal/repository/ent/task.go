package ent

import (
	"context"
	"time"

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
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	task, err := client.Task.Create().
		SetID(t.ID).
		SetTenantID(t.TenantID).
		SetTaskType(string(t.TaskType)).
		SetEntityType(string(t.EntityType)).
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
		r.logger.Error("failed to create task", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to create task").
			WithReportableDetails(map[string]interface{}{
				"task_id":     t.ID,
				"task_type":   t.TaskType,
				"entity_type": t.EntityType,
			}).
			Mark(ierr.ErrDatabase)
	}

	*t = *domainTask.FromEnt(task)
	return nil
}

func (r *taskRepository) Get(ctx context.Context, id string) (*domainTask.Task, error) {
	task, err := r.client.Querier(ctx).Task.Query().
		Where(
			task.ID(id),
			task.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)
	if err != nil {
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

	return domainTask.FromEnt(task), nil
}

func (r *taskRepository) List(ctx context.Context, filter *types.TaskFilter) ([]*domainTask.Task, error) {
	query := r.client.Querier(ctx).Task.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	tasks, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tasks").
			Mark(ierr.ErrDatabase)
	}

	return domainTask.FromEntList(tasks), nil
}

func (r *taskRepository) Count(ctx context.Context, filter *types.TaskFilter) (int, error) {
	query := r.client.Querier(ctx).Task.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count tasks").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (r *taskRepository) Update(ctx context.Context, t *domainTask.Task) error {
	client := r.client.Querier(ctx)

	// Use predicate-based update for optimistic locking
	query := client.Task.Update().
		Where(
			task.ID(t.ID),
			task.TenantID(types.GetTenantID(ctx)),
			task.Status(string(types.StatusPublished)),
		)

	// Set all fields
	query.
		SetTaskType(string(t.TaskType)).
		SetEntityType(string(t.EntityType)).
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
		SetUpdatedAt(time.Now()).
		SetUpdatedBy(types.GetUserID(ctx))

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update task").
			WithReportableDetails(map[string]interface{}{
				"task_id": t.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	if n == 0 {
		return ierr.NewError("task not found").
			WithHint("The task may not exist or may have been deleted").
			WithReportableDetails(map[string]interface{}{
				"task_id": t.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return nil
}

func (r *taskRepository) Delete(ctx context.Context, id string) error {
	_, err := r.client.Querier(ctx).Task.Update().
		Where(
			task.ID(id),
			task.TenantID(types.GetTenantID(ctx)),
			task.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete task").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

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
		return query.Where(task.StatusNotIn(string(types.StatusDeleted)))
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

func (o TaskQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return task.FieldCreatedAt
	case "updated_at":
		return task.FieldUpdatedAt
	default:
		return field
	}
}

func (o TaskQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.TaskFilter, query *ent.TaskQuery) *ent.TaskQuery {
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
