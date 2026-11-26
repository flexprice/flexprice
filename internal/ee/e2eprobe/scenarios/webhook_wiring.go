package scenarios

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
)

// webhookWiring notices a region whose webhooks never reach the probe: every
// clone must produce at least a customer.created, so a long silence after
// clones were made means the tenant is not wired to this inbox.
type webhookWiring struct{}

func newWebhookWiring(scenario.Deps) *webhookWiring { return &webhookWiring{} }

func (*webhookWiring) Name() string               { return "webhook-wiring" }
func (*webhookWiring) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*webhookWiring) Requires() []e2eprobe.Trait { return nil }

func (*webhookWiring) Run(_ context.Context, sc *scenario.Context) e2eprobe.Result {
	silence := sc.Suite().Tenant.Webhooks.SilenceAfter.Duration
	uptime := sc.Now().Sub(sc.StartedAt())
	if uptime < silence {
		return sc.Skip("warmup", fmt.Sprintf("probe up for %s, silence threshold %s", uptime.Round(time.Minute), silence))
	}
	if sc.World().Ephemerals().Registered("customer") == 0 {
		return sc.Skip("warmup", "no customer created yet, so no webhook was due")
	}
	last := sc.Inbox().LastReceived()
	if !last.IsZero() {
		sc.Attr("last_webhook", last.UTC().Format(time.RFC3339))
	}
	if last.IsZero() || sc.Now().Sub(last) > silence {
		return sc.Fail("webhook_silence", fmt.Errorf("no webhook received for %s; add the tenant to webhook.tenants or register a Svix endpoint pointing at this probe's /webhook", silence))
	}
	return sc.Result("webhook_wiring")
}
