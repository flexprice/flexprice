package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

// BillingEntityService owns billing entities and each environment's billing entity mode.
type BillingEntityService interface {
	BillingEntityEnabler

	// WithMode runs fn in a transaction with the mode read from the database; before enabled it holds the shared enable lock.
	WithMode(ctx context.Context, fn func(txCtx context.Context, mode types.BillingEntityMode) error) error
	// ResolveBillingEntityID is the entity a new customer gets: the one asked for when enabled, else the default; nil when off.
	ResolveBillingEntityID(ctx context.Context, mode types.BillingEntityMode, billingEntityID *string) (*string, error)
	// EffectiveEntity is the customer's entity; a customer the enable backfill has not reached belongs to the default.
	EffectiveEntity(ctx context.Context, cust *customer.Customer) (*billingentity.BillingEntity, error)
	ValidateFilter(ctx context.Context, billingEntityID string) error

	Create(ctx context.Context, req dto.CreateBillingEntityRequest) (*dto.BillingEntityResponse, error)
	Get(ctx context.Context, id string) (*dto.BillingEntityResponse, error)
	List(ctx context.Context, filter *types.BillingEntityFilter) (*dto.ListBillingEntitiesResponse, error)
	Update(ctx context.Context, id string, req dto.UpdateBillingEntityRequest) (*dto.BillingEntityResponse, error)
}

type billingEntityService struct {
	ServiceParams
}

func NewBillingEntityService(params ServiceParams) BillingEntityService {
	return &billingEntityService{ServiceParams: params}
}

// billingEntityEnableLockKey is taken shared by writes that depend on the mode and exclusively by enablement.
func billingEntityEnableLockKey(ctx context.Context) string {
	return "billing_entity_enable:" + types.GetTenantID(ctx) + ":" + types.GetEnvironmentID(ctx)
}

func errBillingEntitiesNotEnabled() error {
	return ierr.WithError(billingentity.ErrNotEnabled).
		WithHint("Billing entities are not enabled in this environment").
		Mark(ierr.ErrValidation)
}

// mode reads the context environment's mode from the database; an environment without the setting is off.
func (s *billingEntityService) mode(ctx context.Context) (types.BillingEntityMode, error) {
	stored, err := s.SettingsRepo.GetByKeyUncached(ctx, types.GetEnvironmentID(ctx), types.SettingKeyBillingEntityConfig)
	if ierr.IsNotFound(err) {
		return types.BillingEntityModeOff, nil
	}
	if err != nil {
		return "", err
	}
	cfg, err := utils.ToStruct[types.BillingEntityConfig](stored.Value)
	if err != nil {
		return "", err
	}
	return cfg.Mode.Normalize(), nil
}

// setMode stores the request environment's mode; only enablement calls it.
func (s *billingEntityService) setMode(ctx context.Context, mode types.BillingEntityMode) error {
	value, err := utils.ToMap(types.BillingEntityConfig{Mode: mode})
	if err != nil {
		return err
	}
	environmentID := types.GetEnvironmentID(ctx)
	stored, err := s.SettingsRepo.GetByKeyUncached(ctx, environmentID, types.SettingKeyBillingEntityConfig)
	if ierr.IsNotFound(err) {
		return s.SettingsRepo.Create(ctx, &settings.Setting{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
			EnvironmentID: environmentID,
			Key:           types.SettingKeyBillingEntityConfig,
			Value:         value,
			BaseModel:     types.GetDefaultBaseModel(ctx),
		})
	}
	if err != nil {
		return err
	}
	stored.Value = value
	return s.SettingsRepo.Update(ctx, stored)
}

func (s *billingEntityService) requireEnabled(ctx context.Context) error {
	mode, err := s.mode(ctx)
	if err != nil {
		return err
	}
	if mode != types.BillingEntityModeEnabled {
		return errBillingEntitiesNotEnabled()
	}
	return nil
}

