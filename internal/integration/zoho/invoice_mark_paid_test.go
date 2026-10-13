package zoho

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// fakePaymentRepo is a minimal in-memory payment.Repository for these tests.
type fakePaymentRepo struct {
	payment.Repository
	payments []*payment.Payment
	err      error
}

func (f *fakePaymentRepo) List(_ context.Context, _ *types.PaymentFilter) ([]*payment.Payment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.payments, nil
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
	return newTestInvoiceServiceWithPayments(client, mappingRepo, &fakePaymentRepo{})
}

func newTestInvoiceServiceWithPayments(client ZohoClient, mappingRepo entityintegrationmapping.Repository, paymentRepo payment.Repository) *InvoiceService {
	return &InvoiceService{
		client:      client,
		mappingRepo: mappingRepo,
		paymentRepo: paymentRepo,
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
	assert.Empty(t, req.AccountID(), "unset config must leave Zoho on Undeposited Funds")
	assert.Empty(t, req.ReferenceNumber())
	require.Len(t, req.Invoices(), 1)
	assert.Equal(t, "zoho_inv_1", req.Invoices()[0].InvoiceID())
	assert.True(t, decimal.NewFromInt(160).Equal(req.Invoices()[0].AmountApplied()))
}

func markPaidFixtures() (*fakeZohoClient, *fakeMappingRepo) {
	return &fakeZohoClient{
			getInvoiceResp: &InvoiceResponse{
				InvoiceID:  "zoho_inv_1",
				CustomerID: "zoho_cust_1",
				Balance:    decimal.NewFromInt(160),
			},
		}, &fakeMappingRepo{
			mappings: []*entityintegrationmapping.EntityIntegrationMapping{
				{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
			},
		}
}

func succeededPayment(id, gatewayID string, succeededAt time.Time) *payment.Payment {
	return &payment.Payment{
		ID:               id,
		PaymentStatus:    types.PaymentStatusSucceeded,
		GatewayPaymentID: lo.ToPtr(gatewayID),
		SucceededAt:      lo.ToPtr(succeededAt),
	}
}

func TestMarkInvoicePaidInZoho_SendsConfiguredPaymentModeAndDepositAccount(t *testing.T) {
	client, mappingRepo := markPaidFixtures()
	client.syncConfig = &types.SyncConfig{
		InvoiceSyncSettings: &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
				PaymentMode:        "UPI",
				DepositToAccountID: "460000000000099",
			},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	require.NoError(t, svc.MarkInvoicePaidInZoho(context.Background(), "inv_1"))

	require.Equal(t, 1, client.createPaymentCalls)
	assert.Equal(t, "UPI", client.createPaymentReq.PaymentMode())
	assert.Equal(t, "460000000000099", client.createPaymentReq.AccountID())
}

func TestMarkInvoicePaidInZoho_ReferenceNumber(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		payments []*payment.Payment
		err      error
		want     string
	}{
		{
			name:     "latest settled gateway payment wins",
			payments: []*payment.Payment{succeededPayment("pay_1", "pay_older", base), succeededPayment("pay_2", "pay_newer", base.Add(time.Hour))},
			want:     "pay_newer",
		},
		{
			name: "newer payment without a gateway id does not mask an older one",
			payments: []*payment.Payment{
				succeededPayment("pay_1", "pay_card", base),
				{ID: "pay_2", PaymentStatus: types.PaymentStatusSucceeded, SucceededAt: lo.ToPtr(base.Add(time.Hour))},
			},
			want: "pay_card",
		},
		{
			name: "unsettled payments are ignored",
			payments: []*payment.Payment{
				{ID: "pay_1", PaymentStatus: types.PaymentStatusFailed, GatewayPaymentID: lo.ToPtr("pay_failed"), SucceededAt: lo.ToPtr(base)},
				{ID: "pay_2", PaymentStatus: types.PaymentStatusPending, GatewayPaymentID: lo.ToPtr("pay_pending")},
			},
			want: "",
		},
		{
			name:     "no payments at all",
			payments: nil,
			want:     "",
		},
		{
			name: "lookup failure still records the payment",
			err:  errors.New("db down"),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, mappingRepo := markPaidFixtures()
			svc := newTestInvoiceServiceWithPayments(client, mappingRepo,
				&fakePaymentRepo{payments: tc.payments, err: tc.err})

			require.NoError(t, svc.MarkInvoicePaidInZoho(context.Background(), "inv_1"))

			require.Equal(t, 1, client.createPaymentCalls, "reference lookup must never block mark-paid")
			assert.Equal(t, tc.want, client.createPaymentReq.ReferenceNumber())
		})
	}
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

// account_id and reference_number must vanish from the payload when unset: Zoho reads a
// present-but-empty account_id as an invalid account rather than as "use the default".
func TestCustomerPaymentCreateRequestOmitsUnsetOptionalFields(t *testing.T) {
	body, err := json.Marshal(NewCustomerPaymentCreateRequest(CustomerPaymentCreateParams{
		CustomerID:  "zoho_cust_1",
		PaymentMode: types.DefaultZohoPaymentMode,
		Amount:      decimal.NewFromInt(160),
		Date:        "2026-09-07",
		Invoices:    []CustomerPaymentInvoiceApply{NewCustomerPaymentInvoiceApply("zoho_inv_1", decimal.NewFromInt(160))},
	}))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "account_id")
	assert.NotContains(t, string(body), "reference_number")
}

func TestCustomerPaymentCreateRequestTruncatesReferenceNumber(t *testing.T) {
	req := NewCustomerPaymentCreateRequest(CustomerPaymentCreateParams{
		ReferenceNumber: " " + strings.Repeat("x", 120) + " ",
	})
	assert.Len(t, req.ReferenceNumber(), 100)
}
