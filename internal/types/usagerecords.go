package types

import "time"

// UsageRecordSyncEntry records the outcome of reporting a usage record to one marketplace. Stored in
// usage_records.syncs, keyed by provider type: aws_marketplace, gcp_marketplace or azure_marketplace.
//
// The key is the marketplace, not the connection that did the reporting. A tenant can delete a
// connection and create another one for the same marketplace, which changes the connection id while
// the subscription's agreement on that marketplace stays the same. Keyed by connection, an already
// reported record would look unreported the moment that happens and be sent a second time. Keyed by
// marketplace, the question asked is the one that actually matters: has this record reached AWS for
// this subscription yet.
//
// SkipReasonZeroAmountNotSupported is the SkipReason for a record whose computed amount rounds to
// zero. Azure rejects a zero usage quantity, so the record is resolved as skipped rather than sent.
const SkipReasonZeroAmountNotSupported = "zero_amount_not_supported"

// An entry is one of two kinds, distinguished by Skipped:
//   - A real acceptance: Skipped is false, SyncedAt is when the marketplace accepted it, ReportingID
//     carries its receipt.
//   - A skip: Skipped is true, SyncedAt is left zero because nothing was sent, ReportingID is empty,
//     and SkipReason explains why. Used only when sending is deterministically pointless for this
//     provider (e.g. a zero-amount row on Azure, which documents a zero quantity as invalid). The
//     entry still resolves the marketplace so the row's synced flag can reach true, without claiming
//     anything was posted.
type UsageRecordSyncEntry struct {
	// AgreementID is the subscription's identifier on this marketplace: license_arn on AWS,
	// usage_reporting_id on GCP, resource_id on Azure. It records which agreement the usage was
	// billed against, which stays meaningful after the connection that reported it is replaced.
	AgreementID string `json:"agreement_id"`
	ReportingID string `json:"reporting_id"`
	// SyncedAt is when the marketplace accepted the record. Zero on a skip, since nothing was sent.
	SyncedAt   time.Time `json:"synced_at"`
	Skipped    bool      `json:"skipped,omitempty"`
	SkipReason string    `json:"skip_reason,omitempty"`
	// ConnectionID is the connection that reported this entry, kept for tracing which credentials
	// were used. It is deliberately not part of the map key: a replaced connection must not make an
	// already reported record look unreported.
	ConnectionID string `json:"connection_id"`
}

// IsSynced reports whether this entry represents usage actually delivered to the marketplace, as
// opposed to a skip. Callers pair it with the syncs map lookup's ok: the zero value reports true,
// so it distinguishes a real send from a skip, not a present entry from an absent one.
func (e UsageRecordSyncEntry) IsSynced() bool {
	return !e.Skipped
}

// UsageRecordFilter selects rows from usage_records. Tenant and environment are not fields: every
// query is scoped to the caller's context before these filters are applied.
type UsageRecordFilter struct {
	*QueryFilter

	// Bounds created_at, the time the row was written. The usage window the row covers is
	// PeriodStart/PeriodEnd below.
	*TimeRangeFilter

	// Generic predicate and ordering escape hatch, for comparisons that have no dedicated field
	// below — a range such as period_end at or after a cutoff goes here.
	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	SubscriptionID     string `json:"subscription_id,omitempty" form:"subscription_id" validate:"omitempty"`
	CustomerID         string `json:"customer_id,omitempty" form:"customer_id" validate:"omitempty"`
	CustomerExternalID string `json:"customer_external_id,omitempty" form:"customer_external_id" validate:"omitempty"`
	PlanID             string `json:"plan_id,omitempty" form:"plan_id" validate:"omitempty"`
	Currency           string `json:"currency,omitempty" form:"currency" validate:"omitempty"`

	// Exact-match on the window boundaries. Together with SubscriptionID these identify a single row:
	// they are the columns the table's unique index is built on.
	PeriodStart *time.Time `json:"period_start,omitempty" form:"period_start" validate:"omitempty"`
	PeriodEnd   *time.Time `json:"period_end,omitempty" form:"period_end" validate:"omitempty"`

	Synced *bool `json:"synced,omitempty" form:"synced" validate:"omitempty"`
}

// NewUsageRecordFilter creates a new UsageRecordFilter with default pagination.
func NewUsageRecordFilter() *UsageRecordFilter {
	return &UsageRecordFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitUsageRecordFilter creates a new UsageRecordFilter with no pagination limits.
func NewNoLimitUsageRecordFilter() *UsageRecordFilter {
	return &UsageRecordFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

func (f UsageRecordFilter) Validate() error {
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
	for _, c := range f.Filters {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	for _, c := range f.Sort {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *UsageRecordFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *UsageRecordFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *UsageRecordFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *UsageRecordFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *UsageRecordFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// IsUnlimited implements BaseFilter interface
func (f *UsageRecordFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return false
	}
	return f.QueryFilter.IsUnlimited()
}
