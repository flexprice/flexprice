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
	if sc.Ledger().Recorded(p.Name(), tickEnd) {
		return sc.Result("ingest")
	}
	batch := ledger.Batch(profile, p.ExternalID(), tickEnd, sc.Tick(), sc.RunID())
	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: batch}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	sc.Ledger().Record(p.Name(), tickEnd)
	sc.Attr("events", strconv.Itoa(len(batch)))
	return sc.Result("ingest")
}
