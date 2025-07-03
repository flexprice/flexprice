package dto

import "time"

type TaxConfigCreateRequest struct {
	TaxRateID  string     `json:"tax_rate_id" binding:"required"`
	EntityType string     `json:"entity_type" binding:"required"`
	EntityID   string     `json:"entity_id" binding:"required"`
	Priority   int        `json:"priority" binding:"omitempty"`
	AutoApply  bool       `json:"auto_apply" binding:"omitempty"`
	ValidFrom  *time.Time `json:"valid_from" binding:"omitempty"`
	ValidTo    *time.Time `json:"valid_to" binding:"omitempty"`
}
