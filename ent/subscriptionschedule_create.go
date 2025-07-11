// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/subscriptionschedule"
	"github.com/flexprice/flexprice/ent/subscriptionschedulephase"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionScheduleCreate is the builder for creating a SubscriptionSchedule entity.
type SubscriptionScheduleCreate struct {
	config
	mutation *SubscriptionScheduleMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (ssc *SubscriptionScheduleCreate) SetTenantID(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetTenantID(s)
	return ssc
}

// SetStatus sets the "status" field.
func (ssc *SubscriptionScheduleCreate) SetStatus(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetStatus(s)
	return ssc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableStatus(s *string) *SubscriptionScheduleCreate {
	if s != nil {
		ssc.SetStatus(*s)
	}
	return ssc
}

// SetCreatedAt sets the "created_at" field.
func (ssc *SubscriptionScheduleCreate) SetCreatedAt(t time.Time) *SubscriptionScheduleCreate {
	ssc.mutation.SetCreatedAt(t)
	return ssc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableCreatedAt(t *time.Time) *SubscriptionScheduleCreate {
	if t != nil {
		ssc.SetCreatedAt(*t)
	}
	return ssc
}

// SetUpdatedAt sets the "updated_at" field.
func (ssc *SubscriptionScheduleCreate) SetUpdatedAt(t time.Time) *SubscriptionScheduleCreate {
	ssc.mutation.SetUpdatedAt(t)
	return ssc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableUpdatedAt(t *time.Time) *SubscriptionScheduleCreate {
	if t != nil {
		ssc.SetUpdatedAt(*t)
	}
	return ssc
}

// SetCreatedBy sets the "created_by" field.
func (ssc *SubscriptionScheduleCreate) SetCreatedBy(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetCreatedBy(s)
	return ssc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableCreatedBy(s *string) *SubscriptionScheduleCreate {
	if s != nil {
		ssc.SetCreatedBy(*s)
	}
	return ssc
}

// SetUpdatedBy sets the "updated_by" field.
func (ssc *SubscriptionScheduleCreate) SetUpdatedBy(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetUpdatedBy(s)
	return ssc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableUpdatedBy(s *string) *SubscriptionScheduleCreate {
	if s != nil {
		ssc.SetUpdatedBy(*s)
	}
	return ssc
}

// SetEnvironmentID sets the "environment_id" field.
func (ssc *SubscriptionScheduleCreate) SetEnvironmentID(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetEnvironmentID(s)
	return ssc
}

// SetNillableEnvironmentID sets the "environment_id" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableEnvironmentID(s *string) *SubscriptionScheduleCreate {
	if s != nil {
		ssc.SetEnvironmentID(*s)
	}
	return ssc
}

// SetSubscriptionID sets the "subscription_id" field.
func (ssc *SubscriptionScheduleCreate) SetSubscriptionID(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetSubscriptionID(s)
	return ssc
}

// SetScheduleStatus sets the "schedule_status" field.
func (ssc *SubscriptionScheduleCreate) SetScheduleStatus(tss types.SubscriptionScheduleStatus) *SubscriptionScheduleCreate {
	ssc.mutation.SetScheduleStatus(tss)
	return ssc
}

// SetNillableScheduleStatus sets the "schedule_status" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableScheduleStatus(tss *types.SubscriptionScheduleStatus) *SubscriptionScheduleCreate {
	if tss != nil {
		ssc.SetScheduleStatus(*tss)
	}
	return ssc
}

// SetCurrentPhaseIndex sets the "current_phase_index" field.
func (ssc *SubscriptionScheduleCreate) SetCurrentPhaseIndex(i int) *SubscriptionScheduleCreate {
	ssc.mutation.SetCurrentPhaseIndex(i)
	return ssc
}

// SetNillableCurrentPhaseIndex sets the "current_phase_index" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableCurrentPhaseIndex(i *int) *SubscriptionScheduleCreate {
	if i != nil {
		ssc.SetCurrentPhaseIndex(*i)
	}
	return ssc
}

// SetEndBehavior sets the "end_behavior" field.
func (ssc *SubscriptionScheduleCreate) SetEndBehavior(teb types.ScheduleEndBehavior) *SubscriptionScheduleCreate {
	ssc.mutation.SetEndBehavior(teb)
	return ssc
}

// SetNillableEndBehavior sets the "end_behavior" field if the given value is not nil.
func (ssc *SubscriptionScheduleCreate) SetNillableEndBehavior(teb *types.ScheduleEndBehavior) *SubscriptionScheduleCreate {
	if teb != nil {
		ssc.SetEndBehavior(*teb)
	}
	return ssc
}

// SetStartDate sets the "start_date" field.
func (ssc *SubscriptionScheduleCreate) SetStartDate(t time.Time) *SubscriptionScheduleCreate {
	ssc.mutation.SetStartDate(t)
	return ssc
}

// SetMetadata sets the "metadata" field.
func (ssc *SubscriptionScheduleCreate) SetMetadata(m map[string]string) *SubscriptionScheduleCreate {
	ssc.mutation.SetMetadata(m)
	return ssc
}

// SetID sets the "id" field.
func (ssc *SubscriptionScheduleCreate) SetID(s string) *SubscriptionScheduleCreate {
	ssc.mutation.SetID(s)
	return ssc
}

// AddPhaseIDs adds the "phases" edge to the SubscriptionSchedulePhase entity by IDs.
func (ssc *SubscriptionScheduleCreate) AddPhaseIDs(ids ...string) *SubscriptionScheduleCreate {
	ssc.mutation.AddPhaseIDs(ids...)
	return ssc
}

// AddPhases adds the "phases" edges to the SubscriptionSchedulePhase entity.
func (ssc *SubscriptionScheduleCreate) AddPhases(s ...*SubscriptionSchedulePhase) *SubscriptionScheduleCreate {
	ids := make([]string, len(s))
	for i := range s {
		ids[i] = s[i].ID
	}
	return ssc.AddPhaseIDs(ids...)
}

// SetSubscription sets the "subscription" edge to the Subscription entity.
func (ssc *SubscriptionScheduleCreate) SetSubscription(s *Subscription) *SubscriptionScheduleCreate {
	return ssc.SetSubscriptionID(s.ID)
}

// Mutation returns the SubscriptionScheduleMutation object of the builder.
func (ssc *SubscriptionScheduleCreate) Mutation() *SubscriptionScheduleMutation {
	return ssc.mutation
}

// Save creates the SubscriptionSchedule in the database.
func (ssc *SubscriptionScheduleCreate) Save(ctx context.Context) (*SubscriptionSchedule, error) {
	ssc.defaults()
	return withHooks(ctx, ssc.sqlSave, ssc.mutation, ssc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (ssc *SubscriptionScheduleCreate) SaveX(ctx context.Context) *SubscriptionSchedule {
	v, err := ssc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (ssc *SubscriptionScheduleCreate) Exec(ctx context.Context) error {
	_, err := ssc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (ssc *SubscriptionScheduleCreate) ExecX(ctx context.Context) {
	if err := ssc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (ssc *SubscriptionScheduleCreate) defaults() {
	if _, ok := ssc.mutation.Status(); !ok {
		v := subscriptionschedule.DefaultStatus
		ssc.mutation.SetStatus(v)
	}
	if _, ok := ssc.mutation.CreatedAt(); !ok {
		v := subscriptionschedule.DefaultCreatedAt()
		ssc.mutation.SetCreatedAt(v)
	}
	if _, ok := ssc.mutation.UpdatedAt(); !ok {
		v := subscriptionschedule.DefaultUpdatedAt()
		ssc.mutation.SetUpdatedAt(v)
	}
	if _, ok := ssc.mutation.EnvironmentID(); !ok {
		v := subscriptionschedule.DefaultEnvironmentID
		ssc.mutation.SetEnvironmentID(v)
	}
	if _, ok := ssc.mutation.ScheduleStatus(); !ok {
		v := subscriptionschedule.DefaultScheduleStatus
		ssc.mutation.SetScheduleStatus(v)
	}
	if _, ok := ssc.mutation.CurrentPhaseIndex(); !ok {
		v := subscriptionschedule.DefaultCurrentPhaseIndex
		ssc.mutation.SetCurrentPhaseIndex(v)
	}
	if _, ok := ssc.mutation.EndBehavior(); !ok {
		v := subscriptionschedule.DefaultEndBehavior
		ssc.mutation.SetEndBehavior(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (ssc *SubscriptionScheduleCreate) check() error {
	if _, ok := ssc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "SubscriptionSchedule.tenant_id"`)}
	}
	if v, ok := ssc.mutation.TenantID(); ok {
		if err := subscriptionschedule.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedule.tenant_id": %w`, err)}
		}
	}
	if _, ok := ssc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "SubscriptionSchedule.status"`)}
	}
	if _, ok := ssc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "SubscriptionSchedule.created_at"`)}
	}
	if _, ok := ssc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "SubscriptionSchedule.updated_at"`)}
	}
	if _, ok := ssc.mutation.SubscriptionID(); !ok {
		return &ValidationError{Name: "subscription_id", err: errors.New(`ent: missing required field "SubscriptionSchedule.subscription_id"`)}
	}
	if v, ok := ssc.mutation.SubscriptionID(); ok {
		if err := subscriptionschedule.SubscriptionIDValidator(v); err != nil {
			return &ValidationError{Name: "subscription_id", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedule.subscription_id": %w`, err)}
		}
	}
	if _, ok := ssc.mutation.ScheduleStatus(); !ok {
		return &ValidationError{Name: "schedule_status", err: errors.New(`ent: missing required field "SubscriptionSchedule.schedule_status"`)}
	}
	if _, ok := ssc.mutation.CurrentPhaseIndex(); !ok {
		return &ValidationError{Name: "current_phase_index", err: errors.New(`ent: missing required field "SubscriptionSchedule.current_phase_index"`)}
	}
	if _, ok := ssc.mutation.EndBehavior(); !ok {
		return &ValidationError{Name: "end_behavior", err: errors.New(`ent: missing required field "SubscriptionSchedule.end_behavior"`)}
	}
	if _, ok := ssc.mutation.StartDate(); !ok {
		return &ValidationError{Name: "start_date", err: errors.New(`ent: missing required field "SubscriptionSchedule.start_date"`)}
	}
	if v, ok := ssc.mutation.ID(); ok {
		if err := subscriptionschedule.IDValidator(v); err != nil {
			return &ValidationError{Name: "id", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedule.id": %w`, err)}
		}
	}
	if len(ssc.mutation.SubscriptionIDs()) == 0 {
		return &ValidationError{Name: "subscription", err: errors.New(`ent: missing required edge "SubscriptionSchedule.subscription"`)}
	}
	return nil
}

func (ssc *SubscriptionScheduleCreate) sqlSave(ctx context.Context) (*SubscriptionSchedule, error) {
	if err := ssc.check(); err != nil {
		return nil, err
	}
	_node, _spec := ssc.createSpec()
	if err := sqlgraph.CreateNode(ctx, ssc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected SubscriptionSchedule.ID type: %T", _spec.ID.Value)
		}
	}
	ssc.mutation.id = &_node.ID
	ssc.mutation.done = true
	return _node, nil
}

