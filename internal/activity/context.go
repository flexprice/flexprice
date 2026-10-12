package activity

import (
	"context"
	"slices"
)

type declarationsKey struct{}

type declaration struct {
	entityID string
	action   Action
}

// WithAction declares the action for writes to one entity id in this context; declarations accumulate.
func WithAction(ctx context.Context, entityID string, action Action) context.Context {
	prev, _ := ctx.Value(declarationsKey{}).([]declaration)
	next := append(slices.Clone(prev), declaration{entityID: entityID, action: action})
	return context.WithValue(ctx, declarationsKey{}, next)
}

// declaredAction returns the highest-priority declared action of reg for entityID.
func declaredAction(ctx context.Context, reg *Registration, entityID string) (Action, bool) {
	declared, _ := ctx.Value(declarationsKey{}).([]declaration)
	var best Action
	for _, d := range declared {
		if d.entityID == entityID && slices.Contains(reg.Actions, d.action) && reg.rank(d.action) > reg.rank(best) {
			best = d.action
		}
	}
	return best, best != ""
}
