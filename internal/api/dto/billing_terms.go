package dto

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// BillingTermInput defines a collection rule before owner-aware normalization.
type BillingTermInput struct {
	Kind          types.BillingTermKind      `json:"kind"`
	ValueType     types.BillingTermValueType `json:"value_type"`
	Value         *decimal.Decimal           `json:"value,omitempty" swaggertype:"string"`
	Percent       *decimal.Decimal           `json:"percent,omitempty" swaggertype:"string"`
	BillOn        *time.Time                 `json:"bill_on,omitempty"`
	OnTrigger     bool                       `json:"on_trigger,omitempty"`
	StartDate     *time.Time                 `json:"start_date,omitempty"`
	EndDate       *time.Time                 `json:"end_date,omitempty"`
	Interval      *types.BillingPeriod       `json:"interval,omitempty"`
	IntervalCount *int                       `json:"interval_count,omitempty"`
	BillAt        *types.BillingTermTiming   `json:"bill_at,omitempty"`
	Split         *types.BillingTermSplit    `json:"split,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Metadata      types.Metadata             `json:"metadata,omitempty"`
}

// UnmarshalJSON rejects unsupported fields instead of silently changing collection instructions.
func (r *BillingTermInput) UnmarshalJSON(data []byte) error {
	type plain BillingTermInput
	return decodeBillingJSON(data, (*plain)(r))
}

// RuleInput supplies the shared planner's owner-neutral input.
func (r BillingTermInput) RuleInput() core.RuleInput {
	return core.RuleInput(r)
}

// BillingTermsInput is the complete requested collection arrangement.
type BillingTermsInput struct {
	Rows []BillingTermInput `json:"rows"`
}

// UnmarshalJSON rejects unsupported rule-set fields.
func (r *BillingTermsInput) UnmarshalJSON(data []byte) error {
	type plain BillingTermsInput
	return decodeBillingJSON(data, (*plain)(r))
}

// Validate bounds input before owner-aware economic validation.
func (r BillingTermsInput) Validate() error {
	if len(r.Rows) == 0 || len(r.Rows) > core.MaxRules {
		return ierr.Validationf("billing_terms requires between 1 and %d rows", core.MaxRules)
	}
	return nil
}

// RuleInputs returns inputs for the common planner and persistence service.
func (r BillingTermsInput) RuleInputs() []core.RuleInput {
	rows := make([]core.RuleInput, len(r.Rows))
	for i, row := range r.Rows {
		rows[i] = row.RuleInput()
	}
	return rows
}

// ReplaceBillingTermsRequest replaces all current rules without changing existing invoices.
type ReplaceBillingTermsRequest struct {
	ExpectedState string            `json:"expected_state"`
	Reason        string            `json:"reason"`
	BillingTerms  BillingTermsInput `json:"billing_terms"`
}

// UnmarshalJSON excludes preview-only invoice releases from an executable replacement.
func (r *ReplaceBillingTermsRequest) UnmarshalJSON(data []byte) error {
	type plain ReplaceBillingTermsRequest
	return decodeBillingJSON(data, (*plain)(r))
}

// Validate checks the command envelope; the core verifies allocation and state under locks.
func (r ReplaceBillingTermsRequest) Validate() error {
	if strings.TrimSpace(r.ExpectedState) == "" || len(r.ExpectedState) > 128 {
		return ierr.Validation("expected_state is required and must fit 128 bytes")
	}
	if strings.TrimSpace(r.Reason) == "" || utf8.RuneCountInString(r.Reason) > 255 {
		return ierr.Validation("reason must contain between 1 and 255 characters")
	}
	return r.BillingTerms.Validate()
}

// PreviewBillingTermsRequest previews a replacement, optionally conditional on explicit invoice correction.
type PreviewBillingTermsRequest struct {
	ExpectedState     string            `json:"expected_state,omitempty"`
	Reason            string            `json:"reason"`
	BillingTerms      BillingTermsInput `json:"billing_terms"`
	ReplaceInvoiceIDs []string          `json:"replace_invoice_ids,omitempty"`
}

// UnmarshalJSON rejects unsupported preview controls.
func (r *PreviewBillingTermsRequest) UnmarshalJSON(data []byte) error {
	type plain PreviewBillingTermsRequest
	return decodeBillingJSON(data, (*plain)(r))
}

// Validate bounds the preview command without requiring a previous state token.
func (r PreviewBillingTermsRequest) Validate() error {
	if len(r.ExpectedState) > 128 {
		return ierr.Validation("expected_state must fit 128 bytes")
	}
	return (ReplaceBillingTermsRequest{ExpectedState: "preview", Reason: r.Reason, BillingTerms: r.BillingTerms}).Validate()
}

// TriggerBillingScheduleItemRequest resolves the date of one current manual occurrence.
type TriggerBillingScheduleItemRequest struct {
	ExpectedState string     `json:"expected_state"`
	BillOn        *time.Time `json:"bill_on,omitempty"`
}

// UnmarshalJSON rejects unsupported trigger controls.
func (r *TriggerBillingScheduleItemRequest) UnmarshalJSON(data []byte) error {
	type plain TriggerBillingScheduleItemRequest
	return decodeBillingJSON(data, (*plain)(r))
}

// Validate bounds the concurrency token; the owner validates the trigger date.
func (r TriggerBillingScheduleItemRequest) Validate() error {
	if strings.TrimSpace(r.ExpectedState) == "" || len(r.ExpectedState) > 128 {
		return ierr.Validation("expected_state is required and must fit 128 bytes")
	}
	return nil
}

// ListBillingScheduleItemsRequest selects a bounded page of owner-scoped sources.
type ListBillingScheduleItemsRequest struct {
	Limit           int                         `form:"limit" json:"limit,omitempty"`
	Cursor          string                      `form:"cursor" json:"cursor,omitempty"`
	IncludeArchived bool                        `form:"include_archived" json:"include_archived,omitempty"`
	ScheduleStatus  types.BillingScheduleStatus `form:"schedule_status" json:"schedule_status,omitempty"`
}

// Normalize applies the page default and validates public list bounds.
func (r *ListBillingScheduleItemsRequest) Normalize() error {
	if r.Limit == 0 {
		r.Limit = 50
	}
	if r.Limit < 1 || r.Limit > 100 || len(r.Cursor) > 2048 {
		return ierr.Validation("limit must be between 1 and 100 and cursor must fit 2048 bytes")
	}
	if r.ScheduleStatus != "" {
		return r.ScheduleStatus.Validate()
	}
	return nil
}

// BillingPagination describes a stable owner-scoped continuation.
type BillingPagination struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

// BillingTermResponse exposes a persisted or proposed rule; proposed rules have no ID.
type BillingTermResponse struct {
	*domain.Rule
	ID string `json:"id,omitempty"`
}

// BillingTermsResponse contains the owner's current collection rules.
type BillingTermsResponse struct {
	Rows []*BillingTermResponse `json:"rows"`
}

// BillingScheduleItemResponse retains the exact source and optionally expands its original rule.
type BillingScheduleItemResponse struct {
	*domain.ScheduleItem
	ID            string               `json:"id,omitempty"`
	BillOn        *time.Time           `json:"bill_on"`
	BillingTerm   *BillingTermResponse `json:"billing_term,omitempty"`
	InvoiceStatus *types.InvoiceStatus `json:"invoice_status,omitempty"`
}

// ListBillingScheduleItemsResponse returns concrete charges rather than rule-level invoice statuses.
type ListBillingScheduleItemsResponse struct {
	Items      []*BillingScheduleItemResponse `json:"items"`
	Pagination BillingPagination              `json:"pagination"`
}

// BillingTermsStateResponse exposes current rules, a bounded item page and lifetime collection state.
type BillingTermsStateResponse struct {
	BillingTerms         BillingTermsResponse             `json:"billing_terms"`
	BillingScheduleItems ListBillingScheduleItemsResponse `json:"billing_schedule_items"`
	Collection           *domain.CollectionSummary        `json:"collection"`
	StateToken           string                           `json:"state_token"`
}

// BillingTermsPreviewResponse distinguishes a proposed plan from an executable command.
type BillingTermsPreviewResponse struct {
	StateToken                   string                         `json:"state_token"`
	CanApply                     bool                           `json:"can_apply"`
	Command                      *ReplaceBillingTermsRequest    `json:"command"`
	ArchiveRuleIDs               []string                       `json:"archive_rule_ids"`
	ArchiveItemIDs               []string                       `json:"archive_item_ids"`
	RetainedInvoiceIDs           []string                       `json:"retained_invoice_ids"`
	BlockingInvoiceIDs           []string                       `json:"blocking_invoice_ids"`
	ConditionalReleasedPrincipal decimal.Decimal                `json:"conditional_released_principal" swaggertype:"string"`
	Items                        []*BillingScheduleItemResponse `json:"items"`
	Collection                   *domain.CollectionSummary      `json:"collection"`
	Warnings                     []string                       `json:"warnings"`
}

// TriggerBillingScheduleItemResponse exposes the resolved occurrence and updated collection state.
type TriggerBillingScheduleItemResponse struct {
	Item       *BillingScheduleItemResponse `json:"item"`
	Collection *domain.CollectionSummary    `json:"collection"`
	StateToken string                       `json:"state_token"`
}

func decodeBillingJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
