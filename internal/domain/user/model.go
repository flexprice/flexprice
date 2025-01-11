package user

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type User struct {
	ID    string `db:"id" json:"id"`
	Email string `db:"email" json:"email"`
	types.BaseModel
}

func fromEnt(e *ent.User) *User {
	if e == nil {
		return nil
	}

	return &User{
		ID:    e.ID,
		Email: e.Email,
	}
}

func (u *User) Validate() error {
	return nil
}
