package stripe

// Tests for the paths of EnsureCustomerSyncedToStripe that resolve an already-linked
// Stripe customer and therefore return before any Stripe API call. Creating a customer
// needs a real *Client built from the connection repo, so it is out of unit-test reach;
// the cases below cover the race that stranded saved cards on a duplicate Stripe customer.

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// ── fake interfaces.CustomerService ──────────────────────────────────────────

type syncTestCustomerService struct {
	interfaces.CustomerService
	customer *customer.Customer
	getCalls int
	// stripeIDOnCall simulates the caller holding the lock finishing its sync: from that
	// GetCustomer call onwards, metadata carries stripeID.
	stripeIDOnCall int
	stripeID       string
	updateCalls    []dto.UpdateCustomerRequest
}

func (s *syncTestCustomerService) GetCustomer(_ context.Context, _ string) (*dto.CustomerResponse, error) {
	s.getCalls++
	if s.stripeID != "" && s.stripeIDOnCall > 0 && s.getCalls >= s.stripeIDOnCall {
		if s.customer.Metadata == nil {
			s.customer.Metadata = map[string]string{}
		}
		s.customer.Metadata["stripe_customer_id"] = s.stripeID
	}
	return &dto.CustomerResponse{Customer: s.customer}, nil
}

func (s *syncTestCustomerService) UpdateCustomer(_ context.Context, _ string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error) {
	s.updateCalls = append(s.updateCalls, req)
	s.customer.Metadata = req.Metadata
	return &dto.CustomerResponse{Customer: s.customer}, nil
}

// ── fake entityintegrationmapping.Repository ─────────────────────────────────

type syncTestMappingRepo struct {
	entityintegrationmapping.Repository
	mappings  []*entityintegrationmapping.EntityIntegrationMapping
	listCalls int
}

func (r *syncTestMappingRepo) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	r.listCalls++
	return r.mappings, nil
}

// ── fake cache.Locker ────────────────────────────────────────────────────────

type syncTestLock struct {
	acquired     bool
	releaseCalls *int
}

func (l syncTestLock) AcquiredSuccessfully() bool { return l.acquired }

func (l syncTestLock) Release(_ context.Context) error {
	*l.releaseCalls++
	return nil
}

type syncTestLocker struct {
	// acquiredOnCall is the attempt at which the lock becomes free; earlier attempts
	// simulate another caller still holding it.
	acquiredOnCall int
	acquireCalls   int
	releaseCalls   int
}

func (l *syncTestLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (cache.Lock, error) {
	l.acquireCalls++
	acquired := l.acquiredOnCall > 0 && l.acquireCalls >= l.acquiredOnCall
	return syncTestLock{acquired: acquired, releaseCalls: &l.releaseCalls}, nil
}

func testContext() context.Context {
	ctx := types.SetTenantID(context.Background(), "tenant_test")
	return types.SetEnvironmentID(ctx, "env_test")
}

func TestEnsureCustomerSyncedToStripe(t *testing.T) {
	const (
		customerID = "cust_test001"
		metaCus    = "cus_frommetadata"
		mappedCus  = "cus_frommapping"
		holderCus  = "cus_fromconcurrentholder"
	)

	tests := []struct {
		name string
		// lockAcquiredOnCall only matters when the customer is not already linked.
		lockAcquiredOnCall int
		metadata           map[string]string
		mappings           []*entityintegrationmapping.EntityIntegrationMapping
		stripeIDOnCall     int
		concurrentID       string
		wantStripeID       string
		wantAcquireCalls   int
		wantReleaseCalls   int
		wantUpdateCalls    int
	}{
		{
			name:               "metadata already set returns without taking the lock",
			lockAcquiredOnCall: 1,
			metadata:           map[string]string{"stripe_customer_id": metaCus},
			wantStripeID:       metaCus,
		},
		{
			name:               "mapping present backfills metadata without taking the lock",
			lockAcquiredOnCall: 1,
			mappings: []*entityintegrationmapping.EntityIntegrationMapping{
				{EntityID: customerID, ProviderEntityID: mappedCus},
			},
			wantStripeID:    mappedCus,
			wantUpdateCalls: 1,
		},
		{
			name:               "lock held by a concurrent sync is retried until released",
			lockAcquiredOnCall: 2,
			stripeIDOnCall:     2,
			concurrentID:       holderCus,
			wantStripeID:       holderCus,
			wantAcquireCalls:   2,
			wantReleaseCalls:   1,
		},
		{
			name:               "re-check under the lock picks up a sync that just finished",
			lockAcquiredOnCall: 1,
			stripeIDOnCall:     2,
			concurrentID:       holderCus,
			wantStripeID:       holderCus,
			wantAcquireCalls:   1,
			wantReleaseCalls:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customerSvc := &syncTestCustomerService{
				customer:       &customer.Customer{ID: customerID, Metadata: tt.metadata},
				stripeIDOnCall: tt.stripeIDOnCall,
				stripeID:       tt.concurrentID,
			}
			mappingRepo := &syncTestMappingRepo{mappings: tt.mappings}
			locker := &syncTestLocker{acquiredOnCall: tt.lockAcquiredOnCall}

			svc := NewCustomerService(nil, nil, mappingRepo, locker, logger.NewNoopLogger())

			resp, err := svc.EnsureCustomerSyncedToStripe(testContext(), customerID, customerSvc)

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.wantStripeID, resp.Customer.Metadata["stripe_customer_id"])
			require.Equal(t, tt.wantAcquireCalls, locker.acquireCalls)
			require.Equal(t, tt.wantReleaseCalls, locker.releaseCalls)
			require.Len(t, customerSvc.updateCalls, tt.wantUpdateCalls)
		})
	}
}

func TestCustomerCreateIdempotencyKeyIsStablePerCustomer(t *testing.T) {
	ctx := testContext()

	require.Equal(t,
		customerCreateIdempotencyKey(ctx, "cust_test001"),
		customerCreateIdempotencyKey(ctx, "cust_test001"),
	)
	require.NotEqual(t,
		customerCreateIdempotencyKey(ctx, "cust_test001"),
		customerCreateIdempotencyKey(ctx, "cust_test002"),
	)

	otherEnv := types.SetEnvironmentID(ctx, "env_other")
	require.NotEqual(t,
		customerCreateIdempotencyKey(ctx, "cust_test001"),
		customerCreateIdempotencyKey(otherEnv, "cust_test001"),
	)
}
