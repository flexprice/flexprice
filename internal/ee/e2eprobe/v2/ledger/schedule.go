package ledger

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// TickEnd is the end of the most recent tick that has closed, so every event
// stamped inside it lies in the past.
func TickEnd(now time.Time, interval time.Duration) time.Time {
	return now.UTC().Truncate(interval)
}

// Window is a half-open range [Start, End).
type Window struct {
	Start time.Time
	End   time.Time
}

// HourWindow is the last whole hour that closed at least grace ago, so the
// aggregation pipeline has had time to land its rows.
func HourWindow(now time.Time, grace time.Duration) Window {
	end := now.Add(-grace).UTC().Truncate(time.Hour)
	return Window{Start: end.Add(-time.Hour), End: end}
}

// timestamp places an event inside the tick that ends at tickEnd.
func timestamp(ev Event, tickEnd time.Time, interval time.Duration) time.Time {
	return tickEnd.Add(-interval).Add(time.Duration(ev.At * float64(interval))).Truncate(time.Millisecond)
}

// Batch is the persona's events for one tick. Ids and timestamps derive from
// the tick, so a resend is idempotent on the server.
func Batch(p Profile, personaExtID string, tickEnd time.Time, interval time.Duration, runID string) []types.IngestEventRequest {
	out := make([]types.IngestEventRequest, 0, len(p.Events))
	index := map[string]int{}
	for _, ev := range p.Events {
		m, ok := e2eprobe.MeterByKey(ev.Meter)
		if !ok {
			continue
		}
		i := index[ev.Meter]
		index[ev.Meter]++
		props := map[string]string{"e2eprobe": "true", "e2eprobe_run_id": runID}
		for k, v := range ev.Props {
			props[k] = v
		}
		source := "e2eprobe"
		if s, ok := ev.Props["source"]; ok {
			source = s
		}
		id := fmt.Sprintf("%s-%s-%d-%d", personaExtID, ev.Meter, tickEnd.Unix(), i)
		ts := timestamp(ev, tickEnd, interval).Format(time.RFC3339Nano)
		out = append(out, types.IngestEventRequest{
			EventID:            &id,
			EventName:          m.EventName,
			ExternalCustomerID: personaExtID,
			Source:             &source,
			Timestamp:          &ts,
			Properties:         props,
		})
	}
	return out
}
