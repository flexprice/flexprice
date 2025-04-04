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
	"github.com/flexprice/flexprice/ent/entitlement"
	"github.com/flexprice/flexprice/ent/plan"
	"github.com/flexprice/flexprice/ent/predicate"
)

// EntitlementUpdate is the builder for updating Entitlement entities.
type EntitlementUpdate struct {
	config
	hooks    []Hook
	mutation *EntitlementMutation
}

// Where appends a list predicates to the EntitlementUpdate builder.
func (eu *EntitlementUpdate) Where(ps ...predicate.Entitlement) *EntitlementUpdate {
	eu.mutation.Where(ps...)
	return eu
}

// SetStatus sets the "status" field.
func (eu *EntitlementUpdate) SetStatus(s string) *EntitlementUpdate {
	eu.mutation.SetStatus(s)
	return eu
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableStatus(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetStatus(*s)
	}
	return eu
}

// SetUpdatedAt sets the "updated_at" field.
func (eu *EntitlementUpdate) SetUpdatedAt(t time.Time) *EntitlementUpdate {
	eu.mutation.SetUpdatedAt(t)
	return eu
}

// SetUpdatedBy sets the "updated_by" field.
func (eu *EntitlementUpdate) SetUpdatedBy(s string) *EntitlementUpdate {
	eu.mutation.SetUpdatedBy(s)
	return eu
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableUpdatedBy(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetUpdatedBy(*s)
	}
	return eu
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (eu *EntitlementUpdate) ClearUpdatedBy() *EntitlementUpdate {
	eu.mutation.ClearUpdatedBy()
	return eu
}

// SetPlanID sets the "plan_id" field.
func (eu *EntitlementUpdate) SetPlanID(s string) *EntitlementUpdate {
	eu.mutation.SetPlanID(s)
	return eu
}

// SetNillablePlanID sets the "plan_id" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillablePlanID(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetPlanID(*s)
	}
	return eu
}

// SetFeatureID sets the "feature_id" field.
func (eu *EntitlementUpdate) SetFeatureID(s string) *EntitlementUpdate {
	eu.mutation.SetFeatureID(s)
	return eu
}

// SetNillableFeatureID sets the "feature_id" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableFeatureID(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetFeatureID(*s)
	}
	return eu
}

// SetFeatureType sets the "feature_type" field.
func (eu *EntitlementUpdate) SetFeatureType(s string) *EntitlementUpdate {
	eu.mutation.SetFeatureType(s)
	return eu
}

// SetNillableFeatureType sets the "feature_type" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableFeatureType(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetFeatureType(*s)
	}
	return eu
}

// SetIsEnabled sets the "is_enabled" field.
func (eu *EntitlementUpdate) SetIsEnabled(b bool) *EntitlementUpdate {
	eu.mutation.SetIsEnabled(b)
	return eu
}

// SetNillableIsEnabled sets the "is_enabled" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableIsEnabled(b *bool) *EntitlementUpdate {
	if b != nil {
		eu.SetIsEnabled(*b)
	}
	return eu
}

// SetUsageLimit sets the "usage_limit" field.
func (eu *EntitlementUpdate) SetUsageLimit(i int64) *EntitlementUpdate {
	eu.mutation.ResetUsageLimit()
	eu.mutation.SetUsageLimit(i)
	return eu
}

// SetNillableUsageLimit sets the "usage_limit" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableUsageLimit(i *int64) *EntitlementUpdate {
	if i != nil {
		eu.SetUsageLimit(*i)
	}
	return eu
}

// AddUsageLimit adds i to the "usage_limit" field.
func (eu *EntitlementUpdate) AddUsageLimit(i int64) *EntitlementUpdate {
	eu.mutation.AddUsageLimit(i)
	return eu
}

// ClearUsageLimit clears the value of the "usage_limit" field.
func (eu *EntitlementUpdate) ClearUsageLimit() *EntitlementUpdate {
	eu.mutation.ClearUsageLimit()
	return eu
}

// SetUsageResetPeriod sets the "usage_reset_period" field.
func (eu *EntitlementUpdate) SetUsageResetPeriod(s string) *EntitlementUpdate {
	eu.mutation.SetUsageResetPeriod(s)
	return eu
}

