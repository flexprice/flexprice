// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/subscriptionschedule"
	"github.com/flexprice/flexprice/ent/subscriptionschedulephase"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// SubscriptionSchedulePhaseCreate is the builder for creating a SubscriptionSchedulePhase entity.
type SubscriptionSchedulePhaseCreate struct {
	config
	mutation *SubscriptionSchedulePhaseMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetTenantID(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetTenantID(s)
	return sspc
}

// SetStatus sets the "status" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetStatus(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetStatus(s)
	return sspc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableStatus(s *string) *SubscriptionSchedulePhaseCreate {
	if s != nil {
		sspc.SetStatus(*s)
	}
	return sspc
}

// SetCreatedAt sets the "created_at" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetCreatedAt(t time.Time) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetCreatedAt(t)
	return sspc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableCreatedAt(t *time.Time) *SubscriptionSchedulePhaseCreate {
	if t != nil {
		sspc.SetCreatedAt(*t)
	}
	return sspc
}

// SetUpdatedAt sets the "updated_at" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetUpdatedAt(t time.Time) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetUpdatedAt(t)
	return sspc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableUpdatedAt(t *time.Time) *SubscriptionSchedulePhaseCreate {
	if t != nil {
		sspc.SetUpdatedAt(*t)
	}
	return sspc
}

// SetCreatedBy sets the "created_by" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetCreatedBy(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetCreatedBy(s)
	return sspc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableCreatedBy(s *string) *SubscriptionSchedulePhaseCreate {
	if s != nil {
		sspc.SetCreatedBy(*s)
	}
	return sspc
}

// SetUpdatedBy sets the "updated_by" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetUpdatedBy(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetUpdatedBy(s)
	return sspc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableUpdatedBy(s *string) *SubscriptionSchedulePhaseCreate {
	if s != nil {
		sspc.SetUpdatedBy(*s)
	}
	return sspc
}

// SetEnvironmentID sets the "environment_id" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetEnvironmentID(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetEnvironmentID(s)
	return sspc
}

// SetNillableEnvironmentID sets the "environment_id" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableEnvironmentID(s *string) *SubscriptionSchedulePhaseCreate {
	if s != nil {
		sspc.SetEnvironmentID(*s)
	}
	return sspc
}

// SetScheduleID sets the "schedule_id" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetScheduleID(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetScheduleID(s)
	return sspc
}

// SetPhaseIndex sets the "phase_index" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetPhaseIndex(i int) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetPhaseIndex(i)
	return sspc
}

// SetStartDate sets the "start_date" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetStartDate(t time.Time) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetStartDate(t)
	return sspc
}

// SetEndDate sets the "end_date" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetEndDate(t time.Time) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetEndDate(t)
	return sspc
}

// SetNillableEndDate sets the "end_date" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableEndDate(t *time.Time) *SubscriptionSchedulePhaseCreate {
	if t != nil {
		sspc.SetEndDate(*t)
	}
	return sspc
}

// SetCommitmentAmount sets the "commitment_amount" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetCommitmentAmount(d decimal.Decimal) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetCommitmentAmount(d)
	return sspc
}

// SetNillableCommitmentAmount sets the "commitment_amount" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableCommitmentAmount(d *decimal.Decimal) *SubscriptionSchedulePhaseCreate {
	if d != nil {
		sspc.SetCommitmentAmount(*d)
	}
	return sspc
}

// SetOverageFactor sets the "overage_factor" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetOverageFactor(d decimal.Decimal) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetOverageFactor(d)
	return sspc
}

// SetNillableOverageFactor sets the "overage_factor" field if the given value is not nil.
func (sspc *SubscriptionSchedulePhaseCreate) SetNillableOverageFactor(d *decimal.Decimal) *SubscriptionSchedulePhaseCreate {
	if d != nil {
		sspc.SetOverageFactor(*d)
	}
	return sspc
}

// SetLineItems sets the "line_items" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetLineItems(tpli []types.SchedulePhaseLineItem) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetLineItems(tpli)
	return sspc
}

// SetCreditGrants sets the "credit_grants" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetCreditGrants(tpcg []types.SchedulePhaseCreditGrant) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetCreditGrants(tpcg)
	return sspc
}

// SetMetadata sets the "metadata" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetMetadata(m map[string]string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetMetadata(m)
	return sspc
}

// SetID sets the "id" field.
func (sspc *SubscriptionSchedulePhaseCreate) SetID(s string) *SubscriptionSchedulePhaseCreate {
	sspc.mutation.SetID(s)
	return sspc
}

