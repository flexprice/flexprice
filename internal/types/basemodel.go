package types

import "time"

// BaseModel is a base model for all domain models that need to be persisted in the database
// Any changes to this model should be reflected in the database schema by running migrations
type BaseModel struct {
	Status    Status    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy string    `db:"created_by" json:"created_by"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
}
