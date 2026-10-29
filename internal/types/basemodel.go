package types

import (
	"context"
	"time"
)

// BaseModel is a base model for all domain models that need to be persisted in the database
// Any changes to this model should be reflected in the database schema by running migrations
type BaseModel struct {
	TenantID  string    `json:"tenant_id"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at" activity:"drop"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by" activity:"drop"`
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
