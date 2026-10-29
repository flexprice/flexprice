package billingterms

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResolveFallback(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "collectible", true: "paused owner"}[blocked], func(t *testing.T) {
			ctx, service, db := newCore(t)
			state, err := service.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}})
			require.NoError(t, err)
			item := state.Items()[0]
			if blocked {
				db.owner.CanCollect, db.owner.CanEdit = false, false
			}
			request := FallbackRequest{Owner: db.owner.Owner, ItemID: item.ID, AsOf: db.owner.EndDate.Add(-time.Second)}
			require.NoError(t, service.ResolveFallback(ctx, request))
			require.Nil(t, db.items[item.ID].BillOn)
			request.AsOf = db.owner.EndDate
			for range 2 {
				require.NoError(t, service.ResolveFallback(ctx, request))
				require.True(t, db.items[item.ID].BillOn.Equal(db.owner.EndDate))
				require.Equal(t, types.BillingScheduleScheduled, db.items[item.ID].ScheduleStatus)
				require.Nil(t, db.items[item.ID].InvoiceID)
			}
		})
	}
}
