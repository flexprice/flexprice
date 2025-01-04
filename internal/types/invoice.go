package types

import "time"

type InvoiceCadence string

const (
	// InvoiceCadenceArrear raises an invoice at the end of each billing period (in arrears)
	InvoiceCadenceArrear InvoiceCadence = "ARREAR"
	// InvoiceCadenceAdvance raises an invoice at the beginning of each billing period (in advance)
	InvoiceCadenceAdvance InvoiceCadence = "ADVANCE"
)

type InvoiceType string

const (
	// InvoiceTypeSubscription indicates invoice is for subscription charges
	InvoiceTypeSubscription InvoiceType = "SUBSCRIPTION"
	// InvoiceTypeOneOff indicates invoice is for one-time charges
	InvoiceTypeOneOff InvoiceType = "ONE_OFF"
	// InvoiceTypeCredit indicates invoice is for credit adjustments
	InvoiceTypeCredit InvoiceType = "CREDIT"
)

type InvoiceStatus string

const (
	// InvoiceStatusDraft indicates invoice is in draft state and can be modified
	InvoiceStatusDraft InvoiceStatus = "DRAFT"
	// InvoiceStatusFinalized indicates invoice is finalized and ready for payment
	InvoiceStatusFinalized InvoiceStatus = "FINALIZED"
	// InvoiceStatusVoided indicates invoice has been voided
	InvoiceStatusVoided InvoiceStatus = "VOIDED"
)

type InvoicePaymentStatus string

const (
	// InvoicePaymentStatusPending indicates payment is pending
	InvoicePaymentStatusPending InvoicePaymentStatus = "PENDING"
	// InvoicePaymentStatusSucceeded indicates payment was successful
	InvoicePaymentStatusSucceeded InvoicePaymentStatus = "SUCCEEDED"
	// InvoicePaymentStatusFailed indicates payment failed
	InvoicePaymentStatusFailed InvoicePaymentStatus = "FAILED"
)

type InvoiceBillingReason string

const (
	// InvoiceBillingReasonSubscriptionCreate indicates invoice is for subscription creation
	InvoiceBillingReasonSubscriptionCreate InvoiceBillingReason = "SUBSCRIPTION_CREATE"
	// InvoiceBillingReasonSubscriptionCycle indicates invoice is for subscription renewal
	InvoiceBillingReasonSubscriptionCycle InvoiceBillingReason = "SUBSCRIPTION_CYCLE"
	// InvoiceBillingReasonSubscriptionUpdate indicates invoice is for subscription update
	InvoiceBillingReasonSubscriptionUpdate InvoiceBillingReason = "SUBSCRIPTION_UPDATE"
	// InvoiceBillingReasonManual indicates invoice is created manually
	InvoiceBillingReasonManual InvoiceBillingReason = "MANUAL"
)

const (
	InvoiceDefaultDueDays = 30
)

// InvoiceFilter represents the filter options for listing invoices
type InvoiceFilter struct {
	Filter
	CustomerID     string                 `form:"customer_id" json:"customer_id,omitempty"`
	SubscriptionID string                 `form:"subscription_id" json:"subscription_id,omitempty"`
	InvoiceType    InvoiceType            `form:"invoice_type" json:"invoice_type,omitempty"`
	InvoiceStatus  []InvoiceStatus        `form:"invoice_status" json:"invoice_status,omitempty"`
	PaymentStatus  []InvoicePaymentStatus `form:"payment_status" json:"payment_status,omitempty"`
	StartTime      *time.Time             `form:"start_time" json:"start_time,omitempty"`
	EndTime        *time.Time             `form:"end_time" json:"end_time,omitempty"`
}
