package collections

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// ReminderDecision records what a person did with the reminder of one customer's overdue invoices in one currency:
// a decision, edited text or a chosen tone. Reminders nobody acted on have no decision and are rendered on read.
type ReminderDecision struct {
	ID         string
	CustomerID string
	Currency   string
	Status     types.ReminderStatus
	HoldReason string
	DecidedAt  *time.Time
	// ToneOverride, SubjectOverride and BodyOverride are empty when the person did not set them.
	ToneOverride    types.ReminderTone
	SubjectOverride string
	BodyOverride    string
	// InvoiceIDs and InvoiceSetHash describe the invoices the decision was made on.
	InvoiceIDs     []string
	InvoiceSetHash string
	// Frozen* is the exact text approved; empty unless the status is approved.
	FrozenSubject string
	FrozenBody    string
	FrozenTone    types.ReminderTone
	// Version is the optimistic lock: updates and deletes succeed only against the version they read.
	Version       int
	EnvironmentID string
	types.BaseModel
}

// Edited reports whether the person changed the subject or body.
func (d *ReminderDecision) Edited() bool {
	return d.SubjectOverride != "" || d.BodyOverride != ""
}

// ReminderDecisionRepository stores reminder decisions of the tenant and environment in ctx.
type ReminderDecisionRepository interface {
	// Create fails with ErrAlreadyExists when the customer and currency already have a decision.
	Create(ctx context.Context, decision *ReminderDecision) error
	// ListByKeys returns the decisions of the named customers and currencies.
	ListByKeys(ctx context.Context, keys []GroupKey) ([]*ReminderDecision, error)
	// Update writes the decision against the version it carries and fails with ErrVersionConflict when it changed since.
	Update(ctx context.Context, decision *ReminderDecision) error
	// Delete removes the decision from reads (soft delete) with the same version check as Update.
	Delete(ctx context.Context, decision *ReminderDecision) error
}
