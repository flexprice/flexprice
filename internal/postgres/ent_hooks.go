package postgres

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
)

// truncateTimeFields aligns every time field on every mutation to Postgres's
// microsecond precision before it is written. lib/pq rounds nanoseconds
// half-up, so an untruncated instant can read back later than the value in
// memory and break exact date comparisons. Truncating here guarantees stored
// values round-trip identical for every entity, current and future.
func truncateTimeFields(next ent.Mutator) ent.Mutator {
	return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
		for _, name := range m.Fields() {
			v, ok := m.Field(name)
			if !ok {
				continue
			}
			if t, isTime := v.(time.Time); isTime {
				if truncated := t.Truncate(time.Microsecond); !truncated.Equal(t) {
					if err := m.SetField(name, truncated); err != nil {
						return nil, err
					}
				}
			}
		}
		return next.Mutate(ctx, m)
	})
}
