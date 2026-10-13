package zoho

import (
	"context"
	"encoding/json"
	"testing"

	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	syncGSTIN = "27AAMCM4148E1ZD"
	syncPAN   = "AAMCM4148E"
)

// fakeContactClient captures the contact payloads the sync builds.
type fakeContactClient struct {
	ZohoClient
	createReq    *ContactCreateRequest
	createCalls  int
	updateReq    *ContactUpdateRequest
	updateID     string
	updateCalls  int
	updateErr    error
	queryByEmail *ContactResponse
	createResp   *ContactResponse
	current      *ContactResponse
	updateResp   *ContactResponse
}

func (f *fakeContactClient) GetContact(_ context.Context, contactID string) (*ContactResponse, error) {
	if f.current != nil {
		return f.current, nil
	}
	return &ContactResponse{ContactID: contactID}, nil
}

func (f *fakeContactClient) QueryContactByEmail(_ context.Context, _ string) (*ContactResponse, error) {
	return f.queryByEmail, nil
}

func (f *fakeContactClient) CreateContact(_ context.Context, req *ContactCreateRequest) (*ContactResponse, error) {
	f.createCalls++
	f.createReq = req
	if f.createResp != nil {
		return f.createResp, nil
	}
	return &ContactResponse{ContactID: "zoho_contact_1", ContactName: req.ContactName}, nil
}

func (f *fakeContactClient) UpdateContact(_ context.Context, contactID string, req *ContactUpdateRequest) (*ContactResponse, error) {
	f.updateCalls++
	f.updateID = contactID
	f.updateReq = req
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.updateResp != nil {
		return f.updateResp, nil
	}
	return &ContactResponse{ContactID: contactID}, nil
}

// writableMappingRepo records Create/Update so fingerprint persistence can be asserted.
type writableMappingRepo struct {
	entityintegrationmapping.Repository
	mappings    []*entityintegrationmapping.EntityIntegrationMapping
	updateCalls int
	createCalls int
}

func (f *writableMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (f *writableMappingRepo) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	f.createCalls++
	f.mappings = append(f.mappings, m)
	return nil
}

func (f *writableMappingRepo) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	f.updateCalls++
	return nil
}

func newTestCustomerService(client ZohoClient, repo entityintegrationmapping.Repository) *CustomerService {
	return &CustomerService{
		client:      client,
		mappingRepo: repo,
		logger:      logger.NewNoopLogger(),
	}
}

func indianCustomer() *customerDomain.Customer {
	phone := "9311916570"
	return &customerDomain.Customer{
		ID:                "cust_1",
		Name:              "MCGILL FOODS PRIVATE LIMITED",
		Email:             "billing@mcgill.example",
		Contact:           &phone,
		AddressLine1:      "Ground Floor, B1-002-B",
		AddressLine2:      "Boomerang Building, Chandivali Farm Road",
		AddressCity:       "Mumbai",
		AddressState:      "Maharashtra",
		AddressPostalCode: "400072",
		AddressCountry:    "IN",
		Metadata: map[string]string{
			types.MetadataKeyGSTIN:                          syncGSTIN,
			types.MetadataKeyShippingPrefix + "line1":       "Ground Floor, B1-002-B",
			types.MetadataKeyShippingPrefix + "city":        "Mumbai",
			types.MetadataKeyShippingPrefix + "state":       "Maharashtra",
			types.MetadataKeyShippingPrefix + "postal_code": "400072",
			types.MetadataKeyShippingPrefix + "country":     "IN",
		},
	}
}

