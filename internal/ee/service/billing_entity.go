package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// BillingEntityService owns billing entities and the tenant's billing entity mode.
type BillingEntityService interface {
	BillingEntityEnabler

	// WithMode runs fn in a transaction holding the shared enable lock, with the mode read from the database.
	WithMode(ctx context.Context, fn func(txCtx context.Context, mode types.BillingEntityMode) error) error
	GetOrCreateDefault(ctx context.Context) (*billingentity.BillingEntity, error)
	ResolveForCustomerCreate(ctx context.Context, mode types.BillingEntityMode, requested *string) (*string, error)
	// EffectiveEntity is the customer's entity; a customer the enable backfill has not reached belongs to the default.
	EffectiveEntity(ctx context.Context, cust *customer.Customer) (*billingentity.BillingEntity, error)
	CustomerServesIntegrations(ctx context.Context, customerID string) (bool, error)
	InvoiceServesIntegrations(ctx context.Context, invoiceID string) (bool, error)
	SubscriptionServesIntegrations(ctx context.Context, subscriptionID string) (bool, error)
	RequireCustomerIntegrations(ctx context.Context, customerID string) error
	ValidateFilter(ctx context.Context, billingEntityID string) error
	EntityIDsForCustomers(ctx context.Context, customerIDs []string) (map[string]*string, error)
	// EnsureDefaultForEnvironment gives a new environment its default entity once the tenant is enabling or enabled.
	EnsureDefaultForEnvironment(ctx context.Context, environmentID string) error

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
func billingEntityEnableLockKey(tenantID string) string {
	return "billing_entity_enable:" + tenantID
}

func errBillingEntitiesNotEnabled() error {
	return ierr.NewError("billing entities are not enabled").
		WithHint("Billing entities are not enabled for this tenant").
		Mark(ierr.ErrBillingEntitiesNotEnabled)
}

func (s *billingEntityService) mode(ctx context.Context) (types.BillingEntityMode, error) {
	return s.TenantRepo.GetBillingEntityMode(ctx, types.GetTenantID(ctx))
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
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.DB.LockSharedWithWait(txCtx, postgres.LockRequest{Key: billingEntityEnableLockKey(types.GetTenantID(txCtx))}); err != nil {
			return err
		}
		mode, err := s.mode(txCtx)
		if err != nil {
			return err
		}
		return fn(txCtx, mode)
	})
}

func (s *billingEntityService) GetOrCreateDefault(ctx context.Context) (*billingentity.BillingEntity, error) {
	existing, err := s.BillingEntityRepo.GetDefault(ctx)
	if err == nil {
		return existing, nil
	}
	if !ierr.IsNotFound(err) {
		return nil, err
	}

	var result *billingentity.BillingEntity
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		key := "billing_entity_default:" + types.GetTenantID(txCtx) + ":" + types.GetEnvironmentID(txCtx)
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: key}); err != nil {
			return err
		}

		existing, err := s.BillingEntityRepo.GetDefault(txCtx)
		if err == nil {
			result = existing
			return nil
		}
		if !ierr.IsNotFound(err) {
			return err
		}

		created, err := s.createDefault(txCtx)
		if err != nil {
			return err
		}
		result = created
		return nil
	})
	return result, err
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
		LegalName:           t.Name,
		IsDefault:           true,
		InvoiceNumberPrefix: invoiceConfig.InvoiceNumberPrefix,
		SellerDetails:       billingentity.SellerDetailsFromTenant(t.BillingDetails),
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

func (s *billingEntityService) ResolveForCustomerCreate(ctx context.Context, mode types.BillingEntityMode, requested *string) (*string, error) {
	if lo.FromPtr(requested) == "" {
		requested = nil
	}

	switch mode {
	case types.BillingEntityModeOff:
		if requested != nil {
			return nil, errBillingEntitiesNotEnabled()
		}
		return nil, nil
	case types.BillingEntityModeEnabling:
		if requested != nil {
			return nil, errBillingEntitiesNotEnabled()
		}
	case types.BillingEntityModeEnabled:
		if requested != nil {
			entity, err := s.entityInEnvironment(ctx, *requested)
			if err != nil {
				return nil, err
			}
			return lo.ToPtr(entity.ID), nil
		}
	}

	def, err := s.GetOrCreateDefault(ctx)
	if err != nil {
		return nil, err
	}
	return lo.ToPtr(def.ID), nil
}

