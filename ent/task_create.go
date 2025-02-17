// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/task"
)

// TaskCreate is the builder for creating a Task entity.
type TaskCreate struct {
	config
	mutation *TaskMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (tc *TaskCreate) SetTenantID(s string) *TaskCreate {
	tc.mutation.SetTenantID(s)
	return tc
}

// SetStatus sets the "status" field.
func (tc *TaskCreate) SetStatus(s string) *TaskCreate {
	tc.mutation.SetStatus(s)
	return tc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (tc *TaskCreate) SetNillableStatus(s *string) *TaskCreate {
	if s != nil {
		tc.SetStatus(*s)
	}
	return tc
}

// SetCreatedAt sets the "created_at" field.
func (tc *TaskCreate) SetCreatedAt(t time.Time) *TaskCreate {
	tc.mutation.SetCreatedAt(t)
	return tc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (tc *TaskCreate) SetNillableCreatedAt(t *time.Time) *TaskCreate {
	if t != nil {
		tc.SetCreatedAt(*t)
	}
	return tc
}

// SetUpdatedAt sets the "updated_at" field.
func (tc *TaskCreate) SetUpdatedAt(t time.Time) *TaskCreate {
	tc.mutation.SetUpdatedAt(t)
	return tc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (tc *TaskCreate) SetNillableUpdatedAt(t *time.Time) *TaskCreate {
	if t != nil {
		tc.SetUpdatedAt(*t)
	}
	return tc
}

// SetCreatedBy sets the "created_by" field.
func (tc *TaskCreate) SetCreatedBy(s string) *TaskCreate {
	tc.mutation.SetCreatedBy(s)
	return tc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (tc *TaskCreate) SetNillableCreatedBy(s *string) *TaskCreate {
	if s != nil {
		tc.SetCreatedBy(*s)
	}
	return tc
}

// SetUpdatedBy sets the "updated_by" field.
func (tc *TaskCreate) SetUpdatedBy(s string) *TaskCreate {
	tc.mutation.SetUpdatedBy(s)
	return tc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (tc *TaskCreate) SetNillableUpdatedBy(s *string) *TaskCreate {
	if s != nil {
		tc.SetUpdatedBy(*s)
	}
	return tc
}

// SetTaskType sets the "task_type" field.
func (tc *TaskCreate) SetTaskType(s string) *TaskCreate {
	tc.mutation.SetTaskType(s)
	return tc
}

// SetEntityType sets the "entity_type" field.
func (tc *TaskCreate) SetEntityType(s string) *TaskCreate {
	tc.mutation.SetEntityType(s)
	return tc
}

// SetFileURL sets the "file_url" field.
func (tc *TaskCreate) SetFileURL(s string) *TaskCreate {
	tc.mutation.SetFileURL(s)
	return tc
}

// SetFileName sets the "file_name" field.
func (tc *TaskCreate) SetFileName(s string) *TaskCreate {
	tc.mutation.SetFileName(s)
	return tc
}

// SetNillableFileName sets the "file_name" field if the given value is not nil.
func (tc *TaskCreate) SetNillableFileName(s *string) *TaskCreate {
	if s != nil {
		tc.SetFileName(*s)
	}
	return tc
}

// SetFileType sets the "file_type" field.
func (tc *TaskCreate) SetFileType(s string) *TaskCreate {
	tc.mutation.SetFileType(s)
	return tc
}

// SetTaskStatus sets the "task_status" field.
func (tc *TaskCreate) SetTaskStatus(s string) *TaskCreate {
	tc.mutation.SetTaskStatus(s)
	return tc
}

// SetNillableTaskStatus sets the "task_status" field if the given value is not nil.
func (tc *TaskCreate) SetNillableTaskStatus(s *string) *TaskCreate {
	if s != nil {
		tc.SetTaskStatus(*s)
	}
	return tc
}

// SetTotalRecords sets the "total_records" field.
func (tc *TaskCreate) SetTotalRecords(i int) *TaskCreate {
	tc.mutation.SetTotalRecords(i)
	return tc
}

// SetNillableTotalRecords sets the "total_records" field if the given value is not nil.
func (tc *TaskCreate) SetNillableTotalRecords(i *int) *TaskCreate {
	if i != nil {
		tc.SetTotalRecords(*i)
	}
	return tc
}

// SetProcessedRecords sets the "processed_records" field.
func (tc *TaskCreate) SetProcessedRecords(i int) *TaskCreate {
	tc.mutation.SetProcessedRecords(i)
	return tc
}

// SetNillableProcessedRecords sets the "processed_records" field if the given value is not nil.
func (tc *TaskCreate) SetNillableProcessedRecords(i *int) *TaskCreate {
	if i != nil {
		tc.SetProcessedRecords(*i)
	}
	return tc
}

// SetSuccessfulRecords sets the "successful_records" field.
func (tc *TaskCreate) SetSuccessfulRecords(i int) *TaskCreate {
	tc.mutation.SetSuccessfulRecords(i)
	return tc
}

// SetNillableSuccessfulRecords sets the "successful_records" field if the given value is not nil.
func (tc *TaskCreate) SetNillableSuccessfulRecords(i *int) *TaskCreate {
	if i != nil {
		tc.SetSuccessfulRecords(*i)
	}
	return tc
}

// SetFailedRecords sets the "failed_records" field.
func (tc *TaskCreate) SetFailedRecords(i int) *TaskCreate {
	tc.mutation.SetFailedRecords(i)
	return tc
}

// SetNillableFailedRecords sets the "failed_records" field if the given value is not nil.
func (tc *TaskCreate) SetNillableFailedRecords(i *int) *TaskCreate {
	if i != nil {
		tc.SetFailedRecords(*i)
	}
	return tc
}

// SetErrorSummary sets the "error_summary" field.
func (tc *TaskCreate) SetErrorSummary(s string) *TaskCreate {
	tc.mutation.SetErrorSummary(s)
	return tc
}

// SetNillableErrorSummary sets the "error_summary" field if the given value is not nil.
func (tc *TaskCreate) SetNillableErrorSummary(s *string) *TaskCreate {
	if s != nil {
		tc.SetErrorSummary(*s)
	}
	return tc
}

// SetMetadata sets the "metadata" field.
func (tc *TaskCreate) SetMetadata(m map[string]interface{}) *TaskCreate {
	tc.mutation.SetMetadata(m)
	return tc
}

// SetStartedAt sets the "started_at" field.
func (tc *TaskCreate) SetStartedAt(t time.Time) *TaskCreate {
	tc.mutation.SetStartedAt(t)
	return tc
}

// SetNillableStartedAt sets the "started_at" field if the given value is not nil.
func (tc *TaskCreate) SetNillableStartedAt(t *time.Time) *TaskCreate {
	if t != nil {
		tc.SetStartedAt(*t)
	}
	return tc
}

// SetCompletedAt sets the "completed_at" field.
func (tc *TaskCreate) SetCompletedAt(t time.Time) *TaskCreate {
	tc.mutation.SetCompletedAt(t)
	return tc
}

// SetNillableCompletedAt sets the "completed_at" field if the given value is not nil.
func (tc *TaskCreate) SetNillableCompletedAt(t *time.Time) *TaskCreate {
	if t != nil {
		tc.SetCompletedAt(*t)
	}
	return tc
}

// SetFailedAt sets the "failed_at" field.
func (tc *TaskCreate) SetFailedAt(t time.Time) *TaskCreate {
	tc.mutation.SetFailedAt(t)
	return tc
}

// SetNillableFailedAt sets the "failed_at" field if the given value is not nil.
func (tc *TaskCreate) SetNillableFailedAt(t *time.Time) *TaskCreate {
	if t != nil {
		tc.SetFailedAt(*t)
	}
	return tc
}

// SetID sets the "id" field.
func (tc *TaskCreate) SetID(s string) *TaskCreate {
	tc.mutation.SetID(s)
	return tc
}

// Mutation returns the TaskMutation object of the builder.
func (tc *TaskCreate) Mutation() *TaskMutation {
	return tc.mutation
}

// Save creates the Task in the database.
func (tc *TaskCreate) Save(ctx context.Context) (*Task, error) {
	tc.defaults()
	return withHooks(ctx, tc.sqlSave, tc.mutation, tc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (tc *TaskCreate) SaveX(ctx context.Context) *Task {
	v, err := tc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (tc *TaskCreate) Exec(ctx context.Context) error {
	_, err := tc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (tc *TaskCreate) ExecX(ctx context.Context) {
	if err := tc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (tc *TaskCreate) defaults() {
	if _, ok := tc.mutation.Status(); !ok {
		v := task.DefaultStatus
		tc.mutation.SetStatus(v)
	}
	if _, ok := tc.mutation.CreatedAt(); !ok {
		v := task.DefaultCreatedAt()
		tc.mutation.SetCreatedAt(v)
	}
	if _, ok := tc.mutation.UpdatedAt(); !ok {
		v := task.DefaultUpdatedAt()
		tc.mutation.SetUpdatedAt(v)
	}
	if _, ok := tc.mutation.TaskStatus(); !ok {
		v := task.DefaultTaskStatus
		tc.mutation.SetTaskStatus(v)
	}
	if _, ok := tc.mutation.ProcessedRecords(); !ok {
		v := task.DefaultProcessedRecords
		tc.mutation.SetProcessedRecords(v)
	}
	if _, ok := tc.mutation.SuccessfulRecords(); !ok {
		v := task.DefaultSuccessfulRecords
		tc.mutation.SetSuccessfulRecords(v)
	}
	if _, ok := tc.mutation.FailedRecords(); !ok {
		v := task.DefaultFailedRecords
		tc.mutation.SetFailedRecords(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (tc *TaskCreate) check() error {
	if _, ok := tc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "Task.tenant_id"`)}
	}
	if v, ok := tc.mutation.TenantID(); ok {
		if err := task.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "Task.tenant_id": %w`, err)}
		}
	}
	if _, ok := tc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "Task.status"`)}
	}
	if _, ok := tc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "Task.created_at"`)}
	}
	if _, ok := tc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "Task.updated_at"`)}
	}
	if _, ok := tc.mutation.TaskType(); !ok {
		return &ValidationError{Name: "task_type", err: errors.New(`ent: missing required field "Task.task_type"`)}
	}
	if v, ok := tc.mutation.TaskType(); ok {
		if err := task.TaskTypeValidator(v); err != nil {
			return &ValidationError{Name: "task_type", err: fmt.Errorf(`ent: validator failed for field "Task.task_type": %w`, err)}
		}
	}
	if _, ok := tc.mutation.EntityType(); !ok {
		return &ValidationError{Name: "entity_type", err: errors.New(`ent: missing required field "Task.entity_type"`)}
	}
	if v, ok := tc.mutation.EntityType(); ok {
		if err := task.EntityTypeValidator(v); err != nil {
			return &ValidationError{Name: "entity_type", err: fmt.Errorf(`ent: validator failed for field "Task.entity_type": %w`, err)}
		}
	}
	if _, ok := tc.mutation.FileURL(); !ok {
		return &ValidationError{Name: "file_url", err: errors.New(`ent: missing required field "Task.file_url"`)}
	}
	if v, ok := tc.mutation.FileURL(); ok {
		if err := task.FileURLValidator(v); err != nil {
			return &ValidationError{Name: "file_url", err: fmt.Errorf(`ent: validator failed for field "Task.file_url": %w`, err)}
		}
	}
	if _, ok := tc.mutation.FileType(); !ok {
		return &ValidationError{Name: "file_type", err: errors.New(`ent: missing required field "Task.file_type"`)}
	}
	if v, ok := tc.mutation.FileType(); ok {
		if err := task.FileTypeValidator(v); err != nil {
			return &ValidationError{Name: "file_type", err: fmt.Errorf(`ent: validator failed for field "Task.file_type": %w`, err)}
		}
	}
	if _, ok := tc.mutation.TaskStatus(); !ok {
		return &ValidationError{Name: "task_status", err: errors.New(`ent: missing required field "Task.task_status"`)}
	}
	if _, ok := tc.mutation.ProcessedRecords(); !ok {
		return &ValidationError{Name: "processed_records", err: errors.New(`ent: missing required field "Task.processed_records"`)}
	}
	if _, ok := tc.mutation.SuccessfulRecords(); !ok {
		return &ValidationError{Name: "successful_records", err: errors.New(`ent: missing required field "Task.successful_records"`)}
	}
	if _, ok := tc.mutation.FailedRecords(); !ok {
		return &ValidationError{Name: "failed_records", err: errors.New(`ent: missing required field "Task.failed_records"`)}
	}
	return nil
}

func (tc *TaskCreate) sqlSave(ctx context.Context) (*Task, error) {
	if err := tc.check(); err != nil {
		return nil, err
	}
	_node, _spec := tc.createSpec()
	if err := sqlgraph.CreateNode(ctx, tc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected Task.ID type: %T", _spec.ID.Value)
		}
	}
	tc.mutation.id = &_node.ID
	tc.mutation.done = true
	return _node, nil
}

func (tc *TaskCreate) createSpec() (*Task, *sqlgraph.CreateSpec) {
	var (
		_node = &Task{config: tc.config}
		_spec = sqlgraph.NewCreateSpec(task.Table, sqlgraph.NewFieldSpec(task.FieldID, field.TypeString))
	)
	if id, ok := tc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := tc.mutation.TenantID(); ok {
		_spec.SetField(task.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := tc.mutation.Status(); ok {
		_spec.SetField(task.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := tc.mutation.CreatedAt(); ok {
		_spec.SetField(task.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := tc.mutation.UpdatedAt(); ok {
		_spec.SetField(task.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := tc.mutation.CreatedBy(); ok {
		_spec.SetField(task.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := tc.mutation.UpdatedBy(); ok {
		_spec.SetField(task.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := tc.mutation.TaskType(); ok {
		_spec.SetField(task.FieldTaskType, field.TypeString, value)
		_node.TaskType = value
	}
	if value, ok := tc.mutation.EntityType(); ok {
		_spec.SetField(task.FieldEntityType, field.TypeString, value)
		_node.EntityType = value
	}
	if value, ok := tc.mutation.FileURL(); ok {
		_spec.SetField(task.FieldFileURL, field.TypeString, value)
		_node.FileURL = value
	}
	if value, ok := tc.mutation.FileName(); ok {
		_spec.SetField(task.FieldFileName, field.TypeString, value)
		_node.FileName = &value
	}
	if value, ok := tc.mutation.FileType(); ok {
		_spec.SetField(task.FieldFileType, field.TypeString, value)
		_node.FileType = value
	}
	if value, ok := tc.mutation.TaskStatus(); ok {
		_spec.SetField(task.FieldTaskStatus, field.TypeString, value)
		_node.TaskStatus = value
	}
	if value, ok := tc.mutation.TotalRecords(); ok {
		_spec.SetField(task.FieldTotalRecords, field.TypeInt, value)
		_node.TotalRecords = &value
	}
	if value, ok := tc.mutation.ProcessedRecords(); ok {
		_spec.SetField(task.FieldProcessedRecords, field.TypeInt, value)
		_node.ProcessedRecords = value
	}
	if value, ok := tc.mutation.SuccessfulRecords(); ok {
		_spec.SetField(task.FieldSuccessfulRecords, field.TypeInt, value)
		_node.SuccessfulRecords = value
	}
	if value, ok := tc.mutation.FailedRecords(); ok {
		_spec.SetField(task.FieldFailedRecords, field.TypeInt, value)
		_node.FailedRecords = value
	}
	if value, ok := tc.mutation.ErrorSummary(); ok {
		_spec.SetField(task.FieldErrorSummary, field.TypeString, value)
		_node.ErrorSummary = &value
	}
	if value, ok := tc.mutation.Metadata(); ok {
		_spec.SetField(task.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if value, ok := tc.mutation.StartedAt(); ok {
		_spec.SetField(task.FieldStartedAt, field.TypeTime, value)
		_node.StartedAt = &value
	}
	if value, ok := tc.mutation.CompletedAt(); ok {
		_spec.SetField(task.FieldCompletedAt, field.TypeTime, value)
		_node.CompletedAt = &value
	}
	if value, ok := tc.mutation.FailedAt(); ok {
		_spec.SetField(task.FieldFailedAt, field.TypeTime, value)
		_node.FailedAt = &value
	}
	return _node, _spec
}

// TaskCreateBulk is the builder for creating many Task entities in bulk.
type TaskCreateBulk struct {
	config
	err      error
	builders []*TaskCreate
}

// Save creates the Task entities in the database.
func (tcb *TaskCreateBulk) Save(ctx context.Context) ([]*Task, error) {
	if tcb.err != nil {
		return nil, tcb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(tcb.builders))
	nodes := make([]*Task, len(tcb.builders))
	mutators := make([]Mutator, len(tcb.builders))
	for i := range tcb.builders {
		func(i int, root context.Context) {
			builder := tcb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*TaskMutation)
				if !ok {
					return nil, fmt.Errorf("unexpected mutation type %T", m)
				}
				if err := builder.check(); err != nil {
					return nil, err
				}
				builder.mutation = mutation
				var err error
				nodes[i], specs[i] = builder.createSpec()
				if i < len(mutators)-1 {
					_, err = mutators[i+1].Mutate(root, tcb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, tcb.driver, spec); err != nil {
						if sqlgraph.IsConstraintError(err) {
							err = &ConstraintError{msg: err.Error(), wrap: err}
						}
					}
				}
				if err != nil {
					return nil, err
				}
				mutation.id = &nodes[i].ID
				mutation.done = true
				return nodes[i], nil
			})
			for i := len(builder.hooks) - 1; i >= 0; i-- {
				mut = builder.hooks[i](mut)
			}
			mutators[i] = mut
		}(i, ctx)
	}
	if len(mutators) > 0 {
		if _, err := mutators[0].Mutate(ctx, tcb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (tcb *TaskCreateBulk) SaveX(ctx context.Context) []*Task {
	v, err := tcb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (tcb *TaskCreateBulk) Exec(ctx context.Context) error {
	_, err := tcb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (tcb *TaskCreateBulk) ExecX(ctx context.Context) {
	if err := tcb.Exec(ctx); err != nil {
		panic(err)
	}
}
