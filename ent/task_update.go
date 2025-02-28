// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/task"
)

// TaskUpdate is the builder for updating Task entities.
type TaskUpdate struct {
	config
	hooks    []Hook
	mutation *TaskMutation
}

// Where appends a list predicates to the TaskUpdate builder.
func (tu *TaskUpdate) Where(ps ...predicate.Task) *TaskUpdate {
	tu.mutation.Where(ps...)
	return tu
}

// SetStatus sets the "status" field.
func (tu *TaskUpdate) SetStatus(s string) *TaskUpdate {
	tu.mutation.SetStatus(s)
	return tu
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableStatus(s *string) *TaskUpdate {
	if s != nil {
		tu.SetStatus(*s)
	}
	return tu
}

// SetUpdatedAt sets the "updated_at" field.
func (tu *TaskUpdate) SetUpdatedAt(t time.Time) *TaskUpdate {
	tu.mutation.SetUpdatedAt(t)
	return tu
}

// SetUpdatedBy sets the "updated_by" field.
func (tu *TaskUpdate) SetUpdatedBy(s string) *TaskUpdate {
	tu.mutation.SetUpdatedBy(s)
	return tu
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableUpdatedBy(s *string) *TaskUpdate {
	if s != nil {
		tu.SetUpdatedBy(*s)
	}
	return tu
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (tu *TaskUpdate) ClearUpdatedBy() *TaskUpdate {
	tu.mutation.ClearUpdatedBy()
	return tu
}

// SetTaskType sets the "task_type" field.
func (tu *TaskUpdate) SetTaskType(s string) *TaskUpdate {
	tu.mutation.SetTaskType(s)
	return tu
}

// SetNillableTaskType sets the "task_type" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableTaskType(s *string) *TaskUpdate {
	if s != nil {
		tu.SetTaskType(*s)
	}
	return tu
}

// SetEntityType sets the "entity_type" field.
func (tu *TaskUpdate) SetEntityType(s string) *TaskUpdate {
	tu.mutation.SetEntityType(s)
	return tu
}

// SetNillableEntityType sets the "entity_type" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableEntityType(s *string) *TaskUpdate {
	if s != nil {
		tu.SetEntityType(*s)
	}
	return tu
}

// SetFileURL sets the "file_url" field.
func (tu *TaskUpdate) SetFileURL(s string) *TaskUpdate {
	tu.mutation.SetFileURL(s)
	return tu
}

// SetNillableFileURL sets the "file_url" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableFileURL(s *string) *TaskUpdate {
	if s != nil {
		tu.SetFileURL(*s)
	}
	return tu
}

// SetFileName sets the "file_name" field.
func (tu *TaskUpdate) SetFileName(s string) *TaskUpdate {
	tu.mutation.SetFileName(s)
	return tu
}

// SetNillableFileName sets the "file_name" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableFileName(s *string) *TaskUpdate {
	if s != nil {
		tu.SetFileName(*s)
	}
	return tu
}

// ClearFileName clears the value of the "file_name" field.
func (tu *TaskUpdate) ClearFileName() *TaskUpdate {
	tu.mutation.ClearFileName()
	return tu
}

// SetFileType sets the "file_type" field.
func (tu *TaskUpdate) SetFileType(s string) *TaskUpdate {
	tu.mutation.SetFileType(s)
	return tu
}

// SetNillableFileType sets the "file_type" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableFileType(s *string) *TaskUpdate {
	if s != nil {
		tu.SetFileType(*s)
	}
	return tu
}

// SetTaskStatus sets the "task_status" field.
func (tu *TaskUpdate) SetTaskStatus(s string) *TaskUpdate {
	tu.mutation.SetTaskStatus(s)
	return tu
}

// SetNillableTaskStatus sets the "task_status" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableTaskStatus(s *string) *TaskUpdate {
	if s != nil {
		tu.SetTaskStatus(*s)
	}
	return tu
}

// SetTotalRecords sets the "total_records" field.
func (tu *TaskUpdate) SetTotalRecords(i int) *TaskUpdate {
	tu.mutation.ResetTotalRecords()
	tu.mutation.SetTotalRecords(i)
	return tu
}

// SetNillableTotalRecords sets the "total_records" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableTotalRecords(i *int) *TaskUpdate {
	if i != nil {
		tu.SetTotalRecords(*i)
	}
	return tu
}