func TestCreateContactCarriesGSTFields(t *testing.T) {
	client := &fakeContactClient{}
	svc := newTestCustomerService(client, &writableMappingRepo{})

	id, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer())
	require.NoError(t, err)
	assert.Equal(t, "zoho_contact_1", id)
	require.NotNil(t, client.createReq)

	req := client.createReq
	assert.Equal(t, syncGSTIN, req.GSTNo)
	assert.Equal(t, syncPAN, req.PANNo, "PAN should be derived from the GSTIN")
	assert.Equal(t, "27", req.PlaceOfContact, "place of contact should be the GSTIN state code")

	require.NotNil(t, req.ShippingAddress)
	assert.Equal(t, "Mumbai", req.ShippingAddress.City)
	assert.Equal(t, "400072", req.ShippingAddress.Zip)

	require.NotNil(t, req.BillingAddress)
	assert.Equal(t, "Boomerang Building, Chandivali Farm Road", req.BillingAddress.Street2)
	assert.Equal(t, "MCGILL FOODS PRIVATE LIMITED", req.ShippingAddress.Attention)
	assert.Equal(t, "MCGILL FOODS PRIVATE LIMITED", req.LegalName)
	assert.Equal(t, "MCGILL FOODS PRIVATE LIMITED", req.TraderName)

	require.Len(t, req.ContactPersons, 1)
	assert.Equal(t, "+91-9311916570", req.ContactPersons[0].Mobile)
	assert.Equal(t, "billing@mcgill.example", req.ContactPersons[0].Email)
}

func TestNonIndianCustomerOmitsGSTFields(t *testing.T) {
	client := &fakeContactClient{}
	svc := newTestCustomerService(client, &writableMappingRepo{})

	c := &customerDomain.Customer{
		ID:             "cust_2",
		Name:           "Acme Inc",
		AddressCountry: "US",
	}
	_, err := svc.GetOrCreateZohoCustomer(context.Background(), c)
	require.NoError(t, err)

	req := client.createReq
	assert.Empty(t, req.GSTNo)
	assert.Empty(t, req.PANNo)
	assert.Empty(t, req.PlaceOfContact)
	assert.Nil(t, req.ShippingAddress)
}

func TestCreateFlowNeverUpdates(t *testing.T) {
	t.Run("already-mapped customer returns the mapping without touching Zoho", func(t *testing.T) {
		client := &fakeContactClient{}
		repo := &writableMappingRepo{
			mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
				EntityID:         "cust_1",
				ProviderEntityID: "zoho_contact_1",
			}},
		}
		svc := newTestCustomerService(client, repo)

		id, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer())
		require.NoError(t, err)
		assert.Equal(t, "zoho_contact_1", id)
		assert.Equal(t, 0, client.createCalls)
		assert.Equal(t, 0, client.updateCalls, "the create path must never push updates")
	})

	t.Run("contact adopted by email is mapped, not updated", func(t *testing.T) {
		client := &fakeContactClient{
			queryByEmail: &ContactResponse{ContactID: "zoho_existing_9"},
		}
		repo := &writableMappingRepo{}
		svc := newTestCustomerService(client, repo)

		id, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer())
		require.NoError(t, err)
		assert.Equal(t, "zoho_existing_9", id)
		assert.Equal(t, 0, client.updateCalls)
		assert.Equal(t, 1, repo.createCalls)
	})
}

func TestSyncCustomerUpdate(t *testing.T) {
	t.Run("pushes current details onto the mapped contact", func(t *testing.T) {
		client := &fakeContactClient{}
		repo := &writableMappingRepo{
			mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
				EntityID:         "cust_1",
				ProviderEntityID: "zoho_contact_1",
			}},
		}
		svc := newTestCustomerService(client, repo)

		require.NoError(t, svc.SyncCustomerUpdate(context.Background(), indianCustomer()))

		require.Equal(t, 1, client.updateCalls)
		assert.Equal(t, "zoho_contact_1", client.updateID)
		assert.Equal(t, syncGSTIN, client.updateReq.GSTNo)
		assert.Equal(t, syncPAN, client.updateReq.PANNo)
		assert.Equal(t, "27", client.updateReq.PlaceOfContact)
		assert.Equal(t, 0, client.createCalls, "update must never create a contact")
	})

	t.Run("no-op for a customer never synced to Zoho", func(t *testing.T) {
		client := &fakeContactClient{}
		svc := newTestCustomerService(client, &writableMappingRepo{})

		require.NoError(t, svc.SyncCustomerUpdate(context.Background(), indianCustomer()))

		assert.Equal(t, 0, client.updateCalls,
			"contacts are created lazily at first invoice, not on customer update")
		assert.Equal(t, 0, client.createCalls)
	})

	t.Run("surfaces the Zoho error to the caller", func(t *testing.T) {
		client := &fakeContactClient{updateErr: assert.AnError}
		repo := &writableMappingRepo{
			mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
				EntityID:         "cust_1",
				ProviderEntityID: "zoho_contact_1",
			}},
		}
		svc := newTestCustomerService(client, repo)

		err := svc.SyncCustomerUpdate(context.Background(), indianCustomer())
		require.Error(t, err, "the caller decides whether a push failure is fatal")
	})

	t.Run("nil customer is a no-op", func(t *testing.T) {
		client := &fakeContactClient{}
		svc := newTestCustomerService(client, &writableMappingRepo{})
		require.NoError(t, svc.SyncCustomerUpdate(context.Background(), nil))
		assert.Equal(t, 0, client.updateCalls)
	})
}

