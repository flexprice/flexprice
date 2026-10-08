package admin

import (
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// UpdateTenantConfigRequest changes a tenant's tenant_config setting, as PUT /v1/settings/tenant_config does for the caller's own tenant.
type UpdateTenantConfigRequest struct {
	TenantID string            `json:"tenant_id"`
	Value    TenantConfigValue `json:"value"`
}

// TenantConfigValue holds the limits to change. A limit left out keeps its current value.
type TenantConfigValue struct {
	Production  *int `json:"production"`
	Development *int `json:"development"`
	MaxUsers    *int `json:"max_users"`
}

type TenantConfigResponse struct {
	TenantID    string `json:"tenant_id"`
	Production  int    `json:"production"`
	Development int    `json:"development"`
	MaxUsers    int    `json:"max_users"`
}

func (r *UpdateTenantConfigRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide a request body").
			Mark(ierr.ErrValidation)
	}

	r.TenantID = strings.TrimSpace(r.TenantID)

	if r.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Provide the id of the tenant whose settings to change").
			Mark(ierr.ErrValidation)
	}

	v := r.Value
	if v.Production == nil && v.Development == nil && v.MaxUsers == nil {
		return ierr.NewError("value is empty").
			WithHint("Set production, development or max_users in value").
			Mark(ierr.ErrValidation)
	}
	if v.Production != nil && *v.Production < 0 {
		return ierr.NewError("production limit cannot be negative").
			WithHint("production must be 0 or more").
			Mark(ierr.ErrValidation)
	}
	if v.Development != nil && *v.Development < 0 {
		return ierr.NewError("development limit cannot be negative").
			WithHint("development must be 0 or more").
			Mark(ierr.ErrValidation)
	}
	if v.MaxUsers != nil && *v.MaxUsers < 1 {
		return ierr.NewError("max_users must be at least 1").
			WithHint("max_users must be 1 or more").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToSettingValue returns only the limits the request sets, keyed as tenant_config stores them.
func (r *UpdateTenantConfigRequest) ToSettingValue() map[string]interface{} {
	value := map[string]interface{}{}
	if r == nil {
		return value
	}
	if r.Value.Production != nil {
		value["production"] = *r.Value.Production
	}
	if r.Value.Development != nil {
		value["development"] = *r.Value.Development
	}
	if r.Value.MaxUsers != nil {
		value["max_users"] = *r.Value.MaxUsers
	}
	return value
}

func NewTenantConfigResponse(tenantID string, cfg types.TenantConfig) *TenantConfigResponse {
	return &TenantConfigResponse{
		TenantID:    tenantID,
		Production:  cfg.Production,
		Development: cfg.Development,
		MaxUsers:    cfg.MaxUsers,
	}
}
