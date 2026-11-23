// Package collections holds the payment reminders of overdue invoices and the queries behind the collections board.
package collections

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// Metadata is what the system wrote before a person edited the reminder.
type Metadata struct {
	OriginalTone    types.ReminderTone `json:"original_tone"`
	OriginalSubject string             `json:"original_subject"`
	OriginalBody    string             `json:"original_body"`
}

type InvoiceReminder struct {
	ID             string
	InvoiceID      string
	ReminderStatus types.ReminderStatus
	StatusReason   string
	ApprovedAt     *time.Time
	Metadata       map[string]interface{}
	Tone           types.ReminderTone
	Subject        string
	Body           string
	EnvironmentID  string
	types.BaseModel
}

// Original returns what the system wrote, read from the metadata map.
func (r *InvoiceReminder) Original() Metadata {
	var original Metadata
	if raw, err := json.Marshal(r.Metadata); err == nil {
		_ = json.Unmarshal(raw, &original)
	}
	return original
}

// Edited reports whether the text differs from what the system wrote.
func (r *InvoiceReminder) Edited() bool {
	original := r.Original()
	return r.Tone != original.OriginalTone || r.Subject != original.OriginalSubject || r.Body != original.OriginalBody
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

func (b *invoiceReminderBuilder) WithText(tone types.ReminderTone, subject, body string) *invoiceReminderBuilder {
	b.r.Tone, b.r.Subject, b.r.Body = tone, subject, body
	return b
}

// WithOriginal records what the system wrote in the metadata; the text is set separately.
func (b *invoiceReminderBuilder) WithOriginal(tone types.ReminderTone, subject, body string) *invoiceReminderBuilder {
	b.r.Metadata = map[string]interface{}{"original_tone": string(tone), "original_subject": subject, "original_body": body}
	return b
}

func (b *invoiceReminderBuilder) WithMetadata(metadata map[string]interface{}) *invoiceReminderBuilder {
	b.r.Metadata = metadata
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
