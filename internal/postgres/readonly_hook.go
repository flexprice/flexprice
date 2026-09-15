package postgres

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/ent"
)

// ErrReadOnly is returned for every mutation while the DB write-freeze is on.
var ErrReadOnly = errors.New("database is in read-only mode (cutover in progress)")

// newReadOnlyHook rejects all Create/Update/Delete mutations when enabled.
// enabled is captured at construction; toggling requires a process restart.
func newReadOnlyHook(enabled bool) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if enabled {
				return nil, ErrReadOnly
			}
			return next.Mutate(ctx, m)
		})
	}
}
