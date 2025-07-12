package payload

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/types"
)

// PayloadBuilderFactory interface for getting event-specific payload builders
type PayloadBuilderFactory interface {
	GetBuilder(eventType string) (PayloadBuilder, error)
}

type payloadBuilderFactory struct {
	builders map[string]func() PayloadBuilder
	services *Services
}

// NewPayloadBuilderFactory creates a new factory with registered builders
func NewPayloadBuilderFactory(services *Services) PayloadBuilderFactory {
	f := &payloadBuilderFactory{
		builders: make(map[string]func() PayloadBuilder),
		services: services,
	}

	// Register invoice builders
	f.builders[types.WebhookEventInvoiceCreateDraft] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventInvoiceUpdateFinalized] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventInvoiceUpdateVoided] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventInvoiceUpdatePayment] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventInvoiceAmountsRecalculated] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventInvoicePaymentOverdue] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventInvoicePaymentStatusChanged] = func() PayloadBuilder {
		return NewInvoicePayloadBuilder(f.services)
	}
	// Register subscription builders
	f.builders[types.WebhookEventSubscriptionCreated] = func() PayloadBuilder {
		return NewSubscriptionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventSubscriptionPaused] = func() PayloadBuilder {
		return NewSubscriptionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventSubscriptionCancelled] = func() PayloadBuilder {
		return NewSubscriptionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventSubscriptionResumed] = func() PayloadBuilder {
		return NewSubscriptionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventSubscriptionUpdated] = func() PayloadBuilder {
		return NewSubscriptionPayloadBuilder(f.services)
	}

	// Register feature builders
	f.builders[types.WebhookEventFeatureCreated] = func() PayloadBuilder {
		return NewFeaturePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventFeatureUpdated] = func() PayloadBuilder {
		return NewFeaturePayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventFeatureDeleted] = func() PayloadBuilder {
		return NewFeaturePayloadBuilder(f.services)
	}

	// Register entitlement builders
	f.builders[types.WebhookEventEntitlementCreated] = func() PayloadBuilder {
		return NewEntitlementPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventEntitlementUpdated] = func() PayloadBuilder {
		return NewEntitlementPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventEntitlementDeleted] = func() PayloadBuilder {
		return NewEntitlementPayloadBuilder(f.services)
	}

	// wallet builders
	f.builders[types.WebhookEventWalletCreated] = func() PayloadBuilder {
		return NewWalletPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletUpdated] = func() PayloadBuilder {
		return NewWalletPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletTerminated] = func() PayloadBuilder {
		return NewWalletPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletDepletedOngoingBalance] = func() PayloadBuilder {
		return NewWalletPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletTransactionCreated] = func() PayloadBuilder {
		return NewTransactionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletTransactionUpdated] = func() PayloadBuilder {
		return NewTransactionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletTransactionPaymentFailure] = func() PayloadBuilder {
		return NewTransactionPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventWalletTransactionPaymentSuccess] = func() PayloadBuilder {
		return NewTransactionPayloadBuilder(f.services)
	}

	// customer builders
	f.builders[types.WebhookEventCustomerCreated] = func() PayloadBuilder {
		return NewCustomerPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventCustomerUpdated] = func() PayloadBuilder {
		return NewCustomerPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventCustomerDeleted] = func() PayloadBuilder {
		return NewCustomerPayloadBuilder(f.services)
	}

	// payment builders
	f.builders[types.WebhookEventPaymentCreated] = func() PayloadBuilder {
		return NewPaymentPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventPaymentUpdated] = func() PayloadBuilder {
		return NewPaymentPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventPaymentFailed] = func() PayloadBuilder {
		return NewPaymentPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventPaymentSuccess] = func() PayloadBuilder {
		return NewPaymentPayloadBuilder(f.services)
	}
	f.builders[types.WebhookEventPaymentPending] = func() PayloadBuilder {
		return NewPaymentPayloadBuilder(f.services)
	}

	return f
}

// GetBuilder returns a payload builder for the given event type
func (f *payloadBuilderFactory) GetBuilder(eventType string) (PayloadBuilder, error) {
	builderFn, ok := f.builders[eventType]
	if !ok {
		return nil, fmt.Errorf("no builder registered for event type: %s", eventType)
	}

	return builderFn(), nil
}
