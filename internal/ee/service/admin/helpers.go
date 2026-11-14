package admin

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// environmentContext is ctx scoped to the environment, once the tenant exists and owns it.
func environmentContext(ctx context.Context, s service.ServiceParams, tenantID, environmentID string) (context.Context, error) {
	if _, err := s.TenantRepo.GetByID(ctx, tenantID); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, tenantID)
	env, err := s.EnvironmentRepo.Get(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	if env.TenantID != tenantID {
		return nil, ierr.NewError("environment not found").
			WithHint("The environment does not belong to this tenant").
			Mark(ierr.ErrNotFound)
	}

	return types.SetEnvironmentID(ctx, environmentID), nil
}

// settingContext scopes ctx for a setting: the tenant for tenant-wide keys, otherwise the tenant's environment.
func settingContext(ctx context.Context, s service.ServiceParams, tenantID, environmentID string, key types.SettingKey) (context.Context, error) {
	if key == types.SettingKeyTenantConfig || key == types.SettingKeySAMLConfig {
		if _, err := s.TenantRepo.GetByID(ctx, tenantID); err != nil {
			return nil, err
		}
		return types.SetTenantID(ctx, tenantID), nil
	}
	if environmentID == "" {
		return nil, ierr.NewErrorf("environment_id is required for %s", key).
			WithHint("Provide the environment the setting belongs to").
			Mark(ierr.ErrValidation)
	}
	return environmentContext(ctx, s, tenantID, environmentID)
}
