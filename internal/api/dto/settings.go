package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

// SettingResponse represents a setting in API responses
type SettingResponse struct {
	*settings.Setting
}

func NewSettingResponse(s *settings.Setting) *SettingResponse {
	return &SettingResponse{Setting: s}
}

// CreateSettingRequest represents the request to create a new setting
type CreateSettingRequest struct {
	Key   types.SettingKey       `json:"key" validate:"required"`
	Value map[string]interface{} `json:"value,omitempty"`
}

func (r *CreateSettingRequest) Validate() error {
	// Check if the key is a valid setting key
	if err := r.Key.Validate(); err != nil {
		return err
	}

	if err := types.ValidateSettingValue(r.Key, r.Value); err != nil {
		return err
	}

	return nil
}

func (r *CreateSettingRequest) ToSetting(ctx context.Context) *settings.Setting {
	setting := &settings.Setting{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		BaseModel: types.GetDefaultBaseModel(ctx),
		Key:       r.Key,
		Value:     r.Value,
	}

	// For tenant_config, don't set environment_id (will be NULL in DB)
	// For other settings, use environment_id from context
	if r.Key != types.SettingKeyTenantConfig {
		setting.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	// tenant_config: EnvironmentID remains empty (zero value), repository will set to NULL

	return setting
}

type UpdateSettingRequest struct {
	Value map[string]interface{} `json:"value,omitempty"`
}

// UpdateSettingRequest represents the request to update an existing setting
func (r *UpdateSettingRequest) Validate(key types.SettingKey) error {
	if err := key.Validate(); err != nil {
		return err
	}

	if err := types.ValidateSettingValue(key, r.Value); err != nil {
		return err
	}

	return nil
}

// ResolveInvoiceConfigPaymentTerms rewrites payment_terms_type and payment_terms_days in an invoice_config value
// into the stored payment_terms; they override payment_terms when sent.
func ResolveInvoiceConfigPaymentTerms(value map[string]interface{}) error {
	sent, err := utils.ToStruct[struct {
		PaymentTerms     *types.PaymentTerms     `json:"payment_terms"`
		PaymentTermsType *types.PaymentTermsType `json:"payment_terms_type"`
		PaymentTermsDays *int                    `json:"payment_terms_days"`
	}](value)
	if err != nil {
		return err
	}
	if sent.PaymentTermsType == nil && sent.PaymentTermsDays == nil {
		return nil
	}
	terms, err := types.ResolvePaymentTerms(sent.PaymentTerms, sent.PaymentTermsType, sent.PaymentTermsDays)
	if err != nil {
		return err
	}
	delete(value, "payment_terms_type")
	delete(value, "payment_terms_days")
	value["payment_terms"] = lo.Ternary[any](*terms == "", nil, string(*terms))
	return nil
}

// AddInvoiceConfigPaymentTermsParts adds payment_terms_type and payment_terms_days, derived from payment_terms.
func AddInvoiceConfigPaymentTermsParts(value map[string]interface{}) {
	terms, _ := value["payment_terms"].(string)
	termsType, days := types.PaymentTerms(terms).Parts()
	if termsType != nil {
		value["payment_terms_type"] = *termsType
	}
	if days != nil {
		value["payment_terms_days"] = *days
	}
}
