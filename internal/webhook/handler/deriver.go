package handler

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// maxDerivationDepth is a runtime backstop against derivation loops. The static cycle check in
// NewEventDeriver is the primary guarantee; this bounds damage if a loop ever slips through
// (e.g. a filter added without re-running the check in a hand-built deriver).
const maxDerivationDepth = 3

// Filter reacts to a fixed set of source events and derives follow-on ("derived") events,
// constrained to a declared set of target events. Filters are registered into an EventDeriver,
// which statically validates that the source→target edges form no cycle.
//
// A proxy event (one that has its own webhook, e.g. entitlement.*) uses a Filter to fan out to
// derived events (e.g. subscription.updated). Surfaces with no proxy event of their own emit
// their subscription.updated directly at the mutation site — there is nothing to derive from.
type Filter interface {
	// SourceEvents lists the event names this filter reacts to.
	SourceEvents() []types.WebhookEventName
	// TargetEvents lists every event name this filter may emit. Derive output is validated
	// against this set, and the set feeds the static cycle check.
	TargetEvents() []types.WebhookEventName
	// Derive returns zero or more follow-on events for a matched source event. Best-effort:
	// it never returns an error, so it cannot fail delivery of the source event.
	Derive(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent
}

// EventDeriver fans a consumed event out to any derived events its registered filters produce.
type EventDeriver interface {
	Derive(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent
}

type registeredFilter struct {
	filter  Filter
	targets map[types.WebhookEventName]bool
}

type eventDeriver struct {
	bySource map[types.WebhookEventName][]registeredFilter
	logger   *logger.Logger
}

// NewEventDeriver registers the given filters and panics if their source→target edges form a
// cycle (fail-fast at fx wiring / boot, never in production traffic).
func NewEventDeriver(logger *logger.Logger, filters ...Filter) EventDeriver {
	if path := detectDerivationCycle(filters); path != nil {
		panic(fmt.Sprintf("webhook event derivation cycle detected: %v", path))
	}

	bySource := make(map[types.WebhookEventName][]registeredFilter)
	for _, f := range filters {
		targets := make(map[types.WebhookEventName]bool, len(f.TargetEvents()))
		for _, t := range f.TargetEvents() {
			targets[t] = true
		}
		rf := registeredFilter{filter: f, targets: targets}
		for _, src := range f.SourceEvents() {
			bySource[src] = append(bySource[src], rf)
		}
	}

	return &eventDeriver{bySource: bySource, logger: logger}
}

func (d *eventDeriver) Derive(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent {
	if event == nil {
		return nil
	}

	if event.DerivationDepth >= maxDerivationDepth {
		d.logger.Error(ctx, "webhook derivation depth exceeded; dropping derived events",
			"event_name", event.EventName,
			"depth", event.DerivationDepth,
			"tenant_id", event.TenantID,
		)
		return nil
	}

	var out []*types.WebhookEvent
	for _, rf := range d.bySource[event.EventName] {
		for _, derived := range rf.filter.Derive(ctx, event) {
			if derived == nil {
				continue
			}
			// Runtime gate: a filter may only emit events it declared as targets.
			if !rf.targets[derived.EventName] {
				d.logger.Error(ctx, "filter emitted undeclared target event; dropping",
					"source_event", event.EventName,
					"derived_event", derived.EventName,
				)
				continue
			}
			derived.DerivationDepth = event.DerivationDepth + 1
			out = append(out, derived)
		}
	}
	return out
}

// detectDerivationCycle builds the directed graph of source→target edges across all filters and
// returns a cycle path if one exists, else nil.
func detectDerivationCycle(filters []Filter) []types.WebhookEventName {
	adj := make(map[types.WebhookEventName][]types.WebhookEventName)
	for _, f := range filters {
		for _, s := range f.SourceEvents() {
			adj[s] = append(adj[s], f.TargetEvents()...)
		}
	}

	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[types.WebhookEventName]int)

	var stack []types.WebhookEventName
	var visit func(node types.WebhookEventName) []types.WebhookEventName
	visit = func(node types.WebhookEventName) []types.WebhookEventName {
		color[node] = gray
		stack = append(stack, node)
		for _, next := range adj[node] {
			switch color[next] {
			case gray:
				// Found a back-edge: return the cycle path for a helpful panic message.
				cycle := append([]types.WebhookEventName{}, stack...)
				return append(cycle, next)
			case white:
				if path := visit(next); path != nil {
					return path
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
		return nil
	}

	for node := range adj {
		if color[node] == white {
			if path := visit(node); path != nil {
				return path
			}
		}
	}
	return nil
}

// entitlementFilter derives subscription.updated from subscription-scoped
// entitlement.* events. The entitlement is resolved fresh (works for soft-deleted rows too),
// so no data needs to be carried on the source event.
type entitlementFilter struct {
	entitlementService service.EntitlementService
	logger             *logger.Logger
}

// NewEntitlementSubscriptionFilter builds the entitlement→subscription.updated filter.
func NewEntitlementFilter(entitlementService service.EntitlementService, logger *logger.Logger) Filter {
	return &entitlementFilter{
		entitlementService: entitlementService,
		logger:             logger,
	}
}

func (f *entitlementFilter) SourceEvents() []types.WebhookEventName {
	return []types.WebhookEventName{
		types.WebhookEventEntitlementCreated,
		types.WebhookEventEntitlementUpdated,
		types.WebhookEventEntitlementDeleted,
	}
}

func (f *entitlementFilter) TargetEvents() []types.WebhookEventName {
	return []types.WebhookEventName{types.WebhookEventSubscriptionUpdated}
}

func (f *entitlementFilter) Derive(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent {
	if event.EntityID == "" {
		return nil
	}

	ent, err := f.entitlementService.GetEntitlement(ctx, event.EntityID)
	if err != nil {
		f.logger.Error(ctx, "webhook derivation: failed to resolve entitlement",
			"error", err,
			"entitlement_id", event.EntityID,
			"event_name", event.EventName,
		)
		return nil
	}

	var derivedEvents []*types.WebhookEvent
	switch ent.EntityType {
	case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
		derived, err := types.NewWebhookEvent(types.WebhookEventSubscriptionUpdated).
			WithIdentityFrom(event).
			WithEntity(types.SystemEntityTypeSubscription, ent.EntityID).
			WithPayload(webhookDto.InternalSubscriptionEvent{
				SubscriptionID: ent.EntityID,
				TenantID:       event.TenantID,
			}).
			Build()
		if err != nil {
			f.logger.Error(ctx, "webhook derivation: failed to build subscription.updated event",
				"error", err,
				"subscription_id", ent.EntityID,
			)
			return nil
		}

		derivedEvents = append(derivedEvents, derived)
	}

	return derivedEvents
}
