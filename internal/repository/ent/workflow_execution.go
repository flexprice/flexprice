package ent

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/workflowexecution"
	domainWorkflowExecution "github.com/flexprice/flexprice/internal/domain/workflowexecution"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"

	"entgo.io/ent/dialect/sql"
)

type workflowExecutionRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts WorkflowExecutionQueryOptions
	cache     cache.Cache
}

func NewWorkflowExecutionRepository(client postgres.IClient, log *logger.Logger, c cache.Cache) domainWorkflowExecution.Repository {
	return &workflowExecutionRepository{
		client:    client,
		log:       log,
		queryOpts: WorkflowExecutionQueryOptions{},
		cache:     c,
	}
}

func (r *workflowExecutionRepository) Create(ctx context.Context, exec *domainWorkflowExecution.WorkflowExecution) error {
	span := StartRepositorySpan(ctx, "workflow_execution", "create", map[string]interface{}{
		"workflow_id": exec.WorkflowID,
		"run_id":      exec.RunID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	r.log.Debugw("creating workflow execution",
		"workflow_id", exec.WorkflowID,
		"run_id", exec.RunID,
		"workflow_type", exec.WorkflowType,
	)

	entExec := domainWorkflowExecution.ToEnt(exec)
	createQuery := client.WorkflowExecution.Create().
		SetWorkflowID(entExec.WorkflowID).
		SetRunID(entExec.RunID).
		SetWorkflowType(entExec.WorkflowType).
		SetTaskQueue(entExec.TaskQueue).
		SetStartTime(entExec.StartTime).
		SetTenantID(entExec.TenantID).
		SetEnvironmentID(entExec.EnvironmentID).
		SetCreatedBy(entExec.CreatedBy).
		SetStatus(entExec.Status).
		SetWorkflowStatus(entExec.WorkflowStatus).
		SetCreatedAt(entExec.CreatedAt).
		SetUpdatedAt(entExec.UpdatedAt).
		SetUpdatedBy(entExec.UpdatedBy)

	if exec.Entity != "" {
		createQuery = createQuery.SetEntity(exec.Entity)
	}
	if exec.EntityID != "" {
		createQuery = createQuery.SetEntityID(exec.EntityID)
	}
	if exec.Metadata != nil {
		createQuery = createQuery.SetMetadata(exec.Metadata)
	}

	created, err := createQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create workflow execution").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	exec.ID = created.ID
	exec.CreatedAt = created.CreatedAt
	exec.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *workflowExecutionRepository) Get(ctx context.Context, workflowID, runID string) (*domainWorkflowExecution.WorkflowExecution, error) {
	span := StartRepositorySpan(ctx, "workflow_execution", "get", map[string]interface{}{
		"workflow_id": workflowID,
		"run_id":      runID,
	})
	defer FinishSpan(span)

	if r.cache != nil {
		if cached := r.GetCache(ctx, workflowID, runID); cached != nil {
			return cached, nil
		}
	}

	entExec, err := r.getEnt(ctx, workflowID, runID)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	SetSpanSuccess(span)
	dom := domainWorkflowExecution.FromEnt(entExec)
	if r.cache != nil {
		r.SetCache(ctx, workflowID, runID, dom)
	}
	return dom, nil
}

func (r *workflowExecutionRepository) getEnt(ctx context.Context, workflowID, runID string) (*ent.WorkflowExecution, error) {
	client := r.client.Reader(ctx)
	q := client.WorkflowExecution.Query().
		Where(
			workflowexecution.WorkflowID(workflowID),
			workflowexecution.RunID(runID),
		)
	q = r.queryOpts.ApplyTenantFilter(ctx, q)
	q = r.queryOpts.ApplyEnvironmentFilter(ctx, q)
	entExec, err := q.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Workflow execution not found: %s/%s", workflowID, runID).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get workflow execution").
			Mark(ierr.ErrDatabase)
	}
	return entExec, nil
}

