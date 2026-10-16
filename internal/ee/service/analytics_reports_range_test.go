package service

import (
	"encoding/json"
	"testing"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListReports_MaxRangeDaysOnlyEventsVolume(t *testing.T) {
	res, err := reportsSvc(&testutil.InMemoryReportsRepo{On: true}).ListReports(billingCtx())
	require.NoError(t, err)
	for _, it := range res.Items {
		b, err := json.Marshal(it)
		require.NoError(t, err)
		m := map[string]any{}
		require.NoError(t, json.Unmarshal(b, &m))
		if it.Key == "events_volume" {
			assert.EqualValues(t, 7, m["max_range_days"])
		} else {
			assert.NotContains(t, m, "max_range_days", it.Key)
		}
	}
}

func TestRunReport_EventsVolumeMaxRange(t *testing.T) {
	svc := reportsSvc(&testutil.InMemoryReportsRepo{On: true})

	_, err := svc.RunReport(billingCtx(), "events_volume", &dto.ReportQueryRequest{Shape: "timeseries", Range: "2026-09-01..2026-09-09"})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
	assert.Contains(t, cerrors.FlattenHints(err), "Choose a range of 7 days or less")

	for _, rng := range []string{"2026-09-01..2026-09-08", "last_7_days"} {
		_, err = svc.RunReport(billingCtx(), "events_volume", &dto.ReportQueryRequest{Shape: "timeseries", Range: rng})
		require.NoError(t, err, rng)
	}

	_, err = svc.RunReport(billingCtx(), "invoice_total", &dto.ReportQueryRequest{Shape: "scalar", Range: "2026-01-01..2026-09-09"})
	require.NoError(t, err)
}
