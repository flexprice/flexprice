package billingterms

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// OwnerRef identifies the entity whose obligation is collected.
type OwnerRef struct {
	Type types.BillingTermOwnerType `json:"type"`
	ID   string                     `json:"id"`
}

// MaxEligibilityScopes bounds the submitted lifecycle dependency set before deduplication.
const MaxEligibilityScopes = 500

// EligibilityScope identifies a lifecycle dependency that must be stable during collection.
type EligibilityScope struct {
	Type string
	ID   string
}

// OwnerSnapshot supplies immutable economics and current collection permissions.
type OwnerSnapshot struct {
	Owner                OwnerRef
	TenantID             string
	EnvironmentID        string
	OriginalTotal        decimal.Decimal
	CollectionTarget     decimal.Decimal
	AssignedAmount       decimal.Decimal
	RetainedItemCount    int
	StartDate            time.Time
	EndDate              time.Time
	Timezone             string
	Currency             string
	CustomerID           string
	SubscriptionID       *string
	Delivery             types.BillingTermDelivery
	CanEdit              bool
	CanCollect           bool
	DatedOnceOnly        bool
	Description          string
	Calendar             Calendar
	CalendarAnchor       time.Time
	AlignmentKey         string
	LifecycleKey         string
	DeferredReason       string
	DefaultInterval      types.BillingPeriod
	DefaultIntervalCount int
	EligibilityScopes    []EligibilityScope
}

// Calendar supplies the owner's aligned recurrence boundaries.
type Calendar interface {
	Next(start, end time.Time, interval types.BillingPeriod, count int) (time.Time, error)
}

// BindingState is an integrity-checked schedule source and its invoice state.
type BindingState struct {
	ItemID            string
	InvoiceID         string
	InvoiceLineItemID string
	TenantID          string
	EnvironmentID     string
	InvoiceStatus     types.InvoiceStatus
	InvoiceArchived   bool
}

// CollectionSummary separates lifetime assignment from dated pending charges.
type CollectionSummary struct {
	AsOf              time.Time       `json:"as_of"`
	Currency          string          `json:"currency"`
	TargetAmount      decimal.Decimal `json:"target_amount"`
	AssignedAmount    decimal.Decimal `json:"assigned_amount"`
	InvoicedAmount    decimal.Decimal `json:"invoiced_amount"`
	ScheduledAmount   decimal.Decimal `json:"scheduled_amount"`
	DueAmount         decimal.Decimal `json:"due_amount"`
	UnallocatedAmount decimal.Decimal `json:"unallocated_amount"`
	RequiresReplan    bool            `json:"requires_replan"`
	DeferredReason    *string         `json:"deferred_reason"`
	NextBillOn        *time.Time      `json:"next_bill_on"`
}

// Rule is an immutable collection definition retained after archival.
type Rule struct {
	ID            string                     `json:"id"`
	OwnerType     types.BillingTermOwnerType `json:"owner_type"`
	OwnerID       string                     `json:"owner_id"`
	Kind          types.BillingTermKind      `json:"kind"`
	ValueType     types.BillingTermValueType `json:"value_type"`
	Value         *decimal.Decimal           `json:"value,omitempty" swaggertype:"string"`
	BillOn        *time.Time                 `json:"bill_on,omitempty"`
	OnTrigger     bool                       `json:"on_trigger"`
	StartDate     *time.Time                 `json:"start_date,omitempty"`
	EndDate       *time.Time                 `json:"end_date,omitempty"`
	Interval      *types.BillingPeriod       `json:"interval,omitempty"`
	IntervalCount *int                       `json:"interval_count,omitempty"`
	BillAt        *types.BillingTermTiming   `json:"bill_at,omitempty"`
	Split         *types.BillingTermSplit    `json:"split,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Metadata      types.Metadata             `json:"metadata,omitempty"`
	EnvironmentID string                     `json:"environment_id"`
	types.BaseModel
}

// ScheduleItem is one exact charge and its retained invoice provenance.
type ScheduleItem struct {
	ID                 string                                   `json:"id"`
	OwnerType          types.BillingTermOwnerType               `json:"owner_type"`
	OwnerID            string                                   `json:"owner_id"`
	BillingTermID      *string                                  `json:"billing_term_id,omitempty"`
	CustomerID         string                                   `json:"customer_id"`
	SubscriptionID     *string                                  `json:"subscription_id,omitempty"`
	Currency           string                                   `json:"currency"`
	Kind               types.BillingScheduleItemKind            `json:"kind"`
	BillTrigger        types.BillingScheduleTrigger             `json:"bill_trigger"`
	BillOn             *time.Time                               `json:"bill_on,omitempty"`
	Amount             decimal.Decimal                          `json:"amount" swaggertype:"string"`
	Description        string                                   `json:"description"`
	ServicePeriodStart time.Time                                `json:"service_period_start"`
	ServicePeriodEnd   time.Time                                `json:"service_period_end"`
	Delivery           types.BillingTermDelivery                `json:"delivery"`
	ScheduleStatus     types.BillingScheduleStatus              `json:"schedule_status"`
	InvoiceID          *string                                  `json:"invoice_id,omitempty"`
	InvoiceLineItemID  *string                                  `json:"invoice_line_item_id,omitempty"`
	CancellationReason *types.BillingScheduleCancellationReason `json:"cancellation_reason,omitempty"`
	Metadata           types.Metadata                           `json:"metadata,omitempty"`
	EnvironmentID      string                                   `json:"environment_id"`
	types.BaseModel
}
