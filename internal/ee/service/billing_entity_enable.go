package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// BillingEntityEnabler runs the steps of enabling billing entities for a tenant.
type BillingEntityEnabler interface {
	// Enable starts enablement and returns the resulting mode; it is idempotent.
	Enable(ctx context.Context) (types.BillingEntityMode, error)
	Status(ctx context.Context) (types.BillingEntityMode, error)
	// PrepareEnable creates every environment's default entity and moves the tenant to enabling.
	PrepareEnable(ctx context.Context) error
	AssignCustomerBatch(ctx context.Context, limit int) (int, error)
	// CompleteEnable copies seller details, assigns any remaining customer and moves the tenant to enabled.
	CompleteEnable(ctx context.Context) error
}

func (s *billingEntityService) Status(ctx context.Context) (types.BillingEntityMode, error) {
	return s.mode(ctx)
}

func (s *billingEntityService) Enable(ctx context.Context) (types.BillingEntityMode, error) {
	mode, err := s.mode(ctx)
	if err != nil {
		return "", err
	}
	if mode == types.BillingEntityModeEnabled {
		return mode, nil
	}

	if err := s.rejectExternalTaxEngine(ctx); err != nil {
		return "", err
	}

	_, err = temporalservice.GetGlobalTemporalService().ExecuteWorkflow(ctx, types.TemporalBillingEntityEnableWorkflow, models.BillingEntityEnableWorkflowInput{
		TenantID: types.GetTenantID(ctx),
		UserID:   types.GetUserID(ctx),
	})
	if err != nil && !ierr.IsAlreadyExists(err) {
		return "", err
	}

	s.Logger.Info(ctx, "billing entities enablement started", "tenant_id", types.GetTenantID(ctx))
	return types.BillingEntityModeEnabling, nil
}

func (s *billingEntityService) PrepareEnable(ctx context.Context) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		tenantID := types.GetTenantID(txCtx)
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: billingEntityEnableLockKey(tenantID)}); err != nil {
			return err
		}
		mode, err := s.mode(txCtx)
		if err != nil {
			return err
		}
		if mode != types.BillingEntityModeOff {
			return nil
		}

		if err := s.rejectExternalTaxEngine(txCtx); err != nil {
			return err
		}

		envs, err := s.tenantEnvironments(txCtx)
		if err != nil {
			return err
		}
		for _, env := range envs {
			if _, err := s.GetOrCreateDefault(types.SetEnvironmentID(txCtx, env.ID)); err != nil {
				return err
			}
		}

		if err := s.TenantRepo.SetBillingEntityMode(txCtx, tenantID, types.BillingEntityModeEnabling); err != nil {
			return err
		}
		s.Logger.Info(txCtx, "billing entities enabling", "tenant_id", tenantID, "environments", len(envs))
		return nil
	})
}

func (s *billingEntityService) AssignCustomerBatch(ctx context.Context, limit int) (int, error) {
	return s.CustomerRepo.AssignDefaultBillingEntities(ctx, types.GetTenantID(ctx), limit)
}

func (s *billingEntityService) CompleteEnable(ctx context.Context) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		tenantID := types.GetTenantID(txCtx)
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: billingEntityEnableLockKey(tenantID)}); err != nil {
			return err
		}
		mode, err := s.mode(txCtx)
		if err != nil {
			return err
		}
		if mode == types.BillingEntityModeEnabled {
			return nil
		}
		if mode != types.BillingEntityModeEnabling {
			return ierr.NewErrorf("billing entities cannot complete from mode %s", mode).
				WithHint("Billing entities enablement was not started").
				Mark(ierr.ErrInvalidOperation)
		}

		t, err := s.TenantRepo.GetByID(txCtx, tenantID)
		if err != nil {
			return err
		}
		sellerDetails := billingentity.SellerDetailsFromTenant(t.BillingDetails)

		envs, err := s.tenantEnvironments(txCtx)
		if err != nil {
			return err
		}
		for _, env := range envs {
			envCtx := types.SetEnvironmentID(txCtx, env.ID)
			def, err := s.GetOrCreateDefault(envCtx)
			if err != nil {
				return err
			}
			updated := billingentity.NewBillingEntityBuilder(def).WithSellerDetails(sellerDetails).Build()
			if err := s.BillingEntityRepo.Update(envCtx, updated); err != nil {
				return err
			}
		}

		remaining, err := s.CustomerRepo.AssignDefaultBillingEntities(txCtx, tenantID, 0)
		if err != nil {
			return err
		}

		if err := s.TenantRepo.SetBillingEntityMode(txCtx, tenantID, types.BillingEntityModeEnabled); err != nil {
			return err
		}
		s.Logger.Info(txCtx, "billing entities enabled",
			"tenant_id", tenantID,
			"environments", len(envs),
			"customers_assigned_at_completion", remaining)
		return nil
	})
}

func (s *billingEntityService) tenantEnvironments(ctx context.Context) ([]*environment.Environment, error) {
	return s.EnvironmentRepo.List(ctx, types.Filter{Limit: 1000})
}

// rejectExternalTaxEngine fails when any environment of the tenant calculates tax with an external engine.
func (s *billingEntityService) rejectExternalTaxEngine(ctx context.Context) error {
	envs, err := s.tenantEnvironments(ctx)
	if err != nil {
		return err
	}
	for _, env := range envs {
		cfg, err := taxConfig(types.SetEnvironmentID(ctx, env.ID), s.ServiceParams)
		if err != nil {
			return err
		}
		if cfg.UsesExternalTaxEngine() {
			return ierr.NewErrorf("environment %s uses the %s tax engine", env.ID, cfg.Provider).
				WithHintf("Billing entities cannot be used while an environment calculates tax with %s. Turn the external tax engine off first.", cfg.Provider).
				WithReportableDetails(map[string]any{"environment_id": env.ID, "tax_provider": cfg.Provider}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}
