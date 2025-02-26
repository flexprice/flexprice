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
	"github.com/flexprice/flexprice/ent/environment"
	"github.com/flexprice/flexprice/ent/predicate"
)

// EnvironmentUpdate is the builder for updating Environment entities.
type EnvironmentUpdate struct {
	config
	hooks    []Hook
	mutation *EnvironmentMutation
}

// Where appends a list predicates to the EnvironmentUpdate builder.
func (eu *EnvironmentUpdate) Where(ps ...predicate.Environment) *EnvironmentUpdate {
	eu.mutation.Where(ps...)
	return eu
}

// SetStatus sets the "status" field.
func (eu *EnvironmentUpdate) SetStatus(s string) *EnvironmentUpdate {
	eu.mutation.SetStatus(s)
	return eu
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (eu *EnvironmentUpdate) SetNillableStatus(s *string) *EnvironmentUpdate {
	if s != nil {
		eu.SetStatus(*s)
	}
	return eu
}

// SetUpdatedAt sets the "updated_at" field.
func (eu *EnvironmentUpdate) SetUpdatedAt(t time.Time) *EnvironmentUpdate {
	eu.mutation.SetUpdatedAt(t)
	return eu
}

// SetUpdatedBy sets the "updated_by" field.
func (eu *EnvironmentUpdate) SetUpdatedBy(s string) *EnvironmentUpdate {
	eu.mutation.SetUpdatedBy(s)
	return eu
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (eu *EnvironmentUpdate) SetNillableUpdatedBy(s *string) *EnvironmentUpdate {
	if s != nil {
		eu.SetUpdatedBy(*s)
	}
	return eu
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (eu *EnvironmentUpdate) ClearUpdatedBy() *EnvironmentUpdate {
	eu.mutation.ClearUpdatedBy()
	return eu
}

// SetName sets the "name" field.
func (eu *EnvironmentUpdate) SetName(s string) *EnvironmentUpdate {
	eu.mutation.SetName(s)
	return eu
}

// SetNillableName sets the "name" field if the given value is not nil.
func (eu *EnvironmentUpdate) SetNillableName(s *string) *EnvironmentUpdate {
	if s != nil {
		eu.SetName(*s)
	}
	return eu
}

// SetType sets the "type" field.
func (eu *EnvironmentUpdate) SetType(s string) *EnvironmentUpdate {
	eu.mutation.SetType(s)
	return eu
}

// SetNillableType sets the "type" field if the given value is not nil.
func (eu *EnvironmentUpdate) SetNillableType(s *string) *EnvironmentUpdate {
	if s != nil {
		eu.SetType(*s)
	}
	return eu
}

// Mutation returns the EnvironmentMutation object of the builder.
func (eu *EnvironmentUpdate) Mutation() *EnvironmentMutation {
	return eu.mutation
}

// Save executes the query and returns the number of nodes affected by the update operation.
func (eu *EnvironmentUpdate) Save(ctx context.Context) (int, error) {
	eu.defaults()
	return withHooks(ctx, eu.sqlSave, eu.mutation, eu.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (eu *EnvironmentUpdate) SaveX(ctx context.Context) int {
	affected, err := eu.Save(ctx)
	if err != nil {
		panic(err)
	}
	return affected
}

// Exec executes the query.
func (eu *EnvironmentUpdate) Exec(ctx context.Context) error {
	_, err := eu.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (eu *EnvironmentUpdate) ExecX(ctx context.Context) {
	if err := eu.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (eu *EnvironmentUpdate) defaults() {
	if _, ok := eu.mutation.UpdatedAt(); !ok {
		v := environment.UpdateDefaultUpdatedAt()
		eu.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (eu *EnvironmentUpdate) check() error {
	if v, ok := eu.mutation.Name(); ok {
		if err := environment.NameValidator(v); err != nil {
			return &ValidationError{Name: "name", err: fmt.Errorf(`ent: validator failed for field "Environment.name": %w`, err)}
		}
	}
	if v, ok := eu.mutation.GetType(); ok {
		if err := environment.TypeValidator(v); err != nil {
			return &ValidationError{Name: "type", err: fmt.Errorf(`ent: validator failed for field "Environment.type": %w`, err)}
		}
	}
	return nil
}

func (eu *EnvironmentUpdate) sqlSave(ctx context.Context) (n int, err error) {
	if err := eu.check(); err != nil {
		return n, err
	}
	_spec := sqlgraph.NewUpdateSpec(environment.Table, environment.Columns, sqlgraph.NewFieldSpec(environment.FieldID, field.TypeString))
	if ps := eu.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := eu.mutation.Status(); ok {
		_spec.SetField(environment.FieldStatus, field.TypeString, value)
	}
	if value, ok := eu.mutation.UpdatedAt(); ok {
		_spec.SetField(environment.FieldUpdatedAt, field.TypeTime, value)
	}
	if eu.mutation.CreatedByCleared() {
		_spec.ClearField(environment.FieldCreatedBy, field.TypeString)
	}
	if value, ok := eu.mutation.UpdatedBy(); ok {
		_spec.SetField(environment.FieldUpdatedBy, field.TypeString, value)
	}
	if eu.mutation.UpdatedByCleared() {
		_spec.ClearField(environment.FieldUpdatedBy, field.TypeString)
	}
	if value, ok := eu.mutation.Name(); ok {
		_spec.SetField(environment.FieldName, field.TypeString, value)
	}
	if value, ok := eu.mutation.GetType(); ok {
		_spec.SetField(environment.FieldType, field.TypeString, value)
	}
	if n, err = sqlgraph.UpdateNodes(ctx, eu.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{environment.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return 0, err
	}
	eu.mutation.done = true
	return n, nil
}

// EnvironmentUpdateOne is the builder for updating a single Environment entity.
type EnvironmentUpdateOne struct {
	config
	fields   []string
	hooks    []Hook
	mutation *EnvironmentMutation
}

// SetStatus sets the "status" field.
func (euo *EnvironmentUpdateOne) SetStatus(s string) *EnvironmentUpdateOne {
	euo.mutation.SetStatus(s)
	return euo
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (euo *EnvironmentUpdateOne) SetNillableStatus(s *string) *EnvironmentUpdateOne {
	if s != nil {
		euo.SetStatus(*s)
	}
	return euo
}

// SetUpdatedAt sets the "updated_at" field.
func (euo *EnvironmentUpdateOne) SetUpdatedAt(t time.Time) *EnvironmentUpdateOne {
	euo.mutation.SetUpdatedAt(t)
	return euo
}

// SetUpdatedBy sets the "updated_by" field.
func (euo *EnvironmentUpdateOne) SetUpdatedBy(s string) *EnvironmentUpdateOne {
	euo.mutation.SetUpdatedBy(s)
	return euo
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (euo *EnvironmentUpdateOne) SetNillableUpdatedBy(s *string) *EnvironmentUpdateOne {
	if s != nil {
		euo.SetUpdatedBy(*s)
	}
	return euo
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (euo *EnvironmentUpdateOne) ClearUpdatedBy() *EnvironmentUpdateOne {
	euo.mutation.ClearUpdatedBy()
	return euo
}

// SetName sets the "name" field.
func (euo *EnvironmentUpdateOne) SetName(s string) *EnvironmentUpdateOne {
	euo.mutation.SetName(s)
	return euo
}

// SetNillableName sets the "name" field if the given value is not nil.
func (euo *EnvironmentUpdateOne) SetNillableName(s *string) *EnvironmentUpdateOne {
	if s != nil {
		euo.SetName(*s)
	}
	return euo
}

// SetType sets the "type" field.
func (euo *EnvironmentUpdateOne) SetType(s string) *EnvironmentUpdateOne {
	euo.mutation.SetType(s)
	return euo
}

// SetNillableType sets the "type" field if the given value is not nil.
func (euo *EnvironmentUpdateOne) SetNillableType(s *string) *EnvironmentUpdateOne {
	if s != nil {
		euo.SetType(*s)
	}
	return euo
}

// Mutation returns the EnvironmentMutation object of the builder.
func (euo *EnvironmentUpdateOne) Mutation() *EnvironmentMutation {
	return euo.mutation
}

// Where appends a list predicates to the EnvironmentUpdate builder.
func (euo *EnvironmentUpdateOne) Where(ps ...predicate.Environment) *EnvironmentUpdateOne {
	euo.mutation.Where(ps...)
	return euo
}

// Select allows selecting one or more fields (columns) of the returned entity.
// The default is selecting all fields defined in the entity schema.
func (euo *EnvironmentUpdateOne) Select(field string, fields ...string) *EnvironmentUpdateOne {
	euo.fields = append([]string{field}, fields...)
	return euo
}

// Save executes the query and returns the updated Environment entity.
func (euo *EnvironmentUpdateOne) Save(ctx context.Context) (*Environment, error) {
	euo.defaults()
	return withHooks(ctx, euo.sqlSave, euo.mutation, euo.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (euo *EnvironmentUpdateOne) SaveX(ctx context.Context) *Environment {
	node, err := euo.Save(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// Exec executes the query on the entity.
func (euo *EnvironmentUpdateOne) Exec(ctx context.Context) error {
	_, err := euo.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (euo *EnvironmentUpdateOne) ExecX(ctx context.Context) {
	if err := euo.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (euo *EnvironmentUpdateOne) defaults() {
	if _, ok := euo.mutation.UpdatedAt(); !ok {
		v := environment.UpdateDefaultUpdatedAt()
		euo.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (euo *EnvironmentUpdateOne) check() error {
	if v, ok := euo.mutation.Name(); ok {
		if err := environment.NameValidator(v); err != nil {
			return &ValidationError{Name: "name", err: fmt.Errorf(`ent: validator failed for field "Environment.name": %w`, err)}
		}
	}
	if v, ok := euo.mutation.GetType(); ok {
		if err := environment.TypeValidator(v); err != nil {
			return &ValidationError{Name: "type", err: fmt.Errorf(`ent: validator failed for field "Environment.type": %w`, err)}
		}
	}
	return nil
}

func (euo *EnvironmentUpdateOne) sqlSave(ctx context.Context) (_node *Environment, err error) {
	if err := euo.check(); err != nil {
		return _node, err
	}
	_spec := sqlgraph.NewUpdateSpec(environment.Table, environment.Columns, sqlgraph.NewFieldSpec(environment.FieldID, field.TypeString))
	id, ok := euo.mutation.ID()
	if !ok {
		return nil, &ValidationError{Name: "id", err: errors.New(`ent: missing "Environment.id" for update`)}
	}
	_spec.Node.ID.Value = id
	if fields := euo.fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, environment.FieldID)
		for _, f := range fields {
			if !environment.ValidColumn(f) {
				return nil, &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
			}
			if f != environment.FieldID {
				_spec.Node.Columns = append(_spec.Node.Columns, f)
			}
		}
	}
	if ps := euo.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := euo.mutation.Status(); ok {
		_spec.SetField(environment.FieldStatus, field.TypeString, value)
	}
	if value, ok := euo.mutation.UpdatedAt(); ok {
		_spec.SetField(environment.FieldUpdatedAt, field.TypeTime, value)
	}
	if euo.mutation.CreatedByCleared() {
		_spec.ClearField(environment.FieldCreatedBy, field.TypeString)
	}
	if value, ok := euo.mutation.UpdatedBy(); ok {
		_spec.SetField(environment.FieldUpdatedBy, field.TypeString, value)
	}
	if euo.mutation.UpdatedByCleared() {
		_spec.ClearField(environment.FieldUpdatedBy, field.TypeString)
	}
	if value, ok := euo.mutation.Name(); ok {
		_spec.SetField(environment.FieldName, field.TypeString, value)
	}
	if value, ok := euo.mutation.GetType(); ok {
		_spec.SetField(environment.FieldType, field.TypeString, value)
	}
	_node = &Environment{config: euo.config}
	_spec.Assign = _node.assignValues
	_spec.ScanValues = _node.scanValues
	if err = sqlgraph.UpdateNode(ctx, euo.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{environment.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	euo.mutation.done = true
	return _node, nil
}
