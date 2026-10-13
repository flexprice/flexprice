package service

import (
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	domainActivity "github.com/flexprice/flexprice/internal/domain/activity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func hintOf(err error) string {
	hints := errors.GetAllHints(err)
	if len(hints) == 0 {
		return ""
	}
	return hints[0]
}

func TestResolveWindow(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	day := 24 * time.Hour

	tests := []struct {
		name        string
		start, end  *time.Time
		wantFrom    time.Time
		wantTo      time.Time
		wantErrHint string
	}{
		{"none", nil, nil, now.Add(-30 * day), now, ""},
		{"start only", at(-10 * day), nil, now.Add(-10 * day), now, ""},
		{"end only", nil, at(-5 * day), now.Add(-35 * day), now.Add(-5 * day), ""},
		{"both", at(-20 * day), at(-10 * day), now.Add(-20 * day), now.Add(-10 * day), ""},
		{"window of 31 days", at(-31 * day), at(0), time.Time{}, time.Time{}, "at most 30 days"},
		{"start older than a year", at(-400 * day), at(-380 * day), time.Time{}, time.Time{}, "2025-10-11"},
		{"end only older than a year", nil, at(-400 * day), time.Time{}, time.Time{}, "last year"},
		{"end not after start", at(-5 * day), at(-5 * day), time.Time{}, time.Time{}, "end_time must be after start_time"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := resolveWindow(now, tt.start, tt.end)
			if tt.wantErrHint != "" {
				require.Error(t, err)
				require.True(t, ierr.IsValidation(err))
				require.Contains(t, hintOf(err), tt.wantErrHint)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantFrom, from)
			require.Equal(t, tt.wantTo, to)
		})
	}
}

func TestCursor_RoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 11, 9, 14, 2, 481_000_000, time.UTC)
	for _, c := range []domainActivity.Cursor{{OccurredAt: at, ID: "act_A"}, {OccurredAt: at, ID: "act_B"}} {
		got, err := decodeCursor(encodeCursor(c))
		require.NoError(t, err)
		require.True(t, c.OccurredAt.Equal(got.OccurredAt))
		require.Equal(t, c.ID, got.ID)
	}

	_, err := decodeCursor("%%%")
	require.True(t, ierr.IsValidation(err))
	require.Contains(t, hintOf(err), "The cursor is invalid")
}

func TestBuildQuery_Validation(t *testing.T) {
	svc := &activityService{registry: activity.Registrations()}
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		req      dto.ListActivityRequest
		wantHint string
	}{
		{"unknown entity type", dto.ListActivityRequest{EntityType: "coupon"}, "Unknown entity_type"},
		{"unknown action lists allowed", dto.ListActivityRequest{Actions: []string{"invoice.pay"}}, "Allowed invoice actions: invoice.deleted, invoice.created, invoice.voided, invoice.finalized, invoice.refunded, invoice.paid, invoice.updated."},
		{"valid", dto.ListActivityRequest{EntityType: "invoice", EntityID: "inv_1", Actions: []string{"invoice.voided"}, Limit: 100}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.buildQuery(&tt.req, now)
			if tt.wantHint == "" {
				require.NoError(t, err)
				return
			}
			require.True(t, ierr.IsValidation(err))
			require.Contains(t, hintOf(err), tt.wantHint)
		})
	}
}
