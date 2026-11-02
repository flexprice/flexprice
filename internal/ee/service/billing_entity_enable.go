package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// BillingEntityEnabler runs the steps of enabling billing entities for an environment.
type BillingEntityEnabler interface {
	// Enable starts enablement and returns the resulting mode; it is idempotent.
	Enable(ctx context.Context) (types.BillingEntityMode, error)
	Status(ctx context.Context) (types.BillingEntityMode, error)
	// PrepareEnable creates the environment's default entity and moves the environment to enabling.
	PrepareEnable(ctx context.Context) error
	AssignCustomerBatch(ctx context.Context, limit int) (int, error)
	AssignInvoiceBatch(ctx context.Context, limit int) (int, error)
	// CompleteEnable copies billing details, assigns any remaining customer and invoice and moves the environment to enabled.
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
	if s.Config == nil || !s.Config.BillingEntities.Enabled {
		return "", ierr.NewError("billing entities are disabled in this deployment").
			WithHint("Billing entities are not available yet").
			Mark(ierr.ErrInvalidOperation)
	}

	if err := s.rejectExternalTaxEngine(ctx); err != nil {
		return "", err
	}

	_, err = temporalservice.GetGlobalTemporalService().ExecuteWorkflow(ctx, types.TemporalBillingEntityEnableWorkflow, models.BillingEntityEnableWorkflowInput{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
	})
	if err != nil && !ierr.IsAlreadyExists(err) {
		return "", err
	}

	s.Logger.Info(ctx, "billing entities enablement started",
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx))
	return types.BillingEntityModeEnabling, nil
}

func (s *billingEntityService) PrepareEnable(ctx context.Context) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: billingEntityEnableLockKey(txCtx)}); err != nil {
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
		def, err := s.GetOrCreateDefault(txCtx)
		if err != nil {
			return err
		}
		if err := s.assignConnectionsToEntity(txCtx, def.ID); err != nil {
			return err
		}

		if err := s.setMode(txCtx, types.BillingEntityModeEnabling); err != nil {
			return err
		}
		s.Logger.Info(txCtx, "billing entities enabling", "tenant_id", types.GetTenantID(txCtx), "environment_id", types.GetEnvironmentID(txCtx))
		return nil
	})
}

// assignConnectionsToEntity gives the entity every published non-storage connection of the environment that has no owner.
func (s *billingEntityService) assignConnectionsToEntity(ctx context.Context, entityID string) error {
	conns, err := s.ConnectionRepo.List(ctx, types.NewNoLimitConnectionFilter())
	if err != nil {
		return err
	}
	for _, conn := range conns {
		if connection.IsStorageProvider(conn.ProviderType) || conn.BillingEntityID != nil {
			continue
		}
		conn.BillingEntityID = &entityID
		conn.UpdatedAt = clock.Now()
		conn.UpdatedBy = types.GetUserID(ctx)
		if err := s.ConnectionRepo.UpdateBillingEntity(ctx, conn); err != nil {
			return err
		}
	}
	return nil
}

func (s *billingEntityService) AssignCustomerBatch(ctx context.Context, limit int) (int, error) {
	return s.CustomerRepo.AssignDefaultBillingEntities(ctx, limit)
}

func (s *billingEntityService) AssignInvoiceBatch(ctx context.Context, limit int) (int, error) {
	return s.InvoiceRepo.AssignDefaultBillingEntities(ctx, limit)
}

func (s *billingEntityService) CompleteEnable(ctx context.Context) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		tenantID := types.GetTenantID(txCtx)
		environmentID := types.GetEnvironmentID(txCtx)
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: billingEntityEnableLockKey(txCtx)}); err != nil {
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
		billingDetails := billingentity.BillingDetailsFromTenant(t.BillingDetails)

		def, err := s.GetOrCreateDefault(txCtx)
		if err != nil {
			return err
		}
		updated := billingentity.NewBillingEntityBuilder(def).WithBillingDetails(billingDetails).Build()
		if err := s.BillingEntityRepo.Update(txCtx, updated); err != nil {
			return err
		}

		remaining, err := s.CustomerRepo.AssignDefaultBillingEntities(txCtx, 0)
		if err != nil {
			return err
		}
		remainingInvoices, err := s.InvoiceRepo.AssignDefaultBillingEntities(txCtx, 0)
		if err != nil {
			return err
		}

		if err := s.setMode(txCtx, types.BillingEntityModeEnabled); err != nil {
			return err
		}
		s.Logger.Info(txCtx, "billing entities enabled",
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"customers_assigned_at_completion", remaining,
			"invoices_assigned_at_completion", remainingInvoices)
		return nil
	})
}

// rejectExternalTaxEngine fails when the environment calculates tax with an external engine.
func (s *billingEntityService) rejectExternalTaxEngine(ctx context.Context) error {
	cfg, err := taxConfig(ctx, s.ServiceParams)
	if err != nil {
		return err
	}
	if cfg.UsesExternalTaxEngine() {
		return ierr.NewErrorf("environment %s uses the %s tax engine", types.GetEnvironmentID(ctx), cfg.Provider).
			WithHintf("Billing entities cannot be used while this environment calculates tax with %s. Turn the external tax engine off first.", cfg.Provider).
			WithReportableDetails(map[string]any{"environment_id": types.GetEnvironmentID(ctx), "tax_provider": cfg.Provider}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
