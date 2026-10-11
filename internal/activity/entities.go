package activity

import (
	"fmt"

	entgo "entgo.io/ent"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/paymentmethod"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
)

func snapshot[T any](convert func(*T) any) func(entgo.Value) any {
	return func(v entgo.Value) any {
		row, ok := v.(*T)
		if !ok {
			panic(fmt.Sprintf("activity: unexpected row type %T", v))
		}
		return convert(row)
	}
}

// DefaultRegistry returns the v1 registrations.
func DefaultRegistry() *Registry {
	reg, err := NewRegistry(
		Registration{
			EntityType: types.SystemEntityTypeCustomer, EntName: "Customer",
			Snapshot: snapshot(func(e *ent.Customer) any { return customer.FromEnt(e) }),
			Label:    []string{"name", "external_id"},
			Actions:  []string{CustomerArchived},
			Customer: Self(),
		},
		Registration{
			EntityType: types.SystemEntityTypeSubscription, EntName: "Subscription",
			Snapshot: snapshot(func(e *ent.Subscription) any { return subscription.GetSubscriptionFromEnt(e) }),
			Drop:     []string{"version", "synced_price_sequence"},
			Redact:   []string{"gateway_payment_method_id"},
			Actions:  []string{SubscriptionArchived, SubscriptionCancelled, SubscriptionPlanChanged, SubscriptionActivated},
			Customer: Field("customer_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypeInvoice, EntName: "Invoice",
			Snapshot: snapshot(func(e *ent.Invoice) any { return invoice.FromEnt(e) }),
			Drop:     []string{"version"},
			Label:    []string{"invoice_number"},
			Actions:  []string{InvoiceDeleted, InvoiceVoided, InvoiceFinalized, InvoiceRefunded, InvoicePaid},
			Customer: Field("customer_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypeCreditNote, EntName: "CreditNote",
			Snapshot: snapshot(func(e *ent.CreditNote) any { return creditnote.FromEnt(e) }),
			Label:    []string{"credit_note_number"},
			Actions:  []string{CreditNoteVoided, CreditNoteFinalized},
			Customer: Field("customer_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypeWallet, EntName: "Wallet",
			Snapshot: snapshot(func(e *ent.Wallet) any { return wallet.FromEnt(e) }),
			Label:    []string{"name"},
			Actions:  []string{WalletTerminated, WalletToppedUp},
			Customer: Field("customer_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypePayment, EntName: "Payment",
			Snapshot: snapshot(func(e *ent.Payment) any { return payment.FromEnt(e) }),
			Redact:   []string{"gateway_metadata"},
			Actions:  []string{PaymentArchived, PaymentVoided, PaymentRefunded, PaymentFailed, PaymentSucceeded},
			Customer: FirstOf(
				Parent(types.SystemEntityTypeInvoice, "destination_id").When("destination_type", string(types.PaymentDestinationTypeInvoice)),
				Field("destination_id").When("destination_type", string(types.PaymentDestinationTypeCustomer)),
			),
		},
		Registration{
			EntityType: types.SystemEntityTypePaymentMethod, EntName: "PaymentMethod",
			Snapshot: snapshot(func(e *ent.PaymentMethod) any { return paymentmethod.FromEnt(e) }),
			Redact:   []string{"method_details", "gateway_method_id"},
			Customer: Field("customer_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypeRefund, EntName: "Refund",
			Snapshot: snapshot(func(e *ent.Refund) any { return refund.FromEnt(e) }),
			Redact:   []string{"gateway_metadata"},
			Actions:  []string{RefundFailed, RefundSucceeded},
			Customer: Parent(types.SystemEntityTypeInvoice, "invoice_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypeCheckoutSession, EntName: "CheckoutSession",
			Snapshot: snapshot(func(e *ent.CheckoutSession) any { return checkout.FromEnt(e) }),
			Redact:   []string{"provider_result", "payment_provider_config", "result"},
			Actions:  []string{CheckoutSessionArchived, CheckoutSessionCompleted, CheckoutSessionFailed, CheckoutSessionExpired},
			Customer: Field("customer_id"),
		},
		Registration{
			EntityType: types.SystemEntityTypePlan, EntName: "Plan",
			Snapshot: snapshot(func(e *ent.Plan) any { return plan.FromEnt(e) }),
			Label:    []string{"name"},
			Actions:  []string{PlanArchived},
		},
		Registration{
			EntityType: types.SystemEntityTypePrice, EntName: "Price",
			Snapshot: snapshot(func(e *ent.Price) any { return price.FromEnt(e) }),
			Drop:     []string{"sequence"},
			Label:    []string{"display_name", "lookup_key"},
			Actions:  []string{PriceTerminated},
			Customer: Parent(types.SystemEntityTypeSubscription, "entity_id").When("entity_type", string(types.PRICE_ENTITY_TYPE_SUBSCRIPTION)),
		},
		Registration{
			EntityType: types.SystemEntityTypeFeature, EntName: "Feature",
			Snapshot: snapshot(func(e *ent.Feature) any { return feature.FromEnt(e) }),
			Label:    []string{"name"},
			Actions:  []string{FeatureArchived},
		},
	)
	if err != nil {
		panic(err)
	}
	return reg
}
