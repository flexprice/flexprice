package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduleID_Validate(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		var id ScheduleID
		err := id.Validate()
		require.Error(t, err)
	})

	t.Run("known id", func(t *testing.T) {
		t.Parallel()
		for _, id := range AllTemporalServerScheduleIDs() {
			err := id.Validate()
			require.NoError(t, err, "id=%q", id)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		t.Parallel()
		err := ScheduleID("not-a-registered-cron").Validate()
		require.Error(t, err)
	})
}

func TestAllTemporalServerScheduleIDs_covers_all_consts(t *testing.T) {
	t.Parallel()
	ids := AllTemporalServerScheduleIDs()
	seen := make(map[ScheduleID]struct{}, len(ids))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, c := range []ScheduleID{
		ScheduleIDCreditGrantProcessing,
		ScheduleIDSubscriptionAutoCancellation,
		ScheduleIDWalletCreditExpiry,
		ScheduleIDSubscriptionBillingPeriods,
		ScheduleIDSubscriptionRenewalAlerts,
		ScheduleIDSubscriptionTrialEndDue,
		ScheduleIDOutboundWebhookStaleRetry,
	} {
		_, ok := seen[c]
		require.True(t, ok, "const %q must appear in AllTemporalServerScheduleIDs", c)
	}
	require.Equal(t, 7, len(ids), "expected seven managed server schedule ids")
}
