package dto

import (
	"time"
)

// ManualSyncRequest payload
// swagger:model
// ---
// required:
//   - entity_id
//   - entity_type
//   - time_from
//   - time_to
type ManualSyncRequest struct {
	EntityID   string    `json:"entity_id" binding:"required"`
	EntityType string    `json:"entity_type" binding:"required"` // customer, subscription etc.
	MeterID    *string   `json:"meter_id,omitempty"`
	TimeFrom   time.Time `json:"time_from" binding:"required"` // RFC3339
	TimeTo     time.Time `json:"time_to" binding:"required"`
	ForceRerun bool      `json:"force_rerun"`
}

// RetryFailedBatchesRequest payload
// swagger:model
// ---
type RetryFailedBatchesRequest struct {
	BatchIDs    []string `json:"batch_ids,omitempty"`
	MaxRetryAge string   `json:"max_retry_age,omitempty"` // duration string e.g. 24h
	EntityID    *string  `json:"entity_id,omitempty"`
	MeterID     *string  `json:"meter_id,omitempty"`
}
