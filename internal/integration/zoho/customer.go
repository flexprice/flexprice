package zoho

import (
	"context"
	"time"

	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type ZohoCustomerService interface {
	GetOrCreateZohoCustomer(ctx context.Context, flexpriceCustomer *customerDomain.Customer) (string, error)
}

type CustomerService struct {
	client      ZohoClient
	customerRepo customerDomain.Repository
	mappingRepo entityintegrationmapping.Repository
	logger      *logger.Logger
}

func NewCustomerService(client ZohoClient, customerRepo customerDomain.Repository, mappingRepo entityintegrationmapping.Repository, logger *logger.Logger) ZohoCustomerService {
	return &CustomerService{
		client:       client,
		customerRepo: customerRepo,
		mappingRepo:  mappingRepo,
		logger:       logger,
	}
}

func (s *CustomerService) GetOrCreateZohoCustomer(ctx context.Context, flexpriceCustomer *customerDomain.Customer) (string, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.EntityID = flexpriceCustomer.ID
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}

	mappings, err := s.mappingRepo.List(ctx, filter)
	if err == nil && len(mappings) > 0 {
		return mappings[0].ProviderEntityID, nil
	}

	if flexpriceCustomer.Email != "" {
		existing, err := s.client.QueryContactByEmail(ctx, flexpriceCustomer.Email)
		if err == nil && existing != nil && existing.ContactID != "" {
			_ = s.createCustomerMapping(ctx, flexpriceCustomer, existing)
			return existing.ContactID, nil
		}
	}

	req := &ContactCreateRequest{
		ContactName:     flexpriceCustomer.Name,
		CompanyName:     flexpriceCustomer.Name,
		ContactType:     "customer",
		CustomerSubType: "business",
	}
	if flexpriceCustomer.AddressLine1 != "" || flexpriceCustomer.AddressCity != "" {
		req.BillingAddress = &ContactAddress{
			Address: flexpriceCustomer.AddressLine1,
			City:    flexpriceCustomer.AddressCity,
			State:   flexpriceCustomer.AddressState,
			Zip:     flexpriceCustomer.AddressPostalCode,
			Country: flexpriceCustomer.AddressCountry,
		}
	}
	if flexpriceCustomer.Email != "" {
		req.ContactPersons = []ContactPerson{
			{Email: flexpriceCustomer.Email, IsPrimaryContact: true},
		}
	}

	contact, err := s.client.CreateContact(ctx, req)
	if err != nil {
		return "", err
	}
	if contact == nil || contact.ContactID == "" {
		return "", ierr.NewError("invalid Zoho contact response").Mark(ierr.ErrInternal)
	}
	_ = s.createCustomerMapping(ctx, flexpriceCustomer, contact)
	return contact.ContactID, nil
}

func (s *CustomerService) createCustomerMapping(ctx context.Context, customer *customerDomain.Customer, contact *ContactResponse) error {
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customer.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderZohoBooks),
		ProviderEntityID: contact.ContactID,
		EnvironmentID:    customer.EnvironmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata: map[string]interface{}{
			"synced_at":          time.Now().UTC().Format(time.RFC3339),
			"zoho_contact_name":  contact.ContactName,
			"zoho_primary_email": contact.Email,
		},
	}
	mapping.TenantID = customer.TenantID
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		s.logger.Warnw("failed to create Zoho customer mapping",
			"customer_id", customer.ID,
			"zoho_contact_id", contact.ContactID,
			"error", err)
	}
	return nil
}