// AddTotalRecords adds i to the "total_records" field.
func (tu *TaskUpdate) AddTotalRecords(i int) *TaskUpdate {
	tu.mutation.AddTotalRecords(i)
	return tu
}

// ClearTotalRecords clears the value of the "total_records" field.
func (tu *TaskUpdate) ClearTotalRecords() *TaskUpdate {
	tu.mutation.ClearTotalRecords()
	return tu
}

// SetProcessedRecords sets the "processed_records" field.
func (tu *TaskUpdate) SetProcessedRecords(i int) *TaskUpdate {
	tu.mutation.ResetProcessedRecords()
	tu.mutation.SetProcessedRecords(i)
	return tu
}

// SetNillableProcessedRecords sets the "processed_records" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableProcessedRecords(i *int) *TaskUpdate {
	if i != nil {
		tu.SetProcessedRecords(*i)
	}
	return tu
}

// AddProcessedRecords adds i to the "processed_records" field.
func (tu *TaskUpdate) AddProcessedRecords(i int) *TaskUpdate {
	tu.mutation.AddProcessedRecords(i)
	return tu
}

// SetSuccessfulRecords sets the "successful_records" field.
func (tu *TaskUpdate) SetSuccessfulRecords(i int) *TaskUpdate {
	tu.mutation.ResetSuccessfulRecords()
	tu.mutation.SetSuccessfulRecords(i)
	return tu
}

// SetNillableSuccessfulRecords sets the "successful_records" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableSuccessfulRecords(i *int) *TaskUpdate {
	if i != nil {
		tu.SetSuccessfulRecords(*i)
	}
	return tu
}

// AddSuccessfulRecords adds i to the "successful_records" field.
func (tu *TaskUpdate) AddSuccessfulRecords(i int) *TaskUpdate {
	tu.mutation.AddSuccessfulRecords(i)
	return tu
}

// SetFailedRecords sets the "failed_records" field.
func (tu *TaskUpdate) SetFailedRecords(i int) *TaskUpdate {
	tu.mutation.ResetFailedRecords()
	tu.mutation.SetFailedRecords(i)
	return tu
}

// SetNillableFailedRecords sets the "failed_records" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableFailedRecords(i *int) *TaskUpdate {
	if i != nil {
		tu.SetFailedRecords(*i)
	}
	return tu
}

// AddFailedRecords adds i to the "failed_records" field.
func (tu *TaskUpdate) AddFailedRecords(i int) *TaskUpdate {
	tu.mutation.AddFailedRecords(i)
	return tu
}

// SetErrorSummary sets the "error_summary" field.
func (tu *TaskUpdate) SetErrorSummary(s string) *TaskUpdate {
	tu.mutation.SetErrorSummary(s)
	return tu
}

// SetNillableErrorSummary sets the "error_summary" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableErrorSummary(s *string) *TaskUpdate {
	if s != nil {
		tu.SetErrorSummary(*s)
	}
	return tu
}

// ClearErrorSummary clears the value of the "error_summary" field.
func (tu *TaskUpdate) ClearErrorSummary() *TaskUpdate {
	tu.mutation.ClearErrorSummary()
	return tu
}

// SetMetadata sets the "metadata" field.
func (tu *TaskUpdate) SetMetadata(m map[string]interface{}) *TaskUpdate {
	tu.mutation.SetMetadata(m)
	return tu
}

// ClearMetadata clears the value of the "metadata" field.
func (tu *TaskUpdate) ClearMetadata() *TaskUpdate {
	tu.mutation.ClearMetadata()
	return tu
}

// SetStartedAt sets the "started_at" field.
func (tu *TaskUpdate) SetStartedAt(t time.Time) *TaskUpdate {
	tu.mutation.SetStartedAt(t)
	return tu
}

// SetNillableStartedAt sets the "started_at" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableStartedAt(t *time.Time) *TaskUpdate {
	if t != nil {
		tu.SetStartedAt(*t)
	}
	return tu
}

// ClearStartedAt clears the value of the "started_at" field.
func (tu *TaskUpdate) ClearStartedAt() *TaskUpdate {
	tu.mutation.ClearStartedAt()
	return tu
}

