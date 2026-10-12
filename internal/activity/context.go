package activity

import (
	"context"
	"slices"
)

type declaredKey struct{}

type declaredAction struct {
	entityID string
	action   Action
}

// WithAction declares the action for writes to one entity id in this context; declarations accumulate.
func WithAction(ctx context.Context, entityID string, action Action) context.Context {
	prev, _ := ctx.Value(declaredKey{}).([]declaredAction)
	next := append(slices.Clone(prev), declaredAction{entityID: entityID, action: action})
	return context.WithValue(ctx, declaredKey{}, next)
}

// declaredFor returns the highest-priority declared action of reg for entityID.
func declaredFor(ctx context.Context, reg *Registration, entityID string) (Action, bool) {
	declared, _ := ctx.Value(declaredKey{}).([]declaredAction)
	var best Action
	for _, d := range declared {
		if d.entityID == entityID && slices.Contains(reg.Actions, d.action) && reg.rank(d.action) > reg.rank(best) {
			best = d.action
		}
	}
	return best, best != ""
}