func (r *workflowExecutionRepository) List(ctx context.Context, filter *types.WorkflowExecutionFilter) ([]*domainWorkflowExecution.WorkflowExecution, error) {
	if filter == nil {
		filter = types.NewDefaultWorkflowExecutionFilter()
	}
	span := StartRepositorySpan(ctx, "workflow_execution", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.WorkflowExecution.Query()
	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	executions, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list workflow executions").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainWorkflowExecution.FromEntList(executions), nil
}

func (r *workflowExecutionRepository) Count(ctx context.Context, filter *types.WorkflowExecutionFilter) (int, error) {
	if filter == nil {
		filter = types.NewDefaultWorkflowExecutionFilter()
	}
	span := StartRepositorySpan(ctx, "workflow_execution", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.WorkflowExecution.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, err
	}
	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count workflow executions").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return count, nil
}

func (r *workflowExecutionRepository) ListAll(ctx context.Context, filter *types.WorkflowExecutionFilter) ([]*domainWorkflowExecution.WorkflowExecution, error) {
	if filter == nil {
		filter = types.NewNoLimitWorkflowExecutionFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}
	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}
	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}
	return r.List(ctx, filter)
}

func (r *workflowExecutionRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "workflow_execution", "delete", map[string]interface{}{
		"id": id,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	r.log.Debugw("deleting workflow execution", "id", id)
	err := client.WorkflowExecution.DeleteOneID(id).Exec(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Workflow execution not found: %s", id).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete workflow execution").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *workflowExecutionRepository) UpdateStatus(
	ctx context.Context,
	workflowID, runID string,
	status types.WorkflowExecutionStatus,
	errorMessage string,
	endTime *time.Time,
	durationMs *int64,
) error {
	span := StartRepositorySpan(ctx, "workflow_execution", "update_status", map[string]interface{}{
		"workflow_id": workflowID,
		"run_id":      runID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	r.log.Debugw("updating workflow execution status",
		"workflow_id", workflowID,
		"run_id", runID,
		"status", status,
	)

	entExec, err := r.getEnt(ctx, workflowID, runID)
	if err != nil {
		SetSpanError(span, err)
		return err
	}

	updateQuery := client.WorkflowExecution.
		UpdateOne(entExec).
		SetWorkflowStatus(status)

	if endTime != nil {
		updateQuery = updateQuery.SetEndTime(*endTime)
	}
	if durationMs != nil {
		updateQuery = updateQuery.SetDurationMs(*durationMs)
	}
	if errorMessage != "" {
		metadata := entExec.Metadata
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		metadata["error"] = errorMessage
		updateQuery = updateQuery.SetMetadata(metadata)
	}

	_, err = updateQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHintf("Failed to update workflow execution status: %s/%s", workflowID, runID).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	if r.cache != nil {
		r.DeleteCache(ctx, workflowID, runID)
	}
	return nil
}

// WorkflowExecutionQuery type alias
type WorkflowExecutionQuery = *ent.WorkflowExecutionQuery

// WorkflowExecutionQueryOptions implements BaseQueryOptions for workflow execution queries
type WorkflowExecutionQueryOptions struct{}

func (o WorkflowExecutionQueryOptions) ApplyTenantFilter(ctx context.Context, query WorkflowExecutionQuery) WorkflowExecutionQuery {
	return query.Where(workflowexecution.TenantID(types.GetTenantID(ctx)))
}

func (o WorkflowExecutionQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query WorkflowExecutionQuery) WorkflowExecutionQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(workflowexecution.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o WorkflowExecutionQueryOptions) ApplyStatusFilter(query WorkflowExecutionQuery, _ string) WorkflowExecutionQuery {
	return query
}

func (o WorkflowExecutionQueryOptions) ApplySortFilter(query WorkflowExecutionQuery, field string, order string) WorkflowExecutionQuery {
	order = strings.ToLower(strings.TrimSpace(order))
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	var dir sql.OrderTermOption
	if order == "asc" {
		dir = sql.OrderAsc()
	} else {
		dir = sql.OrderDesc()
	}
	builder := workflowExecutionOrderBuilder(field)
	return query.Order(builder(dir))
}

func (o WorkflowExecutionQueryOptions) ApplyPaginationFilter(query WorkflowExecutionQuery, limit int, offset int) WorkflowExecutionQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o WorkflowExecutionQueryOptions) GetFieldName(field string) string {
	return workflowExecutionFieldName(field)
}

func (o WorkflowExecutionQueryOptions) GetFieldResolver(st string) (string, error) {
	name := o.GetFieldName(st)
	if name == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in workflow execution query", st).
			WithHintf("Unknown field name '%s' in workflow execution query", st).
			Mark(ierr.ErrValidation)
	}
	return name, nil
}

func workflowExecutionOrderBuilder(field string) func(...sql.OrderTermOption) workflowexecution.OrderOption {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "start_time":
		return workflowexecution.ByStartTime
	case "end_time", "close_time":
		return workflowexecution.ByEndTime
	case "created_at":
		return workflowexecution.ByCreatedAt
	case "updated_at":
		return workflowexecution.ByUpdatedAt
	case "workflow_id":
		return workflowexecution.ByWorkflowID
	case "run_id":
		return workflowexecution.ByRunID
	case "workflow_type":
		return workflowexecution.ByWorkflowType
	case "task_queue":
		return workflowexecution.ByTaskQueue
	case "workflow_status", "status":
		return workflowexecution.ByWorkflowStatus
	case "entity":
		return workflowexecution.ByEntity
	case "entity_id":
		return workflowexecution.ByEntityID
	default:
		return workflowexecution.ByStartTime
	}
}

