package billingterm

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Owner is an entity whose value is collected through billing terms.
type Owner interface {
	OwnerType() types.BillingTermOwnerType
	OwnerID() string

	// BilledCustomerID and BilledSubscriptionID are the owner's own customer and subscription, copied onto its
	// instalments; invoices go to the subscription's invoicing customer. The subscription is empty for an owner billed
	// without one.
	BilledCustomerID() string
	BilledSubscriptionID() string

	BillingCurrency() string

	// InstalmentDelivery says whether instalments go on the subscription invoice or on their own.
	InstalmentDelivery() types.InstalmentDelivery

	// CollectionPeriod is the span the terms collect over; End is zero when the owner has no end.
	CollectionPeriod() types.Period

	// CollectionTotal is the value the instalments add up to, rounded to the currency.
	CollectionTotal() decimal.Decimal

	// TermStart is the instant a term without a start_date or bill_on starts from, in the collection period; the term
	// starts at the period holding it.
	TermStart() time.Time
}

// PeriodValue is an owner's value for one period.
type PeriodValue struct {
	types.Period

	Amount decimal.Decimal
}

// Periods are the owner's values over its collection period, cut two ways: Billing on the subscription's billing
// periods, Anniversary in the same lengths counted from the collection start.
type Periods struct {
	Billing     []PeriodValue
	Anniversary []PeriodValue
}

// stepsFor returns the periods a recurring term steps on and their name: anniversary ones when its instalments are
// invoiced separately, billing ones when they go on the subscription invoice.
func (p Periods) stepsFor(delivery types.InstalmentDelivery) ([]PeriodValue, string) {
	if delivery == types.InstalmentDeliverySeparateInvoice {
		return p.Anniversary, "an anniversary period"
	}

	return p.Billing, "a billing period"
}
