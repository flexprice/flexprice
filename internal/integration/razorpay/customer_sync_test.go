package razorpay

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCustomerClient struct {
	RazorpayClient
	createErr    error
	createResult map[string]interface{}
}

func (c *stubCustomerClient) GetConnection(_ context.Context) (*connection.Connection, error) {
	return &connection.Connection{ID: "conn_test", ProviderType: types.SecretProviderRazorpay}, nil
}

func (c *stubCustomerClient) CreateCustomer(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	return c.createResult, c.createErr
}

type inlineMappingStore struct {
	created []*entityintegrationmapping.EntityIntegrationMapping
}

func (s *inlineMappingStore) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	s.created = append(s.created, m)
	return nil
}
func (s *inlineMappingStore) Get(_ context.Context, _ string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (s *inlineMappingStore) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return s.created, nil
}
func (s *inlineMappingStore) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 0, nil
}
func (s *inlineMappingStore) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (s *inlineMappingStore) Delete(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}

func newSyncService(client RazorpayClient, store entityintegrationmapping.Repository) *CustomerService {
	return &CustomerService{
		client:                       client,
		entityIntegrationMappingRepo: store,
		logger:                       logger.NewNoopLogger(),
	}
}

func mappedToRazorpay() *inlineMappingStore {
	return &inlineMappingStore{created: []*entityintegrationmapping.EntityIntegrationMapping{{ProviderEntityID: "cust_rzp"}}}
}

func TestApplyCustomerCreateIdempotency(t *testing.T) {
	got := applyCustomerCreateIdempotency(map[string]interface{}{"email": "ada@example.com"})
	assert.Equal(t, "0", got["fail_existing"])
	assert.Equal(t, "ada@example.com", got["email"])

	got = applyCustomerCreateIdempotency(nil)
	require.NotNil(t, got)
	assert.Equal(t, "0", got["fail_existing"])
}

func TestSyncCustomerToRazorpay_StoresMappingFromCreateResponse(t *testing.T) {
	ctx := context.Background()
	client := &stubCustomerClient{
		createResult: map[string]interface{}{"id": "cust_existing_rp"},
	}
	store := &inlineMappingStore{}
	svc := newSyncService(client, store)

	id, err := svc.SyncCustomerToRazorpay(ctx, &domainCustomer.Customer{
		ID:    "cust_flex",
		Name:  "Ada",
		Email: "ada@example.com",
	})

	require.NoError(t, err)
	assert.Equal(t, "cust_existing_rp", id)
	require.Len(t, store.created, 1)
	assert.Equal(t, "cust_existing_rp", store.created[0].ProviderEntityID)
}

func TestSyncCustomerToRazorpay_PropagatesCreateError(t *testing.T) {
	ctx := context.Background()
	client := &stubCustomerClient{
		createErr: ierr.NewError("failed to create customer in Razorpay").Mark(ierr.ErrInternal),
	}
	svc := newSyncService(client, &inlineMappingStore{})

	_, err := svc.SyncCustomerToRazorpay(ctx, &domainCustomer.Customer{
		ID:    "cust_flex",
		Name:  "Ada",
		Email: "ada@example.com",
	})

	require.Error(t, err)
	assert.True(t, ierr.IsInternal(err))
}

type setupLinkStore struct {
	inlineMappingStore
	mapping *entityintegrationmapping.EntityIntegrationMapping
	saveErr error
}

func (s *setupLinkStore) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	if s.mapping == nil {
		return nil, nil
	}
	return []*entityintegrationmapping.EntityIntegrationMapping{s.mapping}, nil
}

func (s *setupLinkStore) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.mapping = m
	return nil
}

func (s *setupLinkStore) Update(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	return s.saveErr
}

type cancelRecordingClient struct {
	stubCustomerClient
	cancelled []string
}

func (c *cancelRecordingClient) CancelInvoice(_ context.Context, invoiceID string) (*AuthorizationLinkResponse, error) {
	c.cancelled = append(c.cancelled, invoiceID)
	return &AuthorizationLinkResponse{ID: invoiceID, Status: "cancelled"}, nil
}

func TestSaveSetupLink(t *testing.T) {
	saveErr := ierr.NewError("db down").Mark(ierr.ErrDatabase)
	existing := func() *entityintegrationmapping.EntityIntegrationMapping {
		return &entityintegrationmapping.EntityIntegrationMapping{ID: "map_1", EntityID: "cust_1", ProviderEntityID: "inv_old"}
	}

	tests := []struct {
		name          string
		mapping       *entityintegrationmapping.EntityIntegrationMapping
		saveErr       error
		wantLinkID    string
		wantCancelled []string
	}{
		{name: "first link is stored", wantLinkID: "inv_new"},
		{name: "new link replaces and cancels the old one", mapping: existing(), wantLinkID: "inv_new", wantCancelled: []string{"inv_old"}},
		{name: "failed first save cancels the new link", saveErr: saveErr, wantCancelled: []string{"inv_new"}},
		{name: "failed replace cancels only the new link", mapping: existing(), saveErr: saveErr, wantCancelled: []string{"inv_new"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &cancelRecordingClient{}
			store := &setupLinkStore{mapping: tt.mapping, saveErr: tt.saveErr}
			svc := newSyncService(client, store)

			err := svc.SaveSetupLink(context.Background(), "cust_1", "inv_new")
			assert.Equal(t, tt.wantCancelled, client.cancelled)
			if tt.saveErr != nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, store.mapping)
			assert.Equal(t, tt.wantLinkID, store.mapping.ProviderEntityID)
		})
	}
}

type contactSyncClient struct {
	stubCustomerClient
	current map[string]interface{}
	updates []map[string]interface{}
}

func (c *contactSyncClient) FetchCustomer(_ context.Context, _ string) (map[string]interface{}, error) {
	return c.current, nil
}

func (c *contactSyncClient) UpdateCustomer(_ context.Context, _ string, data map[string]interface{}) (map[string]interface{}, error) {
	c.updates = append(c.updates, data)
	return c.current, nil
}

func TestVerifyLinkCustomer(t *testing.T) {
	c := &domainCustomer.Customer{ID: "cust_1", Email: "a@example.com", Contact: lo.ToPtr("9000090000")}
	tests := []struct {
		name    string
		current map[string]interface{}
		wantErr bool
	}{
		{name: "matching details pass", current: map[string]interface{}{"email": "a@example.com", "contact": "9000090000"}},
		{name: "missing phone is refused", current: map[string]interface{}{"email": "a@example.com", "contact": nil}, wantErr: true},
		{name: "different email is refused", current: map[string]interface{}{"email": "b@example.com", "contact": "9000090000"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &contactSyncClient{current: tt.current}
			svc := newSyncService(client, mappedToRazorpay())

			id, err := svc.VerifyLinkCustomer(context.Background(), c)

			assert.Empty(t, client.updates, "verification never writes to Razorpay")
			if tt.wantErr {
				assert.True(t, ierr.IsValidation(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "cust_rzp", id)
		})
	}
}

func TestUpdateCustomerInRazorpay(t *testing.T) {
	client := &contactSyncClient{}
	svc := newSyncService(client, mappedToRazorpay())
	c := &domainCustomer.Customer{ID: "cust_1", Name: "Asha", Email: "a@example.com", Contact: lo.ToPtr("9000090000")}

	require.NoError(t, svc.UpdateCustomerInRazorpay(context.Background(), c))

	assert.Equal(t, []map[string]interface{}{{"name": "Asha", "email": "a@example.com", "contact": "9000090000"}}, client.updates)
}
