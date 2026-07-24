package cascaderules

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// CascadeRule reacts to a fixed set of source events and produces follow-on ("cascaded")
// events, constrained to a declared set of target events. Rules are registered into an
// EventCascader, which statically validates that the source→target edges form no cycle.
//
// A proxy event (one that has its own webhook, e.g. entitlement.*) uses a CascadeRule to
// produce follow-on events (e.g. subscription.updated). Surfaces with no proxy event of
// their own emit their subscription.updated directly at the mutation site — there is
// nothing to cascade from.
type CascadeRule interface {
	// SourceEvents lists the event names this rule reacts to.
	SourceEvents() []types.WebhookEventName
	// TargetEvents lists every event name this rule may emit. Cascade output is validated
	// against this set, and the set feeds the static cycle check.
	TargetEvents() []types.WebhookEventName
	// Cascade returns zero or more follow-on events for a matched source event. Best-effort:
	// it never returns an error, so it cannot fail delivery of the source event.
	Cascade(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent
}
