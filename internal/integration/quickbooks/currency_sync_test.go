package quickbooks

import (
	"context"
	"testing"

	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// QuickBooks locks a customer to the company's home currency when created without a
// CurrencyRef, then reinterprets every invoice sent to it — the same defect fixed in Zoho.

type fakeQBClient struct {
	QuickBooksClient
	createReq   *CustomerCreateRequest
	createCalls int
}

func (f *fakeQBClient) CreateCustomer(_ context.Context, req *CustomerCreateRequest) (*CustomerResponse, error) {
	f.createCalls++
	f.createReq = req
	return &CustomerResponse{ID: "qb_cust_1", DisplayName: req.DisplayName}, nil
}

func (f *fakeQBClient) QueryCustomerByEmail(_ context.Context, _ string) (*CustomerResponse, error) {
	return nil, nil
}

func (f *fakeQBClient) QueryCustomerByName(_ context.Context, _ string) (*CustomerResponse, error) {
	return nil, nil
}

type fakeQBMappingRepo struct {
	entityintegrationmapping.Repository
	mappings []*entityintegrationmapping.EntityIntegrationMapping
}

func (f *fakeQBMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeQBMappingRepo) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	f.mappings = append(f.mappings, m)
	return nil
}

type fakeQBCustomerRepo struct {
	customerDomain.Repository
}

func (f *fakeQBCustomerRepo) Get(_ context.Context, id string) (*customerDomain.Customer, error) {
	c := qbTestCustomer()
	c.ID = id
	return c, nil
}

func newQBTestCustomerService(client QuickBooksClient, repo entityintegrationmapping.Repository) *CustomerService {
	return &CustomerService{
		CustomerServiceParams: CustomerServiceParams{
			Client:                       client,
			CustomerRepo:                 &fakeQBCustomerRepo{},
			EntityIntegrationMappingRepo: repo,
			Logger:                       logger.NewNoopLogger(),
		},
	}
}

func qbTestCustomer() *customerDomain.Customer {
	return &customerDomain.Customer{
		ID:    "cust_1",
		Name:  "Acme Inc",
		Email: "billing@acme.example",
	}
}

func TestQuickBooksCustomerCarriesCurrency(t *testing.T) {
	client := &fakeQBClient{}
	svc := newQBTestCustomerService(client, &fakeQBMappingRepo{})

	id, err := svc.GetOrCreateQuickBooksCustomer(context.Background(), qbTestCustomer(), "usd")
	require.NoError(t, err)
	assert.Equal(t, "qb_cust_1", id)

	require.NotNil(t, client.createReq)
	require.NotNil(t, client.createReq.CurrencyRef,
		"a customer created without CurrencyRef is locked to the company home currency")
	assert.Equal(t, "USD", client.createReq.CurrencyRef.Value,
		"QuickBooks expects the ISO code uppercased")
}

func TestQuickBooksCustomerRejectsEmptyCurrency(t *testing.T) {
	client := &fakeQBClient{}
	svc := newQBTestCustomerService(client, &fakeQBMappingRepo{})

	_, err := svc.GetOrCreateQuickBooksCustomer(context.Background(), qbTestCustomer(), "")

	require.Error(t, err)
	assert.Equal(t, 0, client.createCalls,
		"no customer may be created when its currency is unknown")
}

func TestQuickBooksMappedCustomerSkipsCreation(t *testing.T) {
	client := &fakeQBClient{}
	repo := &fakeQBMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{{
			EntityID:         "cust_1",
			ProviderEntityID: "qb_existing_9",
		}},
	}
	svc := newQBTestCustomerService(client, repo)

	id, err := svc.GetOrCreateQuickBooksCustomer(context.Background(), qbTestCustomer(), "usd")

	require.NoError(t, err)
	assert.Equal(t, "qb_existing_9", id)
	assert.Equal(t, 0, client.createCalls,
		"an existing customer's currency is immutable, so it must not be recreated")
}
