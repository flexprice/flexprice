package activity

import (
	"strings"

	"github.com/flexprice/flexprice/internal/types"
)

func str(f map[string]any, k string) string {
	s, _ := f[k].(string)
	return s
}

// Definitions lists every entity the activity log observes.
func Definitions() []Definition {
	return []Definition{
		{
			EntType: "Customer", EntityType: types.SystemEntityTypeCustomer, Table: "customers",
			LabelFields:  []string{"name", "external_id"},
			RedactFields: []string{"tax_id"},
			CustomerID:   func(f map[string]any) string { return str(f, "id") },
			Label:        func(f map[string]any) string { return firstNonEmpty(str(f, "name"), str(f, "external_id")) },
			Actions: map[string]string{
				"customer.deleted": "{actor} deleted {entity}",
			},
			FieldLabels: map[string]FieldDisplay{"email": {Label: "Email", Format: "text"}},
		},
		{
			EntType: "Subscription", EntityType: types.SystemEntityTypeSubscription, Table: "subscriptions",
			LabelFields:  []string{"lookup_key", "customer_id"},
			IgnoreFields: []string{"version", "current_period_start", "current_period_end", "synced_price_sequence"},
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label:        func(f map[string]any) string { return str(f, "lookup_key") },
			Actions: map[string]string{
				"subscription.paused":       "{actor} paused {entity}",
				"subscription.resumed":      "{actor} resumed {entity}",
				"subscription.cancelled":    "{actor} cancelled {entity}",
				"subscription.plan_changed": "{actor} changed the plan on {entity}",
			},
			FieldLabels: map[string]FieldDisplay{
				"subscription_status": {Label: "Status", Format: "enum"},
				"plan_id":             {Label: "Plan", Format: "text"},
				"billing_period":      {Label: "Billing period", Format: "enum"},
			},
		},
		{
			EntType: "Plan", EntityType: types.SystemEntityTypePlan, Table: "plans",
			LabelFields: []string{"name", "lookup_key"},
			Label:       func(f map[string]any) string { return firstNonEmpty(str(f, "name"), str(f, "lookup_key")) },
		},
		{
			EntType: "Price", EntityType: types.SystemEntityTypePrice, Table: "prices",
			LabelFields: []string{"display_name", "lookup_key", "amount", "currency", "billing_period"},
			Label: func(f map[string]any) string {
				if l := firstNonEmpty(str(f, "display_name"), str(f, "lookup_key")); l != "" {
					return l
				}
				if a := Normalize(f["amount"]); a != "" {
					return a + " " + str(f, "currency") + " / " + str(f, "billing_period")
				}
				return ""
			},
			FieldLabels: map[string]FieldDisplay{"amount": {Label: "Amount", Format: "money"}, "currency": {Label: "Currency", Format: "enum"}},
		},
		{
			EntType: "Invoice", EntityType: types.SystemEntityTypeInvoice, Table: "invoices",
			LabelFields:  []string{"invoice_number", "customer_id"},
			ParentFields: []string{"subscription_id"},
			IgnoreFields: []string{"version"},
			SnapshotMode: SnapshotNone,
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label:        func(f map[string]any) string { return str(f, "invoice_number") },
			Actions: map[string]string{
				"invoice.finalized": "{actor} finalized {entity}",
				"invoice.voided":    "{actor} voided {entity}",
				"invoice.paid":      "{actor} marked {entity} as paid",
			},
			FieldLabels: map[string]FieldDisplay{
				"invoice_status": {Label: "Status", Format: "enum"}, "payment_status": {Label: "Payment status", Format: "enum"},
				"amount_due": {Label: "Amount due", Format: "money"}, "due_date": {Label: "Due date", Format: "date"},
			},
		},
		{
			EntType: "Wallet", EntityType: types.SystemEntityTypeWallet, Table: "wallets",
			LabelFields:  []string{"name", "currency", "wallet_type", "customer_id"},
			IgnoreFields: []string{"balance", "credit_balance"},
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label: func(f map[string]any) string {
				if n := str(f, "name"); n != "" {
					return n
				}
				if c := str(f, "currency"); c != "" {
					return c + " " + strings.ToLower(str(f, "wallet_type")) + " wallet"
				}
				return ""
			},
			Actions: map[string]string{"wallet.terminated": "{actor} terminated {entity}"},
		},
		{
			EntType: "WalletTransaction", EntityType: types.SystemEntityTypeWalletTransaction, Table: "wallet_transactions",
			LabelFields:  []string{"type", "amount", "credit_amount", "currency"},
			ParentFields: []string{"customer_id"},
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label: func(f map[string]any) string {
				return strings.ToLower(str(f, "type")) + " " + Normalize(f["credit_amount"]) + " credits"
			},
			FieldLabels: map[string]FieldDisplay{"amount": {Label: "Amount", Format: "money"}, "credit_amount": {Label: "Credits", Format: "text"}, "transaction_status": {Label: "Status", Format: "enum"}},
		},
		{
			// entitlement_grants has no feature_id/credits columns; the brief named those,
			// the real columns are scope_entity_id (the feature/plan/addon the grant scopes
			// to) and quota (the granted amount). See ent/schema/entitlement_grant.go.
			EntType: "EntitlementGrant", EntityType: types.SystemEntityTypeEntitlementGrant, Table: "entitlement_grants",
			LabelFields:  []string{"scope_entity_id", "quota"},
			ParentFields: []string{"customer_id", "subscription_id"},
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label:        func(f map[string]any) string { return "grant " + Normalize(f["quota"]) },
		},

		// direct customer_id
		{
			EntType: "PaymentMethod", EntityType: types.SystemEntityTypePaymentMethod, Table: "payment_methods",
			// payment_methods has no gateway_payment_method_id/last4/metadata columns; the
			// real sensitive columns are gateway_method_id (the gateway's own token/id) and
			// method_details (jsonb; carries card last4/brand/expiry etc). See
			// ent/schema/paymentmethod.go.
			LabelFields: []string{"type"}, ParentFields: []string{"customer_id"},
			RedactFields: []string{"gateway_method_id", "method_details"},
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label:        func(f map[string]any) string { return strings.ToLower(str(f, "type")) + " payment method" },
		},
		{
			EntType: "CreditNote", EntityType: types.SystemEntityTypeCreditNote, Table: "credit_notes",
			LabelFields: []string{"credit_note_number"}, ParentFields: []string{"customer_id", "subscription_id"},
			SnapshotMode: SnapshotNone,
			CustomerID:   func(f map[string]any) string { return str(f, "customer_id") },
			Label:        func(f map[string]any) string { return str(f, "credit_note_number") },
			Actions:      map[string]string{"credit_note.finalized": "{actor} finalized {entity}", "credit_note.voided": "{actor} voided {entity}"},
		},
		{
			EntType: "InvoiceLineItem", EntityType: types.SystemEntityTypeInvoiceLineItem, Table: "invoice_line_items",
			LabelFields: []string{"display_name", "invoice_id"}, ParentFields: []string{"customer_id", "subscription_id"},
			CustomerID: func(f map[string]any) string { return str(f, "customer_id") },
			Label:      func(f map[string]any) string { return str(f, "display_name") },
		},
		{
			EntType: "SubscriptionLineItem", EntityType: types.SystemEntityTypeSubscriptionLineItem, Table: "subscription_line_items",
			LabelFields: []string{"display_name", "subscription_id"}, ParentFields: []string{"customer_id"},
			CustomerID: func(f map[string]any) string { return str(f, "customer_id") },
			Label:      func(f map[string]any) string { return str(f, "display_name") },
		},
		{
			EntType: "CheckoutSession", EntityType: types.SystemEntityTypeCheckoutSession, Table: "checkout_sessions",
			LabelFields: []string{"checkout_status"}, ParentFields: []string{"customer_id"},
			IgnoreFields: []string{"expires_at"}, RedactFields: []string{"provider_result"},
			CustomerID: func(f map[string]any) string { return str(f, "customer_id") },
			Label:      func(f map[string]any) string { return "checkout " + strings.ToLower(str(f, "checkout_status")) },
		},

		// via subscription
		{
			EntType: "SubscriptionPhase", EntityType: types.SystemEntityTypeSubscriptionPhase, Table: "subscription_phases",
			ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return "phase" },
		},
		{
			EntType: "SubscriptionSchedule", EntityType: types.SystemEntityTypeSubscriptionSchedule, Table: "subscription_schedules",
			ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return "schedule" },
		},
		{
			EntType: "SubscriptionPause", EntityType: types.SystemEntityTypeSubscriptionPause, Table: "subscription_pauses",
			LabelFields: []string{"pause_mode", "reason"}, ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return strings.ToLower(str(f, "pause_mode")) + " pause" },
		},
		{
			EntType: "CreditGrant", EntityType: types.SystemEntityTypeCreditGrant, Table: "credit_grants",
			LabelFields: []string{"name"}, ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return str(f, "name") },
		},
		{
			EntType: "CreditGrantApplication", EntityType: types.SystemEntityTypeCreditGrantApplication, Table: "credit_grant_applications",
			ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return "grant application" },
		},
		{
			EntType: "CouponAssociation", EntityType: types.SystemEntityTypeCouponAssociation, Table: "coupon_associations",
			LabelFields: []string{"coupon_id"}, ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return "coupon" }, FieldLabels: map[string]FieldDisplay{"coupon_id": {Label: "Coupon", Format: "text"}},
		},
		{
			EntType: "CouponApplication", EntityType: types.SystemEntityTypeCouponApplication, Table: "coupon_applications",
			LabelFields: []string{"coupon_id"}, ParentFields: []string{"subscription_id"}, CustomerLookup: customerVia("subscriptions", "subscription_id"),
			Label: func(f map[string]any) string { return "coupon applied" },
		},

		// via invoice or payment
		{
			EntType: "Payment", EntityType: types.SystemEntityTypePayment, Table: "payments",
			LabelFields: []string{"amount", "currency", "payment_gateway", "payment_method_type"}, ParentFields: []string{"destination_type", "destination_id"},
			CustomerLookup: customerViaPayment(), IgnoreFields: []string{"gateway_tracking_id"}, RedactFields: []string{"gateway_metadata"},
			Label: func(f map[string]any) string {
				return Normalize(f["amount"]) + " " + str(f, "currency") + " via " + firstNonEmpty(str(f, "payment_gateway"), str(f, "payment_method_type"))
			},
			Actions:     map[string]string{"payment.succeeded": "{actor} recorded {entity}", "payment.failed": "{actor} recorded a failed {entity}"},
			FieldLabels: map[string]FieldDisplay{"amount": {Label: "Amount", Format: "money"}, "payment_status": {Label: "Status", Format: "enum"}},
		},
		{
			EntType: "Refund", EntityType: types.SystemEntityTypeRefund, Table: "refunds",
			LabelFields: []string{"amount", "currency"}, ParentFields: []string{"invoice_id"}, CustomerLookup: customerVia("invoices", "invoice_id"),
			Label:       func(f map[string]any) string { return "refund " + Normalize(f["amount"]) + " " + str(f, "currency") },
			FieldLabels: map[string]FieldDisplay{"amount": {Label: "Amount", Format: "money"}, "refund_status": {Label: "Status", Format: "enum"}},
		},

		// via entity_type / entity_id
		{
			EntType: "AddonAssociation", EntityType: types.SystemEntityTypeAddonAssociation, Table: "addon_associations",
			LabelFields: []string{"addon_id"}, ParentFields: []string{"entity_type", "entity_id"}, CustomerLookup: customerViaEntity(),
			Label: func(f map[string]any) string { return "addon" },
		},
		{
			EntType: "TaxAssociation", EntityType: types.SystemEntityTypeTaxAssociation, Table: "tax_associations",
			LabelFields: []string{"tax_rate_id"}, ParentFields: []string{"entity_type", "entity_id"}, CustomerLookup: customerViaEntity(),
			Label: func(f map[string]any) string { return "tax rate" },
		},
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
