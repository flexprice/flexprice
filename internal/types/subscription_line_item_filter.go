package types

// SubscriptionLineItemFilter defines filters for querying subscription line items
type SubscriptionLineItemFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Specific filters
	SubscriptionIDs []string
	CustomerIDs     []string
	PlanIDs         []string
	PriceIDs        []string
	MeterIDs        []string
	Currencies      []string
	BillingPeriods  []string
}
