package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/scheduledtask"
	domainst "github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// ScheduledTaskRepository implements the scheduled task repository using ent
type ScheduledTaskRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewScheduledTaskRepository creates a new scheduled task repository
func NewScheduledTaskRepository(client postgres.IClient, logger *logger.Logger) *ScheduledTaskRepository {
	return &ScheduledTaskRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new scheduled task
func (r *ScheduledTaskRepository) Create(ctx context.Context, task *domainst.ScheduledTask) error {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	client := r.client.Writer(ctx)
	create := client.ScheduledTask.
		Create().
		SetID(task.ID).
		SetTenantID(tenantID).
		SetEnvironmentID(environmentID).
		SetConnectionID(task.ConnectionID).
		SetEntityType(task.EntityType).
		SetInterval(task.Interval).
		SetEnabled(task.Enabled).
		SetManagedCopy(task.ManagedCopy).
		SetStatus(string(task.Status)).
		SetCreatedBy(task.CreatedBy).
		SetUpdatedBy(task.UpdatedBy)

	if task.Name != "" {
		create = create.SetName(task.Name)
	}
	if task.Origin != "" {
		create = create.SetOrigin(task.Origin)
	}
	if task.Schedule != nil {
		create = create.SetSchedule(task.Schedule)
	}
	if task.SourceConfig != nil {
		create = create.SetSourceConfig(task.SourceConfig)
	}
	if task.EmailConfig != nil {
		create = create.SetEmailConfig(task.EmailConfig)
	}
	if task.JobConfig != nil {
		create = create.SetJobConfig(task.JobConfig)
	}
	if task.TemporalScheduleID != "" {
		create = create.SetTemporalScheduleID(task.TemporalScheduleID)
	}

	_, err := create.Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create scheduled task").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// Get retrieves a scheduled task by ID
func (r *ScheduledTaskRepository) Get(ctx context.Context, id string) (*domainst.ScheduledTask, error) {
	client := r.client.Reader(ctx)
	task, err := client.ScheduledTask.
		Query().
		Where(
			scheduledtask.ID(id),
			scheduledtask.TenantID(types.GetTenantID(ctx)),
			scheduledtask.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("scheduled task not found").
				WithHint("Scheduled task with given ID not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get scheduled task").
			Mark(ierr.ErrDatabase)
	}

	return r.entToModel(task), nil
}

// Update updates an existing scheduled task
func (r *ScheduledTaskRepository) Update(ctx context.Context, task *domainst.ScheduledTask) error {
	client := r.client.Writer(ctx)
	update := client.ScheduledTask.
		Update().
		Where(
			scheduledtask.ID(task.ID),
			scheduledtask.TenantID(types.GetTenantID(ctx)),
			scheduledtask.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetConnectionID(task.ConnectionID).
		SetEntityType(task.EntityType).
		SetInterval(task.Interval).
		SetEnabled(task.Enabled).
		SetStatus(string(task.Status)).
		SetUpdatedBy(task.UpdatedBy)

	if task.JobConfig != nil {
		update = update.SetJobConfig(task.JobConfig)
	}
	if task.TemporalScheduleID != "" {
		update = update.SetTemporalScheduleID(task.TemporalScheduleID)
	}

	err := update.Exec(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update scheduled task").
			Mark(ierr.ErrDatabase)
	}

	// Check if any rows were affected by doing a count query
	count, err := client.ScheduledTask.
		Query().
		Where(
			scheduledtask.ID(task.ID),
			scheduledtask.TenantID(types.GetTenantID(ctx)),
			scheduledtask.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Count(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to verify update").
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		return ierr.NewError("scheduled task not found").
			WithHint("No rows matched ID + tenant + environment").
			Mark(ierr.ErrNotFound)
	}

	return nil
}

// UpdateSettings writes editable settings.
func (r *ScheduledTaskRepository) UpdateSettings(ctx context.Context, task *domainst.ScheduledTask) error {
	update := r.client.Writer(ctx).ScheduledTask.
		UpdateOneID(task.ID).
		Where(
			scheduledtask.TenantID(types.GetTenantID(ctx)),
			scheduledtask.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetEnabled(task.Enabled).
		SetUpdatedBy(task.UpdatedBy)

	if task.Name != "" {
		update = update.SetName(task.Name)
	} else {
		update = update.ClearName()
	}
	if task.Schedule != nil {
		update = update.SetSchedule(task.Schedule)
	} else {
		update = update.ClearSchedule()
	}
	if task.TemporalScheduleID != "" {
		update = update.SetTemporalScheduleID(task.TemporalScheduleID)
	} else {
		update = update.ClearTemporalScheduleID()
	}
	if task.EmailConfig != nil {
		update = update.SetEmailConfig(task.EmailConfig)
	} else {
		update = update.ClearEmailConfig()
	}

	saved, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.NewError("scheduled task not found").
				WithHint("No rows matched ID + tenant + environment").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update scheduled task settings").
			Mark(ierr.ErrDatabase)
	}
	task.UpdatedAt = saved.UpdatedAt
	return nil
}

// Delete deletes a scheduled task
func (r *ScheduledTaskRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)
	affected, err := client.ScheduledTask.
		Delete().
		Where(
			scheduledtask.ID(id),
			scheduledtask.TenantID(types.GetTenantID(ctx)),
			scheduledtask.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete scheduled task").
			Mark(ierr.ErrDatabase)
	}

	if affected == 0 {
		return ierr.NewError("scheduled task not found").
			WithHint("No rows matched ID + tenant + environment").
			Mark(ierr.ErrNotFound)
	}

	return nil
}

// List retrieves all scheduled tasks with optional filters
func (r *ScheduledTaskRepository) List(ctx context.Context, filters *domainst.ListFilters) ([]*domainst.ScheduledTask, error) {
	client := r.client.Reader(ctx)
	query := client.ScheduledTask.Query()

	// Apply tenant and environment filters
	tenantID := filters.TenantID
	if tenantID == "" {
		tenantID = types.GetTenantID(ctx)
	}
	environmentID := filters.EnvironmentID
	if environmentID == "" {
		environmentID = types.GetEnvironmentID(ctx)
	}

	query = query.Where(
		scheduledtask.TenantID(tenantID),
		scheduledtask.EnvironmentID(environmentID),
		scheduledtask.Status(string(types.StatusPublished)),
	)

	// Apply optional filters
	if filters.ConnectionID != "" {
		query = query.Where(scheduledtask.ConnectionID(filters.ConnectionID))
	}
	if filters.EntityType != "" {
		query = query.Where(scheduledtask.EntityType(filters.EntityType))
	}
	if filters.Interval != "" {
		query = query.Where(scheduledtask.Interval(filters.Interval))
	}
	if filters.Enabled != nil {
		query = query.Where(scheduledtask.Enabled(*filters.Enabled))
	}
	if filters.Origin != "" {
		query = query.Where(scheduledtask.Origin(filters.Origin))
	}
	if filters.ExcludeOrigin != "" {
		query = query.Where(scheduledtask.OriginNEQ(filters.ExcludeOrigin))
	}

	// Apply pagination
	if filters.Limit > 0 {
		query = query.Limit(filters.Limit)
	}
	if filters.Offset > 0 {
		query = query.Offset(filters.Offset)
	}

	tasks, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list scheduled tasks").
			Mark(ierr.ErrDatabase)
	}

	return r.entSliceToModel(tasks), nil
}

// GetByConnection retrieves all scheduled tasks for a specific connection
func (r *ScheduledTaskRepository) GetByConnection(ctx context.Context, connectionID string) ([]*domainst.ScheduledTask, error) {
	client := r.client.Reader(ctx)
	tasks, err := client.ScheduledTask.
		Query().
		Where(
			scheduledtask.ConnectionID(connectionID),
			scheduledtask.TenantID(types.GetTenantID(ctx)),
			scheduledtask.EnvironmentID(types.GetEnvironmentID(ctx)),
			// Only return active tasks; deleted/archived tasks should not be acted on (e.g. during connection deletion).
			scheduledtask.Status(string(types.StatusPublished)),
		).
		All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get scheduled tasks by connection").
			Mark(ierr.ErrDatabase)
	}

	return r.entSliceToModel(tasks), nil
}

// entToModel converts an ent ScheduledTask to domain model
func (r *ScheduledTaskRepository) entToModel(entTask *ent.ScheduledTask) *domainst.ScheduledTask {
	if entTask == nil {
		return nil
	}

	return &domainst.ScheduledTask{
		ID:                 entTask.ID,
		TenantID:           entTask.TenantID,
		EnvironmentID:      entTask.EnvironmentID,
		ConnectionID:       entTask.ConnectionID,
		EntityType:         entTask.EntityType,
		Interval:           entTask.Interval,
		Enabled:            entTask.Enabled,
		JobConfig:          entTask.JobConfig,
		TemporalScheduleID: entTask.TemporalScheduleID,
		Name:               lo.FromPtr(entTask.Name),
		ManagedCopy:        entTask.ManagedCopy,
		Schedule:           entTask.Schedule,
		SourceConfig:       entTask.SourceConfig,
		EmailConfig:        entTask.EmailConfig,
		Origin:             entTask.Origin,
		Status:            types.Status(entTask.Status),
		CreatedAt:          entTask.CreatedAt,
		UpdatedAt:          entTask.UpdatedAt,
		CreatedBy:          entTask.CreatedBy,
		UpdatedBy:          entTask.UpdatedBy,
	}
}

// entSliceToModel converts a slice of ent ScheduledTasks to domain models
func (r *ScheduledTaskRepository) entSliceToModel(entTasks []*ent.ScheduledTask) []*domainst.ScheduledTask {
	if entTasks == nil {
		return nil
	}

	tasks := make([]*domainst.ScheduledTask, len(entTasks))
	for i, entTask := range entTasks {
		tasks[i] = r.entToModel(entTask)
	}
	return tasks
}