func (s *billingEntityService) WithMode(ctx context.Context, fn func(txCtx context.Context, mode types.BillingEntityMode) error) error {
	// Enabled is final, so the lock only guards writes against an enablement still to come.
	mode, err := s.mode(ctx)
	if err != nil {
		return err
	}
	if mode == types.BillingEntityModeEnabled {
		return s.DB.WithTx(ctx, func(txCtx context.Context) error {
			return fn(txCtx, mode)
		})
	}
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.DB.LockSharedWithWait(txCtx, postgres.LockRequest{Key: billingEntityEnableLockKey(txCtx)}); err != nil {
			return err
		}
		mode, err := s.mode(txCtx)
		if err != nil {
			return err
		}
		return fn(txCtx, mode)
	})
}

// createDefault makes the environment's default entity from the tenant and copies the tenant-level tax links to it.
func (s *billingEntityService) createDefault(ctx context.Context) (*billingentity.BillingEntity, error) {
	tenantID := types.GetTenantID(ctx)
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	invoiceConfig, err := GetSetting[types.InvoiceConfig](settingsSvc, ctx, types.SettingKeyInvoiceConfig)
	if err != nil {
		return nil, err
	}

	entity := &billingentity.BillingEntity{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_ENTITY),
		Name:                t.Name,
		IsDefault:           true,
		InvoiceNumberPrefix: invoiceConfig.InvoiceNumberPrefix,
		BillingDetails:      billingentity.BillingDetailsFromTenant(t.BillingDetails),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	if err := s.BillingEntityRepo.Create(ctx, entity); err != nil {
		return nil, err
	}

	taxService := NewTaxService(s.ServiceParams)
	filter := types.NewNoLimitTaxAssociationFilter()
	filter.EntityType = types.TaxRateEntityTypeTenant
	filter.EntityID = tenantID
	tenantLinks, err := taxService.ListTaxAssociations(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(tenantLinks.Items) > 0 {
		if err := taxService.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
			EntityType:              types.TaxRateEntityTypeBillingEntity,
			EntityID:                entity.ID,
			ExistingTaxAssociations: tenantLinks.Items,
		}); err != nil {
			return nil, err
		}
	}

	s.Logger.Info(ctx, "created default billing entity",
		"billing_entity_id", entity.ID,
		"tax_links_copied", len(tenantLinks.Items))
	return entity, nil
}

func (s *billingEntityService) ResolveBillingEntityID(ctx context.Context, mode types.BillingEntityMode, billingEntityID *string) (*string, error) {
	requestedID := lo.FromPtr(billingEntityID)
	if requestedID != "" && mode != types.BillingEntityModeEnabled {
		return nil, errBillingEntitiesNotEnabled()
	}
	if mode == types.BillingEntityModeOff {
		return nil, nil
	}
	if requestedID != "" {
		entity, err := s.entityInEnvironment(ctx, requestedID)
		if err != nil {
			return nil, err
		}
		return lo.ToPtr(entity.ID), nil
	}

	def, err := s.BillingEntityRepo.GetDefault(ctx)
	if err != nil {
		return nil, err
	}
	return lo.ToPtr(def.ID), nil
}

