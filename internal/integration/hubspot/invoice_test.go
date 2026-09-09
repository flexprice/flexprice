package hubspot

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInvoiceMappingRepo struct {
	entityintegrationmapping.Repository
	mappings []*entityintegrationmapping.EntityIntegrationMapping
	listErr  error
}

func (f *fakeInvoiceMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if filter == nil {
			out = append(out, m)
			continue
		}
		if filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		if filter.EntityType != "" && m.EntityType != filter.EntityType {
			continue
		}
		if len(filter.ProviderTypes) > 0 {
			matched := false
			for _, p := range filter.ProviderTypes {
				if m.ProviderType == p {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, m)
	}
	return out, nil
}

type fakeInvoiceRepo struct {
	invoice.Repository
	byID map[string]*invoice.Invoice
}

func (f *fakeInvoiceRepo) Get(_ context.Context, id string) (*invoice.Invoice, error) {
	inv, ok := f.byID[id]
	if !ok {
		return nil, ierr.NewError("invoice not found").Mark(ierr.ErrNotFound)
	}
	return inv, nil
}

func newInvoiceSyncServiceForTest(t *testing.T, mappingRepo *fakeInvoiceMappingRepo, invoiceRepo *fakeInvoiceRepo) *InvoiceSyncService {
	t.Helper()
	return NewInvoiceSyncService(nil, invoiceRepo, mappingRepo, mustTestLogger(t))
}

func TestGetHubSpotContactID_EmptyCustomerIDDoesNotReturnLastLinked(t *testing.T) {
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_linked",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_last_linked",
			},
		},
	}, nil)

	contactID, err := svc.GetHubSpotContactID(context.Background(), "")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Empty(t, contactID)
}

func TestGetHubSpotContactID_UnlinkedCustomerDoesNotReturnLastLinked(t *testing.T) {
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_linked",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_last_linked",
			},
		},
	}, nil)

	contactID, err := svc.GetHubSpotContactID(context.Background(), "cust_unlinked")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Empty(t, contactID)
}

func TestGetHubSpotContactID_LinkedCustomerReturnsMappedID(t *testing.T) {
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_linked",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_linked",
			},
		},
	}, nil)

	contactID, err := svc.GetHubSpotContactID(context.Background(), "cust_linked")

	require.NoError(t, err)
	assert.Equal(t, "hs_contact_linked", contactID)
}

func TestGetHubSpotContactID_ListFailureIsNotNotFound(t *testing.T) {
	listErr := ierr.NewError("mapping store unavailable").Mark(ierr.ErrInternal)
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{listErr: listErr}, nil)

	contactID, err := svc.GetHubSpotContactID(context.Background(), "cust_linked")

	require.Error(t, err)
	assert.False(t, ierr.IsNotFound(err), "transient List errors must stay retryable, not CustomerNotLinked")
	assert.True(t, ierr.IsInternal(err))
	assert.Empty(t, contactID)
}

func TestGetHubSpotContactIDForInvoice_EmptyInvoiceCustomerDoesNotReturnLastLinked(t *testing.T) {
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_linked",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_last_linked",
			},
		},
	}, &fakeInvoiceRepo{
		byID: map[string]*invoice.Invoice{
			"inv_no_customer": {ID: "inv_no_customer"},
		},
	})

	contactID, err := svc.GetHubSpotContactIDForInvoice(context.Background(), "inv_no_customer")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Empty(t, contactID)
}

func TestGetHubSpotContactIDForInvoice_UnlinkedInvoiceCustomerDoesNotReturnLastLinked(t *testing.T) {
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_linked",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_last_linked",
			},
		},
	}, &fakeInvoiceRepo{
		byID: map[string]*invoice.Invoice{
			"inv_unlinked": {ID: "inv_unlinked", CustomerID: "cust_unlinked"},
		},
	})

	contactID, err := svc.GetHubSpotContactIDForInvoice(context.Background(), "inv_unlinked")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Empty(t, contactID)
}

func TestGetHubSpotContactIDForInvoice_UsesInvoiceCustomer(t *testing.T) {
	svc := newInvoiceSyncServiceForTest(t, &fakeInvoiceMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_a",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_a",
			},
			{
				EntityType:       types.IntegrationEntityTypeCustomer,
				EntityID:         "cust_b",
				ProviderType:     string(types.SecretProviderHubSpot),
				ProviderEntityID: "hs_contact_b",
			},
		},
	}, &fakeInvoiceRepo{
		byID: map[string]*invoice.Invoice{
			"inv_b": {ID: "inv_b", CustomerID: "cust_b"},
		},
	})

	contactID, err := svc.GetHubSpotContactIDForInvoice(context.Background(), "inv_b")

	require.NoError(t, err)
	assert.Equal(t, "hs_contact_b", contactID)
}
