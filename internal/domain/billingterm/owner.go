package billingterm

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Owner is an entity whose value is collected through billing terms.
type Owner interface {
	OwnerType() types.BillingTermOwnerType
	OwnerID() string

	// BilledCustomerID and BilledSubscriptionID are the customer and subscription its instalments bill; the
	// subscription is empty for an owner billed without one.
	BilledCustomerID() string
	BilledSubscriptionID() string

	BillingCurrency() string

	// InstalmentDelivery says whether instalments go on the subscription invoice or on their own.
	InstalmentDelivery() types.InstalmentDelivery

	// CollectionPeriod is the span the terms collect over; End is zero when the owner has no end.
	CollectionPeriod() types.Period

	// CollectionTotal is the value the instalments add up to, rounded to the currency.
	CollectionTotal() decimal.Decimal
}

// PeriodValue is an owner's value for one billing period.
type PeriodValue struct {
	types.Period

	Amount decimal.Decimal
}
