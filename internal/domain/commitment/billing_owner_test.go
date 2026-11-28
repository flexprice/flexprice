package commitment

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCommitment_Owner(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)

	t.Run("maps the commitment", func(t *testing.T) {
		c := &Commitment{
			ID:                  "cmt_1",
			CustomerID:          "cust_1",
			SubscriptionID:      "subs_1",
			Currency:            "USD",
			StartDate:           start,
			EndDate:             lo.ToPtr(end),
			TotalAmount:         lo.ToPtr(decimal.NewFromInt(1200)),
			InstalmentInvoicing: lo.ToPtr(types.InstalmentDeliverySeparateInvoice),
		}

		require.Equal(t, types.BillingTermOwnerCommitment, c.OwnerType())
		require.Equal(t, "cmt_1", c.OwnerID())
		require.Equal(t, "cust_1", c.BilledCustomerID())
		require.Equal(t, "subs_1", c.BilledSubscriptionID())
		require.Equal(t, "USD", c.BillingCurrency())
		require.Equal(t, types.InstalmentDeliverySeparateInvoice, c.InstalmentDelivery())
		require.Equal(t, types.Period{Start: start, End: end}, c.CollectionPeriod())
		require.True(t, c.CollectionTotal().Equal(decimal.NewFromInt(1200)))
	})

	t.Run("nil fields return zero values", func(t *testing.T) {
		c := &Commitment{StartDate: start}

		require.Equal(t, types.InstalmentDelivery(""), c.InstalmentDelivery())
		require.Equal(t, types.Period{Start: start}, c.CollectionPeriod())
		require.True(t, c.CollectionTotal().IsZero())
	})
}
