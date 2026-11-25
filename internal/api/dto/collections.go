package dto

import (
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/collections"
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
	// Limit is the number of cards per column. On the board it defaults to 0, which returns counts and totals only;
	// on a column it is the page size (default 15). Counts and totals always cover the whole column.
	Limit int `form:"limit" json:"limit,omitempty"`
	// Page is the 1-based page of a column; only the column endpoint takes it.
	Page int `form:"page" json:"page,omitempty"`
	// AsOf pins a column page to the as_of of the board call (RFC3339), on any page.
	AsOf *time.Time `form:"as_of" json:"as_of,omitempty"`
	// Q keeps receivables whose customer name, customer email or invoice number contains it, ignoring case.
	Q string `form:"q" json:"q,omitempty"`
	// Currency keeps receivables in this currency, ignoring case.
	Currency string `form:"currency" json:"currency,omitempty"`
	// The date range narrows open columns by due date and paid by paid date; without it open columns are unbounded and
	// paid is the last 7 days.
	CollectionsDateFilter
}

// CollectionsMaxRangeYears is the longest From to To span the board accepts.
const CollectionsMaxRangeYears = 5

// CollectionsRange is the date range a board response was read with; a missing side is unbounded.
type CollectionsRange struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

const collectionsDateLayout = "2006-01-02"

// CollectionsDateFilter is the optional date range of the board and the reminder list: From and To are UTC days
// (YYYY-MM-DD), both inclusive. It bounds the due date of open invoices and, on the board, the paid date of paid ones.
type CollectionsDateFilter struct {
	From *time.Time `form:"from" time_format:"2006-01-02" time_utc:"1" json:"from,omitempty"`
	To   *time.Time `form:"to" time_format:"2006-01-02" time_utc:"1" json:"to,omitempty"`
}

// DateRange echoes the range, nil when there is none.
func (f CollectionsDateFilter) DateRange() *CollectionsRange {
	if f.From == nil && f.To == nil {
		return nil
	}

	rng := &CollectionsRange{}
	if f.From != nil {
		from := f.From.UTC().Format(collectionsDateLayout)
		rng.From = &from
	}
	if f.To != nil {
		to := f.To.UTC().Format(collectionsDateLayout)
		rng.To = &to
	}

	return rng
}

// ToDomain turns the inclusive UTC days into the half-open range the repository reads; nil when there is none.
func (f CollectionsDateFilter) ToDomain() *collections.DateRange {
	if f.From == nil && f.To == nil {
		return nil
	}

	rng := &collections.DateRange{From: f.From}
	if f.To != nil {
		to := f.To.AddDate(0, 0, 1)
		rng.To = &to
	}

	return rng
}

// startOfDay moves a bound to the start of its UTC day; an empty value (as in ?to=) is no bound.
func (CollectionsDateFilter) startOfDay(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}

	day := types.UTCDay(*t)
	return &day
}

// validateRange moves both bounds to UTC days, then rejects a reversed range or one longer than the maximum.
func (f *CollectionsDateFilter) validateRange() error {
	f.From, f.To = f.startOfDay(f.From), f.startOfDay(f.To)
	if f.From == nil || f.To == nil {
		return nil
	}

	if f.To.Before(*f.From) {
		return ierr.NewError("from is after to").WithHint("from must be on or before to.").Mark(ierr.ErrValidation)
	}

	if f.To.After(f.From.AddDate(CollectionsMaxRangeYears, 0, 0)) {
		return ierr.NewErrorf("range too long").
			WithHintf("The range between from and to cannot be longer than %d years.", CollectionsMaxRangeYears).
			Mark(ierr.ErrValidation)
	}

	return nil
}

const CollectionsBoardMaxQueryLength = 100

