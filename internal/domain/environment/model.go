package environment

import (
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Environment struct {
	ID   string                `db:"id" json:"id"`
	Name string                `db:"name" json:"name"`
	Type types.EnvironmentType `db:"type" json:"type"`
	Slug string                `db:"slug" json:"slug"`

	types.BaseModel
}

func FromEnt(e *ent.Environment) *Environment {
	if e == nil {
		return nil
	}

	return &Environment{
		ID:   e.ID,
		Name: e.Name,
		Type: types.EnvironmentType(e.Type),
		Slug: e.Slug,
	}
}

func (env *Environment) Validate() error {
	// Set default status if not provided
	if env.Status == "" {
		env.Status = types.StatusPublished
	}

	// Validate Environment Type
	if !isValidEnvironmentType(env.Type) {
		return fmt.Errorf("invalid environment type: %s", env.Type)
	}

	// Validate Status
	if !isValidStatus(env.Status) {
		return fmt.Errorf("invalid status: %s", env.Status)
	}

	return nil
}

func isValidEnvironmentType(envType types.EnvironmentType) bool {
	switch envType {
	case types.EnvironmentDevelopment, types.EnvironmentTesting, types.EnvironmentProduction:
		return true
	default:
		return false
	}
}

func isValidStatus(status types.Status) bool {
	switch status {
	case types.StatusPublished, types.StatusDeleted, types.StatusArchived:
		return true
	default:
		return false
	}
}
