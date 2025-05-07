package service

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Mock implementations
type mockCalculator struct {
	mock.Mock
}

func (m *mockCalculator) Calculate(ctx context.Context, params proration.ProrationParams) (*proration.ProrationResult, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proration.ProrationResult), args.Error(1)
}

type mockInvoiceRepo struct {
	mock.Mock
}

func (m *mockInvoiceRepo) Create(ctx context.Context, inv *invoice.Invoice) error {
	args := m.Called(ctx, inv)
	return args.Error(0)
}

func (m *mockInvoiceRepo) Get(ctx context.Context, id string) (*invoice.Invoice, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*invoice.Invoice), args.Error(1)
}

func (m *mockInvoiceRepo) Update(ctx context.Context, inv *invoice.Invoice) error {
	args := m.Called(ctx, inv)
	return args.Error(0)
}

func (m *mockInvoiceRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockInvoiceRepo) List(ctx context.Context, filter *types.InvoiceFilter) ([]*invoice.Invoice, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*invoice.Invoice), args.Error(1)
}

func (m *mockInvoiceRepo) Count(ctx context.Context, filter *types.InvoiceFilter) (int, error) {
	args := m.Called(ctx, filter)
	return args.Int(0), args.Error(1)
}

func (m *mockInvoiceRepo) AddLineItems(ctx context.Context, invoiceID string, items []*invoice.InvoiceLineItem) error {
	args := m.Called(ctx, invoiceID, items)
	return args.Error(0)
}

func (m *mockInvoiceRepo) RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error {
	args := m.Called(ctx, invoiceID, itemIDs)
	return args.Error(0)
}

func (m *mockInvoiceRepo) CreateWithLineItems(ctx context.Context, inv *invoice.Invoice) error {
	args := m.Called(ctx, inv)
	return args.Error(0)
}

func (m *mockInvoiceRepo) GetByIdempotencyKey(ctx context.Context, key string) (*invoice.Invoice, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*invoice.Invoice), args.Error(1)
}

func (m *mockInvoiceRepo) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	args := m.Called(ctx, subscriptionID, periodStart, periodEnd)
	return args.Bool(0), args.Error(1)
}

