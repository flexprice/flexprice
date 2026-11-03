package webhookDto

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalSubscriptionEvent struct {
	EventType        types.WebhookEventName `json:"event_type"`
	SubscriptionID   string                 `json:"subscription_id"`
	CustomerID       string                 `json:"customer_id,omitempty"`
	PaymentBehavior  string                 `json:"payment_behavior,omitempty"`
	CollectionMethod string                 `json:"collection_method,omitempty"`
	TenantID         string                 `json:"tenant_id"`
	EnvironmentID    string                 `json:"environment_id"`
}

type Subscription struct {
	ID                   string                   `json:"id"`
	CustomerID           string                   `json:"customer_id"`
	InvoicingCustomerID  *string                  `json:"invoicing_customer_id,omitempty"`
	PlanID               string                   `json:"plan_id"`
	LookupKey            string                   `json:"lookup_key,omitempty"`
	SubscriptionStatus   types.SubscriptionStatus `json:"subscription_status"`
	Currency             string                   `json:"currency"`
	StartDate            time.Time                `json:"start_date"`
	EndDate              *time.Time               `json:"end_date,omitempty"`
	CurrentPeriodStart   time.Time                `json:"current_period_start"`
	CurrentPeriodEnd     time.Time                `json:"current_period_end"`
	CancelledAt          *time.Time               `json:"cancelled_at,omitempty"`
	CancelAt             *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd    bool                     `json:"cancel_at_period_end"`
	TrialStart           *time.Time               `json:"trial_start,omitempty"`
	TrialEnd             *time.Time               `json:"trial_end,omitempty"`
	BillingCadence       types.BillingCadence     `json:"billing_cadence"`
	BillingPeriod        types.BillingPeriod      `json:"billing_period"`
	BillingPeriodCount   int                      `json:"billing_period_count"`
	PauseStatus          types.PauseStatus        `json:"pause_status"`
	SubscriptionType     types.SubscriptionType   `json:"subscription_type"`
	ParentSubscriptionID *string                  `json:"parent_subscription_id,omitempty"`
	Metadata             types.Metadata           `json:"metadata,omitempty"`
	Customer             *Customer                `json:"customer,omitempty"`

	// Plan, LineItems and CouponAssociations are populated only on the invoice
	// webhook (newInvoiceSubscription), where a consumer needs to price a line.
	Plan               *Plan                   `json:"plan,omitempty"`
	LineItems          []*SubscriptionLineItem `json:"line_items,omitempty"`
	CouponAssociations []*CouponAssociation    `json:"coupon_associations,omitempty"`
}

type SubscriptionLineItem struct {
	ID                 string                               `json:"id"`
	SubscriptionID     string                               `json:"subscription_id"`
	EntityID           string                               `json:"entity_id,omitempty"`
	EntityType         types.SubscriptionLineItemEntityType `json:"entity_type,omitempty"`
	PlanDisplayName    string                               `json:"plan_display_name,omitempty"`
	PriceID            string                               `json:"price_id"`
	PriceType          types.PriceType                      `json:"price_type,omitempty"`
	MeterID            string                               `json:"meter_id,omitempty"`
	MeterDisplayName   string                               `json:"meter_display_name,omitempty"`
	DisplayName        string                               `json:"display_name,omitempty"`
	Quantity           decimal.Decimal                      `json:"quantity" swaggertype:"string"`
	Currency           string                               `json:"currency"`
	BillingPeriod      types.BillingPeriod                  `json:"billing_period"`
	BillingPeriodCount int                                  `json:"billing_period_count"`
	InvoiceCadence     types.InvoiceCadence                 `json:"invoice_cadence,omitempty"`
	StartDate          time.Time                            `json:"start_date,omitempty"`
	EndDate            time.Time                            `json:"end_date,omitempty"`
	PriceUnit          *string                              `json:"price_unit,omitempty"`
	Metadata           map[string]string                    `json:"metadata,omitempty"`
	Price              *Price                               `json:"price,omitempty"`
}

