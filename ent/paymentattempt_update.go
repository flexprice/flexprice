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
	"github.com/flexprice/flexprice/ent/payment"
	"github.com/flexprice/flexprice/ent/paymentattempt"
	"github.com/flexprice/flexprice/ent/predicate"
)

// PaymentAttemptUpdate is the builder for updating PaymentAttempt entities.
type PaymentAttemptUpdate struct {
	config
	hooks    []Hook
	mutation *PaymentAttemptMutation
}

// Where appends a list predicates to the PaymentAttemptUpdate builder.
func (pau *PaymentAttemptUpdate) Where(ps ...predicate.PaymentAttempt) *PaymentAttemptUpdate {
	pau.mutation.Where(ps...)
	return pau
}

// SetStatus sets the "status" field.
func (pau *PaymentAttemptUpdate) SetStatus(s string) *PaymentAttemptUpdate {
	pau.mutation.SetStatus(s)
	return pau
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillableStatus(s *string) *PaymentAttemptUpdate {
	if s != nil {
		pau.SetStatus(*s)
	}
	return pau
}

// SetUpdatedAt sets the "updated_at" field.
func (pau *PaymentAttemptUpdate) SetUpdatedAt(t time.Time) *PaymentAttemptUpdate {
	pau.mutation.SetUpdatedAt(t)
	return pau
}

// SetUpdatedBy sets the "updated_by" field.
func (pau *PaymentAttemptUpdate) SetUpdatedBy(s string) *PaymentAttemptUpdate {
	pau.mutation.SetUpdatedBy(s)
	return pau
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillableUpdatedBy(s *string) *PaymentAttemptUpdate {
	if s != nil {
		pau.SetUpdatedBy(*s)
	}
	return pau
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (pau *PaymentAttemptUpdate) ClearUpdatedBy() *PaymentAttemptUpdate {
	pau.mutation.ClearUpdatedBy()
	return pau
}

// SetPaymentID sets the "payment_id" field.
func (pau *PaymentAttemptUpdate) SetPaymentID(s string) *PaymentAttemptUpdate {
	pau.mutation.SetPaymentID(s)
	return pau
}

// SetNillablePaymentID sets the "payment_id" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillablePaymentID(s *string) *PaymentAttemptUpdate {
	if s != nil {
		pau.SetPaymentID(*s)
	}
	return pau
}

// SetPaymentStatus sets the "payment_status" field.
func (pau *PaymentAttemptUpdate) SetPaymentStatus(s string) *PaymentAttemptUpdate {
	pau.mutation.SetPaymentStatus(s)
	return pau
}

// SetNillablePaymentStatus sets the "payment_status" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillablePaymentStatus(s *string) *PaymentAttemptUpdate {
	if s != nil {
		pau.SetPaymentStatus(*s)
	}
	return pau
}

// SetAttemptNumber sets the "attempt_number" field.
func (pau *PaymentAttemptUpdate) SetAttemptNumber(i int) *PaymentAttemptUpdate {
	pau.mutation.ResetAttemptNumber()
	pau.mutation.SetAttemptNumber(i)
	return pau
}

// SetNillableAttemptNumber sets the "attempt_number" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillableAttemptNumber(i *int) *PaymentAttemptUpdate {
	if i != nil {
		pau.SetAttemptNumber(*i)
	}
	return pau
}

// AddAttemptNumber adds i to the "attempt_number" field.
func (pau *PaymentAttemptUpdate) AddAttemptNumber(i int) *PaymentAttemptUpdate {
	pau.mutation.AddAttemptNumber(i)
	return pau
}

// SetGatewayAttemptID sets the "gateway_attempt_id" field.
func (pau *PaymentAttemptUpdate) SetGatewayAttemptID(s string) *PaymentAttemptUpdate {
	pau.mutation.SetGatewayAttemptID(s)
	return pau
}

// SetNillableGatewayAttemptID sets the "gateway_attempt_id" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillableGatewayAttemptID(s *string) *PaymentAttemptUpdate {
	if s != nil {
		pau.SetGatewayAttemptID(*s)
	}
	return pau
}