// SetSchedule sets the "schedule" edge to the SubscriptionSchedule entity.
func (sspc *SubscriptionSchedulePhaseCreate) SetSchedule(s *SubscriptionSchedule) *SubscriptionSchedulePhaseCreate {
	return sspc.SetScheduleID(s.ID)
}

// Mutation returns the SubscriptionSchedulePhaseMutation object of the builder.
func (sspc *SubscriptionSchedulePhaseCreate) Mutation() *SubscriptionSchedulePhaseMutation {
	return sspc.mutation
}

// Save creates the SubscriptionSchedulePhase in the database.
func (sspc *SubscriptionSchedulePhaseCreate) Save(ctx context.Context) (*SubscriptionSchedulePhase, error) {
	sspc.defaults()
	return withHooks(ctx, sspc.sqlSave, sspc.mutation, sspc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (sspc *SubscriptionSchedulePhaseCreate) SaveX(ctx context.Context) *SubscriptionSchedulePhase {
	v, err := sspc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (sspc *SubscriptionSchedulePhaseCreate) Exec(ctx context.Context) error {
	_, err := sspc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (sspc *SubscriptionSchedulePhaseCreate) ExecX(ctx context.Context) {
	if err := sspc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (sspc *SubscriptionSchedulePhaseCreate) defaults() {
	if _, ok := sspc.mutation.Status(); !ok {
		v := subscriptionschedulephase.DefaultStatus
		sspc.mutation.SetStatus(v)
	}
	if _, ok := sspc.mutation.CreatedAt(); !ok {
		v := subscriptionschedulephase.DefaultCreatedAt()
		sspc.mutation.SetCreatedAt(v)
	}
	if _, ok := sspc.mutation.UpdatedAt(); !ok {
		v := subscriptionschedulephase.DefaultUpdatedAt()
		sspc.mutation.SetUpdatedAt(v)
	}
	if _, ok := sspc.mutation.EnvironmentID(); !ok {
		v := subscriptionschedulephase.DefaultEnvironmentID
		sspc.mutation.SetEnvironmentID(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (sspc *SubscriptionSchedulePhaseCreate) check() error {
	if _, ok := sspc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.tenant_id"`)}
	}
	if v, ok := sspc.mutation.TenantID(); ok {
		if err := subscriptionschedulephase.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedulePhase.tenant_id": %w`, err)}
		}
	}
	if _, ok := sspc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.status"`)}
	}
	if _, ok := sspc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.created_at"`)}
	}
	if _, ok := sspc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.updated_at"`)}
	}
	if _, ok := sspc.mutation.ScheduleID(); !ok {
		return &ValidationError{Name: "schedule_id", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.schedule_id"`)}
	}
	if v, ok := sspc.mutation.ScheduleID(); ok {
		if err := subscriptionschedulephase.ScheduleIDValidator(v); err != nil {
			return &ValidationError{Name: "schedule_id", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedulePhase.schedule_id": %w`, err)}
		}
	}
	if _, ok := sspc.mutation.PhaseIndex(); !ok {
		return &ValidationError{Name: "phase_index", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.phase_index"`)}
	}
	if v, ok := sspc.mutation.PhaseIndex(); ok {
		if err := subscriptionschedulephase.PhaseIndexValidator(v); err != nil {
			return &ValidationError{Name: "phase_index", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedulePhase.phase_index": %w`, err)}
		}
	}
	if _, ok := sspc.mutation.StartDate(); !ok {
		return &ValidationError{Name: "start_date", err: errors.New(`ent: missing required field "SubscriptionSchedulePhase.start_date"`)}
	}
	if v, ok := sspc.mutation.ID(); ok {
		if err := subscriptionschedulephase.IDValidator(v); err != nil {
			return &ValidationError{Name: "id", err: fmt.Errorf(`ent: validator failed for field "SubscriptionSchedulePhase.id": %w`, err)}
		}
	}
	if len(sspc.mutation.ScheduleIDs()) == 0 {
		return &ValidationError{Name: "schedule", err: errors.New(`ent: missing required edge "SubscriptionSchedulePhase.schedule"`)}
	}
	return nil
}

func (sspc *SubscriptionSchedulePhaseCreate) sqlSave(ctx context.Context) (*SubscriptionSchedulePhase, error) {
	if err := sspc.check(); err != nil {
		return nil, err
	}
	_node, _spec := sspc.createSpec()
	if err := sqlgraph.CreateNode(ctx, sspc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected SubscriptionSchedulePhase.ID type: %T", _spec.ID.Value)
		}
	}
	sspc.mutation.id = &_node.ID
	sspc.mutation.done = true
	return _node, nil
}

func (sspc *SubscriptionSchedulePhaseCreate) createSpec() (*SubscriptionSchedulePhase, *sqlgraph.CreateSpec) {
	var (
		_node = &SubscriptionSchedulePhase{config: sspc.config}
		_spec = sqlgraph.NewCreateSpec(subscriptionschedulephase.Table, sqlgraph.NewFieldSpec(subscriptionschedulephase.FieldID, field.TypeString))
	)
	if id, ok := sspc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := sspc.mutation.TenantID(); ok {
		_spec.SetField(subscriptionschedulephase.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := sspc.mutation.Status(); ok {
		_spec.SetField(subscriptionschedulephase.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := sspc.mutation.CreatedAt(); ok {
		_spec.SetField(subscriptionschedulephase.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := sspc.mutation.UpdatedAt(); ok {
		_spec.SetField(subscriptionschedulephase.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := sspc.mutation.CreatedBy(); ok {
		_spec.SetField(subscriptionschedulephase.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := sspc.mutation.UpdatedBy(); ok {
		_spec.SetField(subscriptionschedulephase.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := sspc.mutation.EnvironmentID(); ok {
		_spec.SetField(subscriptionschedulephase.FieldEnvironmentID, field.TypeString, value)
		_node.EnvironmentID = value
	}
	if value, ok := sspc.mutation.PhaseIndex(); ok {
		_spec.SetField(subscriptionschedulephase.FieldPhaseIndex, field.TypeInt, value)
		_node.PhaseIndex = value
	}
	if value, ok := sspc.mutation.StartDate(); ok {
		_spec.SetField(subscriptionschedulephase.FieldStartDate, field.TypeTime, value)
		_node.StartDate = value
	}
	if value, ok := sspc.mutation.EndDate(); ok {
		_spec.SetField(subscriptionschedulephase.FieldEndDate, field.TypeTime, value)
		_node.EndDate = &value
	}
	if value, ok := sspc.mutation.CommitmentAmount(); ok {
		_spec.SetField(subscriptionschedulephase.FieldCommitmentAmount, field.TypeOther, value)
		_node.CommitmentAmount = &value
	}
	if value, ok := sspc.mutation.OverageFactor(); ok {
		_spec.SetField(subscriptionschedulephase.FieldOverageFactor, field.TypeOther, value)
		_node.OverageFactor = &value
	}
	if value, ok := sspc.mutation.LineItems(); ok {
		_spec.SetField(subscriptionschedulephase.FieldLineItems, field.TypeJSON, value)
		_node.LineItems = value
	}
	if value, ok := sspc.mutation.CreditGrants(); ok {
		_spec.SetField(subscriptionschedulephase.FieldCreditGrants, field.TypeJSON, value)
		_node.CreditGrants = value
	}
	if value, ok := sspc.mutation.Metadata(); ok {
		_spec.SetField(subscriptionschedulephase.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if nodes := sspc.mutation.ScheduleIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   subscriptionschedulephase.ScheduleTable,
			Columns: []string{subscriptionschedulephase.ScheduleColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(subscriptionschedule.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_node.ScheduleID = nodes[0]
		_spec.Edges = append(_spec.Edges, edge)
	}
	return _node, _spec
}

// SubscriptionSchedulePhaseCreateBulk is the builder for creating many SubscriptionSchedulePhase entities in bulk.
type SubscriptionSchedulePhaseCreateBulk struct {
	config
	err      error
	builders []*SubscriptionSchedulePhaseCreate
}

// Save creates the SubscriptionSchedulePhase entities in the database.
func (sspcb *SubscriptionSchedulePhaseCreateBulk) Save(ctx context.Context) ([]*SubscriptionSchedulePhase, error) {
	if sspcb.err != nil {
		return nil, sspcb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(sspcb.builders))
	nodes := make([]*SubscriptionSchedulePhase, len(sspcb.builders))
	mutators := make([]Mutator, len(sspcb.builders))
	for i := range sspcb.builders {
		func(i int, root context.Context) {
			builder := sspcb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*SubscriptionSchedulePhaseMutation)
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
					_, err = mutators[i+1].Mutate(root, sspcb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, sspcb.driver, spec); err != nil {
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
		if _, err := mutators[0].Mutate(ctx, sspcb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (sspcb *SubscriptionSchedulePhaseCreateBulk) SaveX(ctx context.Context) []*SubscriptionSchedulePhase {
	v, err := sspcb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (sspcb *SubscriptionSchedulePhaseCreateBulk) Exec(ctx context.Context) error {
	_, err := sspcb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (sspcb *SubscriptionSchedulePhaseCreateBulk) ExecX(ctx context.Context) {
	if err := sspcb.Exec(ctx); err != nil {
		panic(err)
	}
}