// SetCompletedAt sets the "completed_at" field.
func (tu *TaskUpdate) SetCompletedAt(t time.Time) *TaskUpdate {
	tu.mutation.SetCompletedAt(t)
	return tu
}

// SetNillableCompletedAt sets the "completed_at" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableCompletedAt(t *time.Time) *TaskUpdate {
	if t != nil {
		tu.SetCompletedAt(*t)
	}
	return tu
}

// ClearCompletedAt clears the value of the "completed_at" field.
func (tu *TaskUpdate) ClearCompletedAt() *TaskUpdate {
	tu.mutation.ClearCompletedAt()
	return tu
}

// SetFailedAt sets the "failed_at" field.
func (tu *TaskUpdate) SetFailedAt(t time.Time) *TaskUpdate {
	tu.mutation.SetFailedAt(t)
	return tu
}

// SetNillableFailedAt sets the "failed_at" field if the given value is not nil.
func (tu *TaskUpdate) SetNillableFailedAt(t *time.Time) *TaskUpdate {
	if t != nil {
		tu.SetFailedAt(*t)
	}
	return tu
}

// ClearFailedAt clears the value of the "failed_at" field.
func (tu *TaskUpdate) ClearFailedAt() *TaskUpdate {
	tu.mutation.ClearFailedAt()
	return tu
}

// Mutation returns the TaskMutation object of the builder.
func (tu *TaskUpdate) Mutation() *TaskMutation {
	return tu.mutation
}

