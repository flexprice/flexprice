// Package collections holds the payment reminders of overdue invoices and the queries behind the collections board.
package collections

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

type InvoiceReminder struct {
	ID              string
	InvoiceID       string
	ReminderStatus  types.ReminderStatus
	StatusReason    string
	ApprovedAt      *time.Time
	OriginalTone    types.ReminderTone
	OriginalSubject string
	OriginalBody    string
	MailTone        types.ReminderTone
	MailSubject     string
	MailBody        string
	EnvironmentID   string
	types.BaseModel
}

func (r *InvoiceReminder) Edited() bool {
	return r.MailTone != r.OriginalTone || r.MailSubject != r.OriginalSubject || r.MailBody != r.OriginalBody
}

type invoiceReminderBuilder struct {
	r *InvoiceReminder
}

// NewInvoiceReminderBuilder starts from a copy of r (or an empty reminder when r is nil).
func NewInvoiceReminderBuilder(r *InvoiceReminder) *invoiceReminderBuilder {
	if r == nil {
		return &invoiceReminderBuilder{r: &InvoiceReminder{}}
	}
	cp := *r
	return &invoiceReminderBuilder{r: &cp}
}

func (b *invoiceReminderBuilder) WithStatus(status types.ReminderStatus, reason string, approvedAt *time.Time) *invoiceReminderBuilder {
	b.r.ReminderStatus, b.r.StatusReason, b.r.ApprovedAt = status, reason, approvedAt
	return b
}

func (b *invoiceReminderBuilder) WithMail(tone types.ReminderTone, subject, body string) *invoiceReminderBuilder {
	b.r.MailTone, b.r.MailSubject, b.r.MailBody = tone, subject, body
	return b
}

// WithOriginal sets what the system wrote; the mail is set separately.
func (b *invoiceReminderBuilder) WithOriginal(tone types.ReminderTone, subject, body string) *invoiceReminderBuilder {
	b.r.OriginalTone, b.r.OriginalSubject, b.r.OriginalBody = tone, subject, body
	return b
}

func (b *invoiceReminderBuilder) Build() *InvoiceReminder {
	return b.r
}

type Row struct {
	InvoiceID       string
	InvoiceNumber   string
	CustomerID      string
	Currency        string
	AmountTotal     decimal.Decimal
	AmountRemaining decimal.Decimal
	DueDate         *time.Time
	PaidAt          *time.Time
	Reminder        *InvoiceReminder
}

type rowBuilder struct {
	r *Row
}

// NewRowBuilder starts from a copy of r (or an empty row when r is nil).
func NewRowBuilder(r *Row) *rowBuilder {
	if r == nil {
		return &rowBuilder{r: &Row{}}
	}
	cp := *r
	return &rowBuilder{r: &cp}
}

func (b *rowBuilder) WithInvoice(number, customerID, currency string) *rowBuilder {
	b.r.InvoiceNumber, b.r.CustomerID, b.r.Currency = number, customerID, currency
	return b
}

func (b *rowBuilder) WithAmounts(total, remaining decimal.Decimal) *rowBuilder {
	b.r.AmountTotal, b.r.AmountRemaining = total, remaining
	return b
}

func (b *rowBuilder) WithDates(due, paid *time.Time) *rowBuilder {
	b.r.DueDate, b.r.PaidAt = due, paid
	return b
}

func (b *rowBuilder) WithReminder(reminder *InvoiceReminder) *rowBuilder {
	b.r.Reminder = reminder
	return b
}

func (b *rowBuilder) Build() *Row {
	return b.r
}

type InvoiceReminderRepository interface {
	GetRows(ctx context.Context, asOf time.Time, invoiceIDs []string) ([]Row, error)
	Create(ctx context.Context, reminder *InvoiceReminder) error
	CreateMany(ctx context.Context, reminders []*InvoiceReminder) ([]string, error)
	Update(ctx context.Context, reminder *InvoiceReminder, previous *InvoiceReminder) error
}
