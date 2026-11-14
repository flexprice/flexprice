package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalPaymentEvent struct {
	PaymentID string `json:"payment_id"`
	TenantID  string `json:"tenant_id"`
	AttemptID string `json:"attempt_id,omitempty"`
}

type Payment struct {
	ID                string                       `json:"id"`
	DestinationType   types.PaymentDestinationType `json:"destination_type"`
	DestinationID     string                       `json:"destination_id"`
	ConnectionID      *string                      `json:"connection_id"`
	PaymentMethodType types.PaymentMethodType      `json:"payment_method_type"`
	Amount            decimal.Decimal              `json:"amount" swaggertype:"string"`
	Currency          string                       `json:"currency"`
	PaymentStatus     types.PaymentStatus          `json:"payment_status"`
	PaymentGateway    *string                      `json:"payment_gateway,omitempty"`
	SucceededAt       *time.Time                   `json:"succeeded_at,omitempty"`
	FailedAt          *time.Time                   `json:"failed_at,omitempty"`
	RefundedAt        *time.Time                   `json:"refunded_at,omitempty"`
	VoidedAt          *time.Time                   `json:"voided_at,omitempty"`
	ErrorMessage      *string                      `json:"error_message,omitempty"`
	InvoiceNumber     *string                      `json:"invoice_number,omitempty"`
}

func NewPayment(resp *dto.PaymentResponse) *Payment {
	if resp == nil {
		return nil
	}
	return &Payment{
		ConnectionID:      resp.ConnectionID,
		ID:                resp.ID,
		DestinationType:   resp.DestinationType,
		DestinationID:     resp.DestinationID,
		PaymentMethodType: resp.PaymentMethodType,
		Amount:            resp.Amount,
		Currency:          resp.Currency,
		PaymentStatus:     resp.PaymentStatus,
		PaymentGateway:    resp.PaymentGateway,
		SucceededAt:       resp.SucceededAt,
		FailedAt:          resp.FailedAt,
		RefundedAt:        resp.RefundedAt,
		VoidedAt:          resp.VoidedAt,
		ErrorMessage:      resp.ErrorMessage,
		InvoiceNumber:     resp.InvoiceNumber,
	}
}

type PaymentWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Payment   *Payment               `json:"payment"`
}

func NewPaymentWebhookPayload(payment *dto.PaymentResponse, eventType types.WebhookEventName) *PaymentWebhookPayload {
	return &PaymentWebhookPayload{EventType: eventType, Payment: NewPayment(payment)}
}

type PaymentAttempt struct {
	ID               string         `json:"id"`
	AttemptNumber    int            `json:"attempt_number"`
	GatewayAttemptID *string        `json:"gateway_attempt_id,omitempty"`
	ErrorMessage     *string        `json:"error_message,omitempty"`
	Metadata         types.Metadata `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

func NewPaymentAttempt(resp *dto.PaymentAttemptResponse) *PaymentAttempt {
	if resp == nil {
		return nil
	}
	return &PaymentAttempt{
		ID:               resp.ID,
		AttemptNumber:    resp.AttemptNumber,
		GatewayAttemptID: resp.GatewayAttemptID,
		ErrorMessage:     resp.ErrorMessage,
		Metadata:         resp.Metadata,
		CreatedAt:        resp.CreatedAt,
	}
}

type PaymentAttemptWebhookPayload struct {
	EventType       types.WebhookEventName `json:"event_type"`
	Payment         *Payment               `json:"payment"`
	Attempt         *PaymentAttempt        `json:"attempt"`
	CheckoutSession *CheckoutSession       `json:"checkout_session,omitempty"`
}

// InternalMandateEvent is what the publisher puts in WebhookEvent.Payload for mandate.* events.
type InternalMandateEvent struct {
	Mandate Mandate `json:"mandate"`
}

type Mandate struct {
	ID                 string                   `json:"id"`
	CustomerID         string                   `json:"customer_id"`
	ExternalCustomerID string                   `json:"external_customer_id"`
	Provider           types.PaymentGatewayType `json:"provider"`
	Type               types.PaymentMethodType  `json:"type"`
	Status             string                   `json:"status"`
	FailureReason      *string                  `json:"failure_reason"`
	MaxAmount          *decimal.Decimal         `json:"max_amount,omitempty" swaggertype:"string"`
	Currency           string                   `json:"currency"`
	ExpiresAt          *time.Time               `json:"expires_at,omitempty"`
	BankAccount        *MandateBankAccount      `json:"bank_account,omitempty"`
	Card               *MandateCard             `json:"card,omitempty"`
	UPI                *MandateUPI              `json:"upi,omitempty"`
}

type MandateCard struct {
	Brand    string `json:"brand,omitempty"`
	Last4    string `json:"last4,omitempty"`
	ExpMonth int    `json:"exp_month,omitempty"`
	ExpYear  int    `json:"exp_year,omitempty"`
}

type MandateUPI struct {
	VPA string `json:"vpa,omitempty"`
}

type MandateBankAccount struct {
	Bank        string `json:"bank,omitempty"`
	IFSC        string `json:"ifsc,omitempty"`
	Last4       string `json:"last4,omitempty"`
	AccountType string `json:"account_type,omitempty"`
	AuthType    string `json:"auth_type,omitempty"`
}

// MandateWebhookPayload is the outbound payload for mandate.confirmed, mandate.rejected and mandate.cancelled.
type MandateWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Mandate   Mandate                `json:"mandate"`
}

func NewMandateWebhookPayload(internal *InternalMandateEvent, eventType types.WebhookEventName) *MandateWebhookPayload {
	return &MandateWebhookPayload{EventType: eventType, Mandate: internal.Mandate}
}
