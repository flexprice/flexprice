// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/feature"
)

// FeatureCreate is the builder for creating a Feature entity.
type FeatureCreate struct {
	config
	mutation *FeatureMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (fc *FeatureCreate) SetTenantID(s string) *FeatureCreate {
	fc.mutation.SetTenantID(s)
	return fc
}

// SetStatus sets the "status" field.
func (fc *FeatureCreate) SetStatus(s string) *FeatureCreate {
	fc.mutation.SetStatus(s)
	return fc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableStatus(s *string) *FeatureCreate {
	if s != nil {
		fc.SetStatus(*s)
	}
	return fc
}

// SetCreatedAt sets the "created_at" field.
func (fc *FeatureCreate) SetCreatedAt(t time.Time) *FeatureCreate {
	fc.mutation.SetCreatedAt(t)
	return fc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableCreatedAt(t *time.Time) *FeatureCreate {
	if t != nil {
		fc.SetCreatedAt(*t)
	}
	return fc
}

// SetUpdatedAt sets the "updated_at" field.
func (fc *FeatureCreate) SetUpdatedAt(t time.Time) *FeatureCreate {
	fc.mutation.SetUpdatedAt(t)
	return fc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableUpdatedAt(t *time.Time) *FeatureCreate {
	if t != nil {
		fc.SetUpdatedAt(*t)
	}
	return fc
}

// SetCreatedBy sets the "created_by" field.
func (fc *FeatureCreate) SetCreatedBy(s string) *FeatureCreate {
	fc.mutation.SetCreatedBy(s)
	return fc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableCreatedBy(s *string) *FeatureCreate {
	if s != nil {
		fc.SetCreatedBy(*s)
	}
	return fc
}

// SetUpdatedBy sets the "updated_by" field.
func (fc *FeatureCreate) SetUpdatedBy(s string) *FeatureCreate {
	fc.mutation.SetUpdatedBy(s)
	return fc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableUpdatedBy(s *string) *FeatureCreate {
	if s != nil {
		fc.SetUpdatedBy(*s)
	}
	return fc
}

// SetEnvironmentID sets the "environment_id" field.
func (fc *FeatureCreate) SetEnvironmentID(s string) *FeatureCreate {
	fc.mutation.SetEnvironmentID(s)
	return fc
}

// SetNillableEnvironmentID sets the "environment_id" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableEnvironmentID(s *string) *FeatureCreate {
	if s != nil {
		fc.SetEnvironmentID(*s)
	}
	return fc
}

// SetLookupKey sets the "lookup_key" field.
func (fc *FeatureCreate) SetLookupKey(s string) *FeatureCreate {
	fc.mutation.SetLookupKey(s)
	return fc
}

// SetName sets the "name" field.
func (fc *FeatureCreate) SetName(s string) *FeatureCreate {
	fc.mutation.SetName(s)
	return fc
}

// SetDescription sets the "description" field.
func (fc *FeatureCreate) SetDescription(s string) *FeatureCreate {
	fc.mutation.SetDescription(s)
	return fc
}

// SetNillableDescription sets the "description" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableDescription(s *string) *FeatureCreate {
	if s != nil {
		fc.SetDescription(*s)
	}
	return fc
}

// SetType sets the "type" field.
func (fc *FeatureCreate) SetType(s string) *FeatureCreate {
	fc.mutation.SetType(s)
	return fc
}

// SetMeterID sets the "meter_id" field.
func (fc *FeatureCreate) SetMeterID(s string) *FeatureCreate {
	fc.mutation.SetMeterID(s)
	return fc
}

// SetNillableMeterID sets the "meter_id" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableMeterID(s *string) *FeatureCreate {
	if s != nil {
		fc.SetMeterID(*s)
	}
	return fc
}

// SetMetadata sets the "metadata" field.
func (fc *FeatureCreate) SetMetadata(m map[string]string) *FeatureCreate {
	fc.mutation.SetMetadata(m)
	return fc
}

// SetUnitSingular sets the "unit_singular" field.
func (fc *FeatureCreate) SetUnitSingular(s string) *FeatureCreate {
	fc.mutation.SetUnitSingular(s)
	return fc
}

// SetNillableUnitSingular sets the "unit_singular" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableUnitSingular(s *string) *FeatureCreate {
	if s != nil {
		fc.SetUnitSingular(*s)
	}
	return fc
}

// SetUnitPlural sets the "unit_plural" field.
func (fc *FeatureCreate) SetUnitPlural(s string) *FeatureCreate {
	fc.mutation.SetUnitPlural(s)
	return fc
}

// SetNillableUnitPlural sets the "unit_plural" field if the given value is not nil.
func (fc *FeatureCreate) SetNillableUnitPlural(s *string) *FeatureCreate {
	if s != nil {
		fc.SetUnitPlural(*s)
	}
	return fc
}

// SetID sets the "id" field.
func (fc *FeatureCreate) SetID(s string) *FeatureCreate {
	fc.mutation.SetID(s)
	return fc
}

// Mutation returns the FeatureMutation object of the builder.
func (fc *FeatureCreate) Mutation() *FeatureMutation {
	return fc.mutation
}

// Save creates the Feature in the database.
func (fc *FeatureCreate) Save(ctx context.Context) (*Feature, error) {
	fc.defaults()
	return withHooks(ctx, fc.sqlSave, fc.mutation, fc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (fc *FeatureCreate) SaveX(ctx context.Context) *Feature {
	v, err := fc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (fc *FeatureCreate) Exec(ctx context.Context) error {
	_, err := fc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (fc *FeatureCreate) ExecX(ctx context.Context) {
	if err := fc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (fc *FeatureCreate) defaults() {
	if _, ok := fc.mutation.Status(); !ok {
		v := feature.DefaultStatus
		fc.mutation.SetStatus(v)
	}
	if _, ok := fc.mutation.CreatedAt(); !ok {
		v := feature.DefaultCreatedAt()
		fc.mutation.SetCreatedAt(v)
	}
	if _, ok := fc.mutation.UpdatedAt(); !ok {
		v := feature.DefaultUpdatedAt()
		fc.mutation.SetUpdatedAt(v)
	}
	if _, ok := fc.mutation.EnvironmentID(); !ok {
		v := feature.DefaultEnvironmentID
		fc.mutation.SetEnvironmentID(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (fc *FeatureCreate) check() error {
	if _, ok := fc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "Feature.tenant_id"`)}
	}
	if v, ok := fc.mutation.TenantID(); ok {
		if err := feature.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "Feature.tenant_id": %w`, err)}
		}
	}
	if _, ok := fc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "Feature.status"`)}
	}
	if _, ok := fc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "Feature.created_at"`)}
	}
	if _, ok := fc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "Feature.updated_at"`)}
	}
	if _, ok := fc.mutation.LookupKey(); !ok {
		return &ValidationError{Name: "lookup_key", err: errors.New(`ent: missing required field "Feature.lookup_key"`)}
	}
	if _, ok := fc.mutation.Name(); !ok {
		return &ValidationError{Name: "name", err: errors.New(`ent: missing required field "Feature.name"`)}
	}
	if v, ok := fc.mutation.Name(); ok {
		if err := feature.NameValidator(v); err != nil {
			return &ValidationError{Name: "name", err: fmt.Errorf(`ent: validator failed for field "Feature.name": %w`, err)}
		}
	}
	if _, ok := fc.mutation.GetType(); !ok {
		return &ValidationError{Name: "type", err: errors.New(`ent: missing required field "Feature.type"`)}
	}
	if v, ok := fc.mutation.GetType(); ok {
		if err := feature.TypeValidator(v); err != nil {
			return &ValidationError{Name: "type", err: fmt.Errorf(`ent: validator failed for field "Feature.type": %w`, err)}
		}
	}
	return nil
}

func (fc *FeatureCreate) sqlSave(ctx context.Context) (*Feature, error) {
	if err := fc.check(); err != nil {
		return nil, err
	}
	_node, _spec := fc.createSpec()
	if err := sqlgraph.CreateNode(ctx, fc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected Feature.ID type: %T", _spec.ID.Value)
		}
	}
	fc.mutation.id = &_node.ID
	fc.mutation.done = true
	return _node, nil
}

func (fc *FeatureCreate) createSpec() (*Feature, *sqlgraph.CreateSpec) {
	var (
		_node = &Feature{config: fc.config}
		_spec = sqlgraph.NewCreateSpec(feature.Table, sqlgraph.NewFieldSpec(feature.FieldID, field.TypeString))
	)
	if id, ok := fc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := fc.mutation.TenantID(); ok {
		_spec.SetField(feature.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := fc.mutation.Status(); ok {
		_spec.SetField(feature.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := fc.mutation.CreatedAt(); ok {
		_spec.SetField(feature.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := fc.mutation.UpdatedAt(); ok {
		_spec.SetField(feature.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := fc.mutation.CreatedBy(); ok {
		_spec.SetField(feature.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := fc.mutation.UpdatedBy(); ok {
		_spec.SetField(feature.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := fc.mutation.EnvironmentID(); ok {
		_spec.SetField(feature.FieldEnvironmentID, field.TypeString, value)
		_node.EnvironmentID = value
	}
	if value, ok := fc.mutation.LookupKey(); ok {
		_spec.SetField(feature.FieldLookupKey, field.TypeString, value)
		_node.LookupKey = value
	}
	if value, ok := fc.mutation.Name(); ok {
		_spec.SetField(feature.FieldName, field.TypeString, value)
		_node.Name = value
	}
	if value, ok := fc.mutation.Description(); ok {
		_spec.SetField(feature.FieldDescription, field.TypeString, value)
		_node.Description = &value
	}
	if value, ok := fc.mutation.GetType(); ok {
		_spec.SetField(feature.FieldType, field.TypeString, value)
		_node.Type = value
	}
	if value, ok := fc.mutation.MeterID(); ok {
		_spec.SetField(feature.FieldMeterID, field.TypeString, value)
		_node.MeterID = &value
	}
	if value, ok := fc.mutation.Metadata(); ok {
		_spec.SetField(feature.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if value, ok := fc.mutation.UnitSingular(); ok {
		_spec.SetField(feature.FieldUnitSingular, field.TypeString, value)
		_node.UnitSingular = &value
	}
	if value, ok := fc.mutation.UnitPlural(); ok {
		_spec.SetField(feature.FieldUnitPlural, field.TypeString, value)
		_node.UnitPlural = &value
	}
	return _node, _spec
}

// FeatureCreateBulk is the builder for creating many Feature entities in bulk.
type FeatureCreateBulk struct {
	config
	err      error
	builders []*FeatureCreate
}

// Save creates the Feature entities in the database.
func (fcb *FeatureCreateBulk) Save(ctx context.Context) ([]*Feature, error) {
	if fcb.err != nil {
		return nil, fcb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(fcb.builders))
	nodes := make([]*Feature, len(fcb.builders))
	mutators := make([]Mutator, len(fcb.builders))
	for i := range fcb.builders {
		func(i int, root context.Context) {
			builder := fcb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*FeatureMutation)
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
					_, err = mutators[i+1].Mutate(root, fcb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, fcb.driver, spec); err != nil {
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
		if _, err := mutators[0].Mutate(ctx, fcb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (fcb *FeatureCreateBulk) SaveX(ctx context.Context) []*Feature {
	v, err := fcb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (fcb *FeatureCreateBulk) Exec(ctx context.Context) error {
	_, err := fcb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (fcb *FeatureCreateBulk) ExecX(ctx context.Context) {
	if err := fcb.Exec(ctx); err != nil {
		panic(err)
	}
}
