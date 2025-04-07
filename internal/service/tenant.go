package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
)

type TenantService interface {
	CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error)
	GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error)
	AssignTenantToUser(ctx context.Context, req dto.AssignTenantRequest) error
	GetAllTenants(ctx context.Context) ([]*dto.TenantResponse, error)
	UpdateTenant(ctx context.Context, id string, req dto.UpdateTenantRequest) (*dto.TenantResponse, error)
	GetBillingUsage(ctx context.Context) (*dto.TenantBillingUsage, error)
}

type tenantService struct {
	ServiceParams
}

func NewTenantService(
	params ServiceParams,
) TenantService {
	return &tenantService{
		ServiceParams: params,
	}
}

func (s *tenantService) CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	newTenant := req.ToTenant(ctx)

	if err := s.TenantRepo.Create(ctx, newTenant); err != nil {
		return nil, err
	}

	// Create a customer in the billing tenant for this new tenant
	if err := s.createTenantAsBillingCustomer(ctx, newTenant); err != nil {
		// Log error but don't fail tenant creation
		s.Logger.Errorw("Failed to create billing customer for tenant",
			"tenant_id", newTenant.ID,
			"error", err)
	}

	return dto.NewTenantResponse(newTenant), nil
}

// createTenantAsBillingCustomer creates a customer in the billing tenant using the tenant details
func (s *tenantService) createTenantAsBillingCustomer(ctx context.Context, t *tenant.Tenant) error {
	if s.Config.Billing.TenantID == "" {
		return nil
	}

	// Create a context with the billing tenant ID
	billingCtx := getBillingContext(ctx, s.Config)
	// Create customer request using tenant details
	createCustomerReq := dto.CreateCustomerRequest{
		Name:       t.Name,
		ExternalID: t.ID, // Use tenant ID as external ID for customer lookup
		Email:      t.BillingDetails.Email,
		// Map other fields from tenant as needed
		AddressLine1:      t.BillingDetails.Address.Line1,
		AddressLine2:      t.BillingDetails.Address.Line2,
		AddressCity:       t.BillingDetails.Address.City,
		AddressState:      t.BillingDetails.Address.State,
		AddressCountry:    t.BillingDetails.Address.Country,
		AddressPostalCode: t.BillingDetails.Address.PostalCode,
		Metadata: map[string]string{
			"tenant_id":     t.ID,
			"tenant_status": string(t.Status),
		},
	}

	// Create customer in billing tenant
	customerService := NewCustomerService(s.CustomerRepo, s.SubscriptionRepo, s.InvoiceRepo, s.WalletRepo)
	_, err := customerService.CreateCustomer(billingCtx, createCustomerReq)
	return err
}

func (s *tenantService) GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error) {
	t, err := s.TenantRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.NewTenantResponse(t), nil
}

func (s *tenantService) AssignTenantToUser(ctx context.Context, req dto.AssignTenantRequest) error {
	if err := req.Validate(ctx); err != nil {
		return err
	}

	// Verify tenant exists
	_, err := s.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return err
	}

	authProvider := auth.NewProvider(s.Config)

	// Assign tenant to user using auth provider
	if err := authProvider.AssignUserToTenant(ctx, req.UserID, req.TenantID); err != nil {
		return err
	}

	return nil
}

func (s *tenantService) GetAllTenants(ctx context.Context) ([]*dto.TenantResponse, error) {
	tenants, err := s.TenantRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	tenantResponses := make([]*dto.TenantResponse, 0, len(tenants))
	for _, t := range tenants {
		tenantResponses = append(tenantResponses, dto.NewTenantResponse(t))
	}

	return tenantResponses, nil
}

func (s *tenantService) UpdateTenant(ctx context.Context, id string, req dto.UpdateTenantRequest) (*dto.TenantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing tenant
	existingTenant, err := s.TenantRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var billingDetails tenant.TenantBillingDetails
	if req.BillingDetails != nil {
		billingDetails = tenant.TenantBillingDetails{
			Email:     req.BillingDetails.Email,
			HelpEmail: req.BillingDetails.HelpEmail,
			Phone:     req.BillingDetails.Phone,
			Address: tenant.TenantAddress{
				Line1:      req.BillingDetails.Address.Line1,
				Line2:      req.BillingDetails.Address.Line2,
				City:       req.BillingDetails.Address.City,
				State:      req.BillingDetails.Address.State,
				PostalCode: req.BillingDetails.Address.PostalCode,
				Country:    req.BillingDetails.Address.Country,
			},
		}
	}
	existingTenant.BillingDetails = billingDetails

	// Update the timestamp
	existingTenant.UpdatedAt = time.Now()

	// Save the updated tenant
	if err := s.TenantRepo.Update(ctx, existingTenant); err != nil {
		return nil, err
	}

	return dto.NewTenantResponse(existingTenant), nil
}

func (s *tenantService) GetBillingUsage(ctx context.Context) (*dto.TenantBillingUsage, error) {
	billingService := NewBillingService(s.ServiceParams)
	customerService := NewCustomerService(s.CustomerRepo, s.SubscriptionRepo, s.InvoiceRepo, s.WalletRepo)
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	response := &dto.TenantBillingUsage{}

	if s.Config.Billing.TenantID == "" {
		return response, nil
	}

	billingCtx := getBillingContext(ctx, s.Config)

	customer, err := customerService.GetCustomerByLookupKey(billingCtx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}

	usage, err := billingService.GetCustomerUsageSummary(billingCtx, customer.ID, &dto.GetCustomerUsageSummaryRequest{})
	if err != nil {
		return nil, err
	}

	subscriptions, err := subscriptionService.ListSubscriptions(billingCtx, &types.SubscriptionFilter{
		CustomerID: customer.ID,
	})
	if err != nil {
		return nil, err
	}

	response.Usage = usage
	response.Subscriptions = subscriptions.Items
	return response, nil
}

func getBillingContext(ctx context.Context, config *config.Configuration) context.Context {
	billingCtx := context.WithValue(ctx, types.CtxTenantID, config.Billing.TenantID)
	billingCtx = context.WithValue(billingCtx, types.CtxEnvironmentID, config.Billing.EnvironmentID)
	return billingCtx
}