// SetNillableUsageResetPeriod sets the "usage_reset_period" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableUsageResetPeriod(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetUsageResetPeriod(*s)
	}
	return eu
}

// ClearUsageResetPeriod clears the value of the "usage_reset_period" field.
func (eu *EntitlementUpdate) ClearUsageResetPeriod() *EntitlementUpdate {
	eu.mutation.ClearUsageResetPeriod()
	return eu
}

// SetIsSoftLimit sets the "is_soft_limit" field.
func (eu *EntitlementUpdate) SetIsSoftLimit(b bool) *EntitlementUpdate {
	eu.mutation.SetIsSoftLimit(b)
	return eu
}

// SetNillableIsSoftLimit sets the "is_soft_limit" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableIsSoftLimit(b *bool) *EntitlementUpdate {
	if b != nil {
		eu.SetIsSoftLimit(*b)
	}
	return eu
}

// SetStaticValue sets the "static_value" field.
func (eu *EntitlementUpdate) SetStaticValue(s string) *EntitlementUpdate {
	eu.mutation.SetStaticValue(s)
	return eu
}

// SetNillableStaticValue sets the "static_value" field if the given value is not nil.
func (eu *EntitlementUpdate) SetNillableStaticValue(s *string) *EntitlementUpdate {
	if s != nil {
		eu.SetStaticValue(*s)
	}
	return eu
}

// ClearStaticValue clears the value of the "static_value" field.
func (eu *EntitlementUpdate) ClearStaticValue() *EntitlementUpdate {
	eu.mutation.ClearStaticValue()
	return eu
}

// SetPlan sets the "plan" edge to the Plan entity.
func (eu *EntitlementUpdate) SetPlan(p *Plan) *EntitlementUpdate {
	return eu.SetPlanID(p.ID)
}

// Mutation returns the EntitlementMutation object of the builder.
func (eu *EntitlementUpdate) Mutation() *EntitlementMutation {
	return eu.mutation
}

// ClearPlan clears the "plan" edge to the Plan entity.
func (eu *EntitlementUpdate) ClearPlan() *EntitlementUpdate {
	eu.mutation.ClearPlan()
	return eu
}

