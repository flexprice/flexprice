package checks

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	flexprice "github.com/flexprice/go-sdk/v2"
)

// EventIngestDriver enqueues one event per Run() call into the SDK's async
// client. Combined with a RateScheduler at N/sec, this gives the configured
// sustained ingest rate.
type EventIngestDriver struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	seed   int64
	runID  string

	mu    sync.Mutex
	deck  *e2eprobe.EventDeck
	async e2eprobe.AsyncEventClient
}

func NewEventIngestDriver(c e2eprobe.Client, r e2eprobe.Registry, seed int64, runID string) *EventIngestDriver {
	return &EventIngestDriver{client: c, reg: r, seed: seed, runID: runID}
}

func (d *EventIngestDriver) Name() string         { return "event-ingest-driver" }
func (d *EventIngestDriver) Kind() e2eprobe.Kind { return e2eprobe.KindDriver }

func (d *EventIngestDriver) Run(ctx context.Context) error {
	if err := d.ensureDeck(); err != nil {
		return err
	}
	d.mu.Lock()
	async := d.async
	deck := d.deck
	d.mu.Unlock()
	if deck == nil || async == nil {
		return nil
	}
	draw := deck.Next()

	props := map[string]interface{}{
		"e2eprobe_run_id": d.runID,
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
	if err := async.EnqueueWithOptions(opts); err != nil {
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
	d.deck = e2eprobe.NewEventDeck(e2eprobe.EventDeckOpts{
		Customers:       seeds.PersistentCustomerIDs,
		EventNames:      eventNames,
		OrphanEventName: "e2eprobe_orphan",
		OrphanFrequency: 50,
		Seed:            d.seed,
	})
	if d.async == nil {
		d.async = d.client.NewAsyncEventClient()
	}
	return nil
}
