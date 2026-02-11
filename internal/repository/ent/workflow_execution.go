package ent

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/workflowexecution"
	domainWorkflowExecution "github.com/flexprice/flexprice/internal/domain/workflowexecution"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type workflowExecutionRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewWorkflowExecutionRepository(client postgres.IClient, log *logger.Logger) domainWorkflowExecution.Repository {
	return &workflowExecutionRepository{
		client: client,
		log:    log,
	}
}

func (r *workflowExecutionRepository) Create(ctx context.Context, exec *ent.WorkflowExecution) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating workflow execution",
		"workflow_id", exec.WorkflowID,
		"run_id", exec.RunID,
		"workflow_type", exec.WorkflowType,
	)

	createQuery := client.WorkflowExecution.Create().
		SetWorkflowID(exec.WorkflowID).
		SetRunID(exec.RunID).
		SetWorkflowType(exec.WorkflowType).
		SetTaskQueue(exec.TaskQueue).
		SetStartTime(exec.StartTime).
		SetTenantID(exec.TenantID).
		SetEnvironmentID(exec.EnvironmentID).
		SetCreatedBy(exec.CreatedBy).
		SetStatus(exec.Status).
		SetWorkflowStatus(exec.WorkflowStatus).
		SetCreatedAt(exec.CreatedAt).
		SetUpdatedAt(exec.UpdatedAt).
		SetUpdatedBy(exec.UpdatedBy)

	if exec.Entity != nil {
		createQuery = createQuery.SetEntity(*exec.Entity)
	}
	if exec.EntityID != nil {
		createQuery = createQuery.SetEntityID(*exec.EntityID)
	}
	if exec.Metadata != nil {
		createQuery = createQuery.SetMetadata(exec.Metadata)
	}

	created, err := createQuery.Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create workflow execution").
			Mark(ierr.ErrDatabase)
	}

	*exec = *created
	return nil
}

func (r *workflowExecutionRepository) Get(ctx context.Context, workflowID, runID string) (*ent.WorkflowExecution, error) {
	client := r.client.Reader(ctx)

	r.log.Debugw("getting workflow execution",
		"workflow_id", workflowID,
		"run_id", runID,
	)

	exec, err := client.WorkflowExecution.Query().
		Where(
			workflowexecution.WorkflowID(workflowID),
			workflowexecution.RunID(runID),
		).
		Only(ctx)

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

	return exec, nil
}

