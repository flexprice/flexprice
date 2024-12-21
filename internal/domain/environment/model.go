package environment

import "github.com/flexprice/flexprice/internal/types"

type Environment struct {
	ID   string          `db:"id" json:"id"`
	Name string          `db:"name" json:"name"`
	Type EnvironmentType `db:"type" json:"type"`
	Slug string          `db:"slug" json:"slug"`

	types.BaseModel
}

type EnvironmentType string

const (
	EnvironmentDevelopment EnvironmentType = "development"
	EnvironmentTesting     EnvironmentType = "testing"
	EnvironmentProduction  EnvironmentType = "production"
)
