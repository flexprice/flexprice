package handler

import (
	"context"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	cascaderules "github.com/flexprice/flexprice/internal/webhook/handler/cascade_rules"
	"github.com/samber/lo"
)

// maxCascadeDepth is a runtime backstop against cascade loops. The static cycle check in
// NewEventCascader is the primary guarantee; this bounds damage if a loop ever slips through
// (e.g. a rule added without re-running the check in a hand-built cascader).
const maxCascadeDepth = 3

// EventCascader runs registered CascadeRules against a consumed event and returns any
// follow-on events those rules produce.
type EventCascader interface {
	GetCascadedEvents(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent
}

type eventCascader struct {
	eventRules map[types.WebhookEventName][]cascaderules.CascadeRule
	logger     *logger.Logger
}

// NewEventCascader registers the given cascade rules and panics if their source→target
// edges form a cycle.
func NewEventCascader(logger *logger.Logger, rules ...cascaderules.CascadeRule) EventCascader {
	if path := detectCascadeCycle(rules); path != nil {
		panic(fmt.Sprintf("webhook event cascade cycle detected: %v", path))
	}

	eventRules := make(map[types.WebhookEventName][]cascaderules.CascadeRule)
	for _, r := range rules {
		for _, src := range r.SourceEvents() {
			eventRules[src] = append(eventRules[src], r)
		}
	}

	return &eventCascader{eventRules: eventRules, logger: logger}
}

func (c *eventCascader) GetCascadedEvents(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent {
	if event == nil {
		return nil
	}

	if event.DerivationDepth >= maxCascadeDepth {
		err := ierr.NewError("webhook cascade depth exceeded").
			WithHint("A cascade chain exceeded the max depth backstop; check for a CascadeRule loop.").
			WithReportableDetails(map[string]any{
				"event_name": event.EventName,
				"depth":      event.DerivationDepth,
			}).
			Mark(ierr.ErrInternal)
		c.logger.Error(ctx, err.Error())
		return nil
	}

	var out []*types.WebhookEvent
	for _, rr := range c.eventRules[event.EventName] {
		for _, targetEvent := range rr.Cascade(ctx, event) {
			if targetEvent == nil {
				continue
			}

			if !lo.Contains(rr.TargetEvents(), targetEvent.EventName) {
				err := ierr.NewError("cascade rule emitted undeclared target event").
					WithHint("CascadeRule.Cascade returned an event name not listed in TargetEvents; drop and fix the rule.").
					WithReportableDetails(map[string]any{
						"source_event": event.EventName,
						"target_event": targetEvent.EventName,
					}).
					Mark(ierr.ErrInternal)
				c.logger.Error(ctx, err.Error())
				continue
			}
			targetEvent.DerivationDepth = event.DerivationDepth + 1
			out = append(out, targetEvent)
		}
	}
	return out
}

// detectCascadeCycle builds the directed graph of source→target edges across all cascade
// rules and returns a cycle path if one exists, else nil.
func detectCascadeCycle(rules []cascaderules.CascadeRule) []types.WebhookEventName {
	adj := make(map[types.WebhookEventName][]types.WebhookEventName)
	for _, r := range rules {
		for _, s := range r.SourceEvents() {
			adj[s] = append(adj[s], r.TargetEvents()...)
		}
	}

	const (
		unprocessed = 0 // unvisited
		processing  = 1 // on the current DFS stack
		processed   = 2 // fully explored
	)
	status := make(map[types.WebhookEventName]int)

	var stack []types.WebhookEventName
	var visit func(node types.WebhookEventName) []types.WebhookEventName
	visit = func(node types.WebhookEventName) []types.WebhookEventName {
		status[node] = processing
		stack = append(stack, node)

		for _, next := range adj[node] {
			switch status[next] {
			case processing:
				// Found a back-edge: return the cycle path for a helpful panic message.
				cycle := append([]types.WebhookEventName{}, stack...)
				return append(cycle, next)
			case unprocessed:
				if path := visit(next); path != nil {
					return path
				}
			}
		}
		stack = stack[:len(stack)-1]
		status[node] = processed
		return nil
	}

	for node := range adj {
		if status[node] == unprocessed {
			if path := visit(node); path != nil {
				return path
			}
		}
	}
	return nil
}
