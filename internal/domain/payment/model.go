package payment

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Payment represents a payment transaction
type Payment struct {
	ID                string                       `json:"id"`
	IdempotencyKey    string                       `json:"idempotency_key"`
	DestinationType   types.PaymentDestinationType `json:"destination_type"`
	DestinationID     string                       `json:"destination_id"`
	PaymentMethodType types.PaymentMethodType      `json:"payment_method_type"`
	PaymentMethodID   string                       `json:"payment_method_id"`
	PaymentGateway    *string                      `json:"payment_gateway,omitempty"`
	GatewayPaymentID  *string                      `json:"gateway_payment_id,omitempty"`
	Amount            decimal.Decimal              `json:"amount"`
	Currency          string                       `json:"currency"`
	PaymentStatus     types.PaymentStatus          `json:"payment_status"`
	TrackAttempts     bool                         `json:"track_attempts"`
	Metadata          types.Metadata               `json:"metadata,omitempty"`
	SucceededAt       *time.Time                   `json:"succeeded_at,omitempty"`
	FailedAt          *time.Time                   `json:"failed_at,omitempty"`
	RefundedAt        *time.Time                   `json:"refunded_at,omitempty"`
	ErrorMessage      *string                      `json:"error_message,omitempty"`
	Attempts          []*PaymentAttempt            `json:"attempts,omitempty"`

	types.BaseModel
}

// PaymentAttempt represents an attempt to process a payment
type PaymentAttempt struct {
	ID               string              `json:"id"`
	PaymentID        string              `json:"payment_id"`
	AttemptNumber    int                 `json:"attempt_number"`
	PaymentStatus    types.PaymentStatus `json:"payment_status"`
	GatewayAttemptID *string             `json:"gateway_attempt_id,omitempty"`
	ErrorMessage     *string             `json:"error_message,omitempty"`
	Metadata         types.Metadata      `json:"metadata,omitempty"`

	types.BaseModel
}

// Validate validates the payment
func (p *Payment) Validate() error {
	if p.Amount.IsZero() || p.Amount.IsNegative() {
		return errors.New(errors.ErrCodeValidation, "invalid amount")
	}
	if err := p.DestinationType.Validate(); err != nil {
		return errors.New(errors.ErrCodeValidation, "invalid destination type")
	}
	if p.DestinationID == "" {
		return errors.New(errors.ErrCodeValidation, "invalid destination id")
	}
	if p.PaymentMethodType == "" {
		return errors.New(errors.ErrCodeValidation, "invalid payment method type")
	}
	if p.Currency == "" {
		return errors.New(errors.ErrCodeValidation, "invalid currency")
	}

	// payment method type validations
	if p.PaymentMethodType == types.PaymentMethodTypeOffline {
		if p.PaymentMethodID != "" {
			return errors.New(errors.ErrCodeValidation, "payment method id is not allowed for offline payment method type")
		}
	} else if p.PaymentMethodID == "" {
		return errors.New(errors.ErrCodeValidation, "invalid payment method id")
	}

	return nil
}

// Validate validates the payment attempt
func (pa *PaymentAttempt) Validate() error {
	if pa.PaymentID == "" {
		return errors.New(errors.ErrCodeValidation, "invalid payment id")
	}
	if pa.AttemptNumber <= 0 {
		return errors.New(errors.ErrCodeValidation, "invalid attempt number")
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
		DestinationType:   types.PaymentDestinationType(p.DestinationType),
		DestinationID:     p.DestinationID,
		PaymentMethodType: types.PaymentMethodType(p.PaymentMethodType),
		PaymentMethodID:   p.PaymentMethodID,
		PaymentGateway:    p.PaymentGateway,
		GatewayPaymentID:  p.GatewayPaymentID,
		Amount:            p.Amount,
		Currency:          p.Currency,
		PaymentStatus:     types.PaymentStatus(p.PaymentStatus),
		TrackAttempts:     p.TrackAttempts,
		Metadata:          p.Metadata,
		SucceededAt:       p.SucceededAt,
		FailedAt:          p.FailedAt,
		RefundedAt:        p.RefundedAt,
		ErrorMessage:      p.ErrorMessage,
		IdempotencyKey:    p.IdempotencyKey,
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