func TestGSTTreatmentIsNotSent(t *testing.T) {
	raw, err := json.Marshal(buildContactRequest(indianCustomer()))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "gst_treatment",
		"gst_treatment is optional in Zoho and cannot express SEZ/deemed-export; we omit it")
	assert.Contains(t, string(raw), "gst_no")
}

func TestToContactAddress(t *testing.T) {
	tests := []struct {
		name    string
		country string
		want    *ContactAddress
	}{
		{
			name: "indian address gets the country name and code", country: "IN",
			want: &ContactAddress{Attention: "Acme", Address: "l1", Street2: "l2", City: "c", State: "s", Zip: "z", Country: "India", CountryCode: "IN"},
		},
		{
			name: "non-indian address", country: "US",
			want: &ContactAddress{Attention: "Acme", Address: "l1", Street2: "l2", City: "c", State: "s", Zip: "z", Country: "United States", CountryCode: "US"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toContactAddress("Acme", "l1", "l2", "c", "s", "z", tt.country))
		})
	}

	assert.Nil(t, toContactAddress("Acme", "", "", "", "", "", ""), "attention alone is not an address")
}

func TestFormatMobile(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		country string
		want    string
	}{
		{name: "national number uses the address country", number: "9581401234", country: "IN", want: "+91-9581401234"},
		{name: "own plus prefix wins over the address country", number: "+1 415 555 2671", country: "IN", want: "+1-4155552671"},
		{name: "lowercase country", number: "09581401234", country: "in", want: "+91-9581401234"},
		{name: "no country and no prefix is sent unchanged", number: "9581401234", country: "", want: "9581401234"},
		{name: "garbage is sent unchanged", number: "call me", country: "IN", want: "call me"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatMobile(tt.number, tt.country))
		})
	}
}

func TestBuildContactPersons(t *testing.T) {
	phone := "9581401234"
	extra := &customerDomain.ContactPerson{FirstName: "Fin", LastName: "Person", Email: "fin@example.test", Mobile: "9876543210", Designation: "Finance"}
	primary := &customerDomain.ContactPerson{IsPrimary: true, Salutation: "Mr.", FirstName: "Ravi", LastName: "Kumar", Email: "ravi@example.test", Mobile: "+919581401234"}

	tests := []struct {
		name     string
		customer *customerDomain.Customer
		want     []ContactPerson
	}{
		{
			name:     "no persons uses the customer as primary",
			customer: &customerDomain.Customer{Email: "org@example.test", Contact: &phone, AddressCountry: "IN"},
			want:     []ContactPerson{{IsPrimaryContact: true, Email: "org@example.test", Mobile: "+91-9581401234"}},
		},
		{
			name:     "extras without a primary entry keep the customer as primary",
			customer: &customerDomain.Customer{Email: "org@example.test", AddressCountry: "IN", ContactPersons: []*customerDomain.ContactPerson{extra}},
			want: []ContactPerson{
				{IsPrimaryContact: true, Email: "org@example.test"},
				{FirstName: "Fin", LastName: "Person", Email: "fin@example.test", Mobile: "+91-9876543210", Designation: "Finance"},
			},
		},
		{
			name:     "primary entry replaces the customer as primary",
			customer: &customerDomain.Customer{Email: "org@example.test", Contact: &phone, AddressCountry: "IN", ContactPersons: []*customerDomain.ContactPerson{extra, primary}},
			want: []ContactPerson{
				{FirstName: "Fin", LastName: "Person", Email: "fin@example.test", Mobile: "+91-9876543210", Designation: "Finance"},
				{IsPrimaryContact: true, Salutation: "Mr.", FirstName: "Ravi", LastName: "Kumar", Email: "ravi@example.test", Mobile: "+91-9581401234"},
			},
		},
		{
			name:     "nothing to send",
			customer: &customerDomain.Customer{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildContactPersons(tt.customer, nil, ""))
		})
	}
}