func (o WorkflowExecutionQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.WorkflowExecutionFilter, query WorkflowExecutionQuery) (WorkflowExecutionQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}
	if f.WorkflowID != "" {
		query = query.Where(workflowexecution.WorkflowID(f.WorkflowID))
	}
	if f.WorkflowType != "" {
		query = query.Where(workflowexecution.WorkflowType(f.WorkflowType))
	}
	if f.TaskQueue != "" {
		query = query.Where(workflowexecution.TaskQueue(f.TaskQueue))
	}
	if f.WorkflowStatus != "" {
		query = query.Where(workflowexecution.WorkflowStatus(types.WorkflowExecutionStatus(f.WorkflowStatus)))
	}
	if f.Entity != "" {
		query = query.Where(workflowexecution.EntityEQ(f.Entity))
	}
	if f.EntityID != "" {
		query = query.Where(workflowexecution.EntityIDEQ(f.EntityID))
	}
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(workflowexecution.StartTimeGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(workflowexecution.StartTimeLTE(*f.TimeRangeFilter.EndTime))
		}
	}
	if f.DSLFilter != nil && len(f.Filters) > 0 {
		query, err = dsl.ApplyFilters[WorkflowExecutionQuery, predicate.WorkflowExecution](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.WorkflowExecution { return predicate.WorkflowExecution(p) },
		)
		if err != nil {
			return nil, err
		}
	}
	if f.DSLFilter != nil && len(f.DSLFilter.Sort) > 0 {
		query, err = dsl.ApplySorts[WorkflowExecutionQuery, workflowexecution.OrderOption](
			query,
			f.DSLFilter.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) workflowexecution.OrderOption { return workflowexecution.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}
	return query, nil
}

// workflowExecutionFieldName resolves optional aliases then delegates to ent's ValidColumn so new schema fields are supported automatically.
func workflowExecutionFieldName(field string) string {
	f := strings.ToLower(strings.TrimSpace(field))
	// Optional aliases for API compatibility (ent column name -> used in ValidColumn)
	if f == "close_time" {
		f = "end_time"
	}
	if f == "status" {
		f = "workflow_status"
	}
	if workflowexecution.ValidColumn(f) {
		return f
	}
	return ""
}

func (r *workflowExecutionRepository) GetCache(ctx context.Context, workflowID, runID string) *domainWorkflowExecution.WorkflowExecution {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	key := cache.GenerateKey(cache.PrefixWorkflowExecution, tenantID, environmentID, workflowID+":"+runID)
	if value, found := r.cache.Get(ctx, key); found {
		if exec, ok := value.(*domainWorkflowExecution.WorkflowExecution); ok {
			return exec
		}
	}
	return nil
}

func (r *workflowExecutionRepository) SetCache(ctx context.Context, workflowID, runID string, exec *domainWorkflowExecution.WorkflowExecution) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	key := cache.GenerateKey(cache.PrefixWorkflowExecution, tenantID, environmentID, workflowID+":"+runID)
	r.cache.Set(ctx, key, exec, cache.ExpiryDefaultInMemory)
}

func (r *workflowExecutionRepository) DeleteCache(ctx context.Context, workflowID, runID string) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	key := cache.GenerateKey(cache.PrefixWorkflowExecution, tenantID, environmentID, workflowID+":"+runID)
	r.cache.Delete(ctx, key)
}
