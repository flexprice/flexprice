// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/shopspring/decimal"
)

// InvoiceCreate is the builder for creating a Invoice entity.
type InvoiceCreate struct {
	config
	mutation *InvoiceMutation
	hooks    []Hook
}

// SetTenantID sets the "tenant_id" field.
func (ic *InvoiceCreate) SetTenantID(s string) *InvoiceCreate {
	ic.mutation.SetTenantID(s)
	return ic
}

// SetStatus sets the "status" field.
func (ic *InvoiceCreate) SetStatus(s string) *InvoiceCreate {
	ic.mutation.SetStatus(s)
	return ic
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableStatus(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetStatus(*s)
	}
	return ic
}

// SetCreatedAt sets the "created_at" field.
func (ic *InvoiceCreate) SetCreatedAt(t time.Time) *InvoiceCreate {
	ic.mutation.SetCreatedAt(t)
	return ic
}

// SetNillableCreatedAt sets the "created_at" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableCreatedAt(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetCreatedAt(*t)
	}
	return ic
}

// SetUpdatedAt sets the "updated_at" field.
func (ic *InvoiceCreate) SetUpdatedAt(t time.Time) *InvoiceCreate {
	ic.mutation.SetUpdatedAt(t)
	return ic
}

// SetNillableUpdatedAt sets the "updated_at" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableUpdatedAt(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetUpdatedAt(*t)
	}
	return ic
}

// SetCreatedBy sets the "created_by" field.
func (ic *InvoiceCreate) SetCreatedBy(s string) *InvoiceCreate {
	ic.mutation.SetCreatedBy(s)
	return ic
}

// SetNillableCreatedBy sets the "created_by" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableCreatedBy(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetCreatedBy(*s)
	}
	return ic
}

// SetUpdatedBy sets the "updated_by" field.
func (ic *InvoiceCreate) SetUpdatedBy(s string) *InvoiceCreate {
	ic.mutation.SetUpdatedBy(s)
	return ic
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableUpdatedBy(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetUpdatedBy(*s)
	}
	return ic
}

// SetEnvironmentID sets the "environment_id" field.
func (ic *InvoiceCreate) SetEnvironmentID(s string) *InvoiceCreate {
	ic.mutation.SetEnvironmentID(s)
	return ic
}

// SetNillableEnvironmentID sets the "environment_id" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableEnvironmentID(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetEnvironmentID(*s)
	}
	return ic
}

// SetCustomerID sets the "customer_id" field.
func (ic *InvoiceCreate) SetCustomerID(s string) *InvoiceCreate {
	ic.mutation.SetCustomerID(s)
	return ic
}

// SetSubscriptionID sets the "subscription_id" field.
func (ic *InvoiceCreate) SetSubscriptionID(s string) *InvoiceCreate {
	ic.mutation.SetSubscriptionID(s)
	return ic
}

// SetNillableSubscriptionID sets the "subscription_id" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableSubscriptionID(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetSubscriptionID(*s)
	}
	return ic
}

// SetInvoiceType sets the "invoice_type" field.
func (ic *InvoiceCreate) SetInvoiceType(s string) *InvoiceCreate {
	ic.mutation.SetInvoiceType(s)
	return ic
}

// SetInvoiceStatus sets the "invoice_status" field.
func (ic *InvoiceCreate) SetInvoiceStatus(s string) *InvoiceCreate {
	ic.mutation.SetInvoiceStatus(s)
	return ic
}

// SetNillableInvoiceStatus sets the "invoice_status" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableInvoiceStatus(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetInvoiceStatus(*s)
	}
	return ic
}

// SetPaymentStatus sets the "payment_status" field.
func (ic *InvoiceCreate) SetPaymentStatus(s string) *InvoiceCreate {
	ic.mutation.SetPaymentStatus(s)
	return ic
}

// SetNillablePaymentStatus sets the "payment_status" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillablePaymentStatus(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetPaymentStatus(*s)
	}
	return ic
}

// SetCurrency sets the "currency" field.
func (ic *InvoiceCreate) SetCurrency(s string) *InvoiceCreate {
	ic.mutation.SetCurrency(s)
	return ic
}

// SetAmountDue sets the "amount_due" field.
func (ic *InvoiceCreate) SetAmountDue(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetAmountDue(d)
	return ic
}