// Save executes the query and returns the number of nodes affected by the update operation.
func (eu *EntitlementUpdate) Save(ctx context.Context) (int, error) {
	eu.defaults()
	return withHooks(ctx, eu.sqlSave, eu.mutation, eu.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (eu *EntitlementUpdate) SaveX(ctx context.Context) int {
	affected, err := eu.Save(ctx)
	if err != nil {
		panic(err)
	}
	return affected
}

// Exec executes the query.
func (eu *EntitlementUpdate) Exec(ctx context.Context) error {
	_, err := eu.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (eu *EntitlementUpdate) ExecX(ctx context.Context) {
	if err := eu.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (eu *EntitlementUpdate) defaults() {
	if _, ok := eu.mutation.UpdatedAt(); !ok {
		v := entitlement.UpdateDefaultUpdatedAt()
		eu.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (eu *EntitlementUpdate) check() error {
	if v, ok := eu.mutation.PlanID(); ok {
		if err := entitlement.PlanIDValidator(v); err != nil {
			return &ValidationError{Name: "plan_id", err: fmt.Errorf(`ent: validator failed for field "Entitlement.plan_id": %w`, err)}
		}
	}
	if v, ok := eu.mutation.FeatureID(); ok {
		if err := entitlement.FeatureIDValidator(v); err != nil {
			return &ValidationError{Name: "feature_id", err: fmt.Errorf(`ent: validator failed for field "Entitlement.feature_id": %w`, err)}
		}
	}
	if v, ok := eu.mutation.FeatureType(); ok {
		if err := entitlement.FeatureTypeValidator(v); err != nil {
			return &ValidationError{Name: "feature_type", err: fmt.Errorf(`ent: validator failed for field "Entitlement.feature_type": %w`, err)}
		}
	}
	if eu.mutation.PlanCleared() && len(eu.mutation.PlanIDs()) > 0 {
		return errors.New(`ent: clearing a required unique edge "Entitlement.plan"`)
	}
	return nil
}

func (eu *EntitlementUpdate) sqlSave(ctx context.Context) (n int, err error) {
	if err := eu.check(); err != nil {
		return n, err
	}
	_spec := sqlgraph.NewUpdateSpec(entitlement.Table, entitlement.Columns, sqlgraph.NewFieldSpec(entitlement.FieldID, field.TypeString))
	if ps := eu.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := eu.mutation.Status(); ok {
		_spec.SetField(entitlement.FieldStatus, field.TypeString, value)
	}
	if value, ok := eu.mutation.UpdatedAt(); ok {
		_spec.SetField(entitlement.FieldUpdatedAt, field.TypeTime, value)
	}
	if eu.mutation.CreatedByCleared() {
		_spec.ClearField(entitlement.FieldCreatedBy, field.TypeString)
	}
	if value, ok := eu.mutation.UpdatedBy(); ok {
		_spec.SetField(entitlement.FieldUpdatedBy, field.TypeString, value)
	}
	if eu.mutation.UpdatedByCleared() {
		_spec.ClearField(entitlement.FieldUpdatedBy, field.TypeString)
	}
	if eu.mutation.EnvironmentIDCleared() {
		_spec.ClearField(entitlement.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := eu.mutation.FeatureID(); ok {
		_spec.SetField(entitlement.FieldFeatureID, field.TypeString, value)
	}
	if value, ok := eu.mutation.FeatureType(); ok {
		_spec.SetField(entitlement.FieldFeatureType, field.TypeString, value)
	}
	if value, ok := eu.mutation.IsEnabled(); ok {
		_spec.SetField(entitlement.FieldIsEnabled, field.TypeBool, value)
	}
	if value, ok := eu.mutation.UsageLimit(); ok {
		_spec.SetField(entitlement.FieldUsageLimit, field.TypeInt64, value)
	}
	if value, ok := eu.mutation.AddedUsageLimit(); ok {
		_spec.AddField(entitlement.FieldUsageLimit, field.TypeInt64, value)
	}
	if eu.mutation.UsageLimitCleared() {
		_spec.ClearField(entitlement.FieldUsageLimit, field.TypeInt64)
	}
	if value, ok := eu.mutation.UsageResetPeriod(); ok {
		_spec.SetField(entitlement.FieldUsageResetPeriod, field.TypeString, value)
	}
	if eu.mutation.UsageResetPeriodCleared() {
		_spec.ClearField(entitlement.FieldUsageResetPeriod, field.TypeString)
	}
	if value, ok := eu.mutation.IsSoftLimit(); ok {
		_spec.SetField(entitlement.FieldIsSoftLimit, field.TypeBool, value)
	}
	if value, ok := eu.mutation.StaticValue(); ok {
		_spec.SetField(entitlement.FieldStaticValue, field.TypeString, value)
	}
	if eu.mutation.StaticValueCleared() {
		_spec.ClearField(entitlement.FieldStaticValue, field.TypeString)
	}
	if eu.mutation.PlanCleared() {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   entitlement.PlanTable,
			Columns: []string{entitlement.PlanColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(plan.FieldID, field.TypeString),
			},
		}
		_spec.Edges.Clear = append(_spec.Edges.Clear, edge)
	}
	if nodes := eu.mutation.PlanIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   entitlement.PlanTable,
			Columns: []string{entitlement.PlanColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(plan.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_spec.Edges.Add = append(_spec.Edges.Add, edge)
	}
	if n, err = sqlgraph.UpdateNodes(ctx, eu.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{entitlement.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return 0, err
	}
	eu.mutation.done = true
	return n, nil
}

// EntitlementUpdateOne is the builder for updating a single Entitlement entity.
type EntitlementUpdateOne struct {
	config
	fields   []string
	hooks    []Hook
	mutation *EntitlementMutation
}

// SetStatus sets the "status" field.
func (euo *EntitlementUpdateOne) SetStatus(s string) *EntitlementUpdateOne {
	euo.mutation.SetStatus(s)
	return euo
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableStatus(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetStatus(*s)
	}
	return euo
}

// SetUpdatedAt sets the "updated_at" field.
func (euo *EntitlementUpdateOne) SetUpdatedAt(t time.Time) *EntitlementUpdateOne {
	euo.mutation.SetUpdatedAt(t)
	return euo
}

// SetUpdatedBy sets the "updated_by" field.
func (euo *EntitlementUpdateOne) SetUpdatedBy(s string) *EntitlementUpdateOne {
	euo.mutation.SetUpdatedBy(s)
	return euo
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableUpdatedBy(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetUpdatedBy(*s)
	}
	return euo
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (euo *EntitlementUpdateOne) ClearUpdatedBy() *EntitlementUpdateOne {
	euo.mutation.ClearUpdatedBy()
	return euo
}

// SetPlanID sets the "plan_id" field.
func (euo *EntitlementUpdateOne) SetPlanID(s string) *EntitlementUpdateOne {
	euo.mutation.SetPlanID(s)
	return euo
}

// SetNillablePlanID sets the "plan_id" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillablePlanID(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetPlanID(*s)
	}
	return euo
}

// SetFeatureID sets the "feature_id" field.
func (euo *EntitlementUpdateOne) SetFeatureID(s string) *EntitlementUpdateOne {
	euo.mutation.SetFeatureID(s)
	return euo
}

// SetNillableFeatureID sets the "feature_id" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableFeatureID(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetFeatureID(*s)
	}
	return euo
}

// SetFeatureType sets the "feature_type" field.
func (euo *EntitlementUpdateOne) SetFeatureType(s string) *EntitlementUpdateOne {
	euo.mutation.SetFeatureType(s)
	return euo
}

// SetNillableFeatureType sets the "feature_type" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableFeatureType(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetFeatureType(*s)
	}
	return euo
}

// SetIsEnabled sets the "is_enabled" field.
func (euo *EntitlementUpdateOne) SetIsEnabled(b bool) *EntitlementUpdateOne {
	euo.mutation.SetIsEnabled(b)
	return euo
}

// SetNillableIsEnabled sets the "is_enabled" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableIsEnabled(b *bool) *EntitlementUpdateOne {
	if b != nil {
		euo.SetIsEnabled(*b)
	}
	return euo
}

// SetUsageLimit sets the "usage_limit" field.
func (euo *EntitlementUpdateOne) SetUsageLimit(i int64) *EntitlementUpdateOne {
	euo.mutation.ResetUsageLimit()
	euo.mutation.SetUsageLimit(i)
	return euo
}

// SetNillableUsageLimit sets the "usage_limit" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableUsageLimit(i *int64) *EntitlementUpdateOne {
	if i != nil {
		euo.SetUsageLimit(*i)
	}
	return euo
}

// AddUsageLimit adds i to the "usage_limit" field.
func (euo *EntitlementUpdateOne) AddUsageLimit(i int64) *EntitlementUpdateOne {
	euo.mutation.AddUsageLimit(i)
	return euo
}

// ClearUsageLimit clears the value of the "usage_limit" field.
func (euo *EntitlementUpdateOne) ClearUsageLimit() *EntitlementUpdateOne {
	euo.mutation.ClearUsageLimit()
	return euo
}

// SetUsageResetPeriod sets the "usage_reset_period" field.
func (euo *EntitlementUpdateOne) SetUsageResetPeriod(s string) *EntitlementUpdateOne {
	euo.mutation.SetUsageResetPeriod(s)
	return euo
}

// SetNillableUsageResetPeriod sets the "usage_reset_period" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableUsageResetPeriod(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetUsageResetPeriod(*s)
	}
	return euo
}

