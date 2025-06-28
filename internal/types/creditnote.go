package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type CreditNoteStatus string

const (
	// CreditNoteStatusDraft indicates credit note is in draft state and can be modified
	CreditNoteStatusDraft CreditNoteStatus = "DRAFT"
	// CreditNoteStatusFinalized indicates credit note is finalized and ready for payment
	CreditNoteStatusFinalized CreditNoteStatus = "FINALIZED"
	// CreditNoteStatusVoided indicates credit note has been voided
	CreditNoteStatusVoided CreditNoteStatus = "VOIDED"
)

func (c CreditNoteStatus) String() string {
	return string(c)
}

func (c CreditNoteStatus) Validate() error {

	allowed := []CreditNoteStatus{
		CreditNoteStatusDraft,
		CreditNoteStatusFinalized,
		CreditNoteStatusVoided,
	}

	if !lo.Contains(allowed, c) {
		return ierr.NewError("invalid credit note status").
			WithHint("Please provide a valid credit note status").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type CreditNoteReason string

const (
	CreditNoteReasonDuplicate                CreditNoteReason = "DUPLICATE"
	CreditNoteReasonFraudulent               CreditNoteReason = "FRAUDULENT"
	CreditNoteReasonOrderChange              CreditNoteReason = "ORDER_CHANGE"
	CreditNoteReasonUnsatisfactory           CreditNoteReason = "UNSATISFACTORY"
	CreditNoteReasonService                  CreditNoteReason = "SERVICE_ISSUE"
	CreditNoteReasonBillingError             CreditNoteReason = "BILLING_ERROR"
	CreditNoteReasonSubscriptionCancellation CreditNoteReason = "SUBSCRIPTION_CANCELLATION"
)

func (c CreditNoteReason) String() string {
	return string(c)
}

func (c CreditNoteReason) Validate() error {

	allowed := []CreditNoteReason{
		CreditNoteReasonDuplicate,
		CreditNoteReasonFraudulent,
		CreditNoteReasonOrderChange,
		CreditNoteReasonUnsatisfactory,
		CreditNoteReasonService,
		CreditNoteReasonBillingError,
		CreditNoteReasonSubscriptionCancellation,
	}

	if !lo.Contains(allowed, c) {
		return ierr.NewError("invalid credit note reason").
			WithHint("Please provide a valid credit note reason").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type CreditNoteType string

const (
	CreditNoteTypeAdjustment CreditNoteType = "ADJUSTMENT"
	CreditNoteTypeRefund     CreditNoteType = "REFUND"
)

func (c CreditNoteType) String() string {
	return string(c)
}

func (c CreditNoteType) Validate() error {

	allowed := []CreditNoteType{
		CreditNoteTypeAdjustment,
		CreditNoteTypeRefund,
	}

	if !lo.Contains(allowed, c) {
		return ierr.NewError("invalid credit note type").
			WithHint("Please provide a valid credit note type").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// InvoiceFilter represents the filter options for listing invoices
type CreditNoteFilter struct {
	*QueryFilter
	*TimeRangeFilter
	CreditNoteIDs    []string           `json:"credit_note_ids,omitempty" form:"credit_note_ids"`
	InvoiceID        string             `json:"invoice_id,omitempty" form:"invoice_id"`
	CreditNoteStatus []CreditNoteStatus `json:"credit_note_status,omitempty" form:"credit_note_status"`
	CreditNoteType   CreditNoteType     `json:"credit_note_type,omitempty" form:"credit_note_type"`
}

// NewCreditNoteFilter creates a new credit note filter with default options
func NewCreditNoteFilter() *CreditNoteFilter {
	return &CreditNoteFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCreditNoteFilter creates a new credit note filter without pagination
func NewNoLimitCreditNoteFilter() *CreditNoteFilter {
	return &CreditNoteFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the credit note filter
func (f *CreditNoteFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid query filter").Mark(ierr.ErrValidation)
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid time range").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *CreditNoteFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CreditNoteFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CreditNoteFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CreditNoteFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CreditNoteFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CreditNoteFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CreditNoteFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// CreditNoteLineItemFilter defines filters for querying credit note line items
type CreditNoteLineItemFilter struct {
	*QueryFilter
	*TimeRangeFilter

	CreditNoteIDs      []string `json:"credit_note_ids,omitempty" form:"credit_note_ids"`
	InvoiceLineItemIDs []string `json:"invoice_line_item_ids,omitempty" form:"invoice_line_item_ids"`
}

// NewCreditNoteLineItemFilter creates a new credit note line item filter with default options
func NewCreditNoteLineItemFilter() *CreditNoteLineItemFilter {
	return &CreditNoteLineItemFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCreditNoteLineItemFilter creates a new credit note line item filter without pagination
func NewNoLimitCreditNoteLineItemFilter() *CreditNoteLineItemFilter {
	return &CreditNoteLineItemFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the credit note line item filter
func (f *CreditNoteLineItemFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid query filter").Mark(ierr.ErrValidation)
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid time range").Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *CreditNoteLineItemFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CreditNoteLineItemFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CreditNoteLineItemFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CreditNoteLineItemFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CreditNoteLineItemFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CreditNoteLineItemFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CreditNoteLineItemFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
