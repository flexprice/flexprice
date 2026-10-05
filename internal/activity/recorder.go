package activity

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Entry names the action for an entity touched in the current transaction.
type Entry struct {
	EntityType string
	EntityID   string
	Action     string
	Metadata   map[string]any
	Changes    map[string]Change
	Actor      types.Actor // set by RecordAction from the context
}

// RecordAction is safe to call before or after the repository write.
// Outside a transaction it is a no-op; the hook's direct write path carries
// only the generic verb in that case.
func RecordAction(ctx context.Context, e Entry) {
	e.Actor = types.GetActor(ctx)
	CollectorFrom(ctx).Name(e)
}