// ClearUsageResetPeriod clears the value of the "usage_reset_period" field.
func (euo *EntitlementUpdateOne) ClearUsageResetPeriod() *EntitlementUpdateOne {
	euo.mutation.ClearUsageResetPeriod()
	return euo
}

// SetIsSoftLimit sets the "is_soft_limit" field.
func (euo *EntitlementUpdateOne) SetIsSoftLimit(b bool) *EntitlementUpdateOne {
	euo.mutation.SetIsSoftLimit(b)
	return euo
}

// SetNillableIsSoftLimit sets the "is_soft_limit" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableIsSoftLimit(b *bool) *EntitlementUpdateOne {
	if b != nil {
		euo.SetIsSoftLimit(*b)
	}
	return euo
}

// SetStaticValue sets the "static_value" field.
func (euo *EntitlementUpdateOne) SetStaticValue(s string) *EntitlementUpdateOne {
	euo.mutation.SetStaticValue(s)
	return euo
}

// SetNillableStaticValue sets the "static_value" field if the given value is not nil.
func (euo *EntitlementUpdateOne) SetNillableStaticValue(s *string) *EntitlementUpdateOne {
	if s != nil {
		euo.SetStaticValue(*s)
	}
	return euo
}

// ClearStaticValue clears the value of the "static_value" field.
func (euo *EntitlementUpdateOne) ClearStaticValue() *EntitlementUpdateOne {
	euo.mutation.ClearStaticValue()
	return euo
}