// SetNillableAmountDue sets the "amount_due" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableAmountDue(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetAmountDue(*d)
	}
	return ic
}

// SetAmountPaid sets the "amount_paid" field.
func (ic *InvoiceCreate) SetAmountPaid(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetAmountPaid(d)
	return ic
}

// SetNillableAmountPaid sets the "amount_paid" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableAmountPaid(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetAmountPaid(*d)
	}
	return ic
}

// SetAmountRemaining sets the "amount_remaining" field.
func (ic *InvoiceCreate) SetAmountRemaining(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetAmountRemaining(d)
	return ic
}

// SetNillableAmountRemaining sets the "amount_remaining" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableAmountRemaining(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetAmountRemaining(*d)
	}
	return ic
}

// SetSubtotal sets the "subtotal" field.
func (ic *InvoiceCreate) SetSubtotal(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetSubtotal(d)
	return ic
}

// SetNillableSubtotal sets the "subtotal" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableSubtotal(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetSubtotal(*d)
	}
	return ic
}

// SetAdjustmentAmount sets the "adjustment_amount" field.
func (ic *InvoiceCreate) SetAdjustmentAmount(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetAdjustmentAmount(d)
	return ic
}

// SetNillableAdjustmentAmount sets the "adjustment_amount" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableAdjustmentAmount(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetAdjustmentAmount(*d)
	}
	return ic
}

// SetRefundedAmount sets the "refunded_amount" field.
func (ic *InvoiceCreate) SetRefundedAmount(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetRefundedAmount(d)
	return ic
}

// SetNillableRefundedAmount sets the "refunded_amount" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableRefundedAmount(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetRefundedAmount(*d)
	}
	return ic
}

// SetTotal sets the "total" field.
func (ic *InvoiceCreate) SetTotal(d decimal.Decimal) *InvoiceCreate {
	ic.mutation.SetTotal(d)
	return ic
}

// SetNillableTotal sets the "total" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableTotal(d *decimal.Decimal) *InvoiceCreate {
	if d != nil {
		ic.SetTotal(*d)
	}
	return ic
}

// SetDescription sets the "description" field.
func (ic *InvoiceCreate) SetDescription(s string) *InvoiceCreate {
	ic.mutation.SetDescription(s)
	return ic
}

// SetNillableDescription sets the "description" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableDescription(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetDescription(*s)
	}
	return ic
}

// SetDueDate sets the "due_date" field.
func (ic *InvoiceCreate) SetDueDate(t time.Time) *InvoiceCreate {
	ic.mutation.SetDueDate(t)
	return ic
}

// SetNillableDueDate sets the "due_date" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableDueDate(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetDueDate(*t)
	}
	return ic
}

// SetPaidAt sets the "paid_at" field.
func (ic *InvoiceCreate) SetPaidAt(t time.Time) *InvoiceCreate {
	ic.mutation.SetPaidAt(t)
	return ic
}

// SetNillablePaidAt sets the "paid_at" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillablePaidAt(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetPaidAt(*t)
	}
	return ic
}

// SetVoidedAt sets the "voided_at" field.
func (ic *InvoiceCreate) SetVoidedAt(t time.Time) *InvoiceCreate {
	ic.mutation.SetVoidedAt(t)
	return ic
}

// SetNillableVoidedAt sets the "voided_at" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableVoidedAt(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetVoidedAt(*t)
	}
	return ic
}

// SetFinalizedAt sets the "finalized_at" field.
func (ic *InvoiceCreate) SetFinalizedAt(t time.Time) *InvoiceCreate {
	ic.mutation.SetFinalizedAt(t)
	return ic
}

// SetNillableFinalizedAt sets the "finalized_at" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableFinalizedAt(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetFinalizedAt(*t)
	}
	return ic
}

// SetBillingPeriod sets the "billing_period" field.
func (ic *InvoiceCreate) SetBillingPeriod(s string) *InvoiceCreate {
	ic.mutation.SetBillingPeriod(s)
	return ic
}

// SetNillableBillingPeriod sets the "billing_period" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableBillingPeriod(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetBillingPeriod(*s)
	}
	return ic
}

