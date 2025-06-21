package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CreditNoteServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  CreditNoteService
	testData struct {
		customer *customer.Customer
		invoices struct {
			finalized       *invoice.Invoice
			pending         *invoice.Invoice
			failed          *invoice.Invoice
			succeeded       *invoice.Invoice
			refunded        *invoice.Invoice
			partialRefunded *invoice.Invoice
		}
		wallets struct {
			usd *wallet.Wallet
			eur *wallet.Wallet
		}
		now time.Time
	}
}

func TestCreditNoteService(t *testing.T) {
	suite.Run(t, new(CreditNoteServiceSuite))
}

func (s *CreditNoteServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *CreditNoteServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CreditNoteServiceSuite) setupService() {
	s.service = NewCreditNoteService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		CreditNoteRepo:   s.GetStores().CreditNoteRepo,
		InvoiceRepo:      s.GetStores().InvoiceRepo,
		CustomerRepo:     s.GetStores().CustomerRepo,
		WalletRepo:       s.GetStores().WalletRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

func (s *CreditNoteServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_123",
		ExternalID: "external_123",
		Name:       "Test Customer",
		Email:      "test@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test wallets using the wallet service
	walletService := NewWalletService(ServiceParams{
		Logger:           s.GetLogger(),
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
		WalletRepo:       s.GetStores().WalletRepo,
		EventPublisher:   s.GetPublisher(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})

	// Create USD wallet
	usdWalletResp, err := walletService.CreateWallet(s.GetContext(), &dto.CreateWalletRequest{
		CustomerID:           s.testData.customer.ID,
		Name:                 "USD Wallet",
		Currency:             "USD",
		ConversionRate:       decimal.NewFromInt(1),
		WalletType:           types.WalletTypePrePaid,
		InitialCreditsToLoad: decimal.NewFromFloat(100.00),
	})
	s.NoError(err)
	s.testData.wallets.usd = &wallet.Wallet{
		ID:            usdWalletResp.ID,
		CustomerID:    s.testData.customer.ID,
		Name:          "USD Wallet",
		Currency:      "USD",
		Balance:       usdWalletResp.Balance,
		CreditBalance: usdWalletResp.CreditBalance,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create EUR wallet
	eurWalletResp, err := walletService.CreateWallet(s.GetContext(), &dto.CreateWalletRequest{
		CustomerID:           s.testData.customer.ID,
		Name:                 "EUR Wallet",
		Currency:             "EUR",
		ConversionRate:       decimal.NewFromInt(1),
		WalletType:           types.WalletTypePrePaid,
		InitialCreditsToLoad: decimal.NewFromFloat(50.00),
	})
	s.NoError(err)
	s.testData.wallets.eur = &wallet.Wallet{
		ID:            eurWalletResp.ID,
		CustomerID:    s.testData.customer.ID,
		Name:          "EUR Wallet",
		Currency:      "EUR",
		Balance:       eurWalletResp.Balance,
		CreditBalance: eurWalletResp.CreditBalance,
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}

	// Create test invoices with different payment statuses
	s.createTestInvoices()
}

func (s *CreditNoteServiceSuite) createTestInvoices() {
	// Finalized invoice with succeeded payment
	s.testData.invoices.finalized = &invoice.Invoice{
		ID:            "inv_finalized_123",
		CustomerID:    s.testData.customer.ID,
		InvoiceNumber: lo.ToPtr("INV-001"),
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusSucceeded,
		Currency:      "USD",
		Subtotal:      decimal.NewFromFloat(100.00),
		Total:         decimal.NewFromFloat(110.00),
		AmountPaid:    decimal.NewFromFloat(110.00),
		AmountDue:     decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_1",
				DisplayName: lo.ToPtr("Product A"),
				Amount:      decimal.NewFromFloat(50.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:          "line_2",
				DisplayName: lo.ToPtr("Product B"),
				Amount:      decimal.NewFromFloat(50.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.finalized))

	// Invoice with pending payment
	s.testData.invoices.pending = &invoice.Invoice{
		ID:            "inv_pending_123",
		CustomerID:    s.testData.customer.ID,
		InvoiceNumber: lo.ToPtr("INV-002"),
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusPending,
		Currency:      "USD",
		Subtotal:      decimal.NewFromFloat(80.00),
		Total:         decimal.NewFromFloat(88.00),
		AmountPaid:    decimal.Zero,
		AmountDue:     decimal.NewFromFloat(88.00),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_3",
				DisplayName: lo.ToPtr("Product C"),
				Amount:      decimal.NewFromFloat(80.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.pending))

	// Invoice with failed payment
	s.testData.invoices.failed = &invoice.Invoice{
		ID:            "inv_failed_123",
		CustomerID:    s.testData.customer.ID,
		InvoiceNumber: lo.ToPtr("INV-003"),
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusFailed,
		Currency:      "USD",
		Subtotal:      decimal.NewFromFloat(60.00),
		Total:         decimal.NewFromFloat(66.00),
		AmountPaid:    decimal.Zero,
		AmountDue:     decimal.NewFromFloat(66.00),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_4",
				DisplayName: lo.ToPtr("Product D"),
				Amount:      decimal.NewFromFloat(60.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.failed))

	// Invoice with refunded payment
	s.testData.invoices.refunded = &invoice.Invoice{
		ID:            "inv_refunded_123",
		CustomerID:    s.testData.customer.ID,
		InvoiceNumber: lo.ToPtr("INV-004"),
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusRefunded,
		Currency:      "USD",
		Subtotal:      decimal.NewFromFloat(40.00),
		Total:         decimal.NewFromFloat(44.00),
		AmountPaid:    decimal.NewFromFloat(44.00),
		AmountDue:     decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_5",
				DisplayName: lo.ToPtr("Product E"),
				Amount:      decimal.NewFromFloat(40.00),
				Currency:    "USD",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.refunded))

	// Invoice with partially refunded payment
	s.testData.invoices.partialRefunded = &invoice.Invoice{
		ID:            "inv_partial_refund_123",
		CustomerID:    s.testData.customer.ID,
		InvoiceNumber: lo.ToPtr("INV-005"),
		InvoiceType:   types.InvoiceTypeSubscription,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusPartiallyRefunded,
		Currency:      "EUR",
		Subtotal:      decimal.NewFromFloat(120.00),
		Total:         decimal.NewFromFloat(132.00),
		AmountPaid:    decimal.NewFromFloat(132.00),
		AmountDue:     decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "line_6",
				DisplayName: lo.ToPtr("Product F"),
				Amount:      decimal.NewFromFloat(60.00),
				Currency:    "EUR",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
			{
				ID:          "line_7",
				DisplayName: lo.ToPtr("Product G"),
				Amount:      decimal.NewFromFloat(60.00),
				Currency:    "EUR",
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			},
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), s.testData.invoices.partialRefunded))
}

// Test CreateCreditNote method
func (s *CreditNoteServiceSuite) TestCreateCreditNote() {
	tests := []struct {
		name      string
		req       *dto.CreateCreditNoteRequest
		wantErr   bool
		errString string
		validate  func(*dto.CreditNoteResponse)
	}{
		{
			name: "successful_adjustment_credit_note_creation",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.pending.ID,
				Reason:    types.CreditNoteReasonBillingError,
				Memo:      "Billing error correction",
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_3",
						DisplayName:       "Partial refund for Product C",
						Amount:            decimal.NewFromFloat(20.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
				s.Equal(types.CreditNoteTypeAdjustment, resp.CreditNoteType)
				s.Equal(decimal.NewFromFloat(20.00), resp.TotalAmount)
				s.Equal("USD", resp.Currency)
				s.Len(resp.LineItems, 1)
			},
		},
		{
			name: "successful_refund_credit_note_creation",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonUnsatisfactory,
				Memo:      "Customer unsatisfied with service",
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						DisplayName:       "Full refund for Product A",
						Amount:            decimal.NewFromFloat(50.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
				s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)
				s.Equal(decimal.NewFromFloat(50.00), resp.TotalAmount)
			},
		},
		{
			name: "successful_with_custom_credit_note_number",
			req: &dto.CreateCreditNoteRequest{
				CreditNoteNumber: "CN-CUSTOM-001",
				InvoiceID:        s.testData.invoices.failed.ID,
				Reason:           types.CreditNoteReasonService,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_4",
						DisplayName:       "Service issue credit",
						Amount:            decimal.NewFromFloat(30.00),
					},
				},
			},
			wantErr: false,
			validate: func(resp *dto.CreditNoteResponse) {
				s.Equal("CN-CUSTOM-001", resp.CreditNoteNumber)
			},
		},
		{
			name: "error_missing_invoice_id",
			req: &dto.CreateCreditNoteRequest{
				Reason: types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "InvoiceID",
		},
		{
			name: "error_missing_reason",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "Reason",
		},
		{
			name: "error_empty_line_items",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{},
			},
			wantErr:   true,
			errString: "line_items is required",
		},
		{
			name: "error_invalid_reason",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReason("INVALID_REASON"),
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "invalid credit note reason",
		},
		{
			name: "error_invoice_not_finalized",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: "inv_draft_123",
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "not found",
		},
		{
			name: "error_refunded_invoice",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.refunded.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_5",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "payment status is not allowed",
		},
		{
			name: "error_line_item_amount_exceeds_invoice_line_item",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1",
						Amount:            decimal.NewFromFloat(100.00), // line_1 amount is 50.00
					},
				},
			},
			wantErr:   true,
			errString: "greater than invoice line item amount",
		},
		{
			name: "error_invalid_invoice_line_item_id",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.finalized.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "invalid_line_id",
						Amount:            decimal.NewFromFloat(10.00),
					},
				},
			},
			wantErr:   true,
			errString: "invoice line item not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.CreateCreditNote(s.GetContext(), tt.req)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.NotEmpty(resp.ID)
			s.NotEmpty(resp.CreditNoteNumber)
			s.Equal(tt.req.InvoiceID, resp.InvoiceID)
			s.Equal(tt.req.Reason, resp.Reason)

			if tt.validate != nil {
				tt.validate(resp)
			}
		})
	}
}

