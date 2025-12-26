package nomod

import (
	"context"
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PaymentService handles Nomod payment operations
type PaymentService struct {
	client         NomodClient
	customerSvc    NomodCustomerService
	invoiceSyncSvc *InvoiceSyncService
	logger         *logger.Logger
}

// NewPaymentService creates a new Nomod payment service
func NewPaymentService(
	client NomodClient,
	customerSvc NomodCustomerService,
	invoiceSyncSvc *InvoiceSyncService,
	logger *logger.Logger,
) *PaymentService {
	return &PaymentService{
		client:         client,
		customerSvc:    customerSvc,
		invoiceSyncSvc: invoiceSyncSvc,
		logger:         logger,
	}
}

// CreatePaymentLinkRequest represents a FlexPrice request to create a Nomod payment link
type CreatePaymentLinkReq struct {
	InvoiceID     string
	CustomerID    string
	Amount        decimal.Decimal
	Currency      string
	SuccessURL    string
	FailureURL    string
	Metadata      map[string]string
	PaymentID     string
	EnvironmentID string
}

// CreatePaymentLinkResponse represents the response after creating a payment link
type CreatePaymentLinkResp struct {
	ID                 string          // Nomod payment link ID
	PaymentURL         string          // URL for the payment link
	Amount             decimal.Decimal // Amount in original currency
	Currency           string          // Currency code
	Status             string          // Payment link status
	ReferenceID        string          // Nomod reference ID
	IsNomodInvoiceLink bool            // Whether the payment link is a Nomod invoice link
}

// CreatePaymentLink creates a payment link in Nomod
// This creates a standalone payment link without requiring invoice sync
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req CreatePaymentLinkReq, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*CreatePaymentLinkResp, error) {
	s.logger.Infow("creating payment link in Nomod",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount,
		"currency", req.Currency)

	// Validate invoice and check payment eligibility
	invoiceResp, err := invoiceService.GetInvoice(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.NewError("failed to get invoice").
			WithHint("Invoice not found").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate invoice payment status
	if invoiceResp.PaymentStatus == types.PaymentStatusSucceeded {
		return nil, ierr.NewError("invoice is already paid").
			WithHint("Cannot create payment link for an already paid invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"payment_status": invoiceResp.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if invoiceResp.InvoiceStatus == types.InvoiceStatusVoided {
		return nil, ierr.NewError("invoice is voided").
			WithHint("Cannot create payment link for a voided invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"invoice_status": invoiceResp.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate payment amount against invoice remaining balance
	if req.Amount.GreaterThan(invoiceResp.AmountRemaining) {
		return nil, ierr.NewError("payment amount exceeds invoice remaining balance").
			WithHint("Payment amount cannot be greater than the remaining balance on the invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":        req.InvoiceID,
				"payment_amount":    req.Amount.String(),
				"invoice_remaining": invoiceResp.AmountRemaining.String(),
				"invoice_total":     invoiceResp.AmountDue.String(),
				"invoice_paid":      invoiceResp.AmountPaid.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate currency matches invoice currency
	if req.Currency != invoiceResp.Currency {
		return nil, ierr.NewError("payment currency does not match invoice currency").
			WithHint("Payment currency must match the invoice currency").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":       req.InvoiceID,
				"payment_currency": req.Currency,
				"invoice_currency": invoiceResp.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if invoice is already synced to Nomod (optional - reuse if exists)
	if s.invoiceSyncSvc != nil {
		nomodInvoiceMapping, err := s.invoiceSyncSvc.GetExistingNomodMapping(ctx, req.InvoiceID)
		if err == nil && nomodInvoiceMapping != nil {
			// Check if payment URL is stored in metadata
			if paymentURL, ok := nomodInvoiceMapping.Metadata["nomod_payment_url"].(string); ok && paymentURL != "" {
				nomodInvoiceID := nomodInvoiceMapping.ProviderEntityID

				s.logger.Infow("invoice already synced to Nomod, returning stored payment URL",
					"flexprice_invoice_id", req.InvoiceID,
					"nomod_invoice_id", nomodInvoiceID,
					"payment_url", paymentURL)

				// Parse metadata
				var referenceID, status string
				if refID, ok := nomodInvoiceMapping.Metadata["nomod_reference_id"].(string); ok {
					referenceID = refID
				}
				if st, ok := nomodInvoiceMapping.Metadata["nomod_status"].(string); ok {
					status = st
				}

				// Return the Nomod invoice payment URL
				return &CreatePaymentLinkResp{
					ID:                 nomodInvoiceID,
					PaymentURL:         paymentURL,
					Amount:             req.Amount,
					Currency:           req.Currency,
					Status:             status,
					ReferenceID:        referenceID,
					IsNomodInvoiceLink: true,
				}, nil
			}

			// If no payment URL in metadata, log and continue to create separate payment link
			s.logger.Debugw("invoice synced to Nomod but no payment URL found in metadata",
				"flexprice_invoice_id", req.InvoiceID,
				"nomod_invoice_id", nomodInvoiceMapping.ProviderEntityID)
		}
	}

	// Continue with creating standalone payment link...
	// Ensure customer is synced to Nomod before creating payment link
	flexpriceCustomer, err := s.customerSvc.EnsureCustomerSyncedToNomod(ctx, req.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Nomod").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Nomod customer ID (should exist after sync)
	nomodCustomerID, exists := flexpriceCustomer.Metadata["nomod_customer_id"]
	if !exists || nomodCustomerID == "" {
		return nil, ierr.NewError("customer does not have Nomod customer ID after sync").
			WithHint("Failed to sync customer to Nomod").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Build line items from invoice
	var lineItems []LineItem
	for _, item := range invoiceResp.LineItems {
		// Skip zero-amount items
		if item.Amount.IsZero() {
			continue
		}

		// Get item name with fallback
		itemName := "Subscription"
		if item.DisplayName != nil && *item.DisplayName != "" {
			itemName = *item.DisplayName
		} else if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
			itemName = *item.PlanDisplayName
		}

		// Convert quantity from decimal to int
		quantity := 1
		if !item.Quantity.IsZero() && item.Quantity.GreaterThan(decimal.Zero) {
			qty := int(item.Quantity.IntPart())
			if qty > 0 && qty <= 999 {
				quantity = qty
			}
			// For quantities > 999, we default to 1 and use the total amount
		}

		lineItems = append(lineItems, LineItem{
			Name:     itemName,
			Amount:   item.Amount.StringFixed(2),
			Quantity: quantity,
		})
	}

	if len(lineItems) == 0 {
		return nil, ierr.NewError("invoice has no line items").
			WithHint("Cannot create payment link without line items").
			Mark(ierr.ErrValidation)
	}

	// Build payment link request
	paymentLinkReq := CreatePaymentLinkRequest{
		Currency: strings.ToUpper(req.Currency),
		Items:    lineItems,
	}

	// Add optional title
	if invoiceResp.InvoiceNumber != nil && *invoiceResp.InvoiceNumber != "" {
		title := fmt.Sprintf("Invoice %s", *invoiceResp.InvoiceNumber)
		paymentLinkReq.Title = &title
	}

	// Add note with metadata including customer and invoice IDs
	note := fmt.Sprintf("Payment for - %s ",
		flexpriceCustomer.Name)
	paymentLinkReq.Note = &note

	// Add success and failure URLs if provided
	if req.SuccessURL != "" {
		paymentLinkReq.SuccessURL = &req.SuccessURL
	}
	if req.FailureURL != "" {
		paymentLinkReq.FailureURL = &req.FailureURL
	}

	s.logger.Infow("creating standalone payment link in Nomod",
		"invoice_id", req.InvoiceID,
		"nomod_customer_id", nomodCustomerID,
		"line_items_count", len(lineItems),
		"amount", req.Amount,
		"currency", req.Currency)

	// Create payment link via Nomod API
	nomodPaymentLink, err := s.client.CreatePaymentLink(ctx, paymentLinkReq)
	if err != nil {
		s.logger.Errorw("failed to create payment link in Nomod",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, ierr.WithError(err).
			WithHint("Failed to create payment link in Nomod").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("successfully created standalone payment link in Nomod",
		"invoice_id", req.InvoiceID,
		"nomod_payment_link_id", nomodPaymentLink.ID,
		"payment_url", nomodPaymentLink.URL)

	// Parse amount with error handling - critical for payment links
	amount, err := decimal.NewFromString(nomodPaymentLink.Amount)
	if err != nil {
		s.logger.Errorw("failed to parse payment link amount from Nomod",
			"error", err,
			"invoice_id", req.InvoiceID,
			"nomod_amount", nomodPaymentLink.Amount,
			"payment_link_id", nomodPaymentLink.ID)
		return nil, ierr.WithError(err).
			WithHint("Failed to parse payment amount from Nomod response").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":      req.InvoiceID,
				"payment_link_id": nomodPaymentLink.ID,
				"raw_amount":      nomodPaymentLink.Amount,
			}).
			Mark(ierr.ErrInternal)
	}

	return &CreatePaymentLinkResp{
		ID:                 nomodPaymentLink.ID,
		PaymentURL:         nomodPaymentLink.URL,
		Amount:             amount,
		Currency:           nomodPaymentLink.Currency,
		Status:             nomodPaymentLink.Status,
		ReferenceID:        nomodPaymentLink.ReferenceID,
		IsNomodInvoiceLink: false, // This is a standalone payment link
	}, nil
}
