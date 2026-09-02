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
	// SyncCustomerUpdate pushes a customer's current details onto its Zoho contact.
	// It is a no-op when the customer has never been synced — contacts are created
	// lazily on first invoice, not on customer update.
	SyncCustomerUpdate(ctx context.Context, flexpriceCustomer *customerDomain.Customer) error
}

type CustomerService struct {
	client       ZohoClient
	customerRepo customerDomain.Repository
	mappingRepo  entityintegrationmapping.Repository
	logger       *logger.Logger
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
	mapping, err := s.findMapping(ctx, flexpriceCustomer.ID)
	if err == nil && mapping != nil {
		return mapping.ProviderEntityID, nil
	}

	if flexpriceCustomer.Email != "" {
		existing, err := s.client.QueryContactByEmail(ctx, flexpriceCustomer.Email)
		if err == nil && existing != nil && existing.ContactID != "" {
			_ = s.createCustomerMapping(ctx, flexpriceCustomer, existing)
			return existing.ContactID, nil
		}
	}

	contact, err := s.client.CreateContact(ctx, buildContactRequest(flexpriceCustomer))
	if err != nil {
		return "", err
	}

	if contact == nil || contact.ContactID == "" {
		return "", ierr.NewError("invalid Zoho contact response").Mark(ierr.ErrInternal)
	}

	_ = s.createCustomerMapping(ctx, flexpriceCustomer, contact)
	return contact.ContactID, nil
}

func (s *CustomerService) SyncCustomerUpdate(ctx context.Context, flexpriceCustomer *customerDomain.Customer) error {
	if flexpriceCustomer == nil {
		return nil
	}

	mapping, err := s.findMapping(ctx, flexpriceCustomer.ID)
	if err != nil {
		return err
	}
	if mapping == nil || mapping.ProviderEntityID == "" {
		return nil
	}

	if _, err := s.client.UpdateContact(ctx, mapping.ProviderEntityID, buildContactRequest(flexpriceCustomer)); err != nil {
		return err
	}

	s.logger.Info(ctx, "updated Zoho contact from customer update",
		"customer_id", flexpriceCustomer.ID,
		"zoho_contact_id", mapping.ProviderEntityID)
	return nil
}

func (s *CustomerService) findMapping(ctx context.Context, customerID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.EntityID = customerID
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}

	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	return mappings[0], nil
}

// buildContactRequest maps a FlexPrice customer onto the Zoho contact payload.
func buildContactRequest(c *customerDomain.Customer) *ContactCreateRequest {
	req := &ContactCreateRequest{
		ContactName:     c.Name,
		CompanyName:     c.Name,
		ContactType:     "customer",
		CustomerSubType: "business",
	}

	req.BillingAddress = toContactAddress(
		c.AddressLine1, c.AddressLine2, c.AddressCity,
		c.AddressState, c.AddressPostalCode, c.AddressCountry,
	)

	tax := types.TaxMetadataFromMap(c.Metadata)
	if shipping := tax.ShippingAddress(); shipping != nil {
		req.ShippingAddress = toContactAddress(
			shipping.Line1(), shipping.Line2(), shipping.City(),
			shipping.State(), shipping.PostalCode(), shipping.Country(),
		)
	}

	req.GSTNo = tax.GSTIN()
	req.PANNo = tax.PAN()
	req.PlaceOfContact = tax.PlaceOfSupply()

	if c.Email != "" || c.Contact != nil {
		person := ContactPerson{IsPrimaryContact: true, Email: c.Email}
		if c.Contact != nil {
			person.Phone = *c.Contact
		}
		req.ContactPersons = []ContactPerson{person}
	}

	return req
}

// toContactAddress folds line2 into Zoho's single street field, which is what the
// invoice PDF path does too — Zoho's ContactAddress has no verified second line key.
func toContactAddress(line1, line2, city, state, postalCode, country string) *ContactAddress {
	street := line1
	if line2 != "" {
		if street != "" {
			street += "\n"
		}
		street += line2
	}

	if street == "" && city == "" && state == "" && postalCode == "" && country == "" {
		return nil
	}

	return &ContactAddress{
		Address: street,
		City:    city,
		State:   state,
		Zip:     postalCode,
		Country: country,
	}
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
		s.logger.Error(ctx, "failed to create Zoho customer mapping",
			"customer_id", customer.ID,
			"zoho_contact_id", contact.ContactID,
			"error", err)
	}
	return nil
}