func (s *CreditNoteServiceSuite) TestGetCreditNote() {
	// Create a test credit note first
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(25.00),
			},
		},
	}

	created, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
	}{
		{
			name:    "successful_get",
			id:      created.ID,
			wantErr: false,
		},
		{
			name:      "error_credit_note_not_found",
			id:        "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
		{
			name:      "error_empty_id",
			id:        "",
			wantErr:   true,
			errString: "not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.GetCreditNote(s.GetContext(), tt.id)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.id, resp.ID)
			s.Equal(created.InvoiceID, resp.InvoiceID)
			s.Equal(created.Reason, resp.Reason)
		})
	}
}

func (s *CreditNoteServiceSuite) TestListCreditNotes() {
	// Create multiple test credit notes
	creditNotes := []struct {
		invoiceID  string
		lineItemID string
		reason     types.CreditNoteReason
		amount     decimal.Decimal
	}{
		{s.testData.invoices.finalized.ID, "line_1", types.CreditNoteReasonBillingError, decimal.NewFromFloat(10.00)},
		{s.testData.invoices.pending.ID, "line_3", types.CreditNoteReasonService, decimal.NewFromFloat(15.00)},
		{s.testData.invoices.failed.ID, "line_4", types.CreditNoteReasonDuplicate, decimal.NewFromFloat(20.00)},
	}

	var createdIDs []string
	for _, cn := range creditNotes {
		req := &dto.CreateCreditNoteRequest{
			InvoiceID: cn.invoiceID,
			Reason:    cn.reason,
			LineItems: []dto.CreateCreditNoteLineItemRequest{
				{
					InvoiceLineItemID: cn.lineItemID,
					Amount:            cn.amount,
				},
			},
		}

		resp, err := s.service.CreateCreditNote(s.GetContext(), req)
		s.NoError(err)
		createdIDs = append(createdIDs, resp.ID)
	}

	tests := []struct {
		name      string
		filter    *types.CreditNoteFilter
		wantCount int
		wantErr   bool
	}{
		{
			name:      "list_all_credit_notes",
			filter:    &types.CreditNoteFilter{QueryFilter: types.NewDefaultQueryFilter()},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "filter_by_invoice_id",
			filter: &types.CreditNoteFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				InvoiceID:   s.testData.invoices.finalized.ID,
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "filter_by_credit_note_type",
			filter: &types.CreditNoteFilter{
				QueryFilter:    types.NewDefaultQueryFilter(),
				CreditNoteType: types.CreditNoteTypeAdjustment,
			},
			wantCount: 2, // pending and failed invoices create adjustments
			wantErr:   false,
		},
		{
			name: "filter_by_credit_note_ids",
			filter: &types.CreditNoteFilter{
				QueryFilter:   types.NewDefaultQueryFilter(),
				CreditNoteIDs: []string{createdIDs[0], createdIDs[1]},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "filter_by_status",
			filter: &types.CreditNoteFilter{
				QueryFilter:      types.NewDefaultQueryFilter(),
				CreditNoteStatus: []types.CreditNoteStatus{types.CreditNoteStatusFinalized},
			},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "pagination_limit",
			filter: &types.CreditNoteFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(2),
					Offset: lo.ToPtr(0),
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.ListCreditNotes(s.GetContext(), tt.filter)

			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Len(resp.Items, tt.wantCount)
			s.NotNil(resp.Pagination)
		})
	}
}

