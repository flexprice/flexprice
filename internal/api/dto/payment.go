package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreatePaymentRequest represents a request to create a payment
type CreatePaymentRequest struct {
	IdempotencyKey         string                       `json:"idempotency_key,omitempty"`
	DestinationType        types.PaymentDestinationType `json:"destination_type" binding:"required"`
	DestinationID          string                       `json:"destination_id" binding:"required"`
	PaymentMethodType      types.PaymentMethodType      `json:"payment_method_type" binding:"required"`
	PaymentMethodID        string                       `json:"payment_method_id"`
	PaymentGateway         *types.PaymentGatewayType    `json:"payment_gateway,omitempty"`
	Amount                 decimal.Decimal              `json:"amount" binding:"required"`
	Currency               string                       `json:"currency" binding:"required"`
	SuccessURL             string                       `json:"success_url,omitempty"`
	CancelURL              string                       `json:"cancel_url,omitempty"`
	Metadata               types.Metadata               `json:"metadata,omitempty"`
	ProcessPayment         bool                         `json:"process_payment" default:"true"`
	SaveCardAndMakeDefault bool                         `json:"save_card_and_make_default" default:"false"`
}

// UpdatePaymentRequest represents a request to update a payment
type UpdatePaymentRequest struct {
	PaymentStatus    *string         `json:"payment_status,omitempty"`
	PaymentGateway   *string         `json:"payment_gateway,omitempty"`
	GatewayPaymentID *string         `json:"gateway_payment_id,omitempty"`
	PaymentMethodID  *string         `json:"payment_method_id,omitempty"`
	Metadata         *types.Metadata `json:"metadata,omitempty"`
	SucceededAt      *time.Time      `json:"succeeded_at,omitempty"`
	FailedAt         *time.Time      `json:"failed_at,omitempty"`
	ErrorMessage     *string         `json:"error_message,omitempty"`
}

// PaymentResponse represents a payment response
type PaymentResponse struct {
	ID                     string                       `json:"id"`
	IdempotencyKey         string                       `json:"idempotency_key"`
	DestinationType        types.PaymentDestinationType `json:"destination_type"`
	DestinationID          string                       `json:"destination_id"`
	PaymentMethodType      types.PaymentMethodType      `json:"payment_method_type"`
	PaymentMethodID        string                       `json:"payment_method_id"`
	Amount                 decimal.Decimal              `json:"amount"`
	Currency               string                       `json:"currency"`
	PaymentStatus          types.PaymentStatus          `json:"payment_status"`
	TrackAttempts          bool                         `json:"track_attempts"`
	PaymentGateway         *string                      `json:"payment_gateway,omitempty"`
	GatewayPaymentID       *string                      `json:"gateway_payment_id,omitempty"`
	GatewayTrackingID      *string                      `json:"gateway_tracking_id,omitempty"`
	GatewayMetadata        types.Metadata               `json:"gateway_metadata,omitempty"`
	PaymentURL             *string                      `json:"payment_url,omitempty"`
	Metadata               types.Metadata               `json:"metadata,omitempty"`
	SucceededAt            *time.Time                   `json:"succeeded_at,omitempty"`
	FailedAt               *time.Time                   `json:"failed_at,omitempty"`
	RefundedAt             *time.Time                   `json:"refunded_at,omitempty"`
	ErrorMessage           *string                      `json:"error_message,omitempty"`
	Attempts               []*PaymentAttemptResponse    `json:"attempts,omitempty"`
	InvoiceNumber          *string                      `json:"invoice_number,omitempty"`
	TenantID               string                       `json:"tenant_id"`
	SaveCardAndMakeDefault bool                         `json:"save_card_and_make_default"`
	CreatedAt              time.Time                    `json:"created_at"`
	UpdatedAt              time.Time                    `json:"updated_at"`
	CreatedBy              string                       `json:"created_by"`
	UpdatedBy              string                       `json:"updated_by"`
}