func NewSubscription(resp *dto.SubscriptionResponse) *Subscription {
	if resp == nil || resp.Subscription == nil {
		return nil
	}
	return &Subscription{
		ID:                   resp.ID,
		CustomerID:           resp.CustomerID,
		InvoicingCustomerID:  resp.InvoicingCustomerID,
		PlanID:               resp.PlanID,
		LookupKey:            resp.LookupKey,
		SubscriptionStatus:   resp.SubscriptionStatus,
		Currency:             resp.Currency,
		StartDate:            resp.StartDate,
		EndDate:              resp.EndDate,
		CurrentPeriodStart:   resp.CurrentPeriodStart,
		CurrentPeriodEnd:     resp.CurrentPeriodEnd,
		CancelledAt:          resp.CancelledAt,
		CancelAt:             resp.CancelAt,
		CancelAtPeriodEnd:    resp.CancelAtPeriodEnd,
		TrialStart:           resp.TrialStart,
		TrialEnd:             resp.TrialEnd,
		BillingCadence:       resp.BillingCadence,
		BillingPeriod:        resp.BillingPeriod,
		BillingPeriodCount:   resp.BillingPeriodCount,
		PauseStatus:          resp.PauseStatus,
		SubscriptionType:     resp.SubscriptionType,
		ParentSubscriptionID: resp.ParentSubscriptionID,
		Metadata:             resp.Metadata,
		Customer:             NewCustomer(resp.Customer),
	}
}

func NewSubscriptionFromV2(resp *dto.SubscriptionResponseV2) *Subscription {
	if resp == nil || resp.Subscription == nil {
		return nil
	}
	return &Subscription{
		ID:                   resp.ID,
		CustomerID:           resp.CustomerID,
		InvoicingCustomerID:  resp.InvoicingCustomerID,
		PlanID:               resp.PlanID,
		LookupKey:            resp.LookupKey,
		SubscriptionStatus:   resp.SubscriptionStatus,
		Currency:             resp.Currency,
		StartDate:            resp.StartDate,
		EndDate:              resp.EndDate,
		CurrentPeriodStart:   resp.CurrentPeriodStart,
		CurrentPeriodEnd:     resp.CurrentPeriodEnd,
		CancelledAt:          resp.CancelledAt,
		CancelAt:             resp.CancelAt,
		CancelAtPeriodEnd:    resp.CancelAtPeriodEnd,
		TrialStart:           resp.TrialStart,
		TrialEnd:             resp.TrialEnd,
		BillingCadence:       resp.BillingCadence,
		BillingPeriod:        resp.BillingPeriod,
		BillingPeriodCount:   resp.BillingPeriodCount,
		PauseStatus:          resp.PauseStatus,
		SubscriptionType:     resp.SubscriptionType,
		ParentSubscriptionID: resp.ParentSubscriptionID,
		Metadata:             resp.Metadata,
		Customer:             NewCustomer(resp.Customer),
	}
}

// SubscriptionWebhookPayload represents the detailed payload for subscription payment webhooks.
// Customer is a minimal top-level reference (external_id only) for downstream routing;
// full customer detail remains under Subscription.Customer as a fallback.
type SubscriptionWebhookPayload struct {
	EventType    types.WebhookEventName `json:"event_type"`
	Customer     *Customer              `json:"customer,omitempty"`
	Subscription *Subscription          `json:"subscription"`
}

func NewSubscriptionWebhookPayload(subscription *dto.SubscriptionResponse, eventType types.WebhookEventName) *SubscriptionWebhookPayload {
	payload := &SubscriptionWebhookPayload{
		EventType:    eventType,
		Subscription: NewSubscription(subscription),
	}
	if subscription != nil && subscription.Customer != nil {
		payload.Customer = &Customer{ExternalID: subscription.Customer.ExternalID}
	}
	return payload
}