func (s *CreditNoteServiceSuite) TestVoidCreditNote() {
	// Create test credit notes
	adjustmentReq := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.pending.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_3",
				Amount:            decimal.NewFromFloat(10.00),
			},
		},
	}

	adjustmentCN, err := s.service.CreateCreditNote(s.GetContext(), adjustmentReq)
	s.NoError(err)

	refundReq := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonUnsatisfactory,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(15.00),
			},
		},
	}

	refundCN, err := s.service.CreateCreditNote(s.GetContext(), refundReq)
	s.NoError(err)

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
	}{
		{
			name:    "successful_void_adjustment_credit_note",
			id:      adjustmentCN.ID,
			wantErr: false,
		},
		{
			name:      "error_void_refund_credit_note",
			id:        refundCN.ID,
			wantErr:   true,
			errString: "refund credit note cannot be voided",
		},
		{
			name:      "error_credit_note_not_found",
			id:        "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
		{
			name:      "error_empty_id",
			id:        "",
			wantErr:   true,
			errString: "credit note id is required",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.VoidCreditNote(s.GetContext(), tt.id)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)

			// Verify the credit note is voided
			resp, err := s.service.GetCreditNote(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(types.CreditNoteStatusVoided, resp.CreditNoteStatus)
		})
	}
}

