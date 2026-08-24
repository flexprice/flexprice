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

// fakeZohoClient is a minimal ZohoClient for these tests. Only GetInvoice,
// CreateCustomerPayment, SubmitInvoiceForApproval and GetZohoBooksSyncConfig are
// exercised; all other methods panic if called.
type fakeZohoClient struct {
	ZohoClient
	getInvoiceResp     *InvoiceResponse
	getInvoiceErr      error
	getInvoiceCalls    int
	createPaymentReq   *CustomerPaymentCreateRequest
	createPaymentErr   error
	createPaymentCalls int
	submitErr          error
	submitCalls        int
	syncConfig         *types.SyncConfig
	syncConfigErr      error
	// statusSequence, when set, overrides getInvoiceResp.Status on successive GetInvoice
	// calls so a test can walk an invoice from draft to approved. The last entry sticks
	// once the sequence is exhausted.
	statusSequence []string
	// balanceSequence mirrors statusSequence for the invoice balance, so a test can have
	// Zoho settle the invoice midway through the approval wait.
	balanceSequence []decimal.Decimal
}

func (f *fakeZohoClient) GetInvoice(_ context.Context, _ string) (*InvoiceResponse, error) {
	f.getInvoiceCalls++
	if f.getInvoiceErr != nil {
		return nil, f.getInvoiceErr
	}
	if f.getInvoiceResp == nil || (len(f.statusSequence) == 0 && len(f.balanceSequence) == 0) {
		return f.getInvoiceResp, nil
	}
	clone := *f.getInvoiceResp
	if len(f.statusSequence) > 0 {
		clone.Status = f.statusSequence[min(f.getInvoiceCalls-1, len(f.statusSequence)-1)]
	}
	if len(f.balanceSequence) > 0 {
		clone.Balance = f.balanceSequence[min(f.getInvoiceCalls-1, len(f.balanceSequence)-1)]
	}
	return &clone, nil
}

func (f *fakeZohoClient) SubmitInvoiceForApproval(_ context.Context, _ string) error {
	f.submitCalls++
	return f.submitErr
}

func (f *fakeZohoClient) GetZohoBooksSyncConfig(_ context.Context) (*types.SyncConfig, error) {
	return f.syncConfig, f.syncConfigErr
}

func (f *fakeZohoClient) CreateCustomerPayment(_ context.Context, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error) {
	f.createPaymentCalls++
	f.createPaymentReq = req
	if f.createPaymentErr != nil {
		return nil, f.createPaymentErr
	}
	return NewCustomerPaymentResponse("zoho_payment_1"), nil
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
	assert.Equal(t, "zoho_cust_1", req.CustomerID())
	assert.True(t, decimal.NewFromInt(160).Equal(req.Amount()))
	assert.Equal(t, "others", req.PaymentMode())
	require.Len(t, req.Invoices(), 1)
	assert.Equal(t, "zoho_inv_1", req.Invoices()[0].InvoiceID())
	assert.True(t, decimal.NewFromInt(160).Equal(req.Invoices()[0].AmountApplied()))
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

func markPaidTestMapping() *fakeMappingRepo {
	return &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
}

// The approval guard must not disturb tenants that never opted in: their invoices sit in
// draft too, and payments have always been recorded against them.
func TestMarkInvoicePaidInZoho_ApprovalDisabled_DraftStillPaid(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Status:     InvoiceStatusDraft,
			Balance:    decimal.NewFromInt(100),
		},
		syncConfig: syncConfigWithApproval(false),
	}
	svc := newTestInvoiceService(client, markPaidTestMapping())

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 1, client.createPaymentCalls)
	assert.Equal(t, 0, client.submitCalls)
}

func TestMarkInvoicePaidInZoho_ApprovalEnabled_RejectedIsNotPaid(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Status:     InvoiceStatusRejected,
			Balance:    decimal.NewFromInt(100),
		},
		syncConfig: syncConfigWithApproval(true),
	}
	svc := newTestInvoiceService(client, markPaidTestMapping())

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}

// An invoice that already cleared approval pays without re-submitting or waiting.
func TestMarkInvoicePaidInZoho_ApprovalEnabled_SentIsPaid(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Status:     "sent",
			Balance:    decimal.NewFromInt(100),
		},
		syncConfig: syncConfigWithApproval(true),
	}
	svc := newTestInvoiceService(client, markPaidTestMapping())

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 1, client.createPaymentCalls)
	assert.Equal(t, 0, client.submitCalls)
}
