package commitment

import (
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

var _ billingterm.Owner = (*Commitment)(nil)

func (c *Commitment) OwnerType() types.BillingTermOwnerType {
	return types.BillingTermOwnerCommitment
}

func (c *Commitment) OwnerID() string {
	return c.ID
}

func (c *Commitment) BilledCustomerID() string {
	return c.CustomerID
}

func (c *Commitment) BilledSubscriptionID() string {
	return c.SubscriptionID
}

func (c *Commitment) BillingCurrency() string {
	return c.Currency
}

func (c *Commitment) InstalmentDelivery() types.InstalmentDelivery {
	return lo.FromPtr(c.InstalmentInvoicing)
}

func (c *Commitment) CollectionPeriod() types.Period {
	return types.Period{
		Start: c.StartDate,
		End:   lo.FromPtr(c.EndDate),
	}
}

func (c *Commitment) CollectionTotal() decimal.Decimal {
	return lo.FromPtr(c.TotalAmount)
}