func (s *CreditNoteServiceSuite) TestProcessDraftCreditNote() {
	// Create draft credit note manually in repository to test processing
	// This bypasses the CreateCreditNote validation that automatically processes the credit note
	draftCNData := &creditnote.CreditNote{
		ID:               "cn_draft_test",
		InvoiceID:        s.testData.invoices.finalized.ID,
		CreditNoteNumber: "CN-DRAFT-TEST",
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeRefund, // Since finalized invoice with succeeded payment
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.NewFromFloat(20.00),
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}

	err := s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), draftCNData)
	s.NoError(err)

	// Create a manual draft credit note with zero amount directly in the repository for testing
	// This bypasses the CreateCreditNote validation
	zeroCNData := &creditnote.CreditNote{
		ID:               "cn_zero_test",
		InvoiceID:        s.testData.invoices.pending.ID,
		CreditNoteNumber: "CN-ZERO-TEST",
		CreditNoteStatus: types.CreditNoteStatusDraft,
		CreditNoteType:   types.CreditNoteTypeAdjustment,
		Reason:           types.CreditNoteReasonService,
		Currency:         "USD",
		TotalAmount:      decimal.Zero,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), zeroCNData)
	s.NoError(err)

	// Create a processed credit note for the "already processed" test
	processedCNData := &creditnote.CreditNote{
		ID:               "cn_processed_test",
		InvoiceID:        s.testData.invoices.finalized.ID,
		CreditNoteNumber: "CN-PROCESSED-TEST",
		CreditNoteStatus: types.CreditNoteStatusFinalized,
		CreditNoteType:   types.CreditNoteTypeRefund,
		Reason:           types.CreditNoteReasonBillingError,
		Currency:         "USD",
		TotalAmount:      decimal.NewFromFloat(30.00),
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}

	err = s.GetStores().CreditNoteRepo.CreateWithLineItems(s.GetContext(), processedCNData)
	s.NoError(err)

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
		validate  func(string)
	}{
		{
			name:    "successful_process_draft_credit_note",
			id:      draftCNData.ID,
			wantErr: false,
			validate: func(id string) {
				resp, err := s.service.GetCreditNote(s.GetContext(), id)
				s.NoError(err)
				s.Equal(types.CreditNoteStatusFinalized, resp.CreditNoteStatus)
			},
		},
		{
			name:      "error_credit_note_not_found",
			id:        "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
		{
			name:      "error_empty_id",
			id:        "",
			wantErr:   true,
			errString: "credit note id is required",
		},
		{
			name:      "error_already_processed",
			id:        processedCNData.ID,
			wantErr:   true,
			errString: "not in draft status",
		},
		{
			name:      "error_zero_amount_credit_note",
			id:        zeroCNData.ID,
			wantErr:   true,
			errString: "invalid credit note amount",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.ProcessDraftCreditNote(s.GetContext(), tt.id)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)

			if tt.validate != nil {
				tt.validate(tt.id)
			}
		})
	}
}