// ClearGatewayAttemptID clears the value of the "gateway_attempt_id" field.
func (pau *PaymentAttemptUpdate) ClearGatewayAttemptID() *PaymentAttemptUpdate {
	pau.mutation.ClearGatewayAttemptID()
	return pau
}

// SetErrorMessage sets the "error_message" field.
func (pau *PaymentAttemptUpdate) SetErrorMessage(s string) *PaymentAttemptUpdate {
	pau.mutation.SetErrorMessage(s)
	return pau
}

// SetNillableErrorMessage sets the "error_message" field if the given value is not nil.
func (pau *PaymentAttemptUpdate) SetNillableErrorMessage(s *string) *PaymentAttemptUpdate {
	if s != nil {
		pau.SetErrorMessage(*s)
	}
	return pau
}

// ClearErrorMessage clears the value of the "error_message" field.
func (pau *PaymentAttemptUpdate) ClearErrorMessage() *PaymentAttemptUpdate {
	pau.mutation.ClearErrorMessage()
	return pau
}

// SetMetadata sets the "metadata" field.
func (pau *PaymentAttemptUpdate) SetMetadata(m map[string]string) *PaymentAttemptUpdate {
	pau.mutation.SetMetadata(m)
	return pau
}

// ClearMetadata clears the value of the "metadata" field.
func (pau *PaymentAttemptUpdate) ClearMetadata() *PaymentAttemptUpdate {
	pau.mutation.ClearMetadata()
	return pau
}

// SetPayment sets the "payment" edge to the Payment entity.
func (pau *PaymentAttemptUpdate) SetPayment(p *Payment) *PaymentAttemptUpdate {
	return pau.SetPaymentID(p.ID)
}

// Mutation returns the PaymentAttemptMutation object of the builder.
func (pau *PaymentAttemptUpdate) Mutation() *PaymentAttemptMutation {
	return pau.mutation
}

// ClearPayment clears the "payment" edge to the Payment entity.
func (pau *PaymentAttemptUpdate) ClearPayment() *PaymentAttemptUpdate {
	pau.mutation.ClearPayment()
	return pau
}

