package dto

import (
	"strconv"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func marchStart() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }

func TestDeleteEventsRequest_Validate(t *testing.T) {
	start := marchStart()

	tests := []struct {
		name    string
		req     DeleteEventsRequest
		wantErr bool
	}{
		{
			name: "customers and period only",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 27),
			},
		},
		{
			name: "period_end in the future is refused",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         time.Now().UTC().Add(-time.Hour),
				PeriodEnd:           time.Now().UTC().Add(time.Hour),
			},
			wantErr: true,
		},
		{
			name: "a period that has just closed is allowed",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         time.Now().UTC().Add(-time.Hour),
				PeriodEnd:           time.Now().UTC().Add(-time.Second),
			},
		},
		{
			name: "exactly one month is allowed",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 1, 0),
			},
		},
		{
			name: "feature narrowed",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1", "ext_2"},
				FeatureIDs:          []string{"feat_1"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 10),
			},
		},
		{
			name: "event ids with a single customer",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				EventIDs:            []string{"evt_1", "evt_2"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 10),
			},
		},
		{
			// Sub-second bounds are taken as given, with no widening and no refusal.
			name: "sub-second period bounds are accepted",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         start.Add(500 * time.Millisecond),
				PeriodEnd:           start.AddDate(0, 0, 1).Add(900 * time.Millisecond),
			},
		},
		{
			name:    "no customers",
			req:     DeleteEventsRequest{PeriodStart: start, PeriodEnd: start.AddDate(0, 0, 1)},
			wantErr: true,
		},
		{
			name: "empty customer ids only",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"", ""},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 1),
			},
			wantErr: true,
		},
		{
			name: "period start equals period end",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         start,
				PeriodEnd:           start,
			},
			wantErr: true,
		},
		{
			name: "period inverted",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         start.AddDate(0, 0, 5),
				PeriodEnd:           start,
			},
			wantErr: true,
		},
		{
			name: "period wider than one month",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 1, 1),
			},
			wantErr: true,
		},
		{
			name: "event ids with more than one customer",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1", "ext_2"},
				EventIDs:            []string{"evt_1"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 1),
			},
			wantErr: true,
		},
		{
			name: "event ids alongside feature ids",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				FeatureIDs:          []string{"feat_1"},
				EventIDs:            []string{"evt_1"},
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 1),
			},
			wantErr: true,
		},
		{
			name: "more than 100 customers",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: makeIDs("ext_", 101),
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 1),
			},
			wantErr: true,
		},
		{
			name: "more than 100 features",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				FeatureIDs:          makeIDs("feat_", 101),
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 1),
			},
			wantErr: true,
		},
		{
			name: "more than 1000 event ids",
			req: DeleteEventsRequest{
				ExternalCustomerIDs: []string{"ext_1"},
				EventIDs:            makeIDs("evt_", 1001),
				PeriodStart:         start,
				PeriodEnd:           start.AddDate(0, 0, 1),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, ierr.IsValidation(err), "expected a validation error, got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDeleteEventsRequest_Validate_DedupesAndCompacts(t *testing.T) {
	start := marchStart()
	req := DeleteEventsRequest{
		ExternalCustomerIDs: []string{"ext_1", "ext_1", "", "ext_2"},
		EventIDs:            []string{},
		PeriodStart:         start,
		PeriodEnd:           start.AddDate(0, 0, 1),
	}

	require.NoError(t, req.Validate())
	assert.Equal(t, []string{"ext_1", "ext_2"}, req.ExternalCustomerIDs)
}

func TestDeleteEventsRequest_Validate_DedupeRescuesEventIDsRule(t *testing.T) {
	// The single-customer rule is applied after dedup, so the same customer named
	// twice alongside event_ids is accepted rather than refused.
	start := marchStart()
	req := DeleteEventsRequest{
		ExternalCustomerIDs: []string{"ext_1", "ext_1"},
		EventIDs:            []string{"evt_1"},
		PeriodStart:         start,
		PeriodEnd:           start.AddDate(0, 0, 1),
	}

	require.NoError(t, req.Validate())
	assert.Len(t, req.ExternalCustomerIDs, 1)
}

func makeIDs(prefix string, n int) []string {
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, prefix+strconv.Itoa(i))
	}
	return ids
}
