package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// TenantInternalStatus represents the operational state of a tenant from an internal perspective.
// It is distinct from the external Status field and is managed by Flexprice operators only.
type TenantInternalStatus string

const (
	// TenantInternalStatusTrialing is the default state for new tenants.
	TenantInternalStatusTrialing TenantInternalStatus = "trialing"

	// TenantInternalStatusActive means the tenant is fully operational.
	TenantInternalStatusActive TenantInternalStatus = "active"

	// TenantInternalStatusSuspended means the tenant is blocked from accessing the API.
	TenantInternalStatusSuspended TenantInternalStatus = "suspended"
)

var TenantInternalStatusValues = []TenantInternalStatus{
	TenantInternalStatusTrialing,
	TenantInternalStatusActive,
	TenantInternalStatusSuspended,
}

func (s TenantInternalStatus) String() string {
	return string(s)
}

func (s TenantInternalStatus) Validate() error {
	if !lo.Contains(TenantInternalStatusValues, s) {
		return ierr.NewError("invalid tenant internal status").
			WithHint("Tenant internal status must be trialing, active, or suspended").
			WithReportableDetails(map[string]any{
				"status":         s,
				"allowed_values": TenantInternalStatusValues,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
