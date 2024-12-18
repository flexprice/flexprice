// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/wallettransaction"
	"github.com/shopspring/decimal"
)

// WalletTransactionCreate is the builder for creating a WalletTransaction entity.
type WalletTransactionCreate struct {
	config
	mutation *WalletTransactionMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (wtc *WalletTransactionCreate) SetTenantID(s string) *WalletTransactionCreate {
	wtc.mutation.SetTenantID(s)
	return wtc
}

// SetWalletID sets the "wallet_id" field.
func (wtc *WalletTransactionCreate) SetWalletID(s string) *WalletTransactionCreate {
	wtc.mutation.SetWalletID(s)
	return wtc
}

// SetType sets the "type" field.
func (wtc *WalletTransactionCreate) SetType(s string) *WalletTransactionCreate {
	wtc.mutation.SetType(s)
	return wtc
}

// SetNillableType sets the "type" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableType(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetType(*s)
	}
	return wtc
}

// SetAmount sets the "amount" field.
func (wtc *WalletTransactionCreate) SetAmount(d decimal.Decimal) *WalletTransactionCreate {
	wtc.mutation.SetAmount(d)
	return wtc
}

// SetBalanceBefore sets the "balance_before" field.
func (wtc *WalletTransactionCreate) SetBalanceBefore(d decimal.Decimal) *WalletTransactionCreate {
	wtc.mutation.SetBalanceBefore(d)
	return wtc
}

// SetNillableBalanceBefore sets the "balance_before" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableBalanceBefore(d *decimal.Decimal) *WalletTransactionCreate {
	if d != nil {
		wtc.SetBalanceBefore(*d)
	}
	return wtc
}

// SetBalanceAfter sets the "balance_after" field.
func (wtc *WalletTransactionCreate) SetBalanceAfter(d decimal.Decimal) *WalletTransactionCreate {
	wtc.mutation.SetBalanceAfter(d)
	return wtc
}

// SetNillableBalanceAfter sets the "balance_after" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableBalanceAfter(d *decimal.Decimal) *WalletTransactionCreate {
	if d != nil {
		wtc.SetBalanceAfter(*d)
	}
	return wtc
}

// SetReferenceType sets the "reference_type" field.
func (wtc *WalletTransactionCreate) SetReferenceType(s string) *WalletTransactionCreate {
	wtc.mutation.SetReferenceType(s)
	return wtc
}

// SetNillableReferenceType sets the "reference_type" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableReferenceType(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetReferenceType(*s)
	}
	return wtc
}

// SetReferenceID sets the "reference_id" field.
func (wtc *WalletTransactionCreate) SetReferenceID(s string) *WalletTransactionCreate {
	wtc.mutation.SetReferenceID(s)
	return wtc
}

// SetNillableReferenceID sets the "reference_id" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableReferenceID(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetReferenceID(*s)
	}
	return wtc
}

// SetDescription sets the "description" field.
func (wtc *WalletTransactionCreate) SetDescription(s string) *WalletTransactionCreate {
	wtc.mutation.SetDescription(s)
	return wtc
}

// SetNillableDescription sets the "description" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableDescription(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetDescription(*s)
	}
	return wtc
}

// SetMetadata sets the "metadata" field.
func (wtc *WalletTransactionCreate) SetMetadata(m map[string]string) *WalletTransactionCreate {
	wtc.mutation.SetMetadata(m)
	return wtc
}

// SetTransactionStatus sets the "transaction_status" field.
func (wtc *WalletTransactionCreate) SetTransactionStatus(s string) *WalletTransactionCreate {
	wtc.mutation.SetTransactionStatus(s)
	return wtc
}

// SetNillableTransactionStatus sets the "transaction_status" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableTransactionStatus(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetTransactionStatus(*s)
	}
	return wtc
}

// SetStatus sets the "status" field.
func (wtc *WalletTransactionCreate) SetStatus(s string) *WalletTransactionCreate {
	wtc.mutation.SetStatus(s)
	return wtc
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableStatus(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetStatus(*s)
	}
	return wtc
}