// PaymentAttemptResponse represents a payment attempt response
type PaymentAttemptResponse struct {
	ID            string         `json:"id"`
	PaymentID     string         `json:"payment_id"`
	AttemptNumber int            `json:"attempt_number"`
	ErrorMessage  *string        `json:"error_message,omitempty"`
	Metadata      types.Metadata `json:"metadata,omitempty"`
	TenantID      string         `json:"tenant_id"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	CreatedBy     string         `json:"created_by"`
	UpdatedBy     string         `json:"updated_by"`
}

// ListPaymentsResponse represents a paginated list of payments
type ListPaymentsResponse struct {
	Items      []*PaymentResponse       `json:"items"`
	Pagination types.PaginationResponse `json:"pagination"`
}

// NewPaymentResponse creates a new payment response from a payment
func NewPaymentResponse(p *payment.Payment) *PaymentResponse {
	resp := &PaymentResponse{
		ID:                p.ID,
		IdempotencyKey:    p.IdempotencyKey,
		DestinationType:   p.DestinationType,
		DestinationID:     p.DestinationID,
		PaymentMethodType: p.PaymentMethodType,
		PaymentMethodID:   p.PaymentMethodID,
		Amount:            p.Amount,
		Currency:          p.Currency,
		PaymentStatus:     p.PaymentStatus,
		TrackAttempts:     p.TrackAttempts,
		PaymentGateway:    p.PaymentGateway,
		GatewayPaymentID:  p.GatewayPaymentID,
		GatewayTrackingID: p.GatewayTrackingID,
		GatewayMetadata:   p.GatewayMetadata,
		Metadata:          p.Metadata,
		SucceededAt:       p.SucceededAt,
		FailedAt:          p.FailedAt,
		RefundedAt:        p.RefundedAt,
		ErrorMessage:      p.ErrorMessage,
		TenantID:          p.TenantID,
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
		CreatedBy:         p.CreatedBy,
		UpdatedBy:         p.UpdatedBy,
	}

	// Extract payment URL from gateway metadata for payment links
	if p.PaymentMethodType == types.PaymentMethodTypePaymentLink && p.GatewayMetadata != nil {
		if paymentURL, exists := p.GatewayMetadata["payment_url"]; exists {
			resp.PaymentURL = &paymentURL
		}
	}

	// Extract SaveCardAndMakeDefault from gateway metadata
	if p.GatewayMetadata != nil {
		if saveCardStr, exists := p.GatewayMetadata["save_card_and_make_default"]; exists {
			resp.SaveCardAndMakeDefault = saveCardStr == "true"
		}
	}

	if p.Attempts != nil {
		resp.Attempts = make([]*PaymentAttemptResponse, len(p.Attempts))
		for i, a := range p.Attempts {
			resp.Attempts[i] = NewPaymentAttemptResponse(a)
		}
	}

	return resp
}

// NewPaymentAttemptResponse creates a new payment attempt response from a payment attempt
func NewPaymentAttemptResponse(a *payment.PaymentAttempt) *PaymentAttemptResponse {
	return &PaymentAttemptResponse{
		ID:            a.ID,
		PaymentID:     a.PaymentID,
		AttemptNumber: a.AttemptNumber,
		ErrorMessage:  a.ErrorMessage,
		Metadata:      a.Metadata,
		TenantID:      a.TenantID,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
		CreatedBy:     a.CreatedBy,
		UpdatedBy:     a.UpdatedBy,
	}
}

// ToPayment converts a create payment request to a payment
func (r *CreatePaymentRequest) ToPayment(ctx context.Context) (*payment.Payment, error) {
	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return nil, err
	}

	// Initialize gateway metadata for storing payment link related fields
	gatewayMetadata := types.Metadata{}

	// Store SuccessURL and CancelURL in gateway metadata if provided
	if r.SuccessURL != "" {
		gatewayMetadata["success_url"] = r.SuccessURL
	}
	if r.CancelURL != "" {
		gatewayMetadata["cancel_url"] = r.CancelURL
	}
	if r.SaveCardAndMakeDefault {
		gatewayMetadata["save_card_and_make_default"] = "true"
	}

	p := &payment.Payment{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		IdempotencyKey:    r.IdempotencyKey,
		DestinationType:   r.DestinationType,
		DestinationID:     r.DestinationID,
		PaymentMethodType: r.PaymentMethodType,
		PaymentMethodID:   r.PaymentMethodID,
		Amount:            r.Amount,
		Currency:          strings.ToLower(r.Currency),
		Metadata:          r.Metadata,
		GatewayMetadata:   gatewayMetadata,
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}

	// Set payment status to pending
	p.PaymentStatus = types.PaymentStatusPending

	// Handle payment gateway if provided
	if r.PaymentGateway != nil {
		p.PaymentGateway = lo.ToPtr(string(*r.PaymentGateway))
	}

	if r.PaymentMethodType == types.PaymentMethodTypeOffline {
		p.TrackAttempts = false
		p.PaymentGateway = nil
		p.GatewayPaymentID = nil
		if p.PaymentMethodID != "" {
			return nil, ierr.NewError("payment method id is not allowed for offline payment method type").
				WithHint("Do not provide payment method ID for offline or credits payment methods").
				WithReportableDetails(map[string]interface{}{
					"payment_method_type": r.PaymentMethodType,
					"payment_method_id":   r.PaymentMethodID,
				}).
				Mark(ierr.ErrValidation)
		}
	} else if r.PaymentMethodType == types.PaymentMethodTypePaymentLink {
		// For payment links, set initial status as initiated
		p.PaymentStatus = types.PaymentStatusInitiated
		p.TrackAttempts = true
		p.PaymentMethodID = ""   // Set to empty string for payment links
		p.GatewayPaymentID = nil // Should be nil for payment links initially
		if p.PaymentGateway == nil {
			return nil, ierr.NewError("payment gateway is required for payment link method type").
				WithHint("Payment gateway must be specified for payment link method type").
				WithReportableDetails(map[string]interface{}{
					"payment_method_type": r.PaymentMethodType,
				}).
				Mark(ierr.ErrValidation)
		}
	} else if r.PaymentMethodType != types.PaymentMethodTypeCredits && r.PaymentMethodType != types.PaymentMethodTypePaymentLink && r.PaymentMethodType != types.PaymentMethodTypeCard {
		if p.PaymentMethodID == "" {
			return nil, ierr.NewError("payment method id is required for online payment method type").
				WithHint("Payment method ID is required for online payment methods").
				WithReportableDetails(map[string]interface{}{
					"payment_method_type": r.PaymentMethodType,
				}).
				Mark(ierr.ErrValidation)
		}
		p.TrackAttempts = true
	} else if r.PaymentMethodType == types.PaymentMethodTypeCard {
		// For CARD payments, we'll automatically fetch the customer's saved payment method
		// So payment_method_id is optional - it will be populated during processing
		p.TrackAttempts = true
	}

	return p, nil
}

// GetCustomerPaymentMethodsRequest represents a request to get customer payment methods
type GetCustomerPaymentMethodsRequest struct {
	CustomerID string `json:"customer_id" binding:"required"`
}

// PaymentMethodResponse represents a payment method response
type PaymentMethodResponse struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Customer string                 `json:"customer"`
	Created  int64                  `json:"created"`
	Card     *CardDetails           `json:"card,omitempty"`
	Metadata map[string]interface{} `json:"metadata"`
}

// CardDetails represents card details in a payment method
type CardDetails struct {
	Brand       string `json:"brand"`
	Last4       string `json:"last4"`
	ExpMonth    int    `json:"exp_month"`
	ExpYear     int    `json:"exp_year"`
	Fingerprint string `json:"fingerprint"`
}

// ChargeSavedPaymentMethodRequest represents a request to charge a saved payment method
type ChargeSavedPaymentMethodRequest struct {
	CustomerID      string          `json:"customer_id" binding:"required"`
	PaymentMethodID string          `json:"payment_method_id" binding:"required"`
	Amount          decimal.Decimal `json:"amount" binding:"required"`
	Currency        string          `json:"currency" binding:"required"`
	InvoiceID       string          `json:"invoice_id,omitempty"`
}

// PaymentIntentResponse represents a payment intent response
type PaymentIntentResponse struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	CustomerID    string          `json:"customer_id"`
	PaymentMethod string          `json:"payment_method"`
	CreatedAt     int64           `json:"created_at"`
}

// SetDefaultPaymentMethodRequest represents a request to set default payment method
type SetDefaultPaymentMethodRequest struct {
	CustomerID      string `json:"customer_id" binding:"required"`
	PaymentMethodID string `json:"payment_method_id" binding:"required"`
}

// GetDefaultPaymentMethodRequest represents a request to get default payment method
type GetDefaultPaymentMethodRequest struct {
	CustomerID string `json:"customer_id" binding:"required"`
}