// entityInEnvironment returns the entity, or a validation error when it is not in this environment.
func (s *billingEntityService) entityInEnvironment(ctx context.Context, id string) (*billingentity.BillingEntity, error) {
	entity, err := s.BillingEntityRepo.Get(ctx, id)
	if ierr.IsNotFound(err) {
		return nil, ierr.NewErrorf("billing entity %s not found", id).
			WithHintf("Billing entity %s was not found in this environment", id).
			WithReportableDetails(map[string]any{"billing_entity_id": id}).
			Mark(ierr.ErrValidation)
	}
	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *billingEntityService) EffectiveEntity(ctx context.Context, cust *customer.Customer) (*billingentity.BillingEntity, error) {
	if cust == nil {
		return nil, nil
	}
	if cust.BillingEntityID != nil {
		return s.BillingEntityRepo.Get(ctx, *cust.BillingEntityID)
	}

	mode, err := s.mode(ctx)
	if err != nil {
		return nil, err
	}
	if mode == types.BillingEntityModeOff {
		return nil, nil
	}

	def, err := s.BillingEntityRepo.GetDefault(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return def, nil
}

func (s *billingEntityService) ValidateFilter(ctx context.Context, billingEntityID string) error {
	if billingEntityID == "" {
		return nil
	}
	if err := s.requireEnabled(ctx); err != nil {
		return err
	}
	_, err := s.entityInEnvironment(ctx, billingEntityID)
	return err
}

func (s *billingEntityService) Create(ctx context.Context, req dto.CreateBillingEntityRequest) (*dto.BillingEntityResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	entity := req.ToBillingEntity(ctx)
	if err := s.BillingEntityRepo.Create(ctx, entity); err != nil {
		return nil, err
	}
	return &dto.BillingEntityResponse{BillingEntity: entity}, nil
}

func (s *billingEntityService) Get(ctx context.Context, id string) (*dto.BillingEntityResponse, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}
	entity, err := s.BillingEntityRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &dto.BillingEntityResponse{BillingEntity: entity}, nil
}

func (s *billingEntityService) List(ctx context.Context, filter *types.BillingEntityFilter) (*dto.ListBillingEntitiesResponse, error) {
	if filter == nil {
		filter = types.NewBillingEntityFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	entities, err := s.BillingEntityRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	total, err := s.BillingEntityRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := lo.Map(entities, func(e *billingentity.BillingEntity, _ int) *dto.BillingEntityResponse {
		return &dto.BillingEntityResponse{BillingEntity: e}
	})
	resp := types.NewListResponse(items, total, filter.GetLimit(), filter.GetOffset())
	return &resp, nil
}

func (s *billingEntityService) Update(ctx context.Context, id string, req dto.UpdateBillingEntityRequest) (*dto.BillingEntityResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	existing, err := s.BillingEntityRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	builder := billingentity.NewBillingEntityBuilder(existing)
	if req.Name != nil {
		builder.WithName(*req.Name)
	}
	if req.InvoiceNumberPrefix != nil {
		builder.WithInvoiceNumberPrefix(*req.InvoiceNumberPrefix)
	}
	if req.BillingDetails != nil {
		builder.WithBillingDetails(*req.BillingDetails)
	}
	if req.Metadata != nil {
		builder.WithMetadata(req.Metadata)
	}

	updated := builder.Build()
	if err := s.BillingEntityRepo.Update(ctx, updated); err != nil {
		return nil, err
	}
	return &dto.BillingEntityResponse{BillingEntity: updated}, nil
}

// requireSameBillingEntity rejects combining customers of different billing entities on one invoice.
func requireSameBillingEntity(ctx context.Context, params ServiceParams, customerID, otherCustomerID string) error {
	if customerID == otherCustomerID {
		return nil
	}
	beService := &billingEntityService{ServiceParams: params}
	mode, err := beService.mode(ctx)
	if err != nil {
		return err
	}
	if mode == types.BillingEntityModeOff {
		return nil
	}
	entityIDOf := func(id string) (string, error) {
		cust, err := params.CustomerRepo.Get(ctx, id)
		if err != nil {
			return "", err
		}
		entity, err := beService.EffectiveEntity(ctx, cust)
		if err != nil || entity == nil {
			return "", err
		}
		return entity.ID, nil
	}
	entityID, err := entityIDOf(customerID)
	if err != nil {
		return err
	}
	otherEntityID, err := entityIDOf(otherCustomerID)
	if err != nil {
		return err
	}
	if entityID != otherEntityID {
		return ierr.NewError("customers belong to different billing entities").
			WithHint("Customers of different billing entities cannot be invoiced together").
			WithReportableDetails(map[string]any{"customer_id": customerID, "other_customer_id": otherCustomerID}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
