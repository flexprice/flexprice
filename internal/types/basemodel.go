package types

import (
	"context"
	"time"
)

// BaseModel is a base model for all domain models that need to be persisted in the database
// Any changes to this model should be reflected in the database schema by running migrations
type BaseModel struct {
	TenantID  string    `db:"tenant_id" json:"tenant_id"`
	Status    Status    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy string    `db:"created_by" json:"created_by"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
}

func GetDefaultBaseModel(ctx context.Context) BaseModel {
	now := time.Now().UTC()
	return BaseModel{
		TenantID:  GetTenantID(ctx),
		Status:    StatusPublished,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: GetUserID(ctx),
		UpdatedBy: GetUserID(ctx),
	}
}
