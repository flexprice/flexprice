package activity

import (
	"context"
	"time"
)

// Repository stores and reads activity entries; every call is scoped to the tenant and environment in ctx.
type Repository interface {
	Insert(ctx context.Context, logs []*Log) error
	List(ctx context.Context, q *ListQuery) ([]*Log, error)
	// States returns the stored state per entry id.
	States(ctx context.Context, ids []string, from, to time.Time) (map[string]string, error)
	Get(ctx context.Context, id string, from, to time.Time) (*Log, error)
}
