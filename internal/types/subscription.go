package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// InvoiceBilling determines which customer should receive invoices for a subscription
type InvoiceBilling string

const (
	// InvoiceBillingInvoiceToParent - Invoices are sent to the parent customer
	InvoiceBillingInvoiceToParent InvoiceBilling = "invoice_to_parent"

	// InvoiceBillingInvoiceToSelf - Invoices are sent to the subscription's customer
	InvoiceBillingInvoiceToSelf InvoiceBilling = "invoice_to_self"
)

func (i InvoiceBilling) String() string {
	return string(i)
}

func (i InvoiceBilling) Validate() error {
	allowed := []InvoiceBilling{
		InvoiceBillingInvoiceToParent,
		InvoiceBillingInvoiceToSelf,
	}

	if i != "" && !lo.Contains(allowed, i) {
		return ierr.NewError("invalid invoice billing").
			WithHint("Invalid invoice billing").
			WithReportableDetails(map[string]any{
				"invoice_billing": i,
				"allowed_values":  allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubscriptionLineItemEntityType is the type of the source of a subscription line item
// It is optional and can be used to differentiate between plan and addon line items
type SubscriptionLineItemEntityType string

const (
	SubscriptionLineItemEntityTypePlan  SubscriptionLineItemEntityType = "plan"
	SubscriptionLineItemEntityTypeAddon SubscriptionLineItemEntityType = "addon"
)

// SubscriptionStatus is the status of a subscription
// For now taking inspiration from Stripe's subscription statuses
// https://stripe.com/docs/api/subscriptions/object#subscription_object-status
type SubscriptionStatus string

const (
	SubscriptionStatusActive     SubscriptionStatus = "active"
	SubscriptionStatusPaused     SubscriptionStatus = "paused"
	SubscriptionStatusCancelled  SubscriptionStatus = "cancelled"
	SubscriptionStatusIncomplete SubscriptionStatus = "incomplete"
	SubscriptionStatusTrialing   SubscriptionStatus = "trialing"
	SubscriptionStatusDraft      SubscriptionStatus = "draft"
)

func (s SubscriptionStatus) String() string {
	return string(s)
}

func (s SubscriptionStatus) Validate() error {
	allowed := []SubscriptionStatus{
		SubscriptionStatusActive,
		SubscriptionStatusPaused,
		SubscriptionStatusCancelled,
		SubscriptionStatusIncomplete,
		SubscriptionStatusTrialing,
		SubscriptionStatusDraft,
	}

	if s != "" && !lo.Contains(allowed, s) {
		return ierr.NewError("invalid subscription status").
			WithHint("Invalid subscription status").
			WithReportableDetails(map[string]any{
				"status":         s,
				"allowed_status": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PaymentBehavior determines how subscription payments are handled
type PaymentBehavior string

const (
	// PaymentBehaviorAllowIncomplete - Immediately attempts payment. If fails, subscription becomes incomplete
	PaymentBehaviorAllowIncomplete PaymentBehavior = "allow_incomplete"

	// PaymentBehaviorDefaultIncomplete - Always creates incomplete subscription if payment required
	PaymentBehaviorDefaultIncomplete PaymentBehavior = "default_incomplete"

	// PaymentBehaviorErrorIfIncomplete - Fails subscription creation if payment fails
	PaymentBehaviorErrorIfIncomplete PaymentBehavior = "error_if_incomplete"

	// PaymentBehaviorDefaultActive - Creates active subscription without payment attempt
	PaymentBehaviorDefaultActive PaymentBehavior = "default_active"
)

func (p PaymentBehavior) String() string {
	return string(p)
}

func (p PaymentBehavior) Validate() error {
	allowed := []PaymentBehavior{
		PaymentBehaviorAllowIncomplete,
		PaymentBehaviorDefaultIncomplete,
		PaymentBehaviorErrorIfIncomplete,
		PaymentBehaviorDefaultActive,
	}

	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid payment behavior").
			WithHint("Invalid payment behavior").
			WithReportableDetails(map[string]any{
				"payment_behavior": p,
				"allowed_values":   allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CollectionMethod determines how invoices are collected for subscriptions
type CollectionMethod string

const (
	// CollectionMethodChargeAutomatically - Automatically charge payment method
	CollectionMethodChargeAutomatically CollectionMethod = "charge_automatically"

	// CollectionMethodSendInvoice - Send invoice to customer for manual payment
	CollectionMethodSendInvoice CollectionMethod = "send_invoice"
)

func (c CollectionMethod) String() string {
	return string(c)
}

func (c CollectionMethod) Validate() error {
	allowed := []CollectionMethod{
		CollectionMethodChargeAutomatically,
		CollectionMethodSendInvoice,
	}

	if c != "" && !lo.Contains(allowed, c) {
		return ierr.NewError("invalid collection method").
			WithHint("Invalid collection method").
			WithReportableDetails(map[string]any{
				"collection_method": c,
				"allowed_values":    allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PauseStatus represents the pause state of a subscription
type PauseStatus string

const (
	// PauseStatusNone indicates the subscription is not paused
	PauseStatusNone PauseStatus = "none"

	// PauseStatusActive indicates the subscription is currently paused
	PauseStatusActive PauseStatus = "active"

	// PauseStatusScheduled indicates the subscription is scheduled to be paused
	PauseStatusScheduled PauseStatus = "scheduled"

	// PauseStatusCompleted indicates the pause has been completed (subscription resumed)
	PauseStatusCompleted PauseStatus = "completed"

	// PauseStatusCancelled indicates the pause was cancelled
	PauseStatusCancelled PauseStatus = "cancelled"
)

func (s PauseStatus) String() string {
	return string(s)
}

func (s PauseStatus) Validate() error {
	allowed := []PauseStatus{
		PauseStatusNone,
		PauseStatusActive,
		PauseStatusScheduled,
		PauseStatusCompleted,
		PauseStatusCancelled,
	}

	if s != "" && !lo.Contains(allowed, s) {
		return ierr.NewError("invalid pause status").
			WithHint("Invalid pause status").
			WithReportableDetails(map[string]any{
				"status":         s,
				"allowed_status": allowed,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// SubscriptionFilter represents filters for subscription queries
type SubscriptionFilter struct {
	*QueryFilter
	*TimeRangeFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	SubscriptionIDs []string `json:"subscription_ids,omitempty" form:"subscription_ids"`
	// CustomerID filters by customer ID
	CustomerID string `json:"customer_id,omitempty" form:"customer_id"`
	// ExternalCustomerID filters by external customer ID
	ExternalCustomerID string `json:"external_customer_id,omitempty" form:"external_customer_id"`
	// InvoicingCustomerIDs filters by invoicing customer ID
	InvoicingCustomerIDs []string `json:"invoicing_customer_ids,omitempty" form:"invoicing_customer_ids"`
	// PlanID filters by plan ID
	PlanID string `json:"plan_id,omitempty" form:"plan_id"`
	// SubscriptionStatus filters by subscription status
	SubscriptionStatus []SubscriptionStatus `json:"subscription_status,omitempty" form:"subscription_status"`
	// BillingCadence filters by billing cadence
	BillingCadence []BillingCadence `json:"billing_cadence,omitempty" form:"billing_cadence"`
	// BillingPeriod filters by billing period
	BillingPeriod []BillingPeriod `json:"billing_period,omitempty" form:"billing_period"`
	// SubscriptionStatusNotIn filters by subscription status not in the list
	SubscriptionStatusNotIn []SubscriptionStatus `json:"-"`
	// ActiveAt filters subscriptions that are active at the given time
	ActiveAt *time.Time `json:"active_at,omitempty" form:"active_at"`

	// WithLineItems includes line items in the response
	WithLineItems bool `json:"with_line_items,omitempty" form:"with_line_items"`
}

// NewSubscriptionFilter creates a new SubscriptionFilter with default values
func NewSubscriptionFilter() *SubscriptionFilter {
	return &SubscriptionFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitSubscriptionFilter creates a new SubscriptionFilter with no pagination limits
func NewNoLimitSubscriptionFilter() *SubscriptionFilter {
	return &SubscriptionFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the subscription filter
func (f SubscriptionFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	// Validate subscription status values
	for _, status := range f.SubscriptionStatus {
		if err := status.Validate(); err != nil {
			return err
		}
	}

	// Validate billing cadence values
	for _, cadence := range f.BillingCadence {
		if err := cadence.Validate(); err != nil {
			return err
		}
	}

	// Validate billing period values
	for _, period := range f.BillingPeriod {
		if err := period.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *SubscriptionFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *SubscriptionFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *SubscriptionFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *SubscriptionFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *SubscriptionFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *SubscriptionFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *SubscriptionFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// SubscriptionChangeType defines the type of subscription change
type SubscriptionChangeType string

const (
	SubscriptionChangeTypeUpgrade   SubscriptionChangeType = "upgrade"
	SubscriptionChangeTypeDowngrade SubscriptionChangeType = "downgrade"
	SubscriptionChangeTypeLateral   SubscriptionChangeType = "lateral"
)

var SubscriptionChangeTypeValues = []SubscriptionChangeType{
	SubscriptionChangeTypeUpgrade,
	SubscriptionChangeTypeDowngrade,
	SubscriptionChangeTypeLateral,
}

func (s SubscriptionChangeType) String() string {
	return string(s)
}

func (s SubscriptionChangeType) Validate() error {
	if s != "" && !lo.Contains(SubscriptionChangeTypeValues, s) {
		return ierr.NewError("invalid subscription change type").
			WithHint("Subscription change type must be upgrade, downgrade, or lateral").
			WithReportableDetails(map[string]any{
				"allowed_values": SubscriptionChangeTypeValues,
				"provided_value": s,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubscriptionScheduleChangeType represents the type of subscription schedule change
type SubscriptionScheduleChangeType string

const (
	SubscriptionScheduleChangeTypePlanChange   SubscriptionScheduleChangeType = "plan_change"
	SubscriptionScheduleChangeTypeCancellation SubscriptionScheduleChangeType = "cancellation"
)

var SubscriptionScheduleChangeTypeValues = []SubscriptionScheduleChangeType{
	SubscriptionScheduleChangeTypePlanChange,
	SubscriptionScheduleChangeTypeCancellation,
}

func (s SubscriptionScheduleChangeType) String() string {
	return string(s)
}

func (s SubscriptionScheduleChangeType) Validate() error {
	if s != "" && !lo.Contains(SubscriptionScheduleChangeTypeValues, s) {
		return ierr.NewError("invalid subscription schedule change type").
			WithHint("Subscription schedule change type must be plan_change or cancellation").
			WithReportableDetails(map[string]any{
				"allowed_values": SubscriptionScheduleChangeTypeValues,
				"provided_value": s,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ScheduleStatus represents the status of a schedule
type ScheduleStatus string

const (
	ScheduleStatusPending   ScheduleStatus = "pending"
	ScheduleStatusExecuting ScheduleStatus = "executing"
	ScheduleStatusExecuted  ScheduleStatus = "executed"
	ScheduleStatusCancelled ScheduleStatus = "cancelled"
	ScheduleStatusFailed    ScheduleStatus = "failed"
)

var ScheduleStatusValues = []ScheduleStatus{
	ScheduleStatusPending,
	ScheduleStatusExecuting,
	ScheduleStatusExecuted,
	ScheduleStatusCancelled,
	ScheduleStatusFailed,
}

func (s ScheduleStatus) String() string {
	return string(s)
}

func (s ScheduleStatus) Validate() error {
	if s != "" && !lo.Contains(ScheduleStatusValues, s) {
		return ierr.NewError("invalid schedule status").
			WithHint("Schedule status must be pending, executing, executed, cancelled, or failed").
			WithReportableDetails(map[string]any{
				"allowed_values": ScheduleStatusValues,
				"provided_value": s,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ScheduleType determines when subscription changes take effect (change_at parameter)
type ScheduleType string

const (
	ScheduleTypeImmediate ScheduleType = "immediate"
	ScheduleTypePeriodEnd ScheduleType = "end_of_period"
)

var ScheduleTypeValues = []ScheduleType{
	ScheduleTypeImmediate,
	ScheduleTypePeriodEnd,
}

func (s ScheduleType) String() string {
	return string(s)
}

func (s ScheduleType) Validate() error {
	if s != "" && !lo.Contains(ScheduleTypeValues, s) {
		return ierr.NewError("invalid schedule type").
			WithHint("Schedule type must be 'immediate', 'end_of_period'").
			WithReportableDetails(map[string]any{
				"allowed_values": ScheduleTypeValues,
				"provided_value": s,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubscriptionScheduleFilter represents filters for querying subscription schedules
type SubscriptionScheduleFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// ScheduleIDs filters by schedule IDs
	ScheduleIDs []string `json:"schedule_ids,omitempty" form:"schedule_ids"`

	// SubscriptionIDs filters by subscription IDs
	SubscriptionIDs []string `json:"subscription_ids,omitempty" form:"subscription_ids"`

	// ScheduleType filters by schedule change type
	ScheduleType []SubscriptionScheduleChangeType `json:"schedule_type,omitempty" form:"schedule_type"`

	// Status filters by schedule status
	ScheduleStatus []ScheduleStatus `json:"schedule_status,omitempty" form:"schedule_status"`

	// ScheduledAtStart filters schedules with scheduled_at after this date
	ScheduledAtStart *time.Time `json:"scheduled_at_start,omitempty" form:"scheduled_at_start"`

	// ScheduledAtEnd filters schedules with scheduled_at before this date
	ScheduledAtEnd *time.Time `json:"scheduled_at_end,omitempty" form:"scheduled_at_end"`

	// PendingOnly filters to only pending schedules
	PendingOnly bool `json:"pending_only,omitempty" form:"pending_only"`
}

// NewSubscriptionScheduleFilter creates a new SubscriptionScheduleFilter with default values
func NewSubscriptionScheduleFilter() *SubscriptionScheduleFilter {
	return &SubscriptionScheduleFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// Validate validates the subscription schedule filter
func (f SubscriptionScheduleFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	// Validate schedule types
	for _, scheduleType := range f.ScheduleType {
		if err := scheduleType.Validate(); err != nil {
			return err
		}
	}

	// Validate schedule statuses
	for _, status := range f.ScheduleStatus {
		if err := status.Validate(); err != nil {
			return err
		}
	}

	return nil
}
