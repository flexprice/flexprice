package environment

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
)

type Environment struct {
	ID        string `db:"id" json:"id"`
	Name      string `db:"name" json:"name"`
	IsDefault bool   `db:"is_default" json:"is_default"`
	types.BaseModel
}

func NewEnvironment(name string, tenantID string) *Environment {
	return &Environment{
		ID:        uuid.New().String(),
		Name:      name,
		IsDefault: false,
		BaseModel: types.BaseModel{
			Status:    types.StatusActive,
			TenantID:  tenantID,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}
