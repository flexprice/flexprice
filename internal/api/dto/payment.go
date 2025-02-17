package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreatePaymentRequest represents a request to create a payment
type CreatePaymentRequest struct {
	IdempotencyKey    string                       `json:"idempotency_key,omitempty"`
	DestinationType   types.PaymentDestinationType `json:"destination_type" binding:"required"`
	DestinationID     string                       `json:"destination_id" binding:"required"`
	PaymentMethodType types.PaymentMethodType      `json:"payment_method_type" binding:"required"`
	PaymentMethodID   string                       `json:"payment_method_id"`
	Amount            decimal.Decimal              `json:"amount" binding:"required"`
	Currency          string                       `json:"currency" binding:"required"`
	Metadata          types.Metadata               `json:"metadata,omitempty"`
	ProcessPayment    bool                         `json:"process_payment" default:"true"`
}

// UpdatePaymentRequest represents a request to update a payment
type UpdatePaymentRequest struct {
	PaymentStatus *string         `json:"payment_status,omitempty"`
	Metadata      *types.Metadata `json:"metadata,omitempty"`
}

// PaymentResponse represents a payment response
type PaymentResponse struct {
	ID                string                       `json:"id"`
	IdempotencyKey    string                       `json:"idempotency_key"`
	DestinationType   types.PaymentDestinationType `json:"destination_type"`
	DestinationID     string                       `json:"destination_id"`
	PaymentMethodType types.PaymentMethodType      `json:"payment_method_type"`
	PaymentMethodID   string                       `json:"payment_method_id"`
	Amount            decimal.Decimal              `json:"amount"`
	Currency          string                       `json:"currency"`
	PaymentStatus     types.PaymentStatus          `json:"payment_status"`
	TrackAttempts     bool                         `json:"track_attempts"`
	Metadata          types.Metadata               `json:"metadata,omitempty"`
	SucceededAt       *time.Time                   `json:"succeeded_at,omitempty"`
	FailedAt          *time.Time                   `json:"failed_at,omitempty"`
	RefundedAt        *time.Time                   `json:"refunded_at,omitempty"`
	ErrorMessage      *string                      `json:"error_message,omitempty"`
	Attempts          []*PaymentAttemptResponse    `json:"attempts,omitempty"`
	TenantID          string                       `json:"tenant_id"`
	CreatedAt         time.Time                    `json:"created_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`
	CreatedBy         string                       `json:"created_by"`
	UpdatedBy         string                       `json:"updated_by"`
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
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}

	// Set payment status to pending
	p.PaymentStatus = types.PaymentStatusPending

	if r.PaymentMethodType == types.PaymentMethodTypeOffline || r.PaymentMethodType == types.PaymentMethodTypeCredits {
		p.TrackAttempts = false
		p.PaymentGateway = nil
		p.GatewayPaymentID = nil
		if p.PaymentMethodID != "" {
			return nil, errors.New(errors.ErrCodeValidation, "payment method id is not allowed for offline payment method type")
		}
	} else {
		if p.PaymentMethodID == "" {
			return nil, errors.New(errors.ErrCodeValidation, "payment method id is required for online payment method type")
		}
		p.TrackAttempts = true
	}

	return p, nil
}
