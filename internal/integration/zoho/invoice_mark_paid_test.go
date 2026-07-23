package zoho

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMappingRepo is a minimal in-memory entityintegrationmapping.Repository for these tests.
type fakeMappingRepo struct {
	entityintegrationmapping.Repository
	mappings []*entityintegrationmapping.EntityIntegrationMapping
}

func (f *fakeMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// fakeZohoClient is a minimal ZohoClient for these tests. Only GetInvoice and
// CreateCustomerPayment are exercised; all other methods panic if called.
type fakeZohoClient struct {
	ZohoClient
	getInvoiceResp     *InvoiceResponse
	getInvoiceErr      error
	createPaymentReq   *CustomerPaymentCreateRequest
	createPaymentErr   error
	createPaymentCalls int
}

func (f *fakeZohoClient) GetInvoice(_ context.Context, _ string) (*InvoiceResponse, error) {
	return f.getInvoiceResp, f.getInvoiceErr
}

func (f *fakeZohoClient) CreateCustomerPayment(_ context.Context, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error) {
	f.createPaymentCalls++
	f.createPaymentReq = req
	if f.createPaymentErr != nil {
		return nil, f.createPaymentErr
	}
	return &CustomerPaymentResponse{PaymentID: "zoho_payment_1"}, nil
}

func newTestInvoiceService(client ZohoClient, mappingRepo entityintegrationmapping.Repository) *InvoiceService {
	return &InvoiceService{
		client:      client,
		mappingRepo: mappingRepo,
		logger:      logger.NewNoopLogger(),
	}
}

func TestMarkInvoicePaidInZoho_NoMapping_Skips(t *testing.T) {
	client := &fakeZohoClient{}
	mappingRepo := &fakeMappingRepo{}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}

func TestMarkInvoicePaidInZoho_ZeroBalance_Skips(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Balance:    decimal.Zero,
		},
	}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}

func TestMarkInvoicePaidInZoho_PositiveBalance_RecordsFullBalance(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Balance:    decimal.NewFromInt(160),
		},
	}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	require.Equal(t, 1, client.createPaymentCalls)
	req := client.createPaymentReq
	assert.Equal(t, "zoho_cust_1", req.CustomerID)
	assert.True(t, decimal.NewFromInt(160).Equal(req.Amount))
	assert.Equal(t, "other", req.PaymentMode)
	require.Len(t, req.Invoices, 1)
	assert.Equal(t, "zoho_inv_1", req.Invoices[0].InvoiceID)
	assert.True(t, decimal.NewFromInt(160).Equal(req.Invoices[0].AmountApplied))
}

func TestMarkInvoicePaidInZoho_CreatePaymentError_Propagates(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Balance:    decimal.NewFromInt(50),
		},
		createPaymentErr: assert.AnError,
	}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	assert.Error(t, err)
}

func TestMarkInvoicePaidInZoho_GetInvoiceError_Propagates(t *testing.T) {
	client := &fakeZohoClient{getInvoiceErr: assert.AnError}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	assert.Error(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}