func (m *mockInvoiceRepo) GetNextInvoiceNumber(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func (m *mockInvoiceRepo) GetNextBillingSequence(ctx context.Context, subscriptionID string) (int, error) {
	args := m.Called(ctx, subscriptionID)
	return args.Int(0), args.Error(1)
}

func TestCalculateProration(t *testing.T) {
	mockCalc := new(mockCalculator)
	mockInvRepo := new(mockInvoiceRepo)
	logger := logger.GetLogger()

	svc := NewProrationService(mockCalc, mockInvRepo, logger)
	ctx := context.Background()

	params := proration.ProrationParams{
		SubscriptionID: "sub_123",
		LineItemID:     "li_123",
		Action:         types.ProrationActionUpgrade,
	}

	expectedResult := &proration.ProrationResult{
		NetAmount: decimal.NewFromInt(100),
		Currency:  "USD",
		Action:    types.ProrationActionUpgrade,
	}

	mockCalc.On("Calculate", ctx, params).Return(expectedResult, nil)

	result, err := svc.CalculateProration(ctx, params)

	assert.NoError(t, err)
	assert.Equal(t, expectedResult, result)
	mockCalc.AssertExpectations(t)
}

func TestApplyProration_CreateInvoiceItems_NewInvoice(t *testing.T) {
	mockCalc := new(mockCalculator)
	mockInvRepo := new(mockInvoiceRepo)
	logger := logger.GetLogger()

	svc := NewProrationService(mockCalc, mockInvRepo, logger)
	ctx := context.Background()

	now := time.Now()
	result := &proration.ProrationResult{
		NetAmount:     decimal.NewFromInt(100),
		Currency:      "USD",
		Action:        types.ProrationActionUpgrade,
		ProrationDate: now,
		CreditItems: []proration.ProrationLineItem{
			{
				Description: "Credit for unused time",
				Amount:      decimal.NewFromInt(-50),
				StartDate:   now,
				EndDate:     now.Add(24 * time.Hour),
				Quantity:    decimal.NewFromInt(1),
				PriceID:     "price_old",
				IsCredit:    true,
			},
		},
		ChargeItems: []proration.ProrationLineItem{
			{
				Description: "Charge for upgrade",
				Amount:      decimal.NewFromInt(150),
				StartDate:   now,
				EndDate:     now.Add(24 * time.Hour),
				Quantity:    decimal.NewFromInt(1),
				PriceID:     "price_new",
				IsCredit:    false,
			},
		},
	}

	mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(false, nil)
	mockInvRepo.On("GetNextInvoiceNumber", ctx).Return("INV-001", nil)
	mockInvRepo.On("GetNextBillingSequence", ctx, "sub_123").Return(1, nil)
	mockInvRepo.On("Create", ctx, mock.AnythingOfType("*invoice.Invoice")).Return(nil)
	mockInvRepo.On("AddLineItems", ctx, mock.AnythingOfType("string"), mock.AnythingOfType("[]*invoice.InvoiceLineItem")).Return(nil)

	err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

	assert.NoError(t, err)
	mockInvRepo.AssertExpectations(t)
}

func TestApplyProration_CreateInvoiceItems_ExistingInvoice(t *testing.T) {
	mockCalc := new(mockCalculator)
	mockInvRepo := new(mockInvoiceRepo)
	logger := logger.GetLogger()

	svc := NewProrationService(mockCalc, mockInvRepo, logger)
	ctx := context.Background()

	now := time.Now()
	result := &proration.ProrationResult{
		NetAmount:     decimal.NewFromInt(100),
		Currency:      "USD",
		Action:        types.ProrationActionUpgrade,
		ProrationDate: now,
		CreditItems: []proration.ProrationLineItem{
			{
				Description: "Credit for unused time",
				Amount:      decimal.NewFromInt(-50),
				StartDate:   now,
				EndDate:     now.Add(24 * time.Hour),
				Quantity:    decimal.NewFromInt(1),
				PriceID:     "price_old",
				IsCredit:    true,
			},
		},
		ChargeItems: []proration.ProrationLineItem{
			{
				Description: "Charge for upgrade",
				Amount:      decimal.NewFromInt(150),
				StartDate:   now,
				EndDate:     now.Add(24 * time.Hour),
				Quantity:    decimal.NewFromInt(1),
				PriceID:     "price_new",
				IsCredit:    false,
			},
		},
	}

	existingInvoice := &invoice.Invoice{
		ID:             "inv_123",
		SubscriptionID: &[]string{"sub_123"}[0],
		Currency:       "USD",
	}

	mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(true, nil)
	mockInvRepo.On("List", ctx, mock.AnythingOfType("*types.InvoiceFilter")).Return([]*invoice.Invoice{existingInvoice}, nil)
	mockInvRepo.On("AddLineItems", ctx, "inv_123", mock.AnythingOfType("[]*invoice.InvoiceLineItem")).Return(nil)

	err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

	assert.NoError(t, err)
	mockInvRepo.AssertExpectations(t)
}

func TestApplyProration_NoAction(t *testing.T) {
	mockCalc := new(mockCalculator)
	mockInvRepo := new(mockInvoiceRepo)
	logger := logger.GetLogger()

	svc := NewProrationService(mockCalc, mockInvRepo, logger)
	ctx := context.Background()

	result := &proration.ProrationResult{
		NetAmount: decimal.NewFromInt(100),
		Currency:  "USD",
	}

	err := svc.ApplyProration(ctx, result, types.ProrationBehaviorNone, "tenant_123", "env_123", "sub_123")

	assert.NoError(t, err)
	mockInvRepo.AssertNotCalled(t, "ExistsForPeriod")
	mockInvRepo.AssertNotCalled(t, "Create")
	mockInvRepo.AssertNotCalled(t, "AddLineItems")
}

func TestApplyProration_ErrorCases(t *testing.T) {
	mockCalc := new(mockCalculator)
	mockInvRepo := new(mockInvoiceRepo)
	logger := logger.GetLogger()

	svc := NewProrationService(mockCalc, mockInvRepo, logger)
	ctx := context.Background()

	now := time.Now()
	result := &proration.ProrationResult{
		NetAmount:     decimal.NewFromInt(100),
		Currency:      "USD",
		ProrationDate: now,
	}

	t.Run("ExistsForPeriod error", func(t *testing.T) {
		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(false, assert.AnError).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})

	t.Run("GetNextInvoiceNumber error", func(t *testing.T) {
		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(false, nil).Once()
		mockInvRepo.On("GetNextInvoiceNumber", ctx).Return("", assert.AnError).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})

	t.Run("GetNextBillingSequence error", func(t *testing.T) {
		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(false, nil).Once()
		mockInvRepo.On("GetNextInvoiceNumber", ctx).Return("INV-001", nil).Once()
		mockInvRepo.On("GetNextBillingSequence", ctx, "sub_123").Return(0, assert.AnError).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})

	t.Run("Create invoice error", func(t *testing.T) {
		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(false, nil).Once()
		mockInvRepo.On("GetNextInvoiceNumber", ctx).Return("INV-001", nil).Once()
		mockInvRepo.On("GetNextBillingSequence", ctx, "sub_123").Return(1, nil).Once()
		mockInvRepo.On("Create", ctx, mock.AnythingOfType("*invoice.Invoice")).Return(assert.AnError).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})

	t.Run("List invoices error", func(t *testing.T) {
		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(true, nil).Once()
		mockInvRepo.On("List", ctx, mock.AnythingOfType("*types.InvoiceFilter")).Return(nil, assert.AnError).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})

	t.Run("No invoice found", func(t *testing.T) {
		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(true, nil).Once()
		mockInvRepo.On("List", ctx, mock.AnythingOfType("*types.InvoiceFilter")).Return([]*invoice.Invoice{}, nil).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})

	t.Run("AddLineItems error", func(t *testing.T) {
		existingInvoice := &invoice.Invoice{
			ID:             "inv_123",
			SubscriptionID: &[]string{"sub_123"}[0],
			Currency:       "USD",
		}

		mockInvRepo.On("ExistsForPeriod", ctx, "sub_123", now, now).Return(true, nil).Once()
		mockInvRepo.On("List", ctx, mock.AnythingOfType("*types.InvoiceFilter")).Return([]*invoice.Invoice{existingInvoice}, nil).Once()
		mockInvRepo.On("AddLineItems", ctx, "inv_123", mock.AnythingOfType("[]*invoice.InvoiceLineItem")).Return(assert.AnError).Once()

		err := svc.ApplyProration(ctx, result, types.ProrationBehaviorCreateProrations, "tenant_123", "env_123", "sub_123")

		assert.Error(t, err)
		mockInvRepo.AssertExpectations(t)
	})
}