func TestCreateContactStoresPersonIDs(t *testing.T) {
	client := &fakeContactClient{createResp: &ContactResponse{
		ContactID:      "zoho_contact_1",
		PrimaryContact: "zp_1",
		ContactPersons: []ContactPerson{
			{ContactPersonID: "zp_1", IsPrimaryContact: true, FirstName: "Ravi", Email: "ravi@example.test"},
			{ContactPersonID: "zp_2", FirstName: "Fin", Email: "fin@example.test"},
		},
	}}
	repo := &writableMappingRepo{}
	svc := newTestCustomerService(client, repo)

	c := indianCustomer()
	c.ContactPersons = []*customerDomain.ContactPerson{
		{ID: "cp_1", IsPrimary: true, FirstName: "Ravi", Email: "ravi@example.test"},
		{ID: "cp_2", FirstName: "Fin", Email: "fin@example.test"},
	}
	_, err := svc.GetOrCreateZohoCustomer(context.Background(), c)
	require.NoError(t, err)

	require.Len(t, client.createReq.ContactPersons, 2)
	require.Len(t, repo.mappings, 1)
	md := repo.mappings[0].Metadata
	assert.Equal(t, "zp_1", md["zoho_primary_contact_id"])
	assert.Equal(t, map[string]string{"cp_1": "zp_1", "cp_2": "zp_2"}, md["zoho_contact_person_ids"])
}

