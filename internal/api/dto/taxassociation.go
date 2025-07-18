package dto

import (
	"context"
	"time"

	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type CreateTaxAssociationRequest struct {
	TaxRateCode string                  `json:"tax_rate_code" binding:"required"`
	EntityType  types.TaxrateEntityType `json:"entity_type" binding:"required"`
	EntityID    string                  `json:"entity_id" binding:"required"`
	Priority    int                     `json:"priority" binding:"omitempty"`
	Currency    string                  `json:"currency" binding:"omitempty"`
	AutoApply   bool                    `json:"auto_apply" binding:"omitempty"`
	Metadata    map[string]string       `json:"metadata" binding:"omitempty"`
}

func (r *CreateTaxAssociationRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Explicit validation for required fields
	if r.TaxRateCode == "" {
		return ierr.NewError("tax_rate_code is required").
			WithHint("Tax rate ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if r.EntityID == "" {
		return ierr.NewError("entity_id is required").
			WithHint("Entity ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if r.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	if err := r.EntityType.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *CreateTaxAssociationRequest) ToTaxAssociation(ctx context.Context, taxRateID string) *taxassociation.TaxAssociation {
	return &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     taxRateID,
		EntityType:    r.EntityType,
		EntityID:      r.EntityID,
		Priority:      r.Priority,
		AutoApply:     r.AutoApply,
		Currency:      r.Currency,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
		Metadata:      r.Metadata,
	}
}

type TaxAssociationUpdateRequest struct {
	Priority  *int               `json:"priority" binding:"omitempty"`
	AutoApply *bool              `json:"auto_apply" binding:"omitempty"`
	Metadata  *map[string]string `json:"metadata" binding:"omitempty"`
}

func (r *TaxAssociationUpdateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Priority != nil && *r.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type LinkTaxRateToEntityRequest struct {
	TaxRateOverrides        []*TaxRateOverride        `json:"tax_rate_overrides" binding:"omitempty"`
	ExistingTaxAssociations []*TaxAssociationResponse `json:"existing_tax_associations" binding:"omitempty"`
	EntityType              types.TaxrateEntityType   `json:"entity_type" binding:"required" default:"tenant"`
	EntityID                string                    `json:"entity_id" binding:"required"`
}

func (r *LinkTaxRateToEntityRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	return nil
}

// TaxAssociationResponse represents the response for tax association operations
type TaxAssociationResponse struct {
	ID            string                  `json:"id"`
	TaxRateID     string                  `json:"tax_rate_id"`
	EntityType    types.TaxrateEntityType `json:"entity_type"`
	EntityID      string                  `json:"entity_id"`
	Priority      int                     `json:"priority"`
	AutoApply     bool                    `json:"auto_apply"`
	ValidFrom     *time.Time              `json:"valid_from,omitempty"`
	ValidTo       *time.Time              `json:"valid_to,omitempty"`
	Currency      string                  `json:"currency"`
	Metadata      map[string]string       `json:"metadata,omitempty"`
	EnvironmentID string                  `json:"environment_id"`
	TenantID      string                  `json:"tenant_id"`
	Status        string                  `json:"status"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	CreatedBy     string                  `json:"created_by"`
	UpdatedBy     string                  `json:"updated_by"`
	TaxRate       *TaxRateResponse        `json:"tax_rate,omitempty"`
}

// ToTaxAssociationResponse converts a domain TaxConfig to a TaxConfigResponse
func ToTaxAssociationResponse(tc *taxassociation.TaxAssociation) *TaxAssociationResponse {
	if tc == nil {
		return nil
	}

	return &TaxAssociationResponse{
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

func (r *TaxAssociationResponse) WithTaxRate(taxRate *TaxRateResponse) *TaxAssociationResponse {
	r.TaxRate = taxRate
	return r
}

// ListTaxAssociationsResponse represents the response for listing tax associations
type ListTaxAssociationsResponse = types.ListResponse[*TaxAssociationResponse]

// TaxRateOverride represents a tax rate override for a specific entity
// This is used to override the tax rate for a specific entity i.e if you give `tax_overrides` in the create customer request it will link the tax rate to the customer else it will inherit the tenant tax rate,
// It links an existing tax rate to the entity
// The priority and auto apply fields are used to determine the order of the tax rates
type TaxRateOverride struct {
	TaxRateCode string            `json:"tax_rate_code" binding:"required"`
	Priority    int               `json:"priority" binding:"omitempty"`
	Currency    string            `json:"currency" binding:"required"`
	AutoApply   bool              `json:"auto_apply" binding:"omitempty" default:"true"`
	Metadata    map[string]string `json:"metadata" binding:"omitempty"`
}

func (tr *TaxRateOverride) Validate() error {
	if err := validator.ValidateRequest(tr); err != nil {
		return err
	}

	if tr.Priority < 0 {
		return ierr.NewError("priority cannot be less than 0").
			WithHint("Priority cannot be less than 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (tr *TaxRateOverride) ToTaxAssociationRequest(_ context.Context, entityID string, entityType types.TaxrateEntityType) *CreateTaxAssociationRequest {
	return &CreateTaxAssociationRequest{
		TaxRateCode: tr.TaxRateCode,
		EntityType:  entityType,
		EntityID:    entityID,
		Priority:    tr.Priority,
		AutoApply:   tr.AutoApply,
		Currency:    tr.Currency,
		Metadata:    tr.Metadata,
	}
}
