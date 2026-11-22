package dto

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

const (
	// CollectionsColumnDefaultPageSize is the page size of a column page when limit is not sent.
	CollectionsColumnDefaultPageSize = 15
	CollectionsBoardMaxCardLimit     = 200
	// CollectionsMaxPageDepth caps how deep a list can be paged: page times limit may not exceed it.
	CollectionsMaxPageDepth = 5000
)

// CollectionsBoardRequest holds the query parameters of the collections board and of one of its columns.
type CollectionsBoardRequest struct {
	// Limit is the number of cards per column; card_limit is its older name. On the board it defaults to 0, which
	// returns counts and totals only; on a column it is the page size (default 15). Counts and totals always cover the whole column.
	Limit     int `form:"limit" json:"limit,omitempty"`
	CardLimit int `form:"card_limit" json:"card_limit,omitempty"`
	// Page is the 1-based page of a column; only the column endpoint takes it.
	Page int `form:"page" json:"page,omitempty"`
	// AsOf pins a column page to the as_of of the board call (RFC3339), on any page.
	AsOf string `form:"as_of" json:"as_of,omitempty"`
	// Q keeps receivables whose customer name, customer email or invoice number contains it, ignoring case.
	Q string `form:"q" json:"q,omitempty"`
	// Currency keeps receivables in this currency, ignoring case.
	Currency string `form:"currency" json:"currency,omitempty"`
}

const CollectionsBoardMaxQueryLength = 100

// Validate trims the filters and rejects out-of-range page sizes.
func (r *CollectionsBoardRequest) Validate() error {
	r.Q = strings.TrimSpace(r.Q)
	if runes := []rune(r.Q); len(runes) > CollectionsBoardMaxQueryLength {
		r.Q = string(runes[:CollectionsBoardMaxQueryLength])
	}
	r.Currency = strings.TrimSpace(r.Currency)
	if r.Limit < 0 || r.CardLimit < 0 || r.Limit > CollectionsBoardMaxCardLimit || r.CardLimit > CollectionsBoardMaxCardLimit {
		return ierr.NewError("limit out of range").
			WithHint("limit must be between 0 and 200.").
			Mark(ierr.ErrValidation)
	}
	if r.Page < 0 {
		return ierr.NewError("page out of range").WithHint("page starts at 1.").Mark(ierr.ErrValidation)
	}
	return validatePageDepth(r.PageNumber(), r.PageLimit())
}