func (ssc *SubscriptionScheduleCreate) createSpec() (*SubscriptionSchedule, *sqlgraph.CreateSpec) {
	var (
		_node = &SubscriptionSchedule{config: ssc.config}
		_spec = sqlgraph.NewCreateSpec(subscriptionschedule.Table, sqlgraph.NewFieldSpec(subscriptionschedule.FieldID, field.TypeString))
	)
	if id, ok := ssc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := ssc.mutation.TenantID(); ok {
		_spec.SetField(subscriptionschedule.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := ssc.mutation.Status(); ok {
		_spec.SetField(subscriptionschedule.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := ssc.mutation.CreatedAt(); ok {
		_spec.SetField(subscriptionschedule.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := ssc.mutation.UpdatedAt(); ok {
		_spec.SetField(subscriptionschedule.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := ssc.mutation.CreatedBy(); ok {
		_spec.SetField(subscriptionschedule.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := ssc.mutation.UpdatedBy(); ok {
		_spec.SetField(subscriptionschedule.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := ssc.mutation.EnvironmentID(); ok {
		_spec.SetField(subscriptionschedule.FieldEnvironmentID, field.TypeString, value)
		_node.EnvironmentID = value
	}
	if value, ok := ssc.mutation.ScheduleStatus(); ok {
		_spec.SetField(subscriptionschedule.FieldScheduleStatus, field.TypeString, value)
		_node.ScheduleStatus = value
	}
	if value, ok := ssc.mutation.CurrentPhaseIndex(); ok {
		_spec.SetField(subscriptionschedule.FieldCurrentPhaseIndex, field.TypeInt, value)
		_node.CurrentPhaseIndex = value
	}
	if value, ok := ssc.mutation.EndBehavior(); ok {
		_spec.SetField(subscriptionschedule.FieldEndBehavior, field.TypeString, value)
		_node.EndBehavior = value
	}
	if value, ok := ssc.mutation.StartDate(); ok {
		_spec.SetField(subscriptionschedule.FieldStartDate, field.TypeTime, value)
		_node.StartDate = value
	}
	if value, ok := ssc.mutation.Metadata(); ok {
		_spec.SetField(subscriptionschedule.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if nodes := ssc.mutation.PhasesIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.O2M,
			Inverse: false,
			Table:   subscriptionschedule.PhasesTable,
			Columns: []string{subscriptionschedule.PhasesColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(subscriptionschedulephase.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_spec.Edges = append(_spec.Edges, edge)
	}
	if nodes := ssc.mutation.SubscriptionIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.O2O,
			Inverse: true,
			Table:   subscriptionschedule.SubscriptionTable,
			Columns: []string{subscriptionschedule.SubscriptionColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(subscription.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_node.SubscriptionID = nodes[0]
		_spec.Edges = append(_spec.Edges, edge)
	}
	return _node, _spec
}

// SubscriptionScheduleCreateBulk is the builder for creating many SubscriptionSchedule entities in bulk.
type SubscriptionScheduleCreateBulk struct {
	config
	err      error
	builders []*SubscriptionScheduleCreate
}

// Save creates the SubscriptionSchedule entities in the database.
func (sscb *SubscriptionScheduleCreateBulk) Save(ctx context.Context) ([]*SubscriptionSchedule, error) {
	if sscb.err != nil {
		return nil, sscb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(sscb.builders))
	nodes := make([]*SubscriptionSchedule, len(sscb.builders))
	mutators := make([]Mutator, len(sscb.builders))
	for i := range sscb.builders {
		func(i int, root context.Context) {
			builder := sscb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*SubscriptionScheduleMutation)
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
					_, err = mutators[i+1].Mutate(root, sscb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, sscb.driver, spec); err != nil {
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
		if _, err := mutators[0].Mutate(ctx, sscb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (sscb *SubscriptionScheduleCreateBulk) SaveX(ctx context.Context) []*SubscriptionSchedule {
	v, err := sscb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (sscb *SubscriptionScheduleCreateBulk) Exec(ctx context.Context) error {
	_, err := sscb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (sscb *SubscriptionScheduleCreateBulk) ExecX(ctx context.Context) {
	if err := sscb.Exec(ctx); err != nil {
		panic(err)
	}
}