func (r *workflowExecutionRepository) List(ctx context.Context, filter *domainWorkflowExecution.ListFilter) ([]*ent.WorkflowExecution, int, error) {
	client := r.client.Reader(ctx)

	r.log.Debugw("listing workflow executions", "filter", filter)

	// Build query
	query := client.WorkflowExecution.Query().
		Where(workflowexecution.TenantID(filter.TenantID))

	// Apply optional filters
	if filter.EnvironmentID != "" {
		query = query.Where(workflowexecution.EnvironmentID(filter.EnvironmentID))
	}
	if filter.WorkflowID != "" {
		query = query.Where(workflowexecution.WorkflowID(filter.WorkflowID))
	}
	if filter.WorkflowType != "" {
		query = query.Where(workflowexecution.WorkflowType(filter.WorkflowType))
	}
	if filter.TaskQueue != "" {
		query = query.Where(workflowexecution.TaskQueue(filter.TaskQueue))
	}
	if filter.WorkflowStatus != "" {
		query = query.Where(workflowexecution.WorkflowStatus(types.WorkflowExecutionStatus(filter.WorkflowStatus)))
	}
	if filter.Entity != "" {
		query = query.Where(workflowexecution.EntityEQ(filter.Entity))
	}
	if filter.EntityID != "" {
		query = query.Where(workflowexecution.EntityIDEQ(filter.EntityID))
	}

	// Get total count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to count workflow executions").
			Mark(ierr.ErrDatabase)
	}

	// Apply pagination
	limit := filter.Limit
	if limit <= 0 {
		limit = filter.PageSize // legacy alias
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	// Legacy alias: page/page_size -> offset
	if offset == 0 && filter.Limit <= 0 && filter.PageSize > 0 {
		page := filter.Page
		if page <= 0 {
			page = 1
		}
		offset = (page - 1) * limit
	}

	// Sorting (allowlist)
	sortField := strings.ToLower(strings.TrimSpace(filter.Sort))
	if sortField == "" {
		sortField = "start_time"
	}
	order := strings.ToLower(strings.TrimSpace(filter.Order))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	sortFieldToOrderBuilder := map[string]func(...sql.OrderTermOption) workflowexecution.OrderOption{
		"start_time":      workflowexecution.ByStartTime,
		"end_time":        workflowexecution.ByEndTime,
		"close_time":      workflowexecution.ByEndTime, // alias
		"created_at":      workflowexecution.ByCreatedAt,
		"updated_at":      workflowexecution.ByUpdatedAt,
		"workflow_id":     workflowexecution.ByWorkflowID,
		"run_id":          workflowexecution.ByRunID,
		"workflow_type":   workflowexecution.ByWorkflowType,
		"task_queue":      workflowexecution.ByTaskQueue,
		"workflow_status": workflowexecution.ByWorkflowStatus,
		"entity":          workflowexecution.ByEntity,
		"entity_id":       workflowexecution.ByEntityID,
	}

	builder, ok := sortFieldToOrderBuilder[sortField]
	if !ok {
		builder = workflowexecution.ByStartTime
		sortField = "start_time"
	}

	var dir sql.OrderTermOption
	if order == "asc" {
		dir = sql.OrderAsc()
	} else {
		dir = sql.OrderDesc()
	}

	orderBys := []workflowexecution.OrderOption{builder(dir)}

	// Stable tie-breakers for consistent pagination
	if sortField != "start_time" {
		orderBys = append(orderBys, workflowexecution.ByStartTime(sql.OrderDesc()))
	}
	orderBys = append(orderBys,
		workflowexecution.ByWorkflowID(sql.OrderDesc()),
		workflowexecution.ByRunID(sql.OrderDesc()),
	)

	// Get paginated results
	executions, err := query.
		Order(orderBys...).
		Limit(limit).
		Offset(offset).
		All(ctx)

	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to list workflow executions").
			Mark(ierr.ErrDatabase)
	}

	return executions, total, nil
}

func (r *workflowExecutionRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("deleting workflow execution", "id", id)

	err := client.WorkflowExecution.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Workflow execution not found: %s", id).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete workflow execution").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *workflowExecutionRepository) UpdateStatus(
	ctx context.Context,
	workflowID string,
	runID string,
	status types.WorkflowExecutionStatus,
	errorMessage string,
	endTime *time.Time,
	durationMs *int64,
) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating workflow execution status",
		"workflow_id", workflowID,
		"run_id", runID,
		"status", status,
		"has_end_time", endTime != nil,
		"has_duration", durationMs != nil,
	)

	// Find the workflow execution
	exec, err := r.Get(ctx, workflowID, runID)
	if err != nil {
		return err
	}

	// Build update query
	updateQuery := client.WorkflowExecution.
		UpdateOne(exec).
		SetWorkflowStatus(status).
		SetUpdatedAt(exec.UpdatedAt) // Will be auto-updated by the hook

	// Set end time if provided
	if endTime != nil {
		updateQuery = updateQuery.SetEndTime(*endTime)
	}

	// Set duration if provided
	if durationMs != nil {
		updateQuery = updateQuery.SetDurationMs(*durationMs)
	}

	// Store error message in metadata if provided
	if errorMessage != "" {
		metadata := exec.Metadata
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		metadata["error"] = errorMessage
		updateQuery = updateQuery.SetMetadata(metadata)
	}

	// Execute update
	_, err = updateQuery.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to update workflow execution status: %s/%s", workflowID, runID).
			Mark(ierr.ErrDatabase)
	}

	r.log.Infow("successfully updated workflow execution status",
		"workflow_id", workflowID,
		"run_id", runID,
		"status", status,
		"end_time", endTime,
		"duration_ms", durationMs,
	)

	return nil
}
