package clickhouse

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/activity"
	"github.com/stretchr/testify/require"
)

func TestUniqueEntries(t *testing.T) {
	base := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	// table builds rows newest first; "b*3" is entry b stored three times.
	table := func(spec ...string) []*activity.Log {
		var rows []*activity.Log
		for i, s := range spec {
			id, copies := s, 1
			if len(s) > 2 && s[len(s)-2] == '*' {
				id, copies = s[:len(s)-2], int(s[len(s)-1]-'0')
			}
			for range copies {
				rows = append(rows, &activity.Log{ID: id, OccurredAt: base.Add(-time.Duration(i) * time.Second)})
			}
		}
		return rows
	}
	// fetcher serves the rows after the cursor, up to limit, as the ClickHouse query does.
	fetcher := func(rows []*activity.Log, limit int, calls *int) func(*activity.Cursor) ([]*activity.Log, error) {
		return func(after *activity.Cursor) ([]*activity.Log, error) {
			*calls++
			var out []*activity.Log
			for _, l := range rows {
				if after != nil && !(l.OccurredAt.Before(after.OccurredAt) || (l.OccurredAt.Equal(after.OccurredAt) && l.ID < after.ID)) {
					continue
				}
				if len(out) == limit {
					break
				}
				out = append(out, l)
			}
			return out, nil
		}
	}
	ids := func(logs []*activity.Log) []string {
		out := make([]string, len(logs))
		for i, l := range logs {
			out[i] = l.ID
		}
		return out
	}

	tests := []struct {
		name      string
		rows      []*activity.Log
		limit     int
		want      []string
		wantCalls int
	}{
		{"no copies: one query", table("a", "b", "c", "d"), 3, []string{"a", "b", "c"}, 1},
		{"copies eat into the page: refills", table("a", "b*3", "c", "d", "e"), 3, []string{"a", "b", "c"}, 2},
		{"more copies than the page: cursor skips the rest", table("a*9", "b", "c"), 3, []string{"a", "b", "c"}, 2},
		{"copies across a batch edge", table("a", "b", "c*4", "d"), 3, []string{"a", "b", "c"}, 1},
		{"rows run out after a full batch", table("a*2", "b"), 3, []string{"a", "b"}, 2},
		{"empty", nil, 3, []string{}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			got, err := uniqueEntries(tt.limit, nil, fetcher(tt.rows, tt.limit, &calls))
			require.NoError(t, err)
			require.Equal(t, tt.want, ids(got))
			require.Equal(t, tt.wantCalls, calls)
		})
	}
}
