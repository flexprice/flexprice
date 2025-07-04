package dto

import (
	"context"
	"time"

	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxconfig"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type TaxConfigCreateRequest struct {
	TaxRateID  string `json:"tax_rate_id" binding:"required"`
	EntityType string `json:"entity_type" binding:"required"`
	EntityID   string `json:"entity_id" binding:"required"`
	Priority   int    `json:"priority" binding:"omitempty"`
	AutoApply  bool   `json:"auto_apply" binding:"omitempty"`
}

func (r *TaxConfigCreateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *TaxConfigCreateRequest) ToTaxConfig(ctx context.Context, t taxrate.TaxRate) *taxconfig.TaxConfig {
	return &taxconfig.TaxConfig{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_CONFIG),
		TaxRateID:     r.TaxRateID,
		EntityType:    r.EntityType,
		EntityID:      r.EntityID,
		Priority:      r.Priority,
		AutoApply:     r.AutoApply,
		Currency:      t.Currency,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

type TaxConfigUpdateRequest struct {
	Priority  int               `json:"priority" binding:"omitempty"`
	AutoApply bool              `json:"auto_apply" binding:"omitempty"`
	ValidFrom *time.Time        `json:"valid_from" binding:"omitempty"`
	ValidTo   *time.Time        `json:"valid_to" binding:"omitempty"`
	Metadata  map[string]string `json:"metadata" binding:"omitempty"`
}

func (r *TaxConfigUpdateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// TaxConfigResponse represents the response for tax config operations
type TaxConfigResponse struct {
	ID            string            `json:"id"`
	TaxRateID     string            `json:"tax_rate_id"`
	EntityType    string            `json:"entity_type"`
	EntityID      string            `json:"entity_id"`
	Priority      int               `json:"priority"`
	AutoApply     bool              `json:"auto_apply"`
	ValidFrom     *time.Time        `json:"valid_from,omitempty"`
	ValidTo       *time.Time        `json:"valid_to,omitempty"`
	Currency      string            `json:"currency"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	EnvironmentID string            `json:"environment_id"`
	TenantID      string            `json:"tenant_id"`
	Status        string            `json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	CreatedBy     string            `json:"created_by"`
	UpdatedBy     string            `json:"updated_by"`
}

// ToTaxConfigResponse converts a domain TaxConfig to a TaxConfigResponse
func ToTaxConfigResponse(tc *taxconfig.TaxConfig) *TaxConfigResponse {
	if tc == nil {
		return nil
	}

	return &TaxConfigResponse{
		ID:            tc.ID,
		TaxRateID:     tc.TaxRateID,
		EntityType:    tc.EntityType,
		EntityID:      tc.EntityID,
		Priority:      tc.Priority,
		AutoApply:     tc.AutoApply,
		Currency:      tc.Currency,
		Metadata:      tc.Metadata,
		EnvironmentID: tc.EnvironmentID,
		TenantID:      tc.TenantID,
		Status:        string(tc.Status),
		CreatedAt:     tc.CreatedAt,
		UpdatedAt:     tc.UpdatedAt,
		CreatedBy:     tc.CreatedBy,
		UpdatedBy:     tc.UpdatedBy,
	}
}

// ListTaxConfigsResponse represents the response for listing tax configs
type ListTaxConfigsResponse = types.ListResponse[*TaxConfigResponse]