func TestMergeContactPersons(t *testing.T) {
	hand := ContactPerson{ContactPersonID: "zh", FirstName: "Hand", Email: "hand@example.test"}
	current := &ContactResponse{
		PrimaryContact: "zp",
		ContactPersons: []ContactPerson{
			{ContactPersonID: "zp", IsPrimaryContact: true, Email: "org@example.test"},
			{ContactPersonID: "zx", FirstName: "Fin", Email: "fin@example.test"},
			{ContactPersonID: "zy", FirstName: "Ops", Email: "ops@example.test"},
			hand,
		},
	}
	fin := &customerDomain.ContactPerson{ID: "cp_fin", FirstName: "Fin", Email: "fin@example.test", Designation: "Finance Lead"}
	stored := map[string]interface{}{
		mappingKeyPrimaryContactID: "zp",
		mappingKeyContactPersonIDs: map[string]interface{}{"cp_fin": "zx", "cp_ops": "zy"},
	}

	tests := []struct {
		name     string
		customer *customerDomain.Customer
		current  *ContactResponse
		metadata map[string]interface{}
		want     []ContactPerson
	}{
		{
			name:     "contact from before this change keeps its primary and hand-added persons",
			customer: &customerDomain.Customer{Email: "org@example.test", ContactPersons: []*customerDomain.ContactPerson{fin}},
			current:  &ContactResponse{PrimaryContact: "zp", ContactPersons: []ContactPerson{{ContactPersonID: "zp", IsPrimaryContact: true}, hand}},
			want: []ContactPerson{
				{ContactPersonID: "zp", IsPrimaryContact: true, Email: "org@example.test"},
				{FirstName: "Fin", Email: "fin@example.test", Designation: "Finance Lead"},
				hand,
			},
		},
		{
			name:     "stored ids update in place and a removed person is left out",
			customer: &customerDomain.Customer{Email: "org@example.test", ContactPersons: []*customerDomain.ContactPerson{fin}},
			current:  current,
			metadata: stored,
			want: []ContactPerson{
				{ContactPersonID: "zp", IsPrimaryContact: true, Email: "org@example.test"},
				{ContactPersonID: "zx", FirstName: "Fin", Email: "fin@example.test", Designation: "Finance Lead"},
				hand,
			},
		},
		{
			name:     "stored id deleted in Zoho is sent as new",
			customer: &customerDomain.Customer{Email: "org@example.test", ContactPersons: []*customerDomain.ContactPerson{fin}},
			current:  &ContactResponse{PrimaryContact: "zp", ContactPersons: []ContactPerson{{ContactPersonID: "zp", IsPrimaryContact: true}}},
			metadata: stored,
			want: []ContactPerson{
				{ContactPersonID: "zp", IsPrimaryContact: true, Email: "org@example.test"},
				{FirstName: "Fin", Email: "fin@example.test", Designation: "Finance Lead"},
			},
		},
		{
			name: "new primary entry takes over the primary id",
			customer: &customerDomain.Customer{Email: "org@example.test", ContactPersons: []*customerDomain.ContactPerson{
				{ID: "cp_ravi", IsPrimary: true, FirstName: "Ravi", Email: "ravi@example.test"}, fin,
			}},
			current:  current,
			metadata: stored,
			want: []ContactPerson{
				{ContactPersonID: "zp", IsPrimaryContact: true, FirstName: "Ravi", Email: "ravi@example.test"},
				{ContactPersonID: "zx", FirstName: "Fin", Email: "fin@example.test", Designation: "Finance Lead"},
				hand,
			},
		},
		{
			name: "extra promoted to primary keeps its own id and the old primary is left out",
			customer: &customerDomain.Customer{Email: "org@example.test", ContactPersons: []*customerDomain.ContactPerson{
				{ID: "cp_fin", IsPrimary: true, FirstName: "Fin", Email: "fin@example.test"},
			}},
			current:  current,
			metadata: stored,
			want: []ContactPerson{
				{ContactPersonID: "zx", IsPrimaryContact: true, FirstName: "Fin", Email: "fin@example.test"},
				hand,
			},
		},
		{
			name:     "removed primary entry is left out and the customer becomes primary as a new person",
			customer: &customerDomain.Customer{Email: "org@example.test", ContactPersons: []*customerDomain.ContactPerson{fin}},
			current: &ContactResponse{PrimaryContact: "zn", ContactPersons: []ContactPerson{
				{ContactPersonID: "zn", IsPrimaryContact: true, FirstName: "Neha"},
				{ContactPersonID: "zx", FirstName: "Fin"},
			}},
			metadata: map[string]interface{}{
				mappingKeyPrimaryContactID: "zn",
				mappingKeyContactPersonIDs: map[string]interface{}{"cp_fin": "zx", "cp_neha": "zn"},
			},
			want: []ContactPerson{
				{IsPrimaryContact: true, Email: "org@example.test"},
				{ContactPersonID: "zx", FirstName: "Fin", Email: "fin@example.test", Designation: "Finance Lead"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeContactPersons(tt.customer, tt.current, tt.metadata))
		})
	}
}

func TestSyncCustomerUpdateStoresPersonIDs(t *testing.T) {
	client := &fakeContactClient{updateResp: &ContactResponse{
		ContactID:      "zoho_contact_1",
		PrimaryContact: "zp",
		ContactPersons: []ContactPerson{
			{ContactPersonID: "zp", IsPrimaryContact: true, Email: "billing@mcgill.example"},
			{ContactPersonID: "zx", FirstName: "Fin", Email: "fin@example.test"},
		},
	}}
	repo := &writableMappingRepo{mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
		EntityID:         "cust_1",
		ProviderEntityID: "zoho_contact_1",
		Metadata:         map[string]interface{}{mappingKeyContactName: "MCGILL"},
	}}}
	svc := newTestCustomerService(client, repo)

	c := indianCustomer()
	c.ContactPersons = []*customerDomain.ContactPerson{{ID: "cp_fin", FirstName: "Fin", Email: "fin@example.test"}}
	require.NoError(t, svc.SyncCustomerUpdate(context.Background(), c))

	assert.Equal(t, 1, repo.updateCalls)
	md := repo.mappings[0].Metadata
	assert.Equal(t, "MCGILL", md[mappingKeyContactName], "existing metadata is kept")
	assert.Equal(t, "zp", md[mappingKeyPrimaryContactID])
	assert.Equal(t, map[string]string{"cp_fin": "zx"}, md[mappingKeyContactPersonIDs])
}

func TestContactPersonSendsClearedFields(t *testing.T) {
	raw, err := json.Marshal(ContactPerson{ContactPersonID: "zx", FirstName: "Fin"})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"designation":""`, "a cleared field must reach Zoho")
}