// Validate trims the filters and rejects out-of-range page sizes.
func (r *CollectionsBoardRequest) Validate() error {
	r.Q = strings.TrimSpace(r.Q)
	if runes := []rune(r.Q); len(runes) > CollectionsBoardMaxQueryLength {
		r.Q = string(runes[:CollectionsBoardMaxQueryLength])
	}
	r.Currency = strings.TrimSpace(r.Currency)
	if r.Limit < 0 || r.Limit > CollectionsBoardMaxCardLimit {
		return ierr.NewError("limit out of range").
			WithHint("limit must be between 0 and 200.").
			Mark(ierr.ErrValidation)
	}
	if r.Page < 0 {
		return ierr.NewError("page out of range").WithHint("page starts at 1.").Mark(ierr.ErrValidation)
	}
	if err := r.validateRange(); err != nil {
		return err
	}

	return validatePageDepth(r.PageNumber(), r.PageLimit())
}

// validatePageDepth refuses pages deeper than CollectionsMaxPageDepth rows.
func validatePageDepth(page, limit int) error {
	if limit > 0 && page > CollectionsMaxPageDepth/limit {
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

// BoardCardLimit is the number of cards per column on the board; zero means counts and totals only.
func (r *CollectionsBoardRequest) BoardCardLimit() int {
	return r.Limit
}

// PageLimit is the page size of a column page: limit, else the default.
func (r *CollectionsBoardRequest) PageLimit() int {
	if r.Limit > 0 {
		return r.Limit
	}

	return CollectionsColumnDefaultPageSize
}

// CollectionsCurrencyTotal is a column total in one currency; totals are never summed across currencies.
type CollectionsCurrencyTotal struct {
	Currency string          `json:"currency"`
	Amount   decimal.Decimal `json:"amount" swaggertype:"string"`
}

// CollectionsItem is one invoice. Overdue-type cards carry the reminder fields; ReminderStatus is pending until someone acts.
type CollectionsItem struct {
	InvoiceID     string `json:"invoice_id"`
	InvoiceNumber string `json:"invoice_number"`
	CustomerID    string `json:"customer_id"`
	CustomerName  string `json:"customer_name"`
	// Amount is the remaining balance (not_due, overdue, chasing, held) or the invoice total (paid_this_week).
	Amount               decimal.Decimal      `json:"amount" swaggertype:"string"`
	Currency             string               `json:"currency"`
	DueDate              *time.Time           `json:"due_date,omitempty"`
	DaysOverdue          *int                 `json:"days_overdue,omitempty"`
	DueInDays            *int                 `json:"due_in_days,omitempty"`
	PaidAt               *time.Time           `json:"paid_at,omitempty"`
	ReminderStatus       types.ReminderStatus `json:"reminder_status,omitempty"`
	ReminderTone         types.ReminderTone   `json:"reminder_tone,omitempty"`
	ReminderStatusReason string               `json:"reminder_status_reason,omitempty"`
}

// CollectionsBoardColumn is one board column with whole-column count and totals and a capped card list.
type CollectionsBoardColumn struct {
	Key   types.CollectionsColumnKey `json:"key"`
	Title string                     `json:"title"`
	Hint  string                     `json:"hint"`
	// Count is the number of invoices in the whole column.
	Count  int                        `json:"count"`
	Totals []CollectionsCurrencyTotal `json:"totals"`
	Cards  []CollectionsItem          `json:"cards"`
	// HasMore is true when the column has cards beyond the ones in this response.
	HasMore bool `json:"has_more"`
}

// CollectionsBoardResponse is the collections board.
type CollectionsBoardResponse struct {
	AsOf time.Time `json:"as_of"`
	// Range is the date range the board was read with (open columns by due date, paid by paid date); null when none
	// was given, then open columns are unbounded and paid is the last 7 days.
	Range   *CollectionsRange        `json:"range" extensions:"x-nullable"`
	Columns []CollectionsBoardColumn `json:"columns"`
	// AvailableCurrencies lists the currencies of all open and recently paid receivables, before q and currency apply.
	AvailableCurrencies []string `json:"available_currencies"`
}

// CollectionsColumnPageResponse is one further page of a board column.
type CollectionsColumnPageResponse struct {
	Key     types.CollectionsColumnKey `json:"key"`
	AsOf    time.Time                  `json:"as_of"`
	Range   *CollectionsRange          `json:"range" extensions:"x-nullable"`
	Page    int                        `json:"page"`
	Cards   []CollectionsItem          `json:"cards"`
	HasMore bool                       `json:"has_more"`
}

// InvoiceReminderResponse is the reminder of one overdue invoice; its id is the invoice id.
type InvoiceReminderResponse struct {
	ID            string               `json:"id"`
	InvoiceID     string               `json:"invoice_id"`
	InvoiceNumber string               `json:"invoice_number"`
	CustomerID    string               `json:"customer_id"`
	CustomerName  string               `json:"customer_name"`
	CustomerEmail string               `json:"customer_email"`
	Currency      string               `json:"currency"`
	Amount        decimal.Decimal      `json:"amount" swaggertype:"string"`
	DueDate       *time.Time           `json:"due_date,omitempty"`
	DaysOverdue   int                  `json:"days_overdue"`
	Status        types.ReminderStatus `json:"status"`
	Tone          types.ReminderTone   `json:"tone"`
	Subject       string               `json:"subject"`
	Body          string               `json:"body"`
	StatusReason  string               `json:"status_reason,omitempty"`
	ApprovedAt    *time.Time           `json:"approved_at,omitempty"`
	// Edited is true when the mail differs from what the system wrote; the Original fields then hold the system's version.
	Edited          bool               `json:"edited"`
	OriginalTone    types.ReminderTone `json:"original_tone,omitempty"`
	OriginalSubject string             `json:"original_subject,omitempty"`
	OriginalBody    string             `json:"original_body,omitempty"`
	// ToneReason explains the tone shown: the invoice's age, "Tone set to firm by you", or "Approved with a firm tone, kept as approved". It says nothing about edits or status; use edited and status_reason.
	ToneReason string `json:"tone_reason"`
	// Email is where the email of an approved reminder stands; absent until one was queued.
	Email *ReminderEmailStatus `json:"email,omitempty"`
	// CreatedAt and UpdatedAt are omitted until a person acts on the reminder.
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// ReminderEmailStatus is the latest email of an approved reminder; To is who it actually went to.
type ReminderEmailStatus struct {
	Status       types.EmailDeliveryStatus `json:"status"`
	To           string                    `json:"to"`
	SentAt       *time.Time                `json:"sent_at,omitempty"`
	StatusReason string                    `json:"status_reason,omitempty"`
}

// ReminderSummary totals the overdue invoices and counts reminders by status. It follows the date range, currency and q
// exactly like the board's totals, and ignores customer and status so the status tabs always count every status.
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
	Q          string `form:"q" json:"q,omitempty"`
	CustomerID string `form:"customer_id" json:"customer_id,omitempty"`
	Currency   string `form:"currency" json:"currency,omitempty"`
	// Limit is the page size (default 15, max 100); Page is 1-based. AsOf pins the list to the clock of an earlier response.
	Limit int        `form:"limit" json:"limit,omitempty"`
	Page  int        `form:"page" json:"page,omitempty"`
	AsOf  *time.Time `form:"as_of" json:"as_of,omitempty"`
	// The date range narrows the list and its summary by due date; without it they cover every overdue invoice.
	CollectionsDateFilter
}

const (
	RemindersDefaultLimit = 15
	RemindersMaxLimit     = 100
	// RemindersMaxBulk caps the invoices of one bulk update.
	RemindersMaxBulk = 200
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
	if err := r.validateRange(); err != nil {
		return err
	}
	if r.Status != "" && !types.ReminderStatus(r.Status).Valid() {
		return ierr.NewError("invalid status").WithHint("status must be pending, approved, held or skipped.").Mark(ierr.ErrValidation)
	}
	return nil
}

type ListRemindersResponse struct {
	AsOf time.Time `json:"as_of"`
	// Range is the due date range the list and summary were read with; null when none was given.
	Range   *CollectionsRange         `json:"range" extensions:"x-nullable"`
	Summary ReminderSummary           `json:"summary"`
	Drafts  []InvoiceReminderResponse `json:"drafts"`
	Page    int                       `json:"page"`
	HasMore bool                      `json:"has_more"`
	// AvailableCurrencies lists the currencies of all open and recently paid invoices, before q, currency and range apply.
	AvailableCurrencies []string `json:"available_currencies"`
}

// UpdateReminderRequest moves a reminder to a status and/or edits its mail; the request decides which. Status is
// pending, approved, held or skipped (omit it to keep the current one); status_reason is required for held and optional
// for skipped. A tone change rewrites subject and body from that tone's template, then a subject or body in the same
// request is applied on top.
type UpdateReminderRequest struct {
	Status       types.ReminderStatus `json:"status,omitempty"`
	StatusReason string               `json:"status_reason,omitempty"`
	Tone         *string              `json:"tone,omitempty"`
	Subject      *string              `json:"subject,omitempty"`
	Body         *string              `json:"body,omitempty"`
	// ExpectedSubject and ExpectedBody are the text the reviewer saw. On an approval, the request fails with a conflict
	// when the text that would be approved now differs (the invoice changed since the preview). Optional, approval only.
	ExpectedSubject *string `json:"expected_subject,omitempty"`
	ExpectedBody    *string `json:"expected_body,omitempty"`
}

func (r *UpdateReminderRequest) Validate() error {
	r.StatusReason = strings.TrimSpace(r.StatusReason)
	if r.Status == "" && r.StatusReason == "" && r.Subject == nil && r.Body == nil && r.Tone == nil {
		return ierr.NewError("nothing to update").WithHint("Send status, tone, subject or body.").Mark(ierr.ErrValidation)
	}
	if r.Status != "" && !r.Status.Valid() {
		return ierr.NewError("invalid status").WithHint("status must be pending, approved, held or skipped.").Mark(ierr.ErrValidation)
	}
	switch {
	case r.Status == types.ReminderStatusHeld && r.StatusReason == "":
		return ierr.NewError("reason required").WithHint("Say why this reminder is held with status_reason.").Mark(ierr.ErrValidation)
	case r.StatusReason != "" && r.Status != types.ReminderStatusHeld && r.Status != types.ReminderStatusSkipped:
		return ierr.NewError("unexpected reason").WithHint("status_reason only goes with status held or skipped.").Mark(ierr.ErrValidation)
	}
	if (r.ExpectedSubject != nil || r.ExpectedBody != nil) && r.Status != types.ReminderStatusApproved {
		return ierr.NewError("unexpected preview text").WithHint("expected_subject and expected_body only go with status approved.").Mark(ierr.ErrValidation)
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

// BulkUpdateRemindersRequest applies the same status and tone change to up to 200 invoices. Subject and body are
// per reminder, so they are rejected here.
type BulkUpdateRemindersRequest struct {
	InvoiceIDs []string `json:"invoice_ids"`
	UpdateReminderRequest
}

func (r *BulkUpdateRemindersRequest) Validate() error {
	if len(r.InvoiceIDs) == 0 {
		return ierr.NewError("invoice_ids required").WithHint("Send at least one invoice id.").Mark(ierr.ErrValidation)
	}
	if len(r.InvoiceIDs) > RemindersMaxBulk {
		return ierr.NewError("too many invoices").WithHint("Update at most 200 invoices at a time.").Mark(ierr.ErrValidation)
	}
	if r.ExpectedSubject != nil || r.ExpectedBody != nil {
		return ierr.NewError("expected text not allowed").WithHint("Approve one reminder at a time to check the text you saw.").Mark(ierr.ErrValidation)
	}
	if r.Subject != nil || r.Body != nil {
		return ierr.NewError("subject and body not allowed").WithHint("Edit the subject or body of one reminder at a time.").Mark(ierr.ErrValidation)
	}
	return r.UpdateReminderRequest.Validate()
}

type BulkUpdateRemindersResponse struct {
	Drafts []InvoiceReminderResponse `json:"drafts"`
}