// Save executes the query and returns the number of nodes affected by the update operation.
func (tu *TaskUpdate) Save(ctx context.Context) (int, error) {
	tu.defaults()
	return withHooks(ctx, tu.sqlSave, tu.mutation, tu.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (tu *TaskUpdate) SaveX(ctx context.Context) int {
	affected, err := tu.Save(ctx)
	if err != nil {
		panic(err)
	}
	return affected
}

// Exec executes the query.
func (tu *TaskUpdate) Exec(ctx context.Context) error {
	_, err := tu.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (tu *TaskUpdate) ExecX(ctx context.Context) {
	if err := tu.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (tu *TaskUpdate) defaults() {
	if _, ok := tu.mutation.UpdatedAt(); !ok {
		v := task.UpdateDefaultUpdatedAt()
		tu.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (tu *TaskUpdate) check() error {
	if v, ok := tu.mutation.TaskType(); ok {
		if err := task.TaskTypeValidator(v); err != nil {
			return &ValidationError{Name: "task_type", err: fmt.Errorf(`ent: validator failed for field "Task.task_type": %w`, err)}
		}
	}
	if v, ok := tu.mutation.EntityType(); ok {
		if err := task.EntityTypeValidator(v); err != nil {
			return &ValidationError{Name: "entity_type", err: fmt.Errorf(`ent: validator failed for field "Task.entity_type": %w`, err)}
		}
	}
	if v, ok := tu.mutation.FileURL(); ok {
		if err := task.FileURLValidator(v); err != nil {
			return &ValidationError{Name: "file_url", err: fmt.Errorf(`ent: validator failed for field "Task.file_url": %w`, err)}
		}
	}
	if v, ok := tu.mutation.FileType(); ok {
		if err := task.FileTypeValidator(v); err != nil {
			return &ValidationError{Name: "file_type", err: fmt.Errorf(`ent: validator failed for field "Task.file_type": %w`, err)}
		}
	}
	return nil
}

func (tu *TaskUpdate) sqlSave(ctx context.Context) (n int, err error) {
	if err := tu.check(); err != nil {
		return n, err
	}
	_spec := sqlgraph.NewUpdateSpec(task.Table, task.Columns, sqlgraph.NewFieldSpec(task.FieldID, field.TypeString))
	if ps := tu.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := tu.mutation.Status(); ok {
		_spec.SetField(task.FieldStatus, field.TypeString, value)
	}
	if value, ok := tu.mutation.UpdatedAt(); ok {
		_spec.SetField(task.FieldUpdatedAt, field.TypeTime, value)
	}
	if tu.mutation.CreatedByCleared() {
		_spec.ClearField(task.FieldCreatedBy, field.TypeString)
	}
	if value, ok := tu.mutation.UpdatedBy(); ok {
		_spec.SetField(task.FieldUpdatedBy, field.TypeString, value)
	}
	if tu.mutation.UpdatedByCleared() {
		_spec.ClearField(task.FieldUpdatedBy, field.TypeString)
	}
	if tu.mutation.EnvironmentIDCleared() {
		_spec.ClearField(task.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := tu.mutation.TaskType(); ok {
		_spec.SetField(task.FieldTaskType, field.TypeString, value)
	}
	if value, ok := tu.mutation.EntityType(); ok {
		_spec.SetField(task.FieldEntityType, field.TypeString, value)
	}
	if value, ok := tu.mutation.FileURL(); ok {
		_spec.SetField(task.FieldFileURL, field.TypeString, value)
	}
	if value, ok := tu.mutation.FileName(); ok {
		_spec.SetField(task.FieldFileName, field.TypeString, value)
	}
	if tu.mutation.FileNameCleared() {
		_spec.ClearField(task.FieldFileName, field.TypeString)
	}
	if value, ok := tu.mutation.FileType(); ok {
		_spec.SetField(task.FieldFileType, field.TypeString, value)
	}
	if value, ok := tu.mutation.TaskStatus(); ok {
		_spec.SetField(task.FieldTaskStatus, field.TypeString, value)
	}
	if value, ok := tu.mutation.TotalRecords(); ok {
		_spec.SetField(task.FieldTotalRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.AddedTotalRecords(); ok {
		_spec.AddField(task.FieldTotalRecords, field.TypeInt, value)
	}
	if tu.mutation.TotalRecordsCleared() {
		_spec.ClearField(task.FieldTotalRecords, field.TypeInt)
	}
	if value, ok := tu.mutation.ProcessedRecords(); ok {
		_spec.SetField(task.FieldProcessedRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.AddedProcessedRecords(); ok {
		_spec.AddField(task.FieldProcessedRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.SuccessfulRecords(); ok {
		_spec.SetField(task.FieldSuccessfulRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.AddedSuccessfulRecords(); ok {
		_spec.AddField(task.FieldSuccessfulRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.FailedRecords(); ok {
		_spec.SetField(task.FieldFailedRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.AddedFailedRecords(); ok {
		_spec.AddField(task.FieldFailedRecords, field.TypeInt, value)
	}
	if value, ok := tu.mutation.ErrorSummary(); ok {
		_spec.SetField(task.FieldErrorSummary, field.TypeString, value)
	}
	if tu.mutation.ErrorSummaryCleared() {
		_spec.ClearField(task.FieldErrorSummary, field.TypeString)
	}
	if value, ok := tu.mutation.Metadata(); ok {
		_spec.SetField(task.FieldMetadata, field.TypeJSON, value)
	}
	if tu.mutation.MetadataCleared() {
		_spec.ClearField(task.FieldMetadata, field.TypeJSON)
	}
	if value, ok := tu.mutation.StartedAt(); ok {
		_spec.SetField(task.FieldStartedAt, field.TypeTime, value)
	}
	if tu.mutation.StartedAtCleared() {
		_spec.ClearField(task.FieldStartedAt, field.TypeTime)
	}
	if value, ok := tu.mutation.CompletedAt(); ok {
		_spec.SetField(task.FieldCompletedAt, field.TypeTime, value)
	}
	if tu.mutation.CompletedAtCleared() {
		_spec.ClearField(task.FieldCompletedAt, field.TypeTime)
	}
	if value, ok := tu.mutation.FailedAt(); ok {
		_spec.SetField(task.FieldFailedAt, field.TypeTime, value)
	}
	if tu.mutation.FailedAtCleared() {
		_spec.ClearField(task.FieldFailedAt, field.TypeTime)
	}
	if n, err = sqlgraph.UpdateNodes(ctx, tu.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{task.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return 0, err
	}
	tu.mutation.done = true
	return n, nil
}

// TaskUpdateOne is the builder for updating a single Task entity.
type TaskUpdateOne struct {
	config
	fields   []string
	hooks    []Hook
	mutation *TaskMutation
}

// SetStatus sets the "status" field.
func (tuo *TaskUpdateOne) SetStatus(s string) *TaskUpdateOne {
	tuo.mutation.SetStatus(s)
	return tuo
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableStatus(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetStatus(*s)
	}
	return tuo
}

// SetUpdatedAt sets the "updated_at" field.
func (tuo *TaskUpdateOne) SetUpdatedAt(t time.Time) *TaskUpdateOne {
	tuo.mutation.SetUpdatedAt(t)
	return tuo
}

// SetUpdatedBy sets the "updated_by" field.
func (tuo *TaskUpdateOne) SetUpdatedBy(s string) *TaskUpdateOne {
	tuo.mutation.SetUpdatedBy(s)
	return tuo
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableUpdatedBy(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetUpdatedBy(*s)
	}
	return tuo
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (tuo *TaskUpdateOne) ClearUpdatedBy() *TaskUpdateOne {
	tuo.mutation.ClearUpdatedBy()
	return tuo
}

// SetTaskType sets the "task_type" field.
func (tuo *TaskUpdateOne) SetTaskType(s string) *TaskUpdateOne {
	tuo.mutation.SetTaskType(s)
	return tuo
}

// SetNillableTaskType sets the "task_type" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableTaskType(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetTaskType(*s)
	}
	return tuo
}

// SetEntityType sets the "entity_type" field.
func (tuo *TaskUpdateOne) SetEntityType(s string) *TaskUpdateOne {
	tuo.mutation.SetEntityType(s)
	return tuo
}

// SetNillableEntityType sets the "entity_type" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableEntityType(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetEntityType(*s)
	}
	return tuo
}

// SetFileURL sets the "file_url" field.
func (tuo *TaskUpdateOne) SetFileURL(s string) *TaskUpdateOne {
	tuo.mutation.SetFileURL(s)
	return tuo
}

// SetNillableFileURL sets the "file_url" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableFileURL(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetFileURL(*s)
	}
	return tuo
}

// SetFileName sets the "file_name" field.
func (tuo *TaskUpdateOne) SetFileName(s string) *TaskUpdateOne {
	tuo.mutation.SetFileName(s)
	return tuo
}

// SetNillableFileName sets the "file_name" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableFileName(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetFileName(*s)
	}
	return tuo
}

// ClearFileName clears the value of the "file_name" field.
func (tuo *TaskUpdateOne) ClearFileName() *TaskUpdateOne {
	tuo.mutation.ClearFileName()
	return tuo
}

// SetFileType sets the "file_type" field.
func (tuo *TaskUpdateOne) SetFileType(s string) *TaskUpdateOne {
	tuo.mutation.SetFileType(s)
	return tuo
}

// SetNillableFileType sets the "file_type" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableFileType(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetFileType(*s)
	}
	return tuo
}

// SetTaskStatus sets the "task_status" field.
func (tuo *TaskUpdateOne) SetTaskStatus(s string) *TaskUpdateOne {
	tuo.mutation.SetTaskStatus(s)
	return tuo
}

// SetNillableTaskStatus sets the "task_status" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableTaskStatus(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetTaskStatus(*s)
	}
	return tuo
}

// SetTotalRecords sets the "total_records" field.
func (tuo *TaskUpdateOne) SetTotalRecords(i int) *TaskUpdateOne {
	tuo.mutation.ResetTotalRecords()
	tuo.mutation.SetTotalRecords(i)
	return tuo
}

// SetNillableTotalRecords sets the "total_records" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableTotalRecords(i *int) *TaskUpdateOne {
	if i != nil {
		tuo.SetTotalRecords(*i)
	}
	return tuo
}

// AddTotalRecords adds i to the "total_records" field.
func (tuo *TaskUpdateOne) AddTotalRecords(i int) *TaskUpdateOne {
	tuo.mutation.AddTotalRecords(i)
	return tuo
}

// ClearTotalRecords clears the value of the "total_records" field.
func (tuo *TaskUpdateOne) ClearTotalRecords() *TaskUpdateOne {
	tuo.mutation.ClearTotalRecords()
	return tuo
}

// SetProcessedRecords sets the "processed_records" field.
func (tuo *TaskUpdateOne) SetProcessedRecords(i int) *TaskUpdateOne {
	tuo.mutation.ResetProcessedRecords()
	tuo.mutation.SetProcessedRecords(i)
	return tuo
}

// SetNillableProcessedRecords sets the "processed_records" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableProcessedRecords(i *int) *TaskUpdateOne {
	if i != nil {
		tuo.SetProcessedRecords(*i)
	}
	return tuo
}

// AddProcessedRecords adds i to the "processed_records" field.
func (tuo *TaskUpdateOne) AddProcessedRecords(i int) *TaskUpdateOne {
	tuo.mutation.AddProcessedRecords(i)
	return tuo
}

// SetSuccessfulRecords sets the "successful_records" field.
func (tuo *TaskUpdateOne) SetSuccessfulRecords(i int) *TaskUpdateOne {
	tuo.mutation.ResetSuccessfulRecords()
	tuo.mutation.SetSuccessfulRecords(i)
	return tuo
}

// SetNillableSuccessfulRecords sets the "successful_records" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableSuccessfulRecords(i *int) *TaskUpdateOne {
	if i != nil {
		tuo.SetSuccessfulRecords(*i)
	}
	return tuo
}

// AddSuccessfulRecords adds i to the "successful_records" field.
func (tuo *TaskUpdateOne) AddSuccessfulRecords(i int) *TaskUpdateOne {
	tuo.mutation.AddSuccessfulRecords(i)
	return tuo
}

// SetFailedRecords sets the "failed_records" field.
func (tuo *TaskUpdateOne) SetFailedRecords(i int) *TaskUpdateOne {
	tuo.mutation.ResetFailedRecords()
	tuo.mutation.SetFailedRecords(i)
	return tuo
}

// SetNillableFailedRecords sets the "failed_records" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableFailedRecords(i *int) *TaskUpdateOne {
	if i != nil {
		tuo.SetFailedRecords(*i)
	}
	return tuo
}

// AddFailedRecords adds i to the "failed_records" field.
func (tuo *TaskUpdateOne) AddFailedRecords(i int) *TaskUpdateOne {
	tuo.mutation.AddFailedRecords(i)
	return tuo
}

// SetErrorSummary sets the "error_summary" field.
func (tuo *TaskUpdateOne) SetErrorSummary(s string) *TaskUpdateOne {
	tuo.mutation.SetErrorSummary(s)
	return tuo
}

// SetNillableErrorSummary sets the "error_summary" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableErrorSummary(s *string) *TaskUpdateOne {
	if s != nil {
		tuo.SetErrorSummary(*s)
	}
	return tuo
}

// ClearErrorSummary clears the value of the "error_summary" field.
func (tuo *TaskUpdateOne) ClearErrorSummary() *TaskUpdateOne {
	tuo.mutation.ClearErrorSummary()
	return tuo
}

// SetMetadata sets the "metadata" field.
func (tuo *TaskUpdateOne) SetMetadata(m map[string]interface{}) *TaskUpdateOne {
	tuo.mutation.SetMetadata(m)
	return tuo
}

// ClearMetadata clears the value of the "metadata" field.
func (tuo *TaskUpdateOne) ClearMetadata() *TaskUpdateOne {
	tuo.mutation.ClearMetadata()
	return tuo
}

// SetStartedAt sets the "started_at" field.
func (tuo *TaskUpdateOne) SetStartedAt(t time.Time) *TaskUpdateOne {
	tuo.mutation.SetStartedAt(t)
	return tuo
}

// SetNillableStartedAt sets the "started_at" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableStartedAt(t *time.Time) *TaskUpdateOne {
	if t != nil {
		tuo.SetStartedAt(*t)
	}
	return tuo
}

// ClearStartedAt clears the value of the "started_at" field.
func (tuo *TaskUpdateOne) ClearStartedAt() *TaskUpdateOne {
	tuo.mutation.ClearStartedAt()
	return tuo
}

// SetCompletedAt sets the "completed_at" field.
func (tuo *TaskUpdateOne) SetCompletedAt(t time.Time) *TaskUpdateOne {
	tuo.mutation.SetCompletedAt(t)
	return tuo
}

// SetNillableCompletedAt sets the "completed_at" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableCompletedAt(t *time.Time) *TaskUpdateOne {
	if t != nil {
		tuo.SetCompletedAt(*t)
	}
	return tuo
}

// ClearCompletedAt clears the value of the "completed_at" field.
func (tuo *TaskUpdateOne) ClearCompletedAt() *TaskUpdateOne {
	tuo.mutation.ClearCompletedAt()
	return tuo
}

// SetFailedAt sets the "failed_at" field.
func (tuo *TaskUpdateOne) SetFailedAt(t time.Time) *TaskUpdateOne {
	tuo.mutation.SetFailedAt(t)
	return tuo
}

// SetNillableFailedAt sets the "failed_at" field if the given value is not nil.
func (tuo *TaskUpdateOne) SetNillableFailedAt(t *time.Time) *TaskUpdateOne {
	if t != nil {
		tuo.SetFailedAt(*t)
	}
	return tuo
}

// ClearFailedAt clears the value of the "failed_at" field.
func (tuo *TaskUpdateOne) ClearFailedAt() *TaskUpdateOne {
	tuo.mutation.ClearFailedAt()
	return tuo
}

// Mutation returns the TaskMutation object of the builder.
func (tuo *TaskUpdateOne) Mutation() *TaskMutation {
	return tuo.mutation
}

// Where appends a list predicates to the TaskUpdate builder.
func (tuo *TaskUpdateOne) Where(ps ...predicate.Task) *TaskUpdateOne {
	tuo.mutation.Where(ps...)
	return tuo
}

// Select allows selecting one or more fields (columns) of the returned entity.
// The default is selecting all fields defined in the entity schema.
func (tuo *TaskUpdateOne) Select(field string, fields ...string) *TaskUpdateOne {
	tuo.fields = append([]string{field}, fields...)
	return tuo
}

// Save executes the query and returns the updated Task entity.
func (tuo *TaskUpdateOne) Save(ctx context.Context) (*Task, error) {
	tuo.defaults()
	return withHooks(ctx, tuo.sqlSave, tuo.mutation, tuo.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (tuo *TaskUpdateOne) SaveX(ctx context.Context) *Task {
	node, err := tuo.Save(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// Exec executes the query on the entity.
func (tuo *TaskUpdateOne) Exec(ctx context.Context) error {
	_, err := tuo.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (tuo *TaskUpdateOne) ExecX(ctx context.Context) {
	if err := tuo.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (tuo *TaskUpdateOne) defaults() {
	if _, ok := tuo.mutation.UpdatedAt(); !ok {
		v := task.UpdateDefaultUpdatedAt()
		tuo.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (tuo *TaskUpdateOne) check() error {
	if v, ok := tuo.mutation.TaskType(); ok {
		if err := task.TaskTypeValidator(v); err != nil {
			return &ValidationError{Name: "task_type", err: fmt.Errorf(`ent: validator failed for field "Task.task_type": %w`, err)}
		}
	}
	if v, ok := tuo.mutation.EntityType(); ok {
		if err := task.EntityTypeValidator(v); err != nil {
			return &ValidationError{Name: "entity_type", err: fmt.Errorf(`ent: validator failed for field "Task.entity_type": %w`, err)}
		}
	}
	if v, ok := tuo.mutation.FileURL(); ok {
		if err := task.FileURLValidator(v); err != nil {
			return &ValidationError{Name: "file_url", err: fmt.Errorf(`ent: validator failed for field "Task.file_url": %w`, err)}
		}
	}
	if v, ok := tuo.mutation.FileType(); ok {
		if err := task.FileTypeValidator(v); err != nil {
			return &ValidationError{Name: "file_type", err: fmt.Errorf(`ent: validator failed for field "Task.file_type": %w`, err)}
		}
	}
	return nil
}

func (tuo *TaskUpdateOne) sqlSave(ctx context.Context) (_node *Task, err error) {
	if err := tuo.check(); err != nil {
		return _node, err
	}
	_spec := sqlgraph.NewUpdateSpec(task.Table, task.Columns, sqlgraph.NewFieldSpec(task.FieldID, field.TypeString))
	id, ok := tuo.mutation.ID()
	if !ok {
		return nil, &ValidationError{Name: "id", err: errors.New(`ent: missing "Task.id" for update`)}
	}
	_spec.Node.ID.Value = id
	if fields := tuo.fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, task.FieldID)
		for _, f := range fields {
			if !task.ValidColumn(f) {
				return nil, &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
			}
			if f != task.FieldID {
				_spec.Node.Columns = append(_spec.Node.Columns, f)
			}
		}
	}
	if ps := tuo.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := tuo.mutation.Status(); ok {
		_spec.SetField(task.FieldStatus, field.TypeString, value)
	}
	if value, ok := tuo.mutation.UpdatedAt(); ok {
		_spec.SetField(task.FieldUpdatedAt, field.TypeTime, value)
	}
	if tuo.mutation.CreatedByCleared() {
		_spec.ClearField(task.FieldCreatedBy, field.TypeString)
	}
	if value, ok := tuo.mutation.UpdatedBy(); ok {
		_spec.SetField(task.FieldUpdatedBy, field.TypeString, value)
	}
	if tuo.mutation.UpdatedByCleared() {
		_spec.ClearField(task.FieldUpdatedBy, field.TypeString)
	}
	if tuo.mutation.EnvironmentIDCleared() {
		_spec.ClearField(task.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := tuo.mutation.TaskType(); ok {
		_spec.SetField(task.FieldTaskType, field.TypeString, value)
	}
	if value, ok := tuo.mutation.EntityType(); ok {
		_spec.SetField(task.FieldEntityType, field.TypeString, value)
	}
	if value, ok := tuo.mutation.FileURL(); ok {
		_spec.SetField(task.FieldFileURL, field.TypeString, value)
	}
	if value, ok := tuo.mutation.FileName(); ok {
		_spec.SetField(task.FieldFileName, field.TypeString, value)
	}
	if tuo.mutation.FileNameCleared() {
		_spec.ClearField(task.FieldFileName, field.TypeString)
	}
	if value, ok := tuo.mutation.FileType(); ok {
		_spec.SetField(task.FieldFileType, field.TypeString, value)
	}
	if value, ok := tuo.mutation.TaskStatus(); ok {
		_spec.SetField(task.FieldTaskStatus, field.TypeString, value)
	}
	if value, ok := tuo.mutation.TotalRecords(); ok {
		_spec.SetField(task.FieldTotalRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.AddedTotalRecords(); ok {
		_spec.AddField(task.FieldTotalRecords, field.TypeInt, value)
	}
	if tuo.mutation.TotalRecordsCleared() {
		_spec.ClearField(task.FieldTotalRecords, field.TypeInt)
	}
	if value, ok := tuo.mutation.ProcessedRecords(); ok {
		_spec.SetField(task.FieldProcessedRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.AddedProcessedRecords(); ok {
		_spec.AddField(task.FieldProcessedRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.SuccessfulRecords(); ok {
		_spec.SetField(task.FieldSuccessfulRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.AddedSuccessfulRecords(); ok {
		_spec.AddField(task.FieldSuccessfulRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.FailedRecords(); ok {
		_spec.SetField(task.FieldFailedRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.AddedFailedRecords(); ok {
		_spec.AddField(task.FieldFailedRecords, field.TypeInt, value)
	}
	if value, ok := tuo.mutation.ErrorSummary(); ok {
		_spec.SetField(task.FieldErrorSummary, field.TypeString, value)
	}
	if tuo.mutation.ErrorSummaryCleared() {
		_spec.ClearField(task.FieldErrorSummary, field.TypeString)
	}
	if value, ok := tuo.mutation.Metadata(); ok {
		_spec.SetField(task.FieldMetadata, field.TypeJSON, value)
	}
	if tuo.mutation.MetadataCleared() {
		_spec.ClearField(task.FieldMetadata, field.TypeJSON)
	}
	if value, ok := tuo.mutation.StartedAt(); ok {
		_spec.SetField(task.FieldStartedAt, field.TypeTime, value)
	}
	if tuo.mutation.StartedAtCleared() {
		_spec.ClearField(task.FieldStartedAt, field.TypeTime)
	}
	if value, ok := tuo.mutation.CompletedAt(); ok {
		_spec.SetField(task.FieldCompletedAt, field.TypeTime, value)
	}
	if tuo.mutation.CompletedAtCleared() {
		_spec.ClearField(task.FieldCompletedAt, field.TypeTime)
	}
	if value, ok := tuo.mutation.FailedAt(); ok {
		_spec.SetField(task.FieldFailedAt, field.TypeTime, value)
	}
	if tuo.mutation.FailedAtCleared() {
		_spec.ClearField(task.FieldFailedAt, field.TypeTime)
	}
	_node = &Task{config: tuo.config}
	_spec.Assign = _node.assignValues
	_spec.ScanValues = _node.scanValues
	if err = sqlgraph.UpdateNode(ctx, tuo.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{task.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	tuo.mutation.done = true
	return _node, nil
}