// Save executes the query and returns the number of nodes affected by the update operation.
func (pau *PaymentAttemptUpdate) Save(ctx context.Context) (int, error) {
	pau.defaults()
	return withHooks(ctx, pau.sqlSave, pau.mutation, pau.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (pau *PaymentAttemptUpdate) SaveX(ctx context.Context) int {
	affected, err := pau.Save(ctx)
	if err != nil {
		panic(err)
	}
	return affected
}

// Exec executes the query.
func (pau *PaymentAttemptUpdate) Exec(ctx context.Context) error {
	_, err := pau.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (pau *PaymentAttemptUpdate) ExecX(ctx context.Context) {
	if err := pau.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (pau *PaymentAttemptUpdate) defaults() {
	if _, ok := pau.mutation.UpdatedAt(); !ok {
		v := paymentattempt.UpdateDefaultUpdatedAt()
		pau.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (pau *PaymentAttemptUpdate) check() error {
	if v, ok := pau.mutation.PaymentID(); ok {
		if err := paymentattempt.PaymentIDValidator(v); err != nil {
			return &ValidationError{Name: "payment_id", err: fmt.Errorf(`ent: validator failed for field "PaymentAttempt.payment_id": %w`, err)}
		}
	}
	if v, ok := pau.mutation.PaymentStatus(); ok {
		if err := paymentattempt.PaymentStatusValidator(v); err != nil {
			return &ValidationError{Name: "payment_status", err: fmt.Errorf(`ent: validator failed for field "PaymentAttempt.payment_status": %w`, err)}
		}
	}
	if v, ok := pau.mutation.AttemptNumber(); ok {
		if err := paymentattempt.AttemptNumberValidator(v); err != nil {
			return &ValidationError{Name: "attempt_number", err: fmt.Errorf(`ent: validator failed for field "PaymentAttempt.attempt_number": %w`, err)}
		}
	}
	if pau.mutation.PaymentCleared() && len(pau.mutation.PaymentIDs()) > 0 {
		return errors.New(`ent: clearing a required unique edge "PaymentAttempt.payment"`)
	}
	return nil
}

func (pau *PaymentAttemptUpdate) sqlSave(ctx context.Context) (n int, err error) {
	if err := pau.check(); err != nil {
		return n, err
	}
	_spec := sqlgraph.NewUpdateSpec(paymentattempt.Table, paymentattempt.Columns, sqlgraph.NewFieldSpec(paymentattempt.FieldID, field.TypeString))
	if ps := pau.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := pau.mutation.Status(); ok {
		_spec.SetField(paymentattempt.FieldStatus, field.TypeString, value)
	}
	if value, ok := pau.mutation.UpdatedAt(); ok {
		_spec.SetField(paymentattempt.FieldUpdatedAt, field.TypeTime, value)
	}
	if pau.mutation.CreatedByCleared() {
		_spec.ClearField(paymentattempt.FieldCreatedBy, field.TypeString)
	}
	if value, ok := pau.mutation.UpdatedBy(); ok {
		_spec.SetField(paymentattempt.FieldUpdatedBy, field.TypeString, value)
	}
	if pau.mutation.UpdatedByCleared() {
		_spec.ClearField(paymentattempt.FieldUpdatedBy, field.TypeString)
	}
	if pau.mutation.EnvironmentIDCleared() {
		_spec.ClearField(paymentattempt.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := pau.mutation.PaymentStatus(); ok {
		_spec.SetField(paymentattempt.FieldPaymentStatus, field.TypeString, value)
	}
	if value, ok := pau.mutation.AttemptNumber(); ok {
		_spec.SetField(paymentattempt.FieldAttemptNumber, field.TypeInt, value)
	}
	if value, ok := pau.mutation.AddedAttemptNumber(); ok {
		_spec.AddField(paymentattempt.FieldAttemptNumber, field.TypeInt, value)
	}
	if value, ok := pau.mutation.GatewayAttemptID(); ok {
		_spec.SetField(paymentattempt.FieldGatewayAttemptID, field.TypeString, value)
	}
	if pau.mutation.GatewayAttemptIDCleared() {
		_spec.ClearField(paymentattempt.FieldGatewayAttemptID, field.TypeString)
	}
	if value, ok := pau.mutation.ErrorMessage(); ok {
		_spec.SetField(paymentattempt.FieldErrorMessage, field.TypeString, value)
	}
	if pau.mutation.ErrorMessageCleared() {
		_spec.ClearField(paymentattempt.FieldErrorMessage, field.TypeString)
	}
	if value, ok := pau.mutation.Metadata(); ok {
		_spec.SetField(paymentattempt.FieldMetadata, field.TypeJSON, value)
	}
	if pau.mutation.MetadataCleared() {
		_spec.ClearField(paymentattempt.FieldMetadata, field.TypeJSON)
	}
	if pau.mutation.PaymentCleared() {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   paymentattempt.PaymentTable,
			Columns: []string{paymentattempt.PaymentColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(payment.FieldID, field.TypeString),
			},
		}
		_spec.Edges.Clear = append(_spec.Edges.Clear, edge)
	}
	if nodes := pau.mutation.PaymentIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   paymentattempt.PaymentTable,
			Columns: []string{paymentattempt.PaymentColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(payment.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_spec.Edges.Add = append(_spec.Edges.Add, edge)
	}
	if n, err = sqlgraph.UpdateNodes(ctx, pau.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{paymentattempt.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return 0, err
	}
	pau.mutation.done = true
	return n, nil
}

// PaymentAttemptUpdateOne is the builder for updating a single PaymentAttempt entity.
type PaymentAttemptUpdateOne struct {
	config
	fields   []string
	hooks    []Hook
	mutation *PaymentAttemptMutation
}

// SetStatus sets the "status" field.
func (pauo *PaymentAttemptUpdateOne) SetStatus(s string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetStatus(s)
	return pauo
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillableStatus(s *string) *PaymentAttemptUpdateOne {
	if s != nil {
		pauo.SetStatus(*s)
	}
	return pauo
}

// SetUpdatedAt sets the "updated_at" field.
func (pauo *PaymentAttemptUpdateOne) SetUpdatedAt(t time.Time) *PaymentAttemptUpdateOne {
	pauo.mutation.SetUpdatedAt(t)
	return pauo
}

// SetUpdatedBy sets the "updated_by" field.
func (pauo *PaymentAttemptUpdateOne) SetUpdatedBy(s string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetUpdatedBy(s)
	return pauo
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillableUpdatedBy(s *string) *PaymentAttemptUpdateOne {
	if s != nil {
		pauo.SetUpdatedBy(*s)
	}
	return pauo
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (pauo *PaymentAttemptUpdateOne) ClearUpdatedBy() *PaymentAttemptUpdateOne {
	pauo.mutation.ClearUpdatedBy()
	return pauo
}

// SetPaymentID sets the "payment_id" field.
func (pauo *PaymentAttemptUpdateOne) SetPaymentID(s string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetPaymentID(s)
	return pauo
}

// SetNillablePaymentID sets the "payment_id" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillablePaymentID(s *string) *PaymentAttemptUpdateOne {
	if s != nil {
		pauo.SetPaymentID(*s)
	}
	return pauo
}

// SetPaymentStatus sets the "payment_status" field.
func (pauo *PaymentAttemptUpdateOne) SetPaymentStatus(s string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetPaymentStatus(s)
	return pauo
}

// SetNillablePaymentStatus sets the "payment_status" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillablePaymentStatus(s *string) *PaymentAttemptUpdateOne {
	if s != nil {
		pauo.SetPaymentStatus(*s)
	}
	return pauo
}

// SetAttemptNumber sets the "attempt_number" field.
func (pauo *PaymentAttemptUpdateOne) SetAttemptNumber(i int) *PaymentAttemptUpdateOne {
	pauo.mutation.ResetAttemptNumber()
	pauo.mutation.SetAttemptNumber(i)
	return pauo
}

// SetNillableAttemptNumber sets the "attempt_number" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillableAttemptNumber(i *int) *PaymentAttemptUpdateOne {
	if i != nil {
		pauo.SetAttemptNumber(*i)
	}
	return pauo
}

// AddAttemptNumber adds i to the "attempt_number" field.
func (pauo *PaymentAttemptUpdateOne) AddAttemptNumber(i int) *PaymentAttemptUpdateOne {
	pauo.mutation.AddAttemptNumber(i)
	return pauo
}

// SetGatewayAttemptID sets the "gateway_attempt_id" field.
func (pauo *PaymentAttemptUpdateOne) SetGatewayAttemptID(s string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetGatewayAttemptID(s)
	return pauo
}

// SetNillableGatewayAttemptID sets the "gateway_attempt_id" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillableGatewayAttemptID(s *string) *PaymentAttemptUpdateOne {
	if s != nil {
		pauo.SetGatewayAttemptID(*s)
	}
	return pauo
}

// ClearGatewayAttemptID clears the value of the "gateway_attempt_id" field.
func (pauo *PaymentAttemptUpdateOne) ClearGatewayAttemptID() *PaymentAttemptUpdateOne {
	pauo.mutation.ClearGatewayAttemptID()
	return pauo
}

// SetErrorMessage sets the "error_message" field.
func (pauo *PaymentAttemptUpdateOne) SetErrorMessage(s string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetErrorMessage(s)
	return pauo
}

// SetNillableErrorMessage sets the "error_message" field if the given value is not nil.
func (pauo *PaymentAttemptUpdateOne) SetNillableErrorMessage(s *string) *PaymentAttemptUpdateOne {
	if s != nil {
		pauo.SetErrorMessage(*s)
	}
	return pauo
}

// ClearErrorMessage clears the value of the "error_message" field.
func (pauo *PaymentAttemptUpdateOne) ClearErrorMessage() *PaymentAttemptUpdateOne {
	pauo.mutation.ClearErrorMessage()
	return pauo
}

// SetMetadata sets the "metadata" field.
func (pauo *PaymentAttemptUpdateOne) SetMetadata(m map[string]string) *PaymentAttemptUpdateOne {
	pauo.mutation.SetMetadata(m)
	return pauo
}

// ClearMetadata clears the value of the "metadata" field.
func (pauo *PaymentAttemptUpdateOne) ClearMetadata() *PaymentAttemptUpdateOne {
	pauo.mutation.ClearMetadata()
	return pauo
}

// SetPayment sets the "payment" edge to the Payment entity.
func (pauo *PaymentAttemptUpdateOne) SetPayment(p *Payment) *PaymentAttemptUpdateOne {
	return pauo.SetPaymentID(p.ID)
}

// Mutation returns the PaymentAttemptMutation object of the builder.
func (pauo *PaymentAttemptUpdateOne) Mutation() *PaymentAttemptMutation {
	return pauo.mutation
}

// ClearPayment clears the "payment" edge to the Payment entity.
func (pauo *PaymentAttemptUpdateOne) ClearPayment() *PaymentAttemptUpdateOne {
	pauo.mutation.ClearPayment()
	return pauo
}

// Where appends a list predicates to the PaymentAttemptUpdate builder.
func (pauo *PaymentAttemptUpdateOne) Where(ps ...predicate.PaymentAttempt) *PaymentAttemptUpdateOne {
	pauo.mutation.Where(ps...)
	return pauo
}

// Select allows selecting one or more fields (columns) of the returned entity.
// The default is selecting all fields defined in the entity schema.
func (pauo *PaymentAttemptUpdateOne) Select(field string, fields ...string) *PaymentAttemptUpdateOne {
	pauo.fields = append([]string{field}, fields...)
	return pauo
}

// Save executes the query and returns the updated PaymentAttempt entity.
func (pauo *PaymentAttemptUpdateOne) Save(ctx context.Context) (*PaymentAttempt, error) {
	pauo.defaults()
	return withHooks(ctx, pauo.sqlSave, pauo.mutation, pauo.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (pauo *PaymentAttemptUpdateOne) SaveX(ctx context.Context) *PaymentAttempt {
	node, err := pauo.Save(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// Exec executes the query on the entity.
func (pauo *PaymentAttemptUpdateOne) Exec(ctx context.Context) error {
	_, err := pauo.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (pauo *PaymentAttemptUpdateOne) ExecX(ctx context.Context) {
	if err := pauo.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (pauo *PaymentAttemptUpdateOne) defaults() {
	if _, ok := pauo.mutation.UpdatedAt(); !ok {
		v := paymentattempt.UpdateDefaultUpdatedAt()
		pauo.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (pauo *PaymentAttemptUpdateOne) check() error {
	if v, ok := pauo.mutation.PaymentID(); ok {
		if err := paymentattempt.PaymentIDValidator(v); err != nil {
			return &ValidationError{Name: "payment_id", err: fmt.Errorf(`ent: validator failed for field "PaymentAttempt.payment_id": %w`, err)}
		}
	}
	if v, ok := pauo.mutation.PaymentStatus(); ok {
		if err := paymentattempt.PaymentStatusValidator(v); err != nil {
			return &ValidationError{Name: "payment_status", err: fmt.Errorf(`ent: validator failed for field "PaymentAttempt.payment_status": %w`, err)}
		}
	}
	if v, ok := pauo.mutation.AttemptNumber(); ok {
		if err := paymentattempt.AttemptNumberValidator(v); err != nil {
			return &ValidationError{Name: "attempt_number", err: fmt.Errorf(`ent: validator failed for field "PaymentAttempt.attempt_number": %w`, err)}
		}
	}
	if pauo.mutation.PaymentCleared() && len(pauo.mutation.PaymentIDs()) > 0 {
		return errors.New(`ent: clearing a required unique edge "PaymentAttempt.payment"`)
	}
	return nil
}

func (pauo *PaymentAttemptUpdateOne) sqlSave(ctx context.Context) (_node *PaymentAttempt, err error) {
	if err := pauo.check(); err != nil {
		return _node, err
	}
	_spec := sqlgraph.NewUpdateSpec(paymentattempt.Table, paymentattempt.Columns, sqlgraph.NewFieldSpec(paymentattempt.FieldID, field.TypeString))
	id, ok := pauo.mutation.ID()
	if !ok {
		return nil, &ValidationError{Name: "id", err: errors.New(`ent: missing "PaymentAttempt.id" for update`)}
	}
	_spec.Node.ID.Value = id
	if fields := pauo.fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, paymentattempt.FieldID)
		for _, f := range fields {
			if !paymentattempt.ValidColumn(f) {
				return nil, &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
			}
			if f != paymentattempt.FieldID {
				_spec.Node.Columns = append(_spec.Node.Columns, f)
			}
		}
	}
	if ps := pauo.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := pauo.mutation.Status(); ok {
		_spec.SetField(paymentattempt.FieldStatus, field.TypeString, value)
	}
	if value, ok := pauo.mutation.UpdatedAt(); ok {
		_spec.SetField(paymentattempt.FieldUpdatedAt, field.TypeTime, value)
	}
	if pauo.mutation.CreatedByCleared() {
		_spec.ClearField(paymentattempt.FieldCreatedBy, field.TypeString)
	}
	if value, ok := pauo.mutation.UpdatedBy(); ok {
		_spec.SetField(paymentattempt.FieldUpdatedBy, field.TypeString, value)
	}
	if pauo.mutation.UpdatedByCleared() {
		_spec.ClearField(paymentattempt.FieldUpdatedBy, field.TypeString)
	}
	if pauo.mutation.EnvironmentIDCleared() {
		_spec.ClearField(paymentattempt.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := pauo.mutation.PaymentStatus(); ok {
		_spec.SetField(paymentattempt.FieldPaymentStatus, field.TypeString, value)
	}
	if value, ok := pauo.mutation.AttemptNumber(); ok {
		_spec.SetField(paymentattempt.FieldAttemptNumber, field.TypeInt, value)
	}
	if value, ok := pauo.mutation.AddedAttemptNumber(); ok {
		_spec.AddField(paymentattempt.FieldAttemptNumber, field.TypeInt, value)
	}
	if value, ok := pauo.mutation.GatewayAttemptID(); ok {
		_spec.SetField(paymentattempt.FieldGatewayAttemptID, field.TypeString, value)
	}
	if pauo.mutation.GatewayAttemptIDCleared() {
		_spec.ClearField(paymentattempt.FieldGatewayAttemptID, field.TypeString)
	}
	if value, ok := pauo.mutation.ErrorMessage(); ok {
		_spec.SetField(paymentattempt.FieldErrorMessage, field.TypeString, value)
	}
	if pauo.mutation.ErrorMessageCleared() {
		_spec.ClearField(paymentattempt.FieldErrorMessage, field.TypeString)
	}
	if value, ok := pauo.mutation.Metadata(); ok {
		_spec.SetField(paymentattempt.FieldMetadata, field.TypeJSON, value)
	}
	if pauo.mutation.MetadataCleared() {
		_spec.ClearField(paymentattempt.FieldMetadata, field.TypeJSON)
	}
	if pauo.mutation.PaymentCleared() {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   paymentattempt.PaymentTable,
			Columns: []string{paymentattempt.PaymentColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(payment.FieldID, field.TypeString),
			},
		}
		_spec.Edges.Clear = append(_spec.Edges.Clear, edge)
	}
	if nodes := pauo.mutation.PaymentIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.M2O,
			Inverse: true,
			Table:   paymentattempt.PaymentTable,
			Columns: []string{paymentattempt.PaymentColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(payment.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_spec.Edges.Add = append(_spec.Edges.Add, edge)
	}
	_node = &PaymentAttempt{config: pauo.config}
	_spec.Assign = _node.assignValues
	_spec.ScanValues = _node.scanValues
	if err = sqlgraph.UpdateNode(ctx, pauo.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{paymentattempt.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	pauo.mutation.done = true
	return _node, nil
}