// SetPeriodStart sets the "period_start" field.
func (ic *InvoiceCreate) SetPeriodStart(t time.Time) *InvoiceCreate {
	ic.mutation.SetPeriodStart(t)
	return ic
}

// SetNillablePeriodStart sets the "period_start" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillablePeriodStart(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetPeriodStart(*t)
	}
	return ic
}

// SetPeriodEnd sets the "period_end" field.
func (ic *InvoiceCreate) SetPeriodEnd(t time.Time) *InvoiceCreate {
	ic.mutation.SetPeriodEnd(t)
	return ic
}

// SetNillablePeriodEnd sets the "period_end" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillablePeriodEnd(t *time.Time) *InvoiceCreate {
	if t != nil {
		ic.SetPeriodEnd(*t)
	}
	return ic
}

// SetInvoicePdfURL sets the "invoice_pdf_url" field.
func (ic *InvoiceCreate) SetInvoicePdfURL(s string) *InvoiceCreate {
	ic.mutation.SetInvoicePdfURL(s)
	return ic
}

// SetNillableInvoicePdfURL sets the "invoice_pdf_url" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableInvoicePdfURL(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetInvoicePdfURL(*s)
	}
	return ic
}

// SetBillingReason sets the "billing_reason" field.
func (ic *InvoiceCreate) SetBillingReason(s string) *InvoiceCreate {
	ic.mutation.SetBillingReason(s)
	return ic
}

// SetNillableBillingReason sets the "billing_reason" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableBillingReason(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetBillingReason(*s)
	}
	return ic
}

// SetMetadata sets the "metadata" field.
func (ic *InvoiceCreate) SetMetadata(m map[string]string) *InvoiceCreate {
	ic.mutation.SetMetadata(m)
	return ic
}

// SetVersion sets the "version" field.
func (ic *InvoiceCreate) SetVersion(i int) *InvoiceCreate {
	ic.mutation.SetVersion(i)
	return ic
}

// SetNillableVersion sets the "version" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableVersion(i *int) *InvoiceCreate {
	if i != nil {
		ic.SetVersion(*i)
	}
	return ic
}

// SetInvoiceNumber sets the "invoice_number" field.
func (ic *InvoiceCreate) SetInvoiceNumber(s string) *InvoiceCreate {
	ic.mutation.SetInvoiceNumber(s)
	return ic
}

// SetNillableInvoiceNumber sets the "invoice_number" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableInvoiceNumber(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetInvoiceNumber(*s)
	}
	return ic
}

// SetBillingSequence sets the "billing_sequence" field.
func (ic *InvoiceCreate) SetBillingSequence(i int) *InvoiceCreate {
	ic.mutation.SetBillingSequence(i)
	return ic
}

// SetNillableBillingSequence sets the "billing_sequence" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableBillingSequence(i *int) *InvoiceCreate {
	if i != nil {
		ic.SetBillingSequence(*i)
	}
	return ic
}

// SetIdempotencyKey sets the "idempotency_key" field.
func (ic *InvoiceCreate) SetIdempotencyKey(s string) *InvoiceCreate {
	ic.mutation.SetIdempotencyKey(s)
	return ic
}

// SetNillableIdempotencyKey sets the "idempotency_key" field if the given value is not nil.
func (ic *InvoiceCreate) SetNillableIdempotencyKey(s *string) *InvoiceCreate {
	if s != nil {
		ic.SetIdempotencyKey(*s)
	}
	return ic
}

// SetID sets the "id" field.
func (ic *InvoiceCreate) SetID(s string) *InvoiceCreate {
	ic.mutation.SetID(s)
	return ic
}

// AddLineItemIDs adds the "line_items" edge to the InvoiceLineItem entity by IDs.
func (ic *InvoiceCreate) AddLineItemIDs(ids ...string) *InvoiceCreate {
	ic.mutation.AddLineItemIDs(ids...)
	return ic
}

// AddLineItems adds the "line_items" edges to the InvoiceLineItem entity.
func (ic *InvoiceCreate) AddLineItems(i ...*InvoiceLineItem) *InvoiceCreate {
	ids := make([]string, len(i))
	for j := range i {
		ids[j] = i[j].ID
	}
	return ic.AddLineItemIDs(ids...)
}

