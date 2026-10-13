package analytics

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// View is a persisted, named analytics view definition.
type View struct {
	ID            string
	Name          string
	Version       int
	Definition    *ViewDefinition
	EnvironmentID string
	types.BaseModel
}

// Repository persists and retrieves analytics views.
type Repository interface {
	Create(ctx context.Context, v *View) error
	Get(ctx context.Context, id string) (*View, error)
	List(ctx context.Context) ([]*View, error)
}
