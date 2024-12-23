package environment

import "github.com/flexprice/flexprice/internal/types"

type Environment struct {
	ID   string                `db:"id" json:"id"`
	Name string                `db:"name" json:"name"`
	Type types.EnvironmentType `db:"type" json:"type"`
	Slug string                `db:"slug" json:"slug"`

	types.BaseModel
}
