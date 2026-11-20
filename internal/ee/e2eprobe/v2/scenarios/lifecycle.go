package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// registerFlows adds the one-shot flows; each works on a clone of the bound
// persona rather than on the persona itself.
func registerFlows(cat *scenario.Catalog) {
	flows := []registration{
		entry("lifecycle", e2eprobe.KindFlow, 30*time.Minute, func(d scenario.Deps) scenario.Scenario { return newLifecycle(d) }, e2eprobe.TraitSubscription).ingests(lifecycleEvents),
		entry("cancel", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newCancellation(d) }, e2eprobe.TraitSubscription),
		entry("line-item-change", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newLineItemChange(d) }, e2eprobe.TraitSubscription),
		entry("tax-preview", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newTaxPreview(d) }, e2eprobe.TraitTax).ingests(taxPreviewEvents),
		entry("coupon-preview", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newCouponPreview(d) }, e2eprobe.TraitCoupon),
		entry("commitment-preview", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newCommitmentPreview(d) }, e2eprobe.TraitCommitment).ingests(commitmentLegEvents),
	}
	for _, r := range flows {
		cat.Register(r.info, r.build)
	}
}

// pollWindow bounds a wait on an eventually-consistent read.
type pollWindow struct {
	every  time.Duration
	within time.Duration
}

// until retries attempt until it returns nil or the window closes; the timeout
// error wraps the last attempt's error so its class survives.
func (p pollWindow) until(ctx context.Context, attempt func() error) error {
	deadline, stop := context.WithTimeout(ctx, p.within)
	defer stop()
	for {
		last := attempt()
		if last == nil {
			return nil
		}
		select {
		case <-deadline.Done():
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("not ready within %s: %w", p.within, last)
		case <-time.After(p.every):
		}
	}
}

// awaitSubscriptionUsage polls the clone's usage read until it answers: a fresh
// subscription is readable only once the usage pipeline knows it.
func awaitSubscriptionUsage(ctx context.Context, sc *scenario.Context, p pollWindow, subID string) error {
	return p.until(ctx, func() error {
		_, err := sc.Client().Subscriptions().GetUsage(ctx, types.GetUsageBySubscriptionRequest{SubscriptionID: subID})
		return err
	})
}

// usageBatch is the usage a flow sends its clone: events of one meter, each
// worth value in the meter's field.
type usageBatch struct {
	meter  string
	events int
	value  string
}

// build stamps the events at sc.Now() with ids a retried batch would repeat.
func (b usageBatch) build(sc *scenario.Context, ext string) ([]types.IngestEventRequest, error) {
	m, ok := e2eprobe.MeterByKey(b.meter)
	if !ok {
		return nil, fmt.Errorf("unknown meter %q", b.meter)
	}
	now := sc.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	source := "e2eprobe"
	out := make([]types.IngestEventRequest, 0, b.events)
	for i := 0; i < b.events; i++ {
		id := fmt.Sprintf("%s-%s-%d-%d", ext, sc.Scenario(), now.Unix(), i)
		props := map[string]string{"e2eprobe": "true", "e2eprobe_run_id": sc.RunID()}
		if m.Field != "" {
			props[m.Field] = b.value
		}
		out = append(out, types.IngestEventRequest{
			EventID:            &id,
			EventName:          m.EventName,
			ExternalCustomerID: ext,
			Source:             &source,
			Timestamp:          &stamp,
			Properties:         props,
		})
	}
	return out, nil
}

// noteClone puts the clone's ids on every result the run reports.
func noteClone(sc *scenario.Context, eph *world.Ephemeral) {
	sc.Attr("ephemeral_external_id", eph.ExternalID())
	sc.Attr("ephemeral_customer_id", eph.CustomerID())
	sc.Attr("ephemeral_subscription_id", eph.SubscriptionID())
}

// decimalOf reads an API amount; absent or malformed reads as zero.
func decimalOf(s *string) decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// lifecycle walks a brand-new customer through first usage: clone, ingest a few
// events, read usage back, and note whether customer.created reached the inbox.
// lifecycleEvents is the handful of events a new customer sends before its usage is read back.
const lifecycleEvents = 3

type lifecycle struct {
	maxBacklog int
	batch      usageBatch
	poll       pollWindow
}

func newLifecycle(scenario.Deps) *lifecycle {
	return &lifecycle{
		maxBacklog: 5,
		batch:      usageBatch{meter: "count", events: lifecycleEvents},
		poll:       pollWindow{every: 5 * time.Second, within: 2 * time.Minute},
	}
}

func (*lifecycle) Name() string               { return "lifecycle" }
func (*lifecycle) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*lifecycle) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitSubscription} }

func (s *lifecycle) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	if n := ownClones(sc); n >= s.maxBacklog {
		return sc.Skip("backlog", fmt.Sprintf("%d lifecycle clones still pending cleanup", n))
	}
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	noteClone(sc, eph)
	batch, err := s.batch.build(sc, eph.ExternalID())
	if err != nil {
		return sc.Fail("batch", err)
	}
	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: batch}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	if err := awaitSubscriptionUsage(ctx, sc, s.poll, eph.SubscriptionID()); err != nil {
		return sc.Fail("poll_usage", err)
	}
	// Delivery is webhook-wiring's verdict; here the webhook is only recorded.
	webhook := "no"
	if len(sc.Inbox().Find(inbox.Match{EventType: "customer.created", CustomerID: eph.CustomerID()})) > 0 {
		webhook = "yes"
	}
	sc.Attr("customer_created_webhook", webhook)
	return sc.Result("lifecycle")
}

// ownClones counts the live clones this scenario created; other flows' clones
// are the janitor's business, not a reason to stop.
func ownClones(sc *scenario.Context) int {
	prefix := world.EphemeralPrefix + sc.Scenario() + "-"
	n := 0
	for _, e := range sc.World().Ephemerals().List("customer") {
		if strings.HasPrefix(e.ID, prefix) {
			n++
		}
	}
	return n
}

// units is the quantity the batch puts on its meter: events times the value each carries.
func (b usageBatch) units() decimal.Decimal {
	v := decimal.NewFromInt(1)
	if b.value != "" {
		v = decimal.RequireFromString(b.value)
	}
	return v.Mul(decimal.NewFromInt(int64(b.events)))
}
