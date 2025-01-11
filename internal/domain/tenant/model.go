package tenant

import (
	"time"

	"github.com/flexprice/flexprice/ent"
)

// Tenant represents an organization or group within the system.
type Tenant struct {
	ID        string    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

func FromEnt(e *ent.Tenant) *Tenant {
	if e == nil {
		return nil
	}

	return &Tenant{
		ID:        e.ID,
		Name:      e.Name,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func (t *Tenant) Validate() error {
	return nil
}