// SetPlan sets the "plan" edge to the Plan entity.
func (euo *EntitlementUpdateOne) SetPlan(p *Plan) *EntitlementUpdateOne {
	return euo.SetPlanID(p.ID)
}

// Mutation returns the EntitlementMutation object of the builder.
func (euo *EntitlementUpdateOne) Mutation() *EntitlementMutation {
	return euo.mutation
}

// ClearPlan clears the "plan" edge to the Plan entity.
func (euo *EntitlementUpdateOne) ClearPlan() *EntitlementUpdateOne {
	euo.mutation.ClearPlan()
	return euo
}

// Where appends a list predicates to the EntitlementUpdate builder.
func (euo *EntitlementUpdateOne) Where(ps ...predicate.Entitlement) *EntitlementUpdateOne {
	euo.mutation.Where(ps...)
	return euo
}

// Select allows selecting one or more fields (columns) of the returned entity.
// The default is selecting all fields defined in the entity schema.
func (euo *EntitlementUpdateOne) Select(field string, fields ...string) *EntitlementUpdateOne {
	euo.fields = append([]string{field}, fields...)
	return euo
}

// Save executes the query and returns the updated Entitlement entity.
func (euo *EntitlementUpdateOne) Save(ctx context.Context) (*Entitlement, error) {
	euo.defaults()
	return withHooks(ctx, euo.sqlSave, euo.mutation, euo.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (euo *EntitlementUpdateOne) SaveX(ctx context.Context) *Entitlement {
	node, err := euo.Save(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// Exec executes the query on the entity.
func (euo *EntitlementUpdateOne) Exec(ctx context.Context) error {
	_, err := euo.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (euo *EntitlementUpdateOne) ExecX(ctx context.Context) {
	if err := euo.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (euo *EntitlementUpdateOne) defaults() {
	if _, ok := euo.mutation.UpdatedAt(); !ok {
		v := entitlement.UpdateDefaultUpdatedAt()
		euo.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (euo *EntitlementUpdateOne) check() error {
	if v, ok := euo.mutation.PlanID(); ok {
		if err := entitlement.PlanIDValidator(v); err != nil {
			return &ValidationError{Name: "plan_id", err: fmt.Errorf(`ent: validator failed for field "Entitlement.plan_id": %w`, err)}
		}
	}
	if v, ok := euo.mutation.FeatureID(); ok {
		if err := entitlement.FeatureIDValidator(v); err != nil {
			return &ValidationError{Name: "feature_id", err: fmt.Errorf(`ent: validator failed for field "Entitlement.feature_id": %w`, err)}
		}
	}
	if v, ok := euo.mutation.FeatureType(); ok {
		if err := entitlement.FeatureTypeValidator(v); err != nil {
			return &ValidationError{Name: "feature_type", err: fmt.Errorf(`ent: validator failed for field "Entitlement.feature_type": %w`, err)}
		}
	}
	if euo.mutation.PlanCleared() && len(euo.mutation.PlanIDs()) > 0 {
		return errors.New(`ent: clearing a required unique edge "Entitlement.plan"`)
	}
	return nil
}

func (euo *EntitlementUpdateOne) sqlSave(ctx context.Context) (_node *Entitlement, err error) {
	if err := euo.check(); err != nil {
		return _node, err
	}
	_spec := sqlgraph.NewUpdateSpec(entitlement.Table, entitlement.Columns, sqlgraph.NewFieldSpec(entitlement.FieldID, field.TypeString))
	id, ok := euo.mutation.ID()
	if !ok {
		return nil, &ValidationError{Name: "id", err: errors.New(`ent: missing "Entitlement.id" for update`)}
	}
	_spec.Node.ID.Value = id
	if fields := euo.fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, entitlement.FieldID)
		for _, f := range fields {
			if !entitlement.ValidColumn(f) {
				return nil, &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
			}
			if f != entitlement.FieldID {
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
		_spec.SetField(entitlement.FieldStatus, field.TypeString, value)
	}
	if value, ok := euo.mutation.UpdatedAt(); ok {
		_spec.SetField(entitlement.FieldUpdatedAt, field.TypeTime, value)
	}
	if euo.mutation.CreatedByCleared() {
		_spec.ClearField(entitlement.FieldCreatedBy, field.TypeString)
	}
	if value, ok := euo.mutation.UpdatedBy(); ok {
		_spec.SetField(entitlement.FieldUpdatedBy, field.TypeString, value)
	}
	if euo.mutation.UpdatedByCleared() {
		_spec.ClearField(entitlement.FieldUpdatedBy, field.TypeString)
	}
	if euo.mutation.EnvironmentIDCleared() {
		_spec.ClearField(entitlement.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := euo.mutation.FeatureID(); ok {
		_spec.SetField(entitlement.FieldFeatureID, field.TypeString, value)
	}
	if value, ok := euo.mutation.FeatureType(); ok {
		_spec.SetField(entitlement.FieldFeatureType, field.TypeString, value)
	}
	if value, ok := euo.mutation.IsEnabled(); ok {
		_spec.SetField(entitlement.FieldIsEnabled, field.TypeBool, value)
	}
	if value, ok := euo.mutation.UsageLimit(); ok {
		_spec.SetField(entitlement.FieldUsageLimit, field.TypeInt64, value)
	}
	if value, ok := euo.mutation.AddedUsageLimit(); ok {
		_spec.AddField(entitlement.FieldUsageLimit, field.TypeInt64, value)
	}
	if euo.mutation.UsageLimitCleared() {
		_spec.ClearField(entitlement.FieldUsageLimit, field.TypeInt64)
	}
	if value, ok := euo.mutation.UsageResetPeriod(); ok {
		_spec.SetField(entitlement.FieldUsageResetPeriod, field.TypeString, value)
	}
	if euo.mutation.UsageResetPeriodCleared() {
		_spec.ClearField(entitlement.FieldUsageResetPeriod, field.TypeString)
	}
	if value, ok := euo.mutation.IsSoftLimit(); ok {
		_spec.SetField(entitlement.FieldIsSoftLimit, field.TypeBool, value)
	}
	if value, ok := euo.mutation.StaticValue(); ok {
		_spec.SetField(entitlement.FieldStaticValue, field.TypeString, value)
	}
	if euo.mutation.StaticValueCleared() {
		_spec.ClearField(entitlement.FieldStaticValue, field.TypeString)
	}
	if euo.mutation.PlanCleared() {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   entitlement.PlanTable,
			Columns: []string{entitlement.PlanColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(plan.FieldID, field.TypeString),
			},
		}
		_spec.Edges.Clear = append(_spec.Edges.Clear, edge)
	}
	if nodes := euo.mutation.PlanIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   entitlement.PlanTable,
			Columns: []string{entitlement.PlanColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(plan.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_spec.Edges.Add = append(_spec.Edges.Add, edge)
	}
	_node = &Entitlement{config: euo.config}
	_spec.Assign = _node.assignValues
	_spec.ScanValues = _node.scanValues
	if err = sqlgraph.UpdateNode(ctx, euo.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{entitlement.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	euo.mutation.done = true
	return _node, nil
}