// validatePageDepth refuses pages deeper than CollectionsMaxPageDepth rows.
func validatePageDepth(page, limit int) error {
	if page*limit > CollectionsMaxPageDepth {
		return ierr.NewErrorf("page too deep").
			WithHintf("page times limit cannot exceed %d. Narrow the list with q or currency.", CollectionsMaxPageDepth).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PageNumber is the 1-based page requested, 1 when unset.
func (r *CollectionsBoardRequest) PageNumber() int {
	if r.Page > 0 {
		return r.Page
	}
	return 1
}

// PageOffset is the number of rows before the requested page.
func (r *CollectionsBoardRequest) PageOffset() int {
	return (r.PageNumber() - 1) * r.PageLimit()
}

// pageLimit is limit, else card_limit, else zero.
func (r *CollectionsBoardRequest) pageLimit() int {
	if r.Limit > 0 {
		return r.Limit
	}
	return r.CardLimit
}

// BoardCardLimit is the number of cards per column on the board; zero means counts and totals only.
func (r *CollectionsBoardRequest) BoardCardLimit() int {
	return r.pageLimit()
}

// PageLimit is the page size of a column page: limit, else card_limit, else the default.
func (r *CollectionsBoardRequest) PageLimit() int {
	if limit := r.pageLimit(); limit > 0 {
		return limit
	}
	return CollectionsColumnDefaultPageSize
}

// CollectionsCurrencyTotal is a column total in one currency; totals are never summed across currencies.
type CollectionsCurrencyTotal struct {
	Currency string          `json:"currency"`
	Amount   decimal.Decimal `json:"amount" swaggertype:"string"`
}

// CollectionsBoardCard is one invoice (not_due, paid_this_week) or one customer and currency (overdue).
type CollectionsBoardCard struct {
	CustomerID   string                     `json:"customer_id"`
	CustomerName string                     `json:"customer_name"`
	Source       types.ReceivableSourceType `json:"source"`
	// ExternalID is the source's invoice id; empty when the card groups several invoices.
	ExternalID     string   `json:"external_id,omitempty"`
	InvoiceCount   int      `json:"invoice_count"`
	InvoiceNumbers []string `json:"invoice_numbers"`
	// Amount is the remaining balance (not_due, overdue) or the invoice total (paid_this_week).
	Amount      decimal.Decimal `json:"amount" swaggertype:"string"`
	Currency    string          `json:"currency"`
	DaysOverdue *int            `json:"days_overdue,omitempty"`
	DueInDays   *int            `json:"due_in_days,omitempty"`
	DueDate     *time.Time      `json:"due_date,omitempty"`
	PaidAt      *time.Time      `json:"paid_at,omitempty"`
	// ReminderID, ReminderStatus and ReminderHoldReason describe the reminder of an overdue card; ReminderStatus is pending until someone acts.
	ReminderID         string               `json:"reminder_id,omitempty"`
	ReminderStatus     types.ReminderStatus `json:"reminder_status,omitempty"`
	ReminderHoldReason string               `json:"reminder_hold_reason,omitempty"`
}

// CollectionsBoardColumn is one board column with whole-column count and totals and a capped card list.
type CollectionsBoardColumn struct {
	Key   types.CollectionsColumnKey `json:"key"`
	Title string                     `json:"title"`
	Hint  string                     `json:"hint"`
	// Count is the number of cards in the whole column.
	Count int `json:"count"`
	// InvoiceCount is the number of invoices in the whole column.
	InvoiceCount int                        `json:"invoice_count"`
	Totals       []CollectionsCurrencyTotal `json:"totals"`
	Cards        []CollectionsBoardCard     `json:"cards"`
	// HasMore is true when the column has cards beyond the ones in this response.
	HasMore bool `json:"has_more"`
}

// CollectionsBoardResponse is the collections board.
type CollectionsBoardResponse struct {
	AsOf    time.Time                `json:"as_of"`
	Columns []CollectionsBoardColumn `json:"columns"`
	// AvailableCurrencies lists the currencies of all open and recently paid receivables, before q and currency apply.
	AvailableCurrencies []string `json:"available_currencies"`
}

// CollectionsColumnPageResponse is one further page of a board column.
type CollectionsColumnPageResponse struct {
	Key     types.CollectionsColumnKey `json:"key"`
	AsOf    time.Time                  `json:"as_of"`
	Page    int                        `json:"page"`
	Cards   []CollectionsBoardCard     `json:"cards"`
	HasMore bool                       `json:"has_more"`
}

// ReminderInvoice is one overdue invoice covered by a reminder draft.
type ReminderInvoice struct {
	ID              string          `json:"id"`
	Number          string          `json:"number"`
	AmountRemaining decimal.Decimal `json:"amount_remaining" swaggertype:"string"`
	DueDate         *time.Time      `json:"due_date,omitempty"`
	DaysOverdue     int             `json:"days_overdue"`
}

// ReminderDraftResponse is a reminder draft with its decision and the live state of its invoices.
type ReminderDraftResponse struct {
	ID                string               `json:"id"`
	CustomerID        string               `json:"customer_id"`
	CustomerName      string               `json:"customer_name"`
	CustomerEmail     string               `json:"customer_email"`
	Currency          string               `json:"currency"`
	Status            types.ReminderStatus `json:"status"`
	Tone              types.ReminderTone   `json:"tone"`
	Subject           string               `json:"subject"`
	Body              string               `json:"body"`
	Amount            decimal.Decimal      `json:"amount" swaggertype:"string"`
	InvoiceCount      int                  `json:"invoice_count"`
	OldestDaysOverdue int                  `json:"oldest_days_overdue"`
	Invoices          []ReminderInvoice    `json:"invoices"`
	HoldReason        string               `json:"hold_reason,omitempty"`
	DecidedAt         *time.Time           `json:"decided_at,omitempty"`
	Edited            bool                 `json:"edited"`
	// Stale is true when the invoices changed after the text was written and the draft was not rewritten.
	Stale bool     `json:"stale"`
	Why   []string `json:"why"`
	// CreatedAt and UpdatedAt are omitted until a person acts on the reminder.
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// ReminderSummary totals all overdue invoices and counts drafts by decision, ignoring list filters.
type ReminderSummary struct {
	OverdueTotals []CollectionsCurrencyTotal `json:"overdue_totals"`
	InvoiceCount  int                        `json:"invoice_count"`
	CustomerCount int                        `json:"customer_count"`
	ToReview      int                        `json:"to_review"`
	Approved      int                        `json:"approved"`
	Held          int                        `json:"held"`
	Skipped       int                        `json:"skipped"`
}

// ListRemindersRequest filters the reminder draft list; every field is optional.
type ListRemindersRequest struct {
	Status     string `form:"status" json:"status,omitempty"`
	Tone       string `form:"tone" json:"tone,omitempty"`
	Q          string `form:"q" json:"q,omitempty"`
	CustomerID string `form:"customer_id" json:"customer_id,omitempty"`
	Currency   string `form:"currency" json:"currency,omitempty"`
	// Limit is the page size (default 15, max 100); Page is 1-based. AsOf pins the list to the clock of an earlier response.
	Limit int    `form:"limit" json:"limit,omitempty"`
	Page  int    `form:"page" json:"page,omitempty"`
	AsOf  string `form:"as_of" json:"as_of,omitempty"`
}

const (
	RemindersDefaultLimit = 15
	RemindersMaxLimit     = 100
	// RemindersMaxBulk caps the ids of one bulk approval.
	RemindersMaxBulk = 200
	// ReminderListInvoiceCap caps the invoices shown per draft in the list; the detail view returns all.
	ReminderListInvoiceCap = 25
)

// PageNumber is the 1-based page requested, 1 when unset.
func (r *ListRemindersRequest) PageNumber() int {
	if r.Page > 0 {
		return r.Page
	}
	return 1
}

// PageOffset is the number of drafts before the requested page.
func (r *ListRemindersRequest) PageOffset() int {
	return (r.PageNumber() - 1) * r.PageLimit()
}

// PageLimit is the page size to use.
func (r *ListRemindersRequest) PageLimit() int {
	if r.Limit > 0 {
		return r.Limit
	}
	return RemindersDefaultLimit
}

func (r *ListRemindersRequest) Validate() error {
	r.Q = strings.TrimSpace(r.Q)
	if runes := []rune(r.Q); len(runes) > CollectionsBoardMaxQueryLength {
		r.Q = string(runes[:CollectionsBoardMaxQueryLength])
	}
	if r.Limit < 0 || r.Limit > RemindersMaxLimit {
		return ierr.NewError("limit out of range").WithHint("limit must be between 1 and 100.").Mark(ierr.ErrValidation)
	}
	if r.Page < 0 {
		return ierr.NewError("page out of range").WithHint("page starts at 1.").Mark(ierr.ErrValidation)
	}
	if err := validatePageDepth(r.PageNumber(), r.PageLimit()); err != nil {
		return err
	}
	if r.Status != "" && !types.ReminderStatus(r.Status).Valid() {
		return ierr.NewError("invalid status").WithHint("status must be pending, approved, held or skipped.").Mark(ierr.ErrValidation)
	}
	if r.Tone != "" && !types.ReminderTone(r.Tone).Valid() {
		return ierr.NewError("invalid tone").WithHint("tone must be friendly, firm or final.").Mark(ierr.ErrValidation)
	}
	return nil
}

type ListRemindersResponse struct {
	AsOf    time.Time               `json:"as_of"`
	Summary ReminderSummary         `json:"summary"`
	Drafts  []ReminderDraftResponse `json:"drafts"`
	Page    int                     `json:"page"`
	HasMore bool                    `json:"has_more"`
}

// UpdateReminderRequest edits a pending or held draft. A tone change rewrites subject and body from that tone's template.
type UpdateReminderRequest struct {
	Subject *string `json:"subject,omitempty"`
	Body    *string `json:"body,omitempty"`
	Tone    *string `json:"tone,omitempty"`
}

func (r *UpdateReminderRequest) Validate() error {
	if r.Subject == nil && r.Body == nil && r.Tone == nil {
		return ierr.NewError("nothing to update").WithHint("Send subject, body or tone.").Mark(ierr.ErrValidation)
	}
	if r.Tone != nil && !types.ReminderTone(*r.Tone).Valid() {
		return ierr.NewError("invalid tone").WithHint("tone must be friendly, firm or final.").Mark(ierr.ErrValidation)
	}
	if r.Subject != nil && strings.TrimSpace(*r.Subject) == "" {
		return ierr.NewError("empty subject").WithHint("subject cannot be empty.").Mark(ierr.ErrValidation)
	}
	if r.Body != nil && strings.TrimSpace(*r.Body) == "" {
		return ierr.NewError("empty body").WithHint("body cannot be empty.").Mark(ierr.ErrValidation)
	}
	return nil
}

type HoldReminderRequest struct {
	Reason string `json:"reason"`
}

func (r *HoldReminderRequest) Validate() error {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" {
		return ierr.NewError("reason required").WithHint("Say why this reminder is held.").Mark(ierr.ErrValidation)
	}
	return nil
}

type BulkApproveRemindersRequest struct {
	IDs []string `json:"ids"`
}

func (r *BulkApproveRemindersRequest) Validate() error {
	if len(r.IDs) == 0 {
		return ierr.NewError("ids required").WithHint("Send at least one reminder id.").Mark(ierr.ErrValidation)
	}
	if len(r.IDs) > RemindersMaxBulk {
		return ierr.NewError("too many ids").WithHint("Approve at most 200 reminders at a time.").Mark(ierr.ErrValidation)
	}
	return nil
}

type BulkApproveRemindersResponse struct {
	Drafts []ReminderDraftResponse `json:"drafts"`
}