// entityInEnvironment returns the entity, or a validation error when it is not in this environment.
func (s *billingEntityService) entityInEnvironment(ctx context.Context, id string) (*billingentity.BillingEntity, error) {
	entity, err := s.BillingEntityRepo.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Billing entity %s was not found in this environment", id).
				WithReportableDetails(map[string]any{"billing_entity_id": id}).
				Mark(ierr.ErrValidation)
		}
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

func (s *billingEntityService) CustomerServesIntegrations(ctx context.Context, customerID string) (bool, error) {
	cust, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return false, err
	}
	entity, err := s.EffectiveEntity(ctx, cust)
	if err != nil {
		return false, err
	}
	return entity == nil || entity.IsDefault, nil
}

func (s *billingEntityService) InvoiceServesIntegrations(ctx context.Context, invoiceID string) (bool, error) {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return false, err
	}
	return s.CustomerServesIntegrations(ctx, inv.CustomerID)
}

func (s *billingEntityService) SubscriptionServesIntegrations(ctx context.Context, subscriptionID string) (bool, error) {
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return false, err
	}
	return s.CustomerServesIntegrations(ctx, sub.CustomerID)
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

func (s *billingEntityService) EntityIDsForCustomers(ctx context.Context, customerIDs []string) (map[string]*string, error) {
	return s.CustomerRepo.BillingEntityIDsByCustomerIDs(ctx, lo.Uniq(customerIDs))
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
	if req.LegalName != nil {
		builder.WithLegalName(*req.LegalName)
	}
	if req.InvoiceNumberPrefix != nil {
		builder.WithInvoiceNumberPrefix(*req.InvoiceNumberPrefix)
	}
	if req.SellerDetails != nil {
		builder.WithSellerDetails(*req.SellerDetails)
	}
	if req.TaxIdentifiers != nil {
		builder.WithTaxIdentifiers(*req.TaxIdentifiers)
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

func (s *billingEntityService) EnsureDefaultForEnvironment(ctx context.Context, environmentID string) error {
	mode, err := s.mode(ctx)
	if err != nil {
		return err
	}
	if mode == types.BillingEntityModeOff {
		return nil
	}
	_, err = s.GetOrCreateDefault(types.SetEnvironmentID(ctx, environmentID))
	return err
}

// requireIntegrationsForCustomer rejects an integration request for a customer of a non-default billing entity.
func requireIntegrationsForCustomer(ctx context.Context, params ServiceParams, customerID string) error {
	served, err := NewBillingEntityService(params).CustomerServesIntegrations(ctx, customerID)
	if err != nil {
		return err
	}
	if !served {
		return ierr.NewError("no integration for this customer's billing entity").
			WithHint("No integration is available for this customer's billing entity").
			WithReportableDetails(map[string]any{"customer_id": customerID}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// skipIntegrationsForCustomer reports, and logs, whether an automatic integration step must skip this customer.
func skipIntegrationsForCustomer(ctx context.Context, params ServiceParams, customerID, step string) (bool, error) {
	served, err := NewBillingEntityService(params).CustomerServesIntegrations(ctx, customerID)
	if err != nil {
		return false, err
	}
	if !served {
		params.Logger.Info(ctx, "skipping integration for non-default billing entity",
			"customer_id", customerID,
			"step", step)
	}
	return !served, nil
}

func (s *billingEntityService) RequireCustomerIntegrations(ctx context.Context, customerID string) error {
	return requireIntegrationsForCustomer(ctx, s.ServiceParams, customerID)
}

// requireSameBillingEntity rejects combining customers of different billing entities on one invoice.
func requireSameBillingEntity(ctx context.Context, params ServiceParams, customerID, otherCustomerID string) error {
	if customerID == otherCustomerID {
		return nil
	}
	beService := NewBillingEntityService(params)
	entityIDs := make([]string, 0, 2)
	for _, id := range []string{customerID, otherCustomerID} {
		cust, err := params.CustomerRepo.Get(ctx, id)
		if err != nil {
			return err
		}
		entity, err := beService.EffectiveEntity(ctx, cust)
		if err != nil {
			return err
		}
		entityID := ""
		if entity != nil {
			entityID = entity.ID
		}
		entityIDs = append(entityIDs, entityID)
	}
	if entityIDs[0] != entityIDs[1] {
		return ierr.NewError("customers belong to different billing entities").
			WithHint("Customers of different billing entities cannot be invoiced together").
			WithReportableDetails(map[string]any{"customer_id": customerID, "other_customer_id": otherCustomerID}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
