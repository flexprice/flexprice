package razorpay

import (
	"context"
	"testing"

	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCustomerClient struct {
	RazorpayClient
	createErr    error
	createResult map[string]interface{}
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
	return nil, nil
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
