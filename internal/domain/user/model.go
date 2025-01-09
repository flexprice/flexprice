package user

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

type User struct {
	ID    string `db:"id" json:"id"`
	Email string `db:"email" json:"email"`
	types.BaseModel
}

func NewUser(email, tenantID string) *User {
	return &User{
		ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
		Email: email,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}
