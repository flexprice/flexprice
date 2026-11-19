package scenarios

import (
	"context"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// ingest sends the persona's batch for the current tick, once per tick.
type ingest struct{}

func newIngest(scenario.Deps) *ingest { return &ingest{} }

func (*ingest) Name() string               { return "ingest" }
func (*ingest) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*ingest) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitEvents} }

func (*ingest) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	profile, ok := ledger.Lookup(p.Spec().Events)
	if !ok {
		return sc.Skip("profile", "persona has no events profile")
	}
	tickEnd := ledger.TickEnd(sc.Now(), sc.Tick())
	sc.Attr("tick", tickEnd.Format(time.RFC3339))
	sent := 0
	for _, t := range pendingTicks(sc, p.Name(), tickEnd) {
		batch := ledger.Batch(profile, p.ExternalID(), t, sc.Tick(), sc.RunID())
		if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: batch}); err != nil {
			return sc.Fail("ingest_bulk", err)
		}
		sc.Ledger().Record(p.Name(), t)
		sent += len(batch)
	}
	sc.Attr("events", strconv.Itoa(sent))
	return sc.Result("ingest")
}

// maxBackfill bounds how many missed ticks one run catches up on.
const maxBackfill = 6

// pendingTicks lists the ticks up to tickEnd the persona has not sent: the
// current one on a cold start, else every missed tick since the last, so a run
// that straddled a boundary does not leave a hole in the hour.
func pendingTicks(sc *scenario.Context, persona string, tickEnd time.Time) []time.Time {
	start := tickEnd
	if last, ok := sc.Ledger().Last(persona); ok {
		start = last.Add(sc.Tick())
		if floor := tickEnd.Add(-time.Duration(maxBackfill-1) * sc.Tick()); start.Before(floor) {
			start = floor
		}
	}
	var out []time.Time
	for t := start; !t.After(tickEnd); t = t.Add(sc.Tick()) {
		if !sc.Ledger().Recorded(persona, t) {
			out = append(out, t)
		}
	}
	return out
}
