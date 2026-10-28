package quickbooks

import (
	"context"
	"maps"
	"sync"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type fakeQBClient struct {
	QuickBooksClient
	payment *PaymentResponse
}

func (c *fakeQBClient) GetPayment(_ context.Context, _ string) (*PaymentResponse, error) {
	return c.payment, nil
}

type fakeMappingRepo struct {
	entityintegrationmapping.Repository
}

func (fakeMappingRepo) List(_ context.Context, _ *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return []*entityintegrationmapping.EntityIntegrationMapping{{ID: "eim_1", EntityID: "inv_1"}}, nil
}

// fakePaymentService mimics idempotency-key dedupe and the real transition table.
type fakePaymentService struct {
	interfaces.PaymentService
	mu    sync.Mutex
	byKey map[string]string
	byID  map[string]dto.PaymentResponse
}

func newFakePaymentService() *fakePaymentService {
	return &fakePaymentService{byKey: map[string]string{}, byID: map[string]dto.PaymentResponse{}}
}

func (f *fakePaymentService) seed(key string, p dto.PaymentResponse) {
	f.byID[p.ID] = p
	if key != "" {
		f.byKey[key] = p.ID
	}
}

func (f *fakePaymentService) ListPayments(_ context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*dto.PaymentResponse
	for _, p := range f.byID {
		if filter.GatewayPaymentID != nil && (p.GatewayPaymentID == nil || *p.GatewayPaymentID != *filter.GatewayPaymentID) {
			continue
		}
		cp := p
		items = append(items, &cp)
	}
	return &dto.ListPaymentsResponse{Items: items}, nil
}

func (f *fakePaymentService) CreatePayment(_ context.Context, req *dto.CreatePaymentRequest) (*dto.PaymentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byKey[req.IdempotencyKey]; ok {
		cp := f.byID[id]
		return &cp, nil
	}
	p := dto.PaymentResponse{ID: "pay_" + req.IdempotencyKey, PaymentStatus: types.PaymentStatusInitiated}
	f.byKey[req.IdempotencyKey] = p.ID
	f.byID[p.ID] = p
	return &p, nil
}

func (f *fakePaymentService) GetPayment(_ context.Context, id string) (*dto.PaymentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}
	return &p, nil
}

func (f *fakePaymentService) UpdatePayment(_ context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}
	if req.PaymentStatus != nil {
		if err := p.PaymentStatus.ValidateTransitionTo(types.PaymentStatus(*req.PaymentStatus)); err != nil {
			return nil, err
		}
		p.PaymentStatus = types.PaymentStatus(*req.PaymentStatus)
	}
	if req.GatewayPaymentID != nil {
		p.GatewayPaymentID = req.GatewayPaymentID
	}
	f.byID[id] = p
	cp := p
	return &cp, nil
}

// fakeDB serializes transactions and restores payment state on error.
type fakeDB struct {
	postgres.IClient
	mu       sync.Mutex
	payments *fakePaymentService
}

func (d *fakeDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.payments.mu.Lock()
	savedByID := maps.Clone(d.payments.byID)
	savedByKey := maps.Clone(d.payments.byKey)
	d.payments.mu.Unlock()

	err := fn(ctx)
	if err == nil {
		return nil
	}
	d.payments.mu.Lock()
	defer d.payments.mu.Unlock()
	d.payments.byID = savedByID
	d.payments.byKey = savedByKey
	return err
}

func (d *fakeDB) LockWithWait(_ context.Context, _ postgres.LockRequest) error { return nil }

type fakeInvoiceService struct {
	interfaces.InvoiceService
	mu            sync.Mutex
	reconciled    int
	failReconcile int
}

func (f *fakeInvoiceService) GetInvoice(_ context.Context, id string) (*dto.InvoiceResponse, error) {
	return &dto.InvoiceResponse{Invoice: invoice.Invoice{ID: id, Currency: "usd", AmountDue: decimal.NewFromInt(100)}}, nil
}

func (f *fakeInvoiceService) ReconcilePaymentStatus(_ context.Context, _ string, _ types.PaymentStatus, _ *decimal.Decimal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failReconcile > 0 {
		f.failReconcile--
		return ierr.NewError("reconcile failed").Mark(ierr.ErrInternal)
	}
	f.reconciled++
	return nil
}

