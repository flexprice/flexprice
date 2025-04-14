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
	f.builders[types.WebhookEventSubscriptionExpired] = func() PayloadBuilder {
		return NewSubscriptionPayloadBuilder(f.services)
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
