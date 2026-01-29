package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/workflowexecution"
	domainWorkflowExecution "github.com/flexprice/flexprice/internal/domain/workflowexecution"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
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
		SetCreatedAt(exec.CreatedAt).
		SetUpdatedAt(exec.UpdatedAt).
		SetUpdatedBy(exec.UpdatedBy)

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
	if filter.WorkflowType != "" {
		query = query.Where(workflowexecution.WorkflowType(filter.WorkflowType))
	}
	if filter.TaskQueue != "" {
		query = query.Where(workflowexecution.TaskQueue(filter.TaskQueue))
	}

	// Get total count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to count workflow executions").
			Mark(ierr.ErrDatabase)
	}

	// Apply pagination
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	page := filter.Page
	if page <= 0 {
		page = 1
	}

	offset := (page - 1) * pageSize

	// Get paginated results
	executions, err := query.
		Order(ent.Desc(workflowexecution.FieldStartTime)).
		Limit(pageSize).
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