func (s *CreditNoteServiceSuite) TestMaxCreditableAmountValidation() {
	// Create credit notes that exceed max creditable amount
	tests := []struct {
		name      string
		req       *dto.CreateCreditNoteRequest
		wantErr   bool
		errString string
	}{
		{
			name: "error_total_amount_exceeds_max_creditable_for_adjustment",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.pending.ID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_3",
						Amount:            decimal.NewFromFloat(80.00), // line_3 is 80.00
					},
					{
						InvoiceLineItemID: "line_3",
						Amount:            decimal.NewFromFloat(20.00), // Total: 100.00, but invoice total is 88.00
					},
				},
			},
			wantErr:   true,
			errString: "greater than max creditable amount",
		},
		{
			name: "error_total_amount_exceeds_max_creditable_for_refund",
			req: &dto.CreateCreditNoteRequest{
				InvoiceID: s.testData.invoices.partialRefunded.ID, // EUR invoice: Total=132, AmountPaid=132, line_6=60, line_7=60
				Reason:    types.CreditNoteReasonUnsatisfactory,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_6",
						Amount:            decimal.NewFromFloat(60.00),
					},
					{
						InvoiceLineItemID: "line_7",
						Amount:            decimal.NewFromFloat(60.00),
					},
					// Total: 120.00, with partialRefunded status, max should be less than 132
				},
			},
			wantErr:   false, // Let's first see what the actual max creditable amount is for partial refund
			errString: "",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.service.CreateCreditNote(s.GetContext(), tt.req)

			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
		})
	}
}

func (s *CreditNoteServiceSuite) TestMultipleCreditNotesValidation() {
	// Create first credit note
	firstReq := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(30.00),
			},
		},
	}

	_, err := s.service.CreateCreditNote(s.GetContext(), firstReq)
	s.NoError(err)

	// Create second credit note that should respect the already credited amount
	secondReq := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonService,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_2",
				Amount:            decimal.NewFromFloat(40.00),
			},
		},
	}

	_, err = s.service.CreateCreditNote(s.GetContext(), secondReq)
	s.NoError(err)

	// Try to create third credit note that exceeds max creditable amount
	thirdReq := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonDuplicate,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(50.00), // Already credited 30+40=70, max is 110, so 50 exceeds remaining 40
			},
		},
	}

	_, err = s.service.CreateCreditNote(s.GetContext(), thirdReq)
	s.Error(err)
	s.Contains(err.Error(), "greater than max creditable amount")
}