func TestHandleExternalPaymentFromWebhook(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	require.NoError(t, err)

	qbPayment := &PaymentResponse{
		ID: "qb_pay_1",
		Line: []PaymentLine{{
			Amount:    40,
			LinkedTxn: []LinkedTxn{{TxnId: "qb_inv_1", TxnType: "Invoice"}},
		}},
	}
	gatewayID := "qb_pay_1"

	tests := []struct {
		name           string
		deliveries     int
		concurrent     bool
		seed           *dto.PaymentResponse
		seedKey        string
		failReconcile  int
		wantReconciled int
		wantErrs       int
		wantStatus     types.PaymentStatus
	}{
		{name: "single delivery", deliveries: 1, wantReconciled: 1, wantStatus: types.PaymentStatusSucceeded},
		{name: "sequential duplicate", deliveries: 3, wantReconciled: 1, wantStatus: types.PaymentStatusSucceeded},
		{name: "concurrent duplicate", deliveries: 8, concurrent: true, wantReconciled: 1, wantStatus: types.PaymentStatusSucceeded},
		{
			name: "reconcile fails then retry applies once", deliveries: 3, failReconcile: 1,
			wantReconciled: 1, wantErrs: 1, wantStatus: types.PaymentStatusSucceeded,
		},
		{
			name: "legacy succeeded payment", deliveries: 2,
			seed:       &dto.PaymentResponse{ID: "pay_old", PaymentStatus: types.PaymentStatusSucceeded, GatewayPaymentID: &gatewayID},
			wantStatus: types.PaymentStatusSucceeded,
		},
		{
			name: "refunded payment redelivered", deliveries: 2,
			seed:       &dto.PaymentResponse{ID: "pay_old", PaymentStatus: types.PaymentStatusRefunded, GatewayPaymentID: &gatewayID},
			wantStatus: types.PaymentStatusRefunded,
		},
		{
			name: "keyed succeeded payment without gateway id", deliveries: 2,
			seed:       &dto.PaymentResponse{ID: "pay_quickbooks_payment_qb_pay_1", PaymentStatus: types.PaymentStatusSucceeded},
			seedKey:    "quickbooks_payment_qb_pay_1",
			wantStatus: types.PaymentStatusSucceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payments := newFakePaymentService()
			if tt.seed != nil {
				payments.seed(tt.seedKey, *tt.seed)
			}
			invoices := &fakeInvoiceService{failReconcile: tt.failReconcile}
			db := &fakeDB{payments: payments}
			svc := NewPaymentService(PaymentServiceParams{
				Client:                       &fakeQBClient{payment: qbPayment},
				EntityIntegrationMappingRepo: fakeMappingRepo{},
				Logger:                       log,
			})

			var errMu sync.Mutex
			errs := 0
			deliver := func() {
				if err := svc.HandleExternalPaymentFromWebhook(context.Background(), "qb_pay_1", payments, invoices, db); err != nil {
					errMu.Lock()
					errs++
					errMu.Unlock()
				}
			}
			if tt.concurrent {
				var wg sync.WaitGroup
				for i := 0; i < tt.deliveries; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						deliver()
					}()
				}
				wg.Wait()
			} else {
				for i := 0; i < tt.deliveries; i++ {
					deliver()
				}
			}

			require.Equal(t, tt.wantReconciled, invoices.reconciled)
			require.Equal(t, tt.wantErrs, errs)
			require.Len(t, payments.byID, 1, "no second payment may be created")
			for _, p := range payments.byID {
				require.Equal(t, tt.wantStatus, p.PaymentStatus)
			}
		})
	}
}

func TestHandleExternalPaymentFromWebhook_RollbackKeepsPaymentUnfinished(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	require.NoError(t, err)

	qbPayment := &PaymentResponse{
		ID:   "qb_pay_1",
		Line: []PaymentLine{{Amount: 40, LinkedTxn: []LinkedTxn{{TxnId: "qb_inv_1", TxnType: "Invoice"}}}},
	}
	payments := newFakePaymentService()
	invoices := &fakeInvoiceService{failReconcile: 1}
	db := &fakeDB{payments: payments}
	svc := NewPaymentService(PaymentServiceParams{
		Client:                       &fakeQBClient{payment: qbPayment},
		EntityIntegrationMappingRepo: fakeMappingRepo{},
		Logger:                       log,
	})

	require.Error(t, svc.HandleExternalPaymentFromWebhook(context.Background(), "qb_pay_1", payments, invoices, db))
	require.Zero(t, invoices.reconciled)
	require.Empty(t, payments.byID, "failed apply must leave no succeeded payment")

	require.NoError(t, svc.HandleExternalPaymentFromWebhook(context.Background(), "qb_pay_1", payments, invoices, db))
	require.Equal(t, 1, invoices.reconciled)
}
