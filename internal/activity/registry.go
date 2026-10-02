package activity

import (
	"context"
	"database/sql"

	"github.com/flexprice/flexprice/internal/types"
)

// Querier is the read side of the mutation's connection.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type SnapshotMode int

const (
	SnapshotFull SnapshotMode = iota
	SnapshotNone
)

type FieldDisplay struct {
	Label  string
	Format string // money | date | enum | boolean | text
}

// Definition declares how one ent type is logged.
type Definition struct {
	EntType      string
	EntityType   types.SystemEntityType
	Table        string
	LabelFields  []string
	ParentFields []string // roll-up columns (customer_id, subscription_id, CustomerLookup keys) fetched with old values on update
	IgnoreFields []string
	RedactFields []string
	SnapshotMode SnapshotMode
	CustomerID   func(fields map[string]any) string
	// CustomerLookup resolves the customer through a parent row when the entity
	// has no customer_id column. Runs inside the mutation's transaction.
	CustomerLookup func(ctx context.Context, q Querier, fields map[string]any) (string, error)
	Label          func(fields map[string]any) string
	Actions        map[string]string // action -> template with {actor} {entity}
	FieldLabels    map[string]FieldDisplay
}

type Registry struct {
	byEnt    map[string]Definition
	byEntity map[types.SystemEntityType]Definition
}

func NewRegistry(defs ...Definition) *Registry {
	r := &Registry{byEnt: map[string]Definition{}, byEntity: map[types.SystemEntityType]Definition{}}
	for _, d := range defs {
		r.byEnt[d.EntType] = d
		r.byEntity[d.EntityType] = d
	}
	return r
}

func (r *Registry) ByEntType(name string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	d, ok := r.byEnt[name]
	return d, ok
}

func (r *Registry) ByEntityType(t types.SystemEntityType) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	d, ok := r.byEntity[t]
	return d, ok
}
