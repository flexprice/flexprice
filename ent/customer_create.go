// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/customer"
)

// CustomerCreate is the builder for creating a Customer entity.
type CustomerCreate struct {
	config
	mutation *CustomerMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (cc *CustomerCreate) SetTenantID(s string) *CustomerCreate {
	cc.mutation.SetTenantID(s)
	return cc
}

// SetStatus sets the "status" field.
func (cc *CustomerCreate) SetStatus(s string) *CustomerCreate {
	cc.mutation.SetStatus(s)
	return cc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableStatus(s *string) *CustomerCreate {
	if s != nil {
		cc.SetStatus(*s)
	}
	return cc
}

// SetCreatedAt sets the "created_at" field.
func (cc *CustomerCreate) SetCreatedAt(t time.Time) *CustomerCreate {
	cc.mutation.SetCreatedAt(t)
	return cc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableCreatedAt(t *time.Time) *CustomerCreate {
	if t != nil {
		cc.SetCreatedAt(*t)
	}
	return cc
}

// SetUpdatedAt sets the "updated_at" field.
func (cc *CustomerCreate) SetUpdatedAt(t time.Time) *CustomerCreate {
	cc.mutation.SetUpdatedAt(t)
	return cc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableUpdatedAt(t *time.Time) *CustomerCreate {
	if t != nil {
		cc.SetUpdatedAt(*t)
	}
	return cc
}

// SetCreatedBy sets the "created_by" field.
func (cc *CustomerCreate) SetCreatedBy(s string) *CustomerCreate {
	cc.mutation.SetCreatedBy(s)
	return cc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableCreatedBy(s *string) *CustomerCreate {
	if s != nil {
		cc.SetCreatedBy(*s)
	}
	return cc
}

// SetUpdatedBy sets the "updated_by" field.
func (cc *CustomerCreate) SetUpdatedBy(s string) *CustomerCreate {
	cc.mutation.SetUpdatedBy(s)
	return cc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableUpdatedBy(s *string) *CustomerCreate {
	if s != nil {
		cc.SetUpdatedBy(*s)
	}
	return cc
}

// SetExternalID sets the "external_id" field.
func (cc *CustomerCreate) SetExternalID(s string) *CustomerCreate {
	cc.mutation.SetExternalID(s)
	return cc
}

// SetName sets the "name" field.
func (cc *CustomerCreate) SetName(s string) *CustomerCreate {
	cc.mutation.SetName(s)
	return cc
}

// SetEmail sets the "email" field.
func (cc *CustomerCreate) SetEmail(s string) *CustomerCreate {
	cc.mutation.SetEmail(s)
	return cc
}

// SetNillableEmail sets the "email" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableEmail(s *string) *CustomerCreate {
	if s != nil {
		cc.SetEmail(*s)
	}
	return cc
}

// SetAddressLine1 sets the "address_line1" field.
func (cc *CustomerCreate) SetAddressLine1(s string) *CustomerCreate {
	cc.mutation.SetAddressLine1(s)
	return cc
}

// SetNillableAddressLine1 sets the "address_line1" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableAddressLine1(s *string) *CustomerCreate {
	if s != nil {
		cc.SetAddressLine1(*s)
	}
	return cc
}

// SetAddressLine2 sets the "address_line2" field.
func (cc *CustomerCreate) SetAddressLine2(s string) *CustomerCreate {
	cc.mutation.SetAddressLine2(s)
	return cc
}

// SetNillableAddressLine2 sets the "address_line2" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableAddressLine2(s *string) *CustomerCreate {
	if s != nil {
		cc.SetAddressLine2(*s)
	}
	return cc
}

// SetAddressCity sets the "address_city" field.
func (cc *CustomerCreate) SetAddressCity(s string) *CustomerCreate {
	cc.mutation.SetAddressCity(s)
	return cc
}

// SetNillableAddressCity sets the "address_city" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableAddressCity(s *string) *CustomerCreate {
	if s != nil {
		cc.SetAddressCity(*s)
	}
	return cc
}

// SetAddressState sets the "address_state" field.
func (cc *CustomerCreate) SetAddressState(s string) *CustomerCreate {
	cc.mutation.SetAddressState(s)
	return cc
}

// SetNillableAddressState sets the "address_state" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableAddressState(s *string) *CustomerCreate {
	if s != nil {
		cc.SetAddressState(*s)
	}
	return cc
}

// SetAddressPostalCode sets the "address_postal_code" field.
func (cc *CustomerCreate) SetAddressPostalCode(s string) *CustomerCreate {
	cc.mutation.SetAddressPostalCode(s)
	return cc
}

// SetNillableAddressPostalCode sets the "address_postal_code" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableAddressPostalCode(s *string) *CustomerCreate {
	if s != nil {
		cc.SetAddressPostalCode(*s)
	}
	return cc
}