// Mutation returns the InvoiceMutation object of the builder.
func (ic *InvoiceCreate) Mutation() *InvoiceMutation {
	return ic.mutation
}

// Save creates the Invoice in the database.
func (ic *InvoiceCreate) Save(ctx context.Context) (*Invoice, error) {
	ic.defaults()
	return withHooks(ctx, ic.sqlSave, ic.mutation, ic.hooks)
}

// SaveX calls Save and panics if Save returns an error.
func (ic *InvoiceCreate) SaveX(ctx context.Context) *Invoice {
	v, err := ic.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (ic *InvoiceCreate) Exec(ctx context.Context) error {
	_, err := ic.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (ic *InvoiceCreate) ExecX(ctx context.Context) {
	if err := ic.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (ic *InvoiceCreate) defaults() {
	if _, ok := ic.mutation.Status(); !ok {
		v := invoice.DefaultStatus
		ic.mutation.SetStatus(v)
	}
	if _, ok := ic.mutation.CreatedAt(); !ok {
		v := invoice.DefaultCreatedAt()
		ic.mutation.SetCreatedAt(v)
	}
	if _, ok := ic.mutation.UpdatedAt(); !ok {
		v := invoice.DefaultUpdatedAt()
		ic.mutation.SetUpdatedAt(v)
	}
	if _, ok := ic.mutation.EnvironmentID(); !ok {
		v := invoice.DefaultEnvironmentID
		ic.mutation.SetEnvironmentID(v)
	}
	if _, ok := ic.mutation.InvoiceStatus(); !ok {
		v := invoice.DefaultInvoiceStatus
		ic.mutation.SetInvoiceStatus(v)
	}
	if _, ok := ic.mutation.PaymentStatus(); !ok {
		v := invoice.DefaultPaymentStatus
		ic.mutation.SetPaymentStatus(v)
	}
	if _, ok := ic.mutation.AmountDue(); !ok {
		v := invoice.DefaultAmountDue
		ic.mutation.SetAmountDue(v)
	}
	if _, ok := ic.mutation.AmountPaid(); !ok {
		v := invoice.DefaultAmountPaid
		ic.mutation.SetAmountPaid(v)
	}
	if _, ok := ic.mutation.AmountRemaining(); !ok {
		v := invoice.DefaultAmountRemaining
		ic.mutation.SetAmountRemaining(v)
	}
	if _, ok := ic.mutation.Subtotal(); !ok {
		v := invoice.DefaultSubtotal
		ic.mutation.SetSubtotal(v)
	}
	if _, ok := ic.mutation.AdjustmentAmount(); !ok {
		v := invoice.DefaultAdjustmentAmount
		ic.mutation.SetAdjustmentAmount(v)
	}
	if _, ok := ic.mutation.RefundedAmount(); !ok {
		v := invoice.DefaultRefundedAmount
		ic.mutation.SetRefundedAmount(v)
	}
	if _, ok := ic.mutation.Total(); !ok {
		v := invoice.DefaultTotal
		ic.mutation.SetTotal(v)
	}
	if _, ok := ic.mutation.Version(); !ok {
		v := invoice.DefaultVersion
		ic.mutation.SetVersion(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (ic *InvoiceCreate) check() error {
	if _, ok := ic.mutation.TenantID(); !ok {
		return &ValidationError{Name: "tenant_id", err: errors.New(`ent: missing required field "Invoice.tenant_id"`)}
	}
	if v, ok := ic.mutation.TenantID(); ok {
		if err := invoice.TenantIDValidator(v); err != nil {
			return &ValidationError{Name: "tenant_id", err: fmt.Errorf(`ent: validator failed for field "Invoice.tenant_id": %w`, err)}
		}
	}
	if _, ok := ic.mutation.Status(); !ok {
		return &ValidationError{Name: "status", err: errors.New(`ent: missing required field "Invoice.status"`)}
	}
	if _, ok := ic.mutation.CreatedAt(); !ok {
		return &ValidationError{Name: "created_at", err: errors.New(`ent: missing required field "Invoice.created_at"`)}
	}
	if _, ok := ic.mutation.UpdatedAt(); !ok {
		return &ValidationError{Name: "updated_at", err: errors.New(`ent: missing required field "Invoice.updated_at"`)}
	}
	if _, ok := ic.mutation.CustomerID(); !ok {
		return &ValidationError{Name: "customer_id", err: errors.New(`ent: missing required field "Invoice.customer_id"`)}
	}
	if v, ok := ic.mutation.CustomerID(); ok {
		if err := invoice.CustomerIDValidator(v); err != nil {
			return &ValidationError{Name: "customer_id", err: fmt.Errorf(`ent: validator failed for field "Invoice.customer_id": %w`, err)}
		}
	}
	if _, ok := ic.mutation.InvoiceType(); !ok {
		return &ValidationError{Name: "invoice_type", err: errors.New(`ent: missing required field "Invoice.invoice_type"`)}
	}
	if v, ok := ic.mutation.InvoiceType(); ok {
		if err := invoice.InvoiceTypeValidator(v); err != nil {
			return &ValidationError{Name: "invoice_type", err: fmt.Errorf(`ent: validator failed for field "Invoice.invoice_type": %w`, err)}
		}
	}
	if _, ok := ic.mutation.InvoiceStatus(); !ok {
		return &ValidationError{Name: "invoice_status", err: errors.New(`ent: missing required field "Invoice.invoice_status"`)}
	}
	if _, ok := ic.mutation.PaymentStatus(); !ok {
		return &ValidationError{Name: "payment_status", err: errors.New(`ent: missing required field "Invoice.payment_status"`)}
	}
	if _, ok := ic.mutation.Currency(); !ok {
		return &ValidationError{Name: "currency", err: errors.New(`ent: missing required field "Invoice.currency"`)}
	}
	if v, ok := ic.mutation.Currency(); ok {
		if err := invoice.CurrencyValidator(v); err != nil {
			return &ValidationError{Name: "currency", err: fmt.Errorf(`ent: validator failed for field "Invoice.currency": %w`, err)}
		}
	}
	if _, ok := ic.mutation.AmountDue(); !ok {
		return &ValidationError{Name: "amount_due", err: errors.New(`ent: missing required field "Invoice.amount_due"`)}
	}
	if _, ok := ic.mutation.AmountPaid(); !ok {
		return &ValidationError{Name: "amount_paid", err: errors.New(`ent: missing required field "Invoice.amount_paid"`)}
	}
	if _, ok := ic.mutation.AmountRemaining(); !ok {
		return &ValidationError{Name: "amount_remaining", err: errors.New(`ent: missing required field "Invoice.amount_remaining"`)}
	}
	if _, ok := ic.mutation.Version(); !ok {
		return &ValidationError{Name: "version", err: errors.New(`ent: missing required field "Invoice.version"`)}
	}
	return nil
}

func (ic *InvoiceCreate) sqlSave(ctx context.Context) (*Invoice, error) {
	if err := ic.check(); err != nil {
		return nil, err
	}
	_node, _spec := ic.createSpec()
	if err := sqlgraph.CreateNode(ctx, ic.driver, _spec); err != nil {
		if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	if _spec.ID.Value != nil {
		if id, ok := _spec.ID.Value.(string); ok {
			_node.ID = id
		} else {
			return nil, fmt.Errorf("unexpected Invoice.ID type: %T", _spec.ID.Value)
		}
	}
	ic.mutation.id = &_node.ID
	ic.mutation.done = true
	return _node, nil
}

func (ic *InvoiceCreate) createSpec() (*Invoice, *sqlgraph.CreateSpec) {
	var (
		_node = &Invoice{config: ic.config}
		_spec = sqlgraph.NewCreateSpec(invoice.Table, sqlgraph.NewFieldSpec(invoice.FieldID, field.TypeString))
	)
	if id, ok := ic.mutation.ID(); ok {
		_node.ID = id
		_spec.ID.Value = id
	}
	if value, ok := ic.mutation.TenantID(); ok {
		_spec.SetField(invoice.FieldTenantID, field.TypeString, value)
		_node.TenantID = value
	}
	if value, ok := ic.mutation.Status(); ok {
		_spec.SetField(invoice.FieldStatus, field.TypeString, value)
		_node.Status = value
	}
	if value, ok := ic.mutation.CreatedAt(); ok {
		_spec.SetField(invoice.FieldCreatedAt, field.TypeTime, value)
		_node.CreatedAt = value
	}
	if value, ok := ic.mutation.UpdatedAt(); ok {
		_spec.SetField(invoice.FieldUpdatedAt, field.TypeTime, value)
		_node.UpdatedAt = value
	}
	if value, ok := ic.mutation.CreatedBy(); ok {
		_spec.SetField(invoice.FieldCreatedBy, field.TypeString, value)
		_node.CreatedBy = value
	}
	if value, ok := ic.mutation.UpdatedBy(); ok {
		_spec.SetField(invoice.FieldUpdatedBy, field.TypeString, value)
		_node.UpdatedBy = value
	}
	if value, ok := ic.mutation.EnvironmentID(); ok {
		_spec.SetField(invoice.FieldEnvironmentID, field.TypeString, value)
		_node.EnvironmentID = value
	}
	if value, ok := ic.mutation.CustomerID(); ok {
		_spec.SetField(invoice.FieldCustomerID, field.TypeString, value)
		_node.CustomerID = value
	}
	if value, ok := ic.mutation.SubscriptionID(); ok {
		_spec.SetField(invoice.FieldSubscriptionID, field.TypeString, value)
		_node.SubscriptionID = &value
	}
	if value, ok := ic.mutation.InvoiceType(); ok {
		_spec.SetField(invoice.FieldInvoiceType, field.TypeString, value)
		_node.InvoiceType = value
	}
	if value, ok := ic.mutation.InvoiceStatus(); ok {
		_spec.SetField(invoice.FieldInvoiceStatus, field.TypeString, value)
		_node.InvoiceStatus = value
	}
	if value, ok := ic.mutation.PaymentStatus(); ok {
		_spec.SetField(invoice.FieldPaymentStatus, field.TypeString, value)
		_node.PaymentStatus = value
	}
	if value, ok := ic.mutation.Currency(); ok {
		_spec.SetField(invoice.FieldCurrency, field.TypeString, value)
		_node.Currency = value
	}
	if value, ok := ic.mutation.AmountDue(); ok {
		_spec.SetField(invoice.FieldAmountDue, field.TypeOther, value)
		_node.AmountDue = value
	}
	if value, ok := ic.mutation.AmountPaid(); ok {
		_spec.SetField(invoice.FieldAmountPaid, field.TypeOther, value)
		_node.AmountPaid = value
	}
	if value, ok := ic.mutation.AmountRemaining(); ok {
		_spec.SetField(invoice.FieldAmountRemaining, field.TypeOther, value)
		_node.AmountRemaining = value
	}
	if value, ok := ic.mutation.Subtotal(); ok {
		_spec.SetField(invoice.FieldSubtotal, field.TypeOther, value)
		_node.Subtotal = value
	}
	if value, ok := ic.mutation.AdjustmentAmount(); ok {
		_spec.SetField(invoice.FieldAdjustmentAmount, field.TypeOther, value)
		_node.AdjustmentAmount = value
	}
	if value, ok := ic.mutation.RefundedAmount(); ok {
		_spec.SetField(invoice.FieldRefundedAmount, field.TypeOther, value)
		_node.RefundedAmount = value
	}
	if value, ok := ic.mutation.Total(); ok {
		_spec.SetField(invoice.FieldTotal, field.TypeOther, value)
		_node.Total = value
	}
	if value, ok := ic.mutation.Description(); ok {
		_spec.SetField(invoice.FieldDescription, field.TypeString, value)
		_node.Description = value
	}
	if value, ok := ic.mutation.DueDate(); ok {
		_spec.SetField(invoice.FieldDueDate, field.TypeTime, value)
		_node.DueDate = &value
	}
	if value, ok := ic.mutation.PaidAt(); ok {
		_spec.SetField(invoice.FieldPaidAt, field.TypeTime, value)
		_node.PaidAt = &value
	}
	if value, ok := ic.mutation.VoidedAt(); ok {
		_spec.SetField(invoice.FieldVoidedAt, field.TypeTime, value)
		_node.VoidedAt = &value
	}
	if value, ok := ic.mutation.FinalizedAt(); ok {
		_spec.SetField(invoice.FieldFinalizedAt, field.TypeTime, value)
		_node.FinalizedAt = &value
	}
	if value, ok := ic.mutation.BillingPeriod(); ok {
		_spec.SetField(invoice.FieldBillingPeriod, field.TypeString, value)
		_node.BillingPeriod = &value
	}
	if value, ok := ic.mutation.PeriodStart(); ok {
		_spec.SetField(invoice.FieldPeriodStart, field.TypeTime, value)
		_node.PeriodStart = &value
	}
	if value, ok := ic.mutation.PeriodEnd(); ok {
		_spec.SetField(invoice.FieldPeriodEnd, field.TypeTime, value)
		_node.PeriodEnd = &value
	}
	if value, ok := ic.mutation.InvoicePdfURL(); ok {
		_spec.SetField(invoice.FieldInvoicePdfURL, field.TypeString, value)
		_node.InvoicePdfURL = &value
	}
	if value, ok := ic.mutation.BillingReason(); ok {
		_spec.SetField(invoice.FieldBillingReason, field.TypeString, value)
		_node.BillingReason = value
	}
	if value, ok := ic.mutation.Metadata(); ok {
		_spec.SetField(invoice.FieldMetadata, field.TypeJSON, value)
		_node.Metadata = value
	}
	if value, ok := ic.mutation.Version(); ok {
		_spec.SetField(invoice.FieldVersion, field.TypeInt, value)
		_node.Version = value
	}
	if value, ok := ic.mutation.InvoiceNumber(); ok {
		_spec.SetField(invoice.FieldInvoiceNumber, field.TypeString, value)
		_node.InvoiceNumber = &value
	}
	if value, ok := ic.mutation.BillingSequence(); ok {
		_spec.SetField(invoice.FieldBillingSequence, field.TypeInt, value)
		_node.BillingSequence = &value
	}
	if value, ok := ic.mutation.IdempotencyKey(); ok {
		_spec.SetField(invoice.FieldIdempotencyKey, field.TypeString, value)
		_node.IdempotencyKey = &value
	}
	if nodes := ic.mutation.LineItemsIDs(); len(nodes) > 0 {
		edge := &sqlgraph.EdgeSpec{
			Rel:     sqlgraph.O2M,
			Inverse: false,
			Table:   invoice.LineItemsTable,
			Columns: []string{invoice.LineItemsColumn},
			Bidi:    false,
			Target: &sqlgraph.EdgeTarget{
				IDSpec: sqlgraph.NewFieldSpec(invoicelineitem.FieldID, field.TypeString),
			},
		}
		for _, k := range nodes {
			edge.Target.Nodes = append(edge.Target.Nodes, k)
		}
		_spec.Edges = append(_spec.Edges, edge)
	}
	return _node, _spec
}

// InvoiceCreateBulk is the builder for creating many Invoice entities in bulk.
type InvoiceCreateBulk struct {
	config
	err      error
	builders []*InvoiceCreate
}

// Save creates the Invoice entities in the database.
func (icb *InvoiceCreateBulk) Save(ctx context.Context) ([]*Invoice, error) {
	if icb.err != nil {
		return nil, icb.err
	}
	specs := make([]*sqlgraph.CreateSpec, len(icb.builders))
	nodes := make([]*Invoice, len(icb.builders))
	mutators := make([]Mutator, len(icb.builders))
	for i := range icb.builders {
		func(i int, root context.Context) {
			builder := icb.builders[i]
			builder.defaults()
			var mut Mutator = MutateFunc(func(ctx context.Context, m Mutation) (Value, error) {
				mutation, ok := m.(*InvoiceMutation)
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
					_, err = mutators[i+1].Mutate(root, icb.builders[i+1].mutation)
				} else {
					spec := &sqlgraph.BatchCreateSpec{Nodes: specs}
					// Invoke the actual operation on the latest mutation in the chain.
					if err = sqlgraph.BatchCreate(ctx, icb.driver, spec); err != nil {
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
		if _, err := mutators[0].Mutate(ctx, icb.builders[0].mutation); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// SaveX is like Save, but panics if an error occurs.
func (icb *InvoiceCreateBulk) SaveX(ctx context.Context) []*Invoice {
	v, err := icb.Save(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

// Exec executes the query.
func (icb *InvoiceCreateBulk) Exec(ctx context.Context) error {
	_, err := icb.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (icb *InvoiceCreateBulk) ExecX(ctx context.Context) {
	if err := icb.Exec(ctx); err != nil {
		panic(err)
	}
}
