package activity

import (
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

// Registrations returns the v1 registrations.
func Registrations() *Registry {
	reg, err := NewRegistry(
		Registration{
			EntityType: types.SystemEntityTypeCustomer, EntName: "Customer",
			Snapshot: snapshotOf(customer.FromEnt),
			Label:    []string{"external_id"},
			Actions:  []Action{CustomerArchived},
		},
		Registration{
			EntityType: types.SystemEntityTypeSubscription, EntName: "Subscription",
			Snapshot: snapshotOf(subscription.GetSubscriptionFromEnt),
			Actions:  []Action{SubscriptionArchived, SubscriptionCancelled, SubscriptionPlanChanged, SubscriptionActivated},
		},
		Registration{
			EntityType: types.SystemEntityTypeInvoice, EntName: "Invoice",
			Snapshot: snapshotOf(invoice.FromEnt),
			Label:    []string{"invoice_number"},
			Actions:  []Action{InvoiceDeleted, InvoiceVoided, InvoiceFinalized, InvoiceRefunded, InvoicePaid},
		},
		Registration{
			EntityType: types.SystemEntityTypeCreditNote, EntName: "CreditNote",
			Snapshot: snapshotOf(creditnote.FromEnt),
			Label:    []string{"credit_note_number"},
			Actions:  []Action{CreditNoteVoided, CreditNoteFinalized},
		},
		Registration{
			EntityType: types.SystemEntityTypeWallet, EntName: "Wallet",
			Snapshot: snapshotOf(wallet.FromEnt),
			Label:    []string{"name"},
			Actions:  []Action{WalletTerminated, WalletToppedUp},
		},
		Registration{
			EntityType: types.SystemEntityTypePayment, EntName: "Payment",
			Snapshot: snapshotOf(payment.FromEnt),
			Actions:  []Action{PaymentArchived, PaymentVoided, PaymentRefunded, PaymentFailed, PaymentSucceeded},
		},
		Registration{
			EntityType: types.SystemEntityTypePaymentMethod, EntName: "PaymentMethod",
			Snapshot: snapshotOf(paymentmethod.FromEnt),
		},
		Registration{
			EntityType: types.SystemEntityTypeRefund, EntName: "Refund",
			Snapshot: snapshotOf(refund.FromEnt),
			Actions:  []Action{RefundFailed, RefundSucceeded},
		},
		Registration{
			EntityType: types.SystemEntityTypeCheckoutSession, EntName: "CheckoutSession",
			Snapshot: snapshotOf(checkout.FromEnt),
			Actions:  []Action{CheckoutSessionArchived, CheckoutSessionCompleted, CheckoutSessionFailed, CheckoutSessionExpired},
		},
		Registration{
			EntityType: types.SystemEntityTypePlan, EntName: "Plan",
			Snapshot: snapshotOf(plan.FromEnt),
			Label:    []string{"name"},
			Actions:  []Action{PlanArchived},
		},
		Registration{
			EntityType: types.SystemEntityTypePrice, EntName: "Price",
			Snapshot: snapshotOf(price.FromEnt),
			Label:    []string{"display_name", "lookup_key"},
			Actions:  []Action{PriceTerminated},
		},
		Registration{
			EntityType: types.SystemEntityTypeFeature, EntName: "Feature",
			Snapshot: snapshotOf(feature.FromEnt),
			Label:    []string{"name"},
			Actions:  []Action{FeatureArchived},
		},
	)
	if err != nil {
		panic(err)
	}
	return reg
}
