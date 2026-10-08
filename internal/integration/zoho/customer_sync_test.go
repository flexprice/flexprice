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
	syncConfig   *types.SyncConfig
}

func (f *fakeContactClient) GetZohoBooksSyncConfig(_ context.Context) (*types.SyncConfig, error) {
	return f.syncConfig, nil
}

func (f *fakeContactClient) QueryContactByEmail(_ context.Context, _ string) (*ContactResponse, error) {
	return f.queryByEmail, nil
}

func (f *fakeContactClient) CreateContact(_ context.Context, req *ContactCreateRequest) (*ContactResponse, error) {
	f.createCalls++
	f.createReq = req
	return &ContactResponse{ContactID: "zoho_contact_1", ContactName: req.ContactName}, nil
}

func (f *fakeContactClient) UpdateContact(_ context.Context, contactID string, req *ContactUpdateRequest) (*ContactResponse, error) {
	f.updateCalls++
	f.updateID = contactID
	f.updateReq = req
	if f.updateErr != nil {
		return nil, f.updateErr
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

func TestContactCarriesReceivableAccount(t *testing.T) {
	client := &fakeContactClient{syncConfig: &types.SyncConfig{
		InvoiceSyncSettings: &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{ReceivableAccountID: "460000000000462"},
		},
	}}
	svc := newTestCustomerService(client, &writableMappingRepo{})

	_, err := svc.GetOrCreateZohoCustomer(context.Background(), indianCustomer())
	require.NoError(t, err)
	assert.Equal(t, "460000000000462", client.createReq.AccountID)
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
	raw, err := json.Marshal(buildContactRequest(indianCustomer(), ""))
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
