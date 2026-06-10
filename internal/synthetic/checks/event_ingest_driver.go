package checks

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/synthetic"
	flexprice "github.com/flexprice/go-sdk/v2"
)

// EventIngestDriver enqueues one event per Run() call into the SDK's async
// client. Combined with a RateScheduler at N/sec, this gives the configured
// sustained ingest rate.
type EventIngestDriver struct {
	client synthetic.Client
	reg    synthetic.Registry
	seed   int64
	runID  string

	mu    sync.Mutex
	deck  *synthetic.EventDeck
	async synthetic.AsyncEventClient
}

func NewEventIngestDriver(c synthetic.Client, r synthetic.Registry, seed int64, runID string) *EventIngestDriver {
	return &EventIngestDriver{client: c, reg: r, seed: seed, runID: runID}
}

func (d *EventIngestDriver) Name() string         { return "event-ingest-driver" }
func (d *EventIngestDriver) Kind() synthetic.Kind { return synthetic.KindDriver }

func (d *EventIngestDriver) Run(ctx context.Context) error {
	if err := d.ensureDeck(); err != nil {
		return err
	}
	if d.deck == nil {
		return nil
	}
	draw := d.deck.Next()

	props := map[string]interface{}{
		"synthetic_run_id": d.runID,
	}
	for k, v := range draw.Properties {
		props[k] = v
	}

	opts := flexprice.EventOptions{
		EventName:          draw.EventName,
		ExternalCustomerID: draw.ExternalCustomerID,
		Source:             draw.Source,
		Properties:         props,
	}
	if err := d.async.EnqueueWithOptions(opts); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}

func (d *EventIngestDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.async == nil {
		return nil
	}
	if err := d.async.Flush(); err != nil {
		return err
	}
	return d.async.Close()
}

func (d *EventIngestDriver) ensureDeck() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	seeds := d.reg.Seeds()
	if len(seeds.PersistentCustomerIDs) == 0 || len(seeds.MeterIDs) == 0 {
		return nil
	}
	if d.deck != nil {
		return nil
	}
	eventNames := make([]string, 0, len(seeds.MeterIDs))
	for name := range seeds.MeterIDs {
		eventNames = append(eventNames, name)
	}
	d.deck = synthetic.NewEventDeck(synthetic.EventDeckOpts{
		Customers:       seeds.PersistentCustomerIDs,
		EventNames:      eventNames,
		OrphanEventName: "synthetic_orphan",
		OrphanFrequency: 50,
		Seed:            d.seed,
	})
	if d.async == nil {
		d.async = d.client.NewAsyncEventClient()
	}
	return nil
}