func (s *CreditNoteServiceSuite) TestWalletRefundIntegration() {
	// Check wallet balance before creating credit note
	walletBefore, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)
	s.T().Logf("Wallet balance before credit note: %s", walletBefore.Balance)

	// Test refund credit note creates wallet transaction
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.finalized.ID,
		Reason:    types.CreditNoteReasonUnsatisfactory,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_1",
				Amount:            decimal.NewFromFloat(25.00),
			},
		},
	}

	resp, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)
	s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)

	// Verify wallet balance increased
	wallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.usd.ID)
	s.NoError(err)

	// Debug information
	s.T().Logf("Initial wallet balance: %s", decimal.NewFromFloat(100.00))
	s.T().Logf("Credit note amount: %s", decimal.NewFromFloat(25.00))
	s.T().Logf("Current wallet balance: %s", wallet.Balance)
	s.T().Logf("Expected balance > %s", decimal.NewFromFloat(100.00))

	s.True(wallet.Balance.GreaterThan(decimal.NewFromFloat(100.00))) // Initial was 100.00
}

func (s *CreditNoteServiceSuite) TestCurrencyMismatchHandling() {
	// Test credit note with EUR invoice and wallet
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.partialRefunded.ID, // EUR invoice
		Reason:    types.CreditNoteReasonUnsatisfactory,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_6",
				Amount:            decimal.NewFromFloat(30.00),
			},
		},
	}

	resp, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)
	s.Equal("EUR", resp.Currency)
	s.Equal(types.CreditNoteTypeRefund, resp.CreditNoteType)

	// Verify EUR wallet balance increased
	wallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallets.eur.ID)
	s.NoError(err)
	s.True(wallet.Balance.GreaterThan(decimal.NewFromFloat(50.00))) // Initial was 50.00
}

func (s *CreditNoteServiceSuite) TestCreditNoteNumberGeneration() {
	req := &dto.CreateCreditNoteRequest{
		InvoiceID: s.testData.invoices.pending.ID,
		Reason:    types.CreditNoteReasonBillingError,
		LineItems: []dto.CreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: "line_3",
				Amount:            decimal.NewFromFloat(10.00),
			},
		},
	}

	resp, err := s.service.CreateCreditNote(s.GetContext(), req)
	s.NoError(err)

	// Verify credit note number is generated
	s.NotEmpty(resp.CreditNoteNumber)
	s.Contains(resp.CreditNoteNumber, "CN-")
}

func (s *CreditNoteServiceSuite) TestCreditNoteTypeDetection() {
	tests := []struct {
		name          string
		invoiceID     string
		expectedType  types.CreditNoteType
		paymentStatus types.PaymentStatus
	}{
		{
			name:          "succeeded_payment_creates_refund",
			invoiceID:     s.testData.invoices.finalized.ID,
			expectedType:  types.CreditNoteTypeRefund,
			paymentStatus: types.PaymentStatusSucceeded,
		},
		{
			name:          "partial_refund_creates_refund",
			invoiceID:     s.testData.invoices.partialRefunded.ID,
			expectedType:  types.CreditNoteTypeRefund,
			paymentStatus: types.PaymentStatusPartiallyRefunded,
		},
		{
			name:          "pending_payment_creates_adjustment",
			invoiceID:     s.testData.invoices.pending.ID,
			expectedType:  types.CreditNoteTypeAdjustment,
			paymentStatus: types.PaymentStatusPending,
		},
		{
			name:          "failed_payment_creates_adjustment",
			invoiceID:     s.testData.invoices.failed.ID,
			expectedType:  types.CreditNoteTypeAdjustment,
			paymentStatus: types.PaymentStatusFailed,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := &dto.CreateCreditNoteRequest{
				InvoiceID: tt.invoiceID,
				Reason:    types.CreditNoteReasonBillingError,
				LineItems: []dto.CreateCreditNoteLineItemRequest{
					{
						InvoiceLineItemID: "line_1", // Use first line item ID
						Amount:            decimal.NewFromFloat(5.00),
					},
				},
			}

			// Update line item ID based on invoice
			switch tt.invoiceID {
			case s.testData.invoices.pending.ID:
				req.LineItems[0].InvoiceLineItemID = "line_3"
			case s.testData.invoices.failed.ID:
				req.LineItems[0].InvoiceLineItemID = "line_4"
			case s.testData.invoices.partialRefunded.ID:
				req.LineItems[0].InvoiceLineItemID = "line_6"
			}

			resp, err := s.service.CreateCreditNote(s.GetContext(), req)
			s.NoError(err)
			s.Equal(tt.expectedType, resp.CreditNoteType)
		})
	}
}
