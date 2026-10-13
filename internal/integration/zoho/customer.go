package zoho

import (
	"context"
	"fmt"
	"strings"
	"time"

	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nyaruka/phonenumbers"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

const (
	mappingKeySyncedAt         = "synced_at"
	mappingKeyContactName      = "zoho_contact_name"
	mappingKeyPrimaryEmail     = "zoho_primary_email"
	mappingKeyPrimaryContactID = "zoho_primary_contact_id"
	mappingKeyContactPersonIDs = "zoho_contact_person_ids"
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

	current, err := s.client.GetContact(ctx, mapping.ProviderEntityID)
	if err != nil {
		return err
	}

	req := buildContactRequest(flexpriceCustomer)
	req.ContactPersons = mergeContactPersons(flexpriceCustomer, current, mapping.Metadata)
	updated, err := s.client.UpdateContact(ctx, mapping.ProviderEntityID, req)
	if err != nil {
		return err
	}

	if mapping.Metadata == nil {
		mapping.Metadata = map[string]interface{}{}
	}
	mapping.Metadata[mappingKeyPrimaryContactID] = updated.PrimaryContact
	mapping.Metadata[mappingKeyContactPersonIDs] = contactPersonIDs(flexpriceCustomer, updated)
	if err := s.mappingRepo.Update(ctx, mapping); err != nil {
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
		LegalName:       c.Name,
		TraderName:      c.Name,
		ContactType:     "customer",
		CustomerSubType: "business",
	}

	req.BillingAddress = toContactAddress(
		c.Name, c.AddressLine1, c.AddressLine2, c.AddressCity,
		c.AddressState, c.AddressPostalCode, c.AddressCountry,
	)

	tax := types.TaxMetadataFromMap(c.Metadata)
	if shipping := tax.ShippingAddress(); shipping != nil {
		req.ShippingAddress = toContactAddress(
			c.Name, shipping.Line1(), shipping.Line2(), shipping.City(),
			shipping.State(), shipping.PostalCode(), shipping.Country(),
		)
	}

	req.GSTNo = tax.GSTIN()
	req.PANNo = tax.PAN()
	req.PlaceOfContact = tax.PlaceOfSupply()

	req.ContactPersons = buildContactPersons(c, nil, "")

	return req
}

// buildContactPersons sends the customer's contact persons with their known Zoho ids; without
// one marked primary, the customer's own email and contact become the primary.
// e.g. [Ravi (primary), Fin] -> Ravi primary, Fin extra; [Fin] -> customer email primary, Fin extra.
func buildContactPersons(c *customerDomain.Customer, zohoIDs map[string]string, primaryID string) []ContactPerson {
	var persons []ContactPerson
	hasPrimary := false
	for _, p := range c.ContactPersons {
		hasPrimary = hasPrimary || p.IsPrimary
		id := zohoIDs[p.ID]
		if p.IsPrimary && id == "" {
			id = primaryID
		}
		persons = append(persons, ContactPerson{
			ContactPersonID:  id,
			Salutation:       p.Salutation,
			FirstName:        p.FirstName,
			LastName:         p.LastName,
			Email:            p.Email,
			Mobile:           formatMobile(p.Mobile, c.AddressCountry),
			Designation:      p.Designation,
			Department:       p.Department,
			IsPrimaryContact: p.IsPrimary,
		})
	}
	if hasPrimary || (c.Email == "" && c.Contact == nil) {
		return persons
	}

	primary := ContactPerson{ContactPersonID: primaryID, IsPrimaryContact: true, Email: c.Email}
	if c.Contact != nil {
		primary.Mobile = formatMobile(*c.Contact, c.AddressCountry)
	}
	return append([]ContactPerson{primary}, persons...)
}

func toContactAddress(attention, line1, line2, city, state, postalCode, country string) *ContactAddress {
	if line1 == "" && line2 == "" && city == "" && state == "" && postalCode == "" && country == "" {
		return nil
	}

	addr := &ContactAddress{
		Attention:   attention,
		Address:     line1,
		Street2:     line2,
		City:        city,
		State:       state,
		Zip:         postalCode,
		Country:     country,
		CountryCode: country,
	}
	if region, err := language.ParseRegion(country); err == nil {
		if name := display.English.Regions().Name(region); name != "" {
			addr.Country = name
		}
	}
	return addr
}

// formatMobile renders a number as "+<dial code>-<national number>", the form Zoho accepts
// without a warning. Unparseable numbers are returned unchanged.
func formatMobile(number, country string) string {
	parsed, err := phonenumbers.Parse(number, strings.ToUpper(country))
	if err != nil {
		return number
	}
	return fmt.Sprintf("+%d-%s", parsed.GetCountryCode(), phonenumbers.GetNationalSignificantNumber(parsed))
}

// mergeContactPersons builds the full list for a contact update: our persons with their Zoho ids,
// plus persons added in Zoho, which are kept. Ours that are no longer listed are left out, so Zoho deletes them.
// e.g. Zoho has P (our primary), X (Fin), Y (removed in Flexprice), H (added in Zoho) -> sends P, X, H; Zoho deletes Y.
func mergeContactPersons(c *customerDomain.Customer, current *ContactResponse, metadata map[string]interface{}) []ContactPerson {
	exists := make(map[string]bool, len(current.ContactPersons))
	for _, p := range current.ContactPersons {
		exists[p.ContactPersonID] = true
	}

	zohoIDs := make(map[string]string)
	switch stored := metadata[mappingKeyContactPersonIDs].(type) {
	case map[string]string:
		for cpID, id := range stored {
			zohoIDs[cpID] = id
		}
	case map[string]interface{}:
		for cpID, id := range stored {
			zohoIDs[cpID], _ = id.(string)
		}
	}
	primaryID, _ := metadata[mappingKeyPrimaryContactID].(string)
	if primaryID == "" {
		primaryID = current.PrimaryContact
	}
	ours := map[string]bool{primaryID: true}
	for cpID, id := range zohoIDs {
		if id == primaryID {
			primaryID = ""
		}
		ours[id] = true
		if !exists[id] {
			delete(zohoIDs, cpID)
		}
	}

	if !exists[primaryID] {
		primaryID = ""
	}

	persons := buildContactPersons(c, zohoIDs, primaryID)
	for _, p := range current.ContactPersons {
		if !ours[p.ContactPersonID] {
			p.IsPrimaryContact = false
			persons = append(persons, p)
		}
	}
	return persons
}

// contactPersonIDs maps our contact person ids to the Zoho ids returned for the same person.
func contactPersonIDs(c *customerDomain.Customer, contact *ContactResponse) map[string]string {
	zohoIDs := make(map[string]string, len(contact.ContactPersons))
	for _, p := range contact.ContactPersons {
		zohoIDs[strings.ToLower(p.Email+"|"+p.FirstName+"|"+p.LastName)] = p.ContactPersonID
	}

	ids := make(map[string]string, len(c.ContactPersons))
	for _, p := range c.ContactPersons {
		if id := zohoIDs[strings.ToLower(p.Email+"|"+p.FirstName+"|"+p.LastName)]; id != "" {
			ids[p.ID] = id
		}
	}

	return ids
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
			mappingKeySyncedAt:         time.Now().UTC().Format(time.RFC3339),
			mappingKeyContactName:      contact.ContactName,
			mappingKeyPrimaryEmail:     contact.Email,
			mappingKeyPrimaryContactID: contact.PrimaryContact,
			mappingKeyContactPersonIDs: contactPersonIDs(customer, contact),
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