// SetAddressCountry sets the "address_country" field.
func (cc *CustomerCreate) SetAddressCountry(s string) *CustomerCreate {
	cc.mutation.SetAddressCountry(s)
	return cc
}

// SetNillableAddressCountry sets the "address_country" field if the given value is not nil.
func (cc *CustomerCreate) SetNillableAddressCountry(s *string) *CustomerCreate {
	if s != nil {
		cc.SetAddressCountry(*s)
	}
	return cc
}

// SetMetadata sets the "metadata" field.
func (cc *CustomerCreate) SetMetadata(m map[string]string) *CustomerCreate {
	cc.mutation.SetMetadata(m)
	return cc
}

// SetID sets the "id" field.
func (cc *CustomerCreate) SetID(s string) *CustomerCreate {
	cc.mutation.SetID(s)
	return cc
}

// Mutation returns the CustomerMutation object of the builder.
func (cc *CustomerCreate) Mutation() *CustomerMutation {
	return cc.mutation
}

// Save creates the Customer in the database.
func (cc *CustomerCreate) Save(ctx context.Context) (*Customer, error) {
	cc.defaults()
	return withHooks(ctx, cc.sqlSave, cc.mutation, cc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (cc *CustomerCreate) SaveX(ctx context.Context) *Customer {
	v, err := cc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (cc *CustomerCreate) Exec(ctx context.Context) error {
	_, err := cc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (cc *CustomerCreate) ExecX(ctx context.Context) {
	if err := cc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (cc *CustomerCreate) defaults() {
	if _, ok := cc.mutation.Status(); !ok {
		v := customer.DefaultStatus
		cc.mutation.SetStatus(v)
	}
	if _, ok := cc.mutation.CreatedAt(); !ok {
		v := customer.DefaultCreatedAt()
		cc.mutation.SetCreatedAt(v)
	}
	if _, ok := cc.mutation.UpdatedAt(); !ok {
		v := customer.DefaultUpdatedAt()
		cc.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (cc *CustomerCreate) check() error {
	if _, ok := cc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "Customer.tenant_id"`)}
	}
	if v, ok := cc.mutation.TenantID(); ok {
		if err := customer.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "Customer.tenant_id": %w`, err)}
		}
	}
	if _, ok := cc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "Customer.status"`)}
	}
	if _, ok := cc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "Customer.created_at"`)}
	}
	if _, ok := cc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "Customer.updated_at"`)}
	}
	if _, ok := cc.mutation.ExternalID(); !ok {
		return &ValidationError{Name: "external_id", err: errors.New(`ent: missing required field "Customer.external_id"`)}
	}
	if v, ok := cc.mutation.ExternalID(); ok {
		if err := customer.ExternalIDValidator(v); err != nil {
			return &ValidationError{Name: "external_id", err: fmt.Errorf(`ent: validator failed for field "Customer.external_id": %w`, err)}
		}
	}
	if _, ok := cc.mutation.Name(); !ok {
		return &ValidationError{Name: "name", err: errors.New(`ent: missing required field "Customer.name"`)}
	}
	if v, ok := cc.mutation.Name(); ok {
		if err := customer.NameValidator(v); err != nil {
			return &ValidationError{Name: "name", err: fmt.Errorf(`ent: validator failed for field "Customer.name": %w`, err)}
		}
	}
	return nil
}

func (cc *CustomerCreate) sqlSave(ctx context.Context) (*Customer, error) {
	if err := cc.check(); err != nil {
		return nil, err
	}
	_node, _spec := cc.createSpec()
	if err := sqlgraph.CreateNode(ctx, cc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected Customer.ID type: %T", _spec.ID.Value)
		}
	}
	cc.mutation.id = &_node.ID
	cc.mutation.done = true
	return _node, nil
}

func (cc *CustomerCreate) createSpec() (*Customer, *sqlgraph.CreateSpec) {
	var (
		_node = &Customer{config: cc.config}
		_spec = sqlgraph.NewCreateSpec(customer.Table, sqlgraph.NewFieldSpec(customer.FieldID, field.TypeString))
	)
	if id, ok := cc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := cc.mutation.TenantID(); ok {
		_spec.SetField(customer.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := cc.mutation.Status(); ok {
		_spec.SetField(customer.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := cc.mutation.CreatedAt(); ok {
		_spec.SetField(customer.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := cc.mutation.UpdatedAt(); ok {
		_spec.SetField(customer.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := cc.mutation.CreatedBy(); ok {
		_spec.SetField(customer.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := cc.mutation.UpdatedBy(); ok {
		_spec.SetField(customer.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := cc.mutation.ExternalID(); ok {
		_spec.SetField(customer.FieldExternalID, field.TypeString, value)
		_node.ExternalID = value
	}
	if value, ok := cc.mutation.Name(); ok {
		_spec.SetField(customer.FieldName, field.TypeString, value)
		_node.Name = value
	}
	if value, ok := cc.mutation.Email(); ok {
		_spec.SetField(customer.FieldEmail, field.TypeString, value)
		_node.Email = value
	}
	if value, ok := cc.mutation.AddressLine1(); ok {
		_spec.SetField(customer.FieldAddressLine1, field.TypeString, value)
		_node.AddressLine1 = value
	}
	if value, ok := cc.mutation.AddressLine2(); ok {
		_spec.SetField(customer.FieldAddressLine2, field.TypeString, value)
		_node.AddressLine2 = value
	}
	if value, ok := cc.mutation.AddressCity(); ok {
		_spec.SetField(customer.FieldAddressCity, field.TypeString, value)
		_node.AddressCity = value
	}
	if value, ok := cc.mutation.AddressState(); ok {
		_spec.SetField(customer.FieldAddressState, field.TypeString, value)
		_node.AddressState = value
	}
	if value, ok := cc.mutation.AddressPostalCode(); ok {
		_spec.SetField(customer.FieldAddressPostalCode, field.TypeString, value)
		_node.AddressPostalCode = value
	}
	if value, ok := cc.mutation.AddressCountry(); ok {
		_spec.SetField(customer.FieldAddressCountry, field.TypeString, value)
		_node.AddressCountry = value
	}
	if value, ok := cc.mutation.Metadata(); ok {
		_spec.SetField(customer.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	return _node, _spec
}

// CustomerCreateBulk is the builder for creating many Customer entities in bulk.
type CustomerCreateBulk struct {
	config
	err      error
	builders []*CustomerCreate
}

// Save creates the Customer entities in the database.
func (ccb *CustomerCreateBulk) Save(ctx context.Context) ([]*Customer, error) {
	if ccb.err != nil {
		return nil, ccb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(ccb.builders))
	nodes := make([]*Customer, len(ccb.builders))
	mutators := make([]Mutator, len(ccb.builders))
	for i := range ccb.builders {
		func(i int, root context.Context) {
			builder := ccb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*CustomerMutation)
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
					_, err = mutators[i+1].Mutate(root, ccb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, ccb.driver, spec); err != nil {
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
		if _, err := mutators[0].Mutate(ctx, ccb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (ccb *CustomerCreateBulk) SaveX(ctx context.Context) []*Customer {
	v, err := ccb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (ccb *CustomerCreateBulk) Exec(ctx context.Context) error {
	_, err := ccb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (ccb *CustomerCreateBulk) ExecX(ctx context.Context) {
	if err := ccb.Exec(ctx); err != nil {
		panic(err)
	}
}
