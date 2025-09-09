package payment

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Payment represents a payment transaction
type Payment struct {
	// Unique identifier for this payment transaction
	ID string `json:"id"`
	// Unique key used in the idempotency_key field to prevent duplicate payment processing
	IdempotencyKey string `json:"idempotency_key"`
	// The destination_type indicates what entity this payment is being made to (invoice, subscription, etc.)
	DestinationType types.PaymentDestinationType `json:"destination_type"`
	// The destination_id specifies which specific entity is receiving this payment
	DestinationID string `json:"destination_id"`
	// The payment_method_type defines how the payment will be processed (credit_card, bank_transfer, offline, etc.)
	PaymentMethodType types.PaymentMethodType `json:"payment_method_type"`
	// The payment_method_id identifies which specific payment method to use for processing
	PaymentMethodID string `json:"payment_method_id"`
	// The payment_gateway field contains the name of the gateway used to process this transaction (optional)
	PaymentGateway *string `json:"payment_gateway,omitempty"`
	// The gateway_payment_id is the transaction identifier from the external payment gateway (optional)
	GatewayPaymentID *string `json:"gateway_payment_id,omitempty"`
	// The gateway_tracking_id is the tracking identifier from the external payment gateway (optional)
	GatewayTrackingID *string `json:"gateway_tracking_id,omitempty"`
	// The gateway_metadata field contains gateway-specific metadata (optional)
	GatewayMetadata types.Metadata `json:"gateway_metadata,omitempty"`
	// The amount field specifies the payment value in the given currency
	Amount decimal.Decimal `json:"amount"`
	// The currency field uses a three-letter ISO code (USD, EUR, GBP, etc.)
	Currency string `json:"currency"`
	// The payment_status shows the current state of this payment (pending, succeeded, failed, etc.)
	PaymentStatus types.PaymentStatus `json:"payment_status"`
	// The track_attempts flag indicates whether payment processing attempts are being monitored
	TrackAttempts bool `json:"track_attempts"`
	// The metadata field contains additional custom key-value pairs for this payment (optional)
	Metadata types.Metadata `json:"metadata,omitempty"`
	// The succeeded_at timestamp shows when this payment was successfully completed (optional)
	SucceededAt *time.Time `json:"succeeded_at,omitempty"`
	// The failed_at timestamp indicates when this payment failed (optional)
	FailedAt *time.Time `json:"failed_at,omitempty"`
	// The refunded_at timestamp shows when this payment was refunded (optional)
	RefundedAt *time.Time `json:"refunded_at,omitempty"`
	// The recorded_at timestamp indicates when this payment was manually recorded (optional)
	RecordedAt *time.Time `json:"recorded_at,omitempty"`
	// The error_message field provides details about why the payment failed (optional)
	ErrorMessage *string `json:"error_message,omitempty"`
	// The attempts array contains all processing attempts made for this payment (optional)
	Attempts []*PaymentAttempt `json:"attempts,omitempty"`
	// The environment_id identifies which environment this payment belongs to
	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// PaymentAttempt represents an attempt to process a payment
type PaymentAttempt struct {
	// Unique identifier for this specific payment attempt
	ID string `json:"id"`
	// The payment_id links this attempt to its parent payment transaction
	PaymentID string `json:"payment_id"`
	// The attempt_number shows the sequential order of this processing attempt
	AttemptNumber int `json:"attempt_number"`
	// The payment_status indicates the outcome of this specific attempt (pending, succeeded, failed, etc.)
	PaymentStatus types.PaymentStatus `json:"payment_status"`
	// The gateway_attempt_id is the identifier from the external payment gateway for this attempt (optional)
	GatewayAttemptID *string `json:"gateway_attempt_id,omitempty"`
	// The error_message field explains why this particular attempt failed (optional)
	ErrorMessage *string `json:"error_message,omitempty"`
	// The metadata field stores additional custom data for this attempt (optional)
	Metadata types.Metadata `json:"metadata,omitempty"`
	// The environment_id specifies which environment this attempt belongs to
	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// Validate validates the payment
func (p *Payment) Validate() error {
	if p.Amount.IsZero() || p.Amount.IsNegative() {
		return ierr.NewError("invalid amount").
			WithHint("Amount must be greater than 0").
			Mark(ierr.ErrValidation)
	}
	if err := p.DestinationType.Validate(); err != nil {
		return ierr.NewError("invalid destination type").
			WithHint("Destination type is invalid").
			Mark(ierr.ErrValidation)
	}
	if p.DestinationID == "" {
		return ierr.NewError("invalid destination id").
			WithHint("Destination id is invalid").
			Mark(ierr.ErrValidation)
	}
	if p.PaymentMethodType == "" {
		return ierr.NewError("invalid payment method type").
			WithHint("Payment method type is invalid").
			Mark(ierr.ErrValidation)
	}
	if p.Currency == "" {
		return ierr.NewError("invalid currency").
			WithHint("Currency is invalid").
			Mark(ierr.ErrValidation)
	}

	// payment method type validations
	if p.PaymentMethodType == types.PaymentMethodTypeOffline {
		if p.PaymentMethodID != "" {
			return ierr.NewError("payment method id is not allowed for offline payment method type").
				WithHint("Payment method id is invalid").
				Mark(ierr.ErrValidation)
		}
	} else if p.PaymentMethodType == types.PaymentMethodTypePaymentLink {
		// For payment links, payment method ID should be empty
		if p.PaymentMethodID != "" {
			return ierr.NewError("payment method id is not allowed for payment link method type").
				WithHint("Payment method id is invalid for payment links").
				Mark(ierr.ErrValidation)
		}
	} else if p.PaymentMethodType == types.PaymentMethodTypeCard {
		// For card payments, payment method ID is optional - it will be fetched automatically if empty
		// No validation needed here as the payment processor will handle fetching the saved payment method
	} else if p.PaymentMethodID == "" {
		return ierr.NewError("invalid payment method id").
			WithHint("Payment method id is invalid").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the payment attempt
func (pa *PaymentAttempt) Validate() error {
	if pa.PaymentID == "" {
		return ierr.NewError("invalid payment id").
			WithHint("Payment id is invalid").
			Mark(ierr.ErrValidation)
	}
	if pa.AttemptNumber <= 0 {
		return ierr.NewError("invalid attempt number").
			WithHint("Attempt number is invalid").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TableName returns the table name for the payment
func (p *Payment) TableName() string {
	return "payments"
}

// TableName returns the table name for the payment attempt
func (pa *PaymentAttempt) TableName() string {
	return "payment_attempts"
}

// FromEnt converts an Ent payment to a domain payment
func FromEnt(p *ent.Payment) *Payment {
	if p == nil {
		return nil
	}

	payment := &Payment{
		ID:                p.ID,
		IdempotencyKey:    p.IdempotencyKey,
		DestinationType:   types.PaymentDestinationType(p.DestinationType),
		DestinationID:     p.DestinationID,
		PaymentMethodType: types.PaymentMethodType(p.PaymentMethodType),
		PaymentMethodID:   p.PaymentMethodID,
		PaymentGateway:    p.PaymentGateway,
		GatewayPaymentID:  p.GatewayPaymentID,
		GatewayTrackingID: p.GatewayTrackingID,
		GatewayMetadata:   p.GatewayMetadata,
		Amount:            p.Amount,
		Currency:          p.Currency,
		PaymentStatus:     types.PaymentStatus(p.PaymentStatus),
		TrackAttempts:     p.TrackAttempts,
		Metadata:          p.Metadata,
		SucceededAt:       p.SucceededAt,
		FailedAt:          p.FailedAt,
		RefundedAt:        p.RefundedAt,
		RecordedAt:        p.RecordedAt,
		ErrorMessage:      p.ErrorMessage,
		EnvironmentID:     p.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  p.TenantID,
			Status:    types.Status(p.Status),
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
			CreatedBy: p.CreatedBy,
			UpdatedBy: p.UpdatedBy,
		},
	}

	if p.Edges.Attempts != nil {
		payment.Attempts = make([]*PaymentAttempt, len(p.Edges.Attempts))
		for i, a := range p.Edges.Attempts {
			payment.Attempts[i] = FromEntAttempt(a)
		}
	}

	return payment
}

// FromEntAttempt converts an Ent payment attempt to a domain payment attempt
func FromEntAttempt(a *ent.PaymentAttempt) *PaymentAttempt {
	if a == nil {
		return nil
	}

	metadata := types.Metadata{}
	if a.Metadata != nil {
		for k, v := range a.Metadata {
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	return &PaymentAttempt{
		ID:               a.ID,
		PaymentID:        a.PaymentID,
		AttemptNumber:    a.AttemptNumber,
		PaymentStatus:    types.PaymentStatus(a.PaymentStatus),
		GatewayAttemptID: a.GatewayAttemptID,
		ErrorMessage:     a.ErrorMessage,
		Metadata:         metadata,
		EnvironmentID:    a.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  a.TenantID,
			Status:    types.Status(a.Status),
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
			CreatedBy: a.CreatedBy,
			UpdatedBy: a.UpdatedBy,
		},
	}
}

// FromEntList converts a list of Ent payments to domain payments
func FromEntList(payments []*ent.Payment) []*Payment {
	if payments == nil {
		return nil
	}

	result := make([]*Payment, len(payments))
	for i, p := range payments {
		result[i] = FromEnt(p)
	}
	return result
}

// FromEntAttemptList converts a list of Ent payment attempts to domain payment attempts
func FromEntAttemptList(attempts []*ent.PaymentAttempt) []*PaymentAttempt {
	if attempts == nil {
		return nil
	}

	result := make([]*PaymentAttempt, len(attempts))
	for i, a := range attempts {
		result[i] = FromEntAttempt(a)
	}
	return result
}
