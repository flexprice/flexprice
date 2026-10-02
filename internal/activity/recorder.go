package activity

import "context"

// Entry names the action for an entity touched in the current transaction.
type Entry struct {
	EntityType string
	EntityID   string
	Action     string
	Metadata   map[string]any
	Changes    map[string]Change
}

// RecordAction is safe to call before or after the repository write.
// Outside a transaction it is a no-op; the hook's direct write path carries
// only the generic verb in that case.
func RecordAction(ctx context.Context, e Entry) {
	CollectorFrom(ctx).Name(e)
}