// SetCreatedAt sets the "created_at" field.
func (wtc *WalletTransactionCreate) SetCreatedAt(t time.Time) *WalletTransactionCreate {
	wtc.mutation.SetCreatedAt(t)
	return wtc
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableCreatedAt(t *time.Time) *WalletTransactionCreate {
	if t != nil {
		wtc.SetCreatedAt(*t)
	}
	return wtc
}

// SetCreatedBy sets the "created_by" field.
func (wtc *WalletTransactionCreate) SetCreatedBy(s string) *WalletTransactionCreate {
	wtc.mutation.SetCreatedBy(s)
	return wtc
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableCreatedBy(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetCreatedBy(*s)
	}
	return wtc
}

// SetUpdatedAt sets the "updated_at" field.
func (wtc *WalletTransactionCreate) SetUpdatedAt(t time.Time) *WalletTransactionCreate {
	wtc.mutation.SetUpdatedAt(t)
	return wtc
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableUpdatedAt(t *time.Time) *WalletTransactionCreate {
	if t != nil {
		wtc.SetUpdatedAt(*t)
	}
	return wtc
}

// SetUpdatedBy sets the "updated_by" field.
func (wtc *WalletTransactionCreate) SetUpdatedBy(s string) *WalletTransactionCreate {
	wtc.mutation.SetUpdatedBy(s)
	return wtc
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (wtc *WalletTransactionCreate) SetNillableUpdatedBy(s *string) *WalletTransactionCreate {
	if s != nil {
		wtc.SetUpdatedBy(*s)
	}
	return wtc
}

// SetID sets the "id" field.
func (wtc *WalletTransactionCreate) SetID(s string) *WalletTransactionCreate {
	wtc.mutation.SetID(s)
	return wtc
}

// Mutation returns the WalletTransactionMutation object of the builder.
func (wtc *WalletTransactionCreate) Mutation() *WalletTransactionMutation {
	return wtc.mutation
}

// Save creates the WalletTransaction in the database.
func (wtc *WalletTransactionCreate) Save(ctx context.Context) (*WalletTransaction, error) {
	wtc.defaults()
	return withHooks(ctx, wtc.sqlSave, wtc.mutation, wtc.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (wtc *WalletTransactionCreate) SaveX(ctx context.Context) *WalletTransaction {
	v, err := wtc.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (wtc *WalletTransactionCreate) Exec(ctx context.Context) error {
	_, err := wtc.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (wtc *WalletTransactionCreate) ExecX(ctx context.Context) {
	if err := wtc.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (wtc *WalletTransactionCreate) defaults() {
	if _, ok := wtc.mutation.GetType(); !ok {
		v := wallettransaction.DefaultType
		wtc.mutation.SetType(v)
	}
	if _, ok := wtc.mutation.BalanceBefore(); !ok {
		v := wallettransaction.DefaultBalanceBefore
		wtc.mutation.SetBalanceBefore(v)
	}
	if _, ok := wtc.mutation.BalanceAfter(); !ok {
		v := wallettransaction.DefaultBalanceAfter
		wtc.mutation.SetBalanceAfter(v)
	}
	if _, ok := wtc.mutation.TransactionStatus(); !ok {
		v := wallettransaction.DefaultTransactionStatus
		wtc.mutation.SetTransactionStatus(v)
	}
	if _, ok := wtc.mutation.Status(); !ok {
		v := wallettransaction.DefaultStatus
		wtc.mutation.SetStatus(v)
	}
	if _, ok := wtc.mutation.CreatedAt(); !ok {
		v := wallettransaction.DefaultCreatedAt()
		wtc.mutation.SetCreatedAt(v)
	}
	if _, ok := wtc.mutation.UpdatedAt(); !ok {
		v := wallettransaction.DefaultUpdatedAt()
		wtc.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (wtc *WalletTransactionCreate) check() error {
	if _, ok := wtc.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "WalletTransaction.tenant_id"`)}
	}
	if v, ok := wtc.mutation.TenantID(); ok {
		if err := wallettransaction.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "WalletTransaction.tenant_id": %w`, err)}
		}
	}
	if _, ok := wtc.mutation.WalletID(); !ok {
		return &ValidationError{Name: "wallet_id", err: errors.New(`ent: missing required field "WalletTransaction.wallet_id"`)}
	}
	if v, ok := wtc.mutation.WalletID(); ok {
		if err := wallettransaction.WalletIDValidator(v); err != nil {
			return &ValidationError{Name: "wallet_id", err: fmt.Errorf(`ent: validator failed for field "WalletTransaction.wallet_id": %w`, err)}
		}
	}
	if _, ok := wtc.mutation.GetType(); !ok {
		return &ValidationError{Name: "type", err: errors.New(`ent: missing required field "WalletTransaction.type"`)}
	}
	if v, ok := wtc.mutation.GetType(); ok {
		if err := wallettransaction.TypeValidator(v); err != nil {
			return &ValidationError{Name: "type", err: fmt.Errorf(`ent: validator failed for field "WalletTransaction.type": %w`, err)}
		}
	}
	if _, ok := wtc.mutation.Amount(); !ok {
		return &ValidationError{Name: "amount", err: errors.New(`ent: missing required field "WalletTransaction.amount"`)}
	}
	if _, ok := wtc.mutation.BalanceBefore(); !ok {
		return &ValidationError{Name: "balance_before", err: errors.New(`ent: missing required field "WalletTransaction.balance_before"`)}
	}
	if _, ok := wtc.mutation.BalanceAfter(); !ok {
		return &ValidationError{Name: "balance_after", err: errors.New(`ent: missing required field "WalletTransaction.balance_after"`)}
	}
	if _, ok := wtc.mutation.TransactionStatus(); !ok {
		return &ValidationError{Name: "transaction_status", err: errors.New(`ent: missing required field "WalletTransaction.transaction_status"`)}
	}
	if _, ok := wtc.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "WalletTransaction.status"`)}
	}
	if _, ok := wtc.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "WalletTransaction.created_at"`)}
	}
	if _, ok := wtc.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "WalletTransaction.updated_at"`)}
	}
	return nil
}

func (wtc *WalletTransactionCreate) sqlSave(ctx context.Context) (*WalletTransaction, error) {
	if err := wtc.check(); err != nil {
		return nil, err
	}
	_node, _spec := wtc.createSpec()
	if err := sqlgraph.CreateNode(ctx, wtc.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected WalletTransaction.ID type: %T", _spec.ID.Value)
		}
	}
	wtc.mutation.id = &_node.ID
	wtc.mutation.done = true
	return _node, nil
}

func (wtc *WalletTransactionCreate) createSpec() (*WalletTransaction, *sqlgraph.CreateSpec) {
	var (
		_node = &WalletTransaction{config: wtc.config}
		_spec = sqlgraph.NewCreateSpec(wallettransaction.Table, sqlgraph.NewFieldSpec(wallettransaction.FieldID, field.TypeString))
	)
	if id, ok := wtc.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := wtc.mutation.TenantID(); ok {
		_spec.SetField(wallettransaction.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := wtc.mutation.WalletID(); ok {
		_spec.SetField(wallettransaction.FieldWalletID, field.TypeString, value)
		_node.WalletID = value
	}
	if value, ok := wtc.mutation.GetType(); ok {
		_spec.SetField(wallettransaction.FieldType, field.TypeString, value)
		_node.Type = value
	}
	if value, ok := wtc.mutation.Amount(); ok {
		_spec.SetField(wallettransaction.FieldAmount, field.TypeOther, value)
		_node.Amount = value
	}
	if value, ok := wtc.mutation.BalanceBefore(); ok {
		_spec.SetField(wallettransaction.FieldBalanceBefore, field.TypeOther, value)
		_node.BalanceBefore = value
	}
	if value, ok := wtc.mutation.BalanceAfter(); ok {
		_spec.SetField(wallettransaction.FieldBalanceAfter, field.TypeOther, value)
		_node.BalanceAfter = value
	}
	if value, ok := wtc.mutation.ReferenceType(); ok {
		_spec.SetField(wallettransaction.FieldReferenceType, field.TypeString, value)
		_node.ReferenceType = value
	}
	if value, ok := wtc.mutation.ReferenceID(); ok {
		_spec.SetField(wallettransaction.FieldReferenceID, field.TypeString, value)
		_node.ReferenceID = value
	}
	if value, ok := wtc.mutation.Description(); ok {
		_spec.SetField(wallettransaction.FieldDescription, field.TypeString, value)
		_node.Description = value
	}
	if value, ok := wtc.mutation.Metadata(); ok {
		_spec.SetField(wallettransaction.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if value, ok := wtc.mutation.TransactionStatus(); ok {
		_spec.SetField(wallettransaction.FieldTransactionStatus, field.TypeString, value)
		_node.TransactionStatus = value
	}
	if value, ok := wtc.mutation.Status(); ok {
		_spec.SetField(wallettransaction.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := wtc.mutation.CreatedAt(); ok {
		_spec.SetField(wallettransaction.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := wtc.mutation.CreatedBy(); ok {
		_spec.SetField(wallettransaction.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := wtc.mutation.UpdatedAt(); ok {
		_spec.SetField(wallettransaction.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := wtc.mutation.UpdatedBy(); ok {
		_spec.SetField(wallettransaction.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	return _node, _spec
}

// WalletTransactionCreateBulk is the builder for creating many WalletTransaction entities in bulk.
type WalletTransactionCreateBulk struct {
	config
	err      error
	builders []*WalletTransactionCreate
}

// Save creates the WalletTransaction entities in the database.
func (wtcb *WalletTransactionCreateBulk) Save(ctx context.Context) ([]*WalletTransaction, error) {
	if wtcb.err != nil {
		return nil, wtcb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(wtcb.builders))
	nodes := make([]*WalletTransaction, len(wtcb.builders))
	mutators := make([]Mutator, len(wtcb.builders))
	for i := range wtcb.builders {
		func(i int, root context.Context) {
			builder := wtcb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*WalletTransactionMutation)
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
					_, err = mutators[i+1].Mutate(root, wtcb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, wtcb.driver, spec); err != nil {
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
		if _, err := mutators[0].Mutate(ctx, wtcb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (wtcb *WalletTransactionCreateBulk) SaveX(ctx context.Context) []*WalletTransaction {
	v, err := wtcb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (wtcb *WalletTransactionCreateBulk) Exec(ctx context.Context) error {
	_, err := wtcb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (wtcb *WalletTransactionCreateBulk) ExecX(ctx context.Context) {
	if err := wtcb.Exec(ctx); err != nil {
		panic(err)
	}
}