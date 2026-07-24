package handler

import (
	"context"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	cascaderules "github.com/flexprice/flexprice/internal/webhook/handler/cascade_rules"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
	"github.com/samber/lo"
)

// maxCascadeDepth is a runtime backstop against cascade loops. The static cycle check in
// NewEventCascader is the primary guarantee; this bounds damage if a loop ever slips through
// (e.g. a rule added without re-running the check in a hand-built cascader).
const maxCascadeDepth = 3

// EventCascader runs registered CascadeRules against a consumed event and publishes any
// follow-on events those rules produce back onto the webhook topic.
type EventCascader interface {
	// Cascade resolves follow-on events for the consumed event and publishes them.
	// Best-effort: publish failures are logged;
	Cascade(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent
}

type eventCascader struct {
	eventRules map[types.WebhookEventName][]cascaderules.CascadeRule
	publisher  publisher.WebhookPublisher
	logger     *logger.Logger
}

// NewEventCascader registers the given cascade rules and panics if their source→target
// edges form a cycle.
func NewEventCascader(
	logger *logger.Logger,
	pub publisher.WebhookPublisher,
	rules ...cascaderules.CascadeRule,
) EventCascader {
	if path := detectCascadeCycle(rules); path != nil {
		panic(fmt.Sprintf("webhook event cascade cycle detected: %v", path))
	}

	eventRules := make(map[types.WebhookEventName][]cascaderules.CascadeRule)
	for _, r := range rules {
		for _, src := range r.SourceEvents() {
			eventRules[src] = append(eventRules[src], r)
		}
	}

	return &eventCascader{
		eventRules: eventRules,
		publisher:  pub,
		logger:     logger,
	}
}

// Cascade publishes any follow-on events implied by the consumed event back onto the
// webhook topic, so they flow through the normal publish → consume → deliver pipeline
// (and get their own system_events row + retry semantics). Best-effort: failures are logged.
func (c *eventCascader) Cascade(ctx context.Context, event *types.WebhookEvent) []*types.WebhookEvent {
	if event == nil {
		return nil
	}

	if event.CascadeDepth >= maxCascadeDepth {
		err := ierr.NewError("webhook cascade depth exceeded").
			WithHint("A cascade chain exceeded the max depth backstop; check for a CascadeRule loop.").
			WithReportableDetails(map[string]any{
				"event_name": event.EventName,
				"depth":      event.CascadeDepth,
			}).
			Mark(ierr.ErrInternal)
		c.logger.Error(ctx, "webhook cascade depth exceeded; dropping cascaded events",
			"error", err,
			"event_name", event.EventName,
			"depth", event.CascadeDepth,
			"tenant_id", event.TenantID,
		)
		return nil
	}

	var out []*types.WebhookEvent
	for _, rule := range c.eventRules[event.EventName] {
		for _, eventToCascade := range rule.GetEventsToCascade(ctx, event) {
			if eventToCascade == nil {
				continue
			}

			if !lo.Contains(rule.TargetEvents(), eventToCascade.EventName) {
				err := ierr.NewError("cascade rule emitted undeclared target event").Mark(ierr.ErrInternal)
				c.logger.Error(ctx, "cascade rule emitted undeclared target event; dropping",
					"error", err,
					"source_event", event.EventName,
					"target_event", eventToCascade.EventName,
				)
				continue
			}

			eventToCascade.CascadeDepth = event.CascadeDepth + 1
			if c.publisher != nil {
				if err := c.publisher.PublishWebhook(ctx, eventToCascade); err != nil {
					c.logger.Error(ctx, "failed to publish cascaded webhook event",
						"error", err,
						"source_event", event.EventName,
						"target_event", eventToCascade.EventName,
						"tenant_id", eventToCascade.TenantID,
					)
					continue
				}
			}
			out = append(out, eventToCascade)
		}
	}

	return out
}

func detectCascadeCycle(rules []cascaderules.CascadeRule) []types.WebhookEventName {
	adj := make(map[types.WebhookEventName][]types.WebhookEventName)
	for _, r := range rules {
		for _, s := range r.SourceEvents() {
			adj[s] = append(adj[s], r.TargetEvents()...)
		}
	}

	const (
		unprocessed = 0
		processing  = 1
		processed   = 2
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
