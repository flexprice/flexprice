package activity

import (
	"testing"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/addonassociation"
	"github.com/flexprice/flexprice/ent/checkoutsession"
	"github.com/flexprice/flexprice/ent/couponapplication"
	"github.com/flexprice/flexprice/ent/couponassociation"
	"github.com/flexprice/flexprice/ent/creditgrant"
	"github.com/flexprice/flexprice/ent/creditgrantapplication"
	"github.com/flexprice/flexprice/ent/creditnote"
	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/ent/entitlementgrant"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/flexprice/flexprice/ent/payment"
	"github.com/flexprice/flexprice/ent/paymentmethod"
	"github.com/flexprice/flexprice/ent/plan"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/ent/refund"
	"github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/subscriptionlineitem"
	"github.com/flexprice/flexprice/ent/subscriptionpause"
	"github.com/flexprice/flexprice/ent/subscriptionphase"
	"github.com/flexprice/flexprice/ent/subscriptionschedule"
	"github.com/flexprice/flexprice/ent/taxassociation"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/ent/wallettransaction"
)

// notCustomerScoped lists entities that legitimately resolve no customer_id:
// plans and prices are catalog/tenant-level, not customer-level, so they
// carry neither CustomerID nor CustomerLookup.
var notCustomerScoped = map[string]bool{
	"Plan":  true,
	"Price": true,
}

func TestDefinitionsRegisterCleanly(t *testing.T) {
	defs := Definitions()

	seenEntType := map[string]bool{}
	seenEntityType := map[string]bool{}
	for _, d := range defs {
		if seenEntType[d.EntType] {
			t.Errorf("duplicate EntType %q", d.EntType)
		}
		seenEntType[d.EntType] = true

		if seenEntityType[string(d.EntityType)] {
			t.Errorf("duplicate EntityType %q", d.EntityType)
		}
		seenEntityType[string(d.EntityType)] = true

		if d.Table == "" {
			t.Errorf("%s: empty Table", d.EntType)
		}

		hasCustomerID := d.CustomerID != nil
		hasCustomerLookup := d.CustomerLookup != nil
		if !hasCustomerID && !hasCustomerLookup && !notCustomerScoped[d.EntType] {
			t.Errorf("%s: neither CustomerID nor CustomerLookup set", d.EntType)
		}
	}

	reg := NewRegistry(defs...)
	if len(seenEntType) != len(defs) {
		t.Fatalf("NewRegistry would have overwritten a duplicate EntType")
	}
	for _, d := range defs {
		if _, ok := reg.ByEntType(d.EntType); !ok {
			t.Errorf("%s: not retrievable by EntType after registration", d.EntType)
		}
		if _, ok := reg.ByEntityType(d.EntityType); !ok {
			t.Errorf("%s: not retrievable by EntityType after registration", d.EntType)
		}
	}
}

// An update only carries the columns it changed, so an entity that reads its
// customer from its own customer_id column must fetch it with the old values,
// or its updates drop out of the customer roll-up.
func TestOwnCustomerIDIsFetchedOnUpdate(t *testing.T) {
	for _, d := range Definitions() {
		if d.CustomerID == nil || d.CustomerID(map[string]any{"customer_id": "c"}) != "c" {
			continue
		}
		fetched := append(append([]string{}, d.LabelFields...), d.ParentFields...)
		if !containsStr(fetched, "customer_id") {
			t.Errorf("%s: customer_id is not in LabelFields or ParentFields", d.EntType)
		}
	}
}

// entSchema is the generated ent identity of one registered entity: the
// mutation type name the hook dispatches on, its table, and its columns.
type entSchema struct {
	typ     string
	table   string
	columns []string
}

var entSchemas = map[string]entSchema{
	"Customer":               {flexent.TypeCustomer, customer.Table, customer.Columns},
	"Subscription":           {flexent.TypeSubscription, subscription.Table, subscription.Columns},
	"Plan":                   {flexent.TypePlan, plan.Table, plan.Columns},
	"Price":                  {flexent.TypePrice, price.Table, price.Columns},
	"Invoice":                {flexent.TypeInvoice, invoice.Table, invoice.Columns},
	"Wallet":                 {flexent.TypeWallet, wallet.Table, wallet.Columns},
	"WalletTransaction":      {flexent.TypeWalletTransaction, wallettransaction.Table, wallettransaction.Columns},
	"EntitlementGrant":       {flexent.TypeEntitlementGrant, entitlementgrant.Table, entitlementgrant.Columns},
	"PaymentMethod":          {flexent.TypePaymentMethod, paymentmethod.Table, paymentmethod.Columns},
	"CreditNote":             {flexent.TypeCreditNote, creditnote.Table, creditnote.Columns},
	"InvoiceLineItem":        {flexent.TypeInvoiceLineItem, invoicelineitem.Table, invoicelineitem.Columns},
	"SubscriptionLineItem":   {flexent.TypeSubscriptionLineItem, subscriptionlineitem.Table, subscriptionlineitem.Columns},
	"CheckoutSession":        {flexent.TypeCheckoutSession, checkoutsession.Table, checkoutsession.Columns},
	"SubscriptionPhase":      {flexent.TypeSubscriptionPhase, subscriptionphase.Table, subscriptionphase.Columns},
	"SubscriptionSchedule":   {flexent.TypeSubscriptionSchedule, subscriptionschedule.Table, subscriptionschedule.Columns},
	"SubscriptionPause":      {flexent.TypeSubscriptionPause, subscriptionpause.Table, subscriptionpause.Columns},
	"CreditGrant":            {flexent.TypeCreditGrant, creditgrant.Table, creditgrant.Columns},
	"CreditGrantApplication": {flexent.TypeCreditGrantApplication, creditgrantapplication.Table, creditgrantapplication.Columns},
	"CouponAssociation":      {flexent.TypeCouponAssociation, couponassociation.Table, couponassociation.Columns},
	"CouponApplication":      {flexent.TypeCouponApplication, couponapplication.Table, couponapplication.Columns},
	"Payment":                {flexent.TypePayment, payment.Table, payment.Columns},
	"Refund":                 {flexent.TypeRefund, refund.Table, refund.Columns},
	"AddonAssociation":       {flexent.TypeAddonAssociation, addonassociation.Table, addonassociation.Columns},
	"TaxAssociation":         {flexent.TypeTaxAssociation, taxassociation.Table, taxassociation.Columns},
}

// funcKeys lists the field keys each definition's Label, CustomerID and
// CustomerLookup funcs read. Funcs are opaque to the test, so a new or edited
// func must update this list; a definition with a func and no entry fails.
var funcKeys = map[string][]string{
	"Customer":               {"id", "name", "external_id"},
	"Subscription":           {"lookup_key", "customer_id"},
	"Plan":                   {"name", "lookup_key"},
	"Price":                  {"display_name", "lookup_key", "amount", "currency", "billing_period"},
	"Invoice":                {"invoice_number", "customer_id"},
	"Wallet":                 {"name", "currency", "wallet_type", "customer_id"},
	"WalletTransaction":      {"type", "credit_amount", "customer_id"},
	"EntitlementGrant":       {"quota", "customer_id"},
	"PaymentMethod":          {"type", "customer_id"},
	"CreditNote":             {"credit_note_number", "customer_id"},
	"InvoiceLineItem":        {"display_name", "customer_id"},
	"SubscriptionLineItem":   {"display_name", "customer_id"},
	"CheckoutSession":        {"checkout_status", "customer_id"},
	"SubscriptionPhase":      {"subscription_id"},
	"SubscriptionSchedule":   {"subscription_id"},
	"SubscriptionPause":      {"pause_mode", "subscription_id"},
	"CreditGrant":            {"name", "subscription_id"},
	"CreditGrantApplication": {"subscription_id"},
	"CouponAssociation":      {"subscription_id"},
	"CouponApplication":      {"subscription_id"},
	"Payment":                {"amount", "currency", "payment_gateway", "payment_method_type", "destination_type", "destination_id"},
	"Refund":                 {"amount", "currency", "invoice_id"},
	"AddonAssociation":       {"entity_type", "entity_id"},
	"TaxAssociation":         {"entity_type", "entity_id"},
}

// inertFields are names a definition lists that are deliberately not columns.
// They are never selected (redact/ignore lists only filter), so they cannot
// abort a transaction; each carries the reason it is kept.
var inertFields = map[string]map[string]string{
	"Customer": {"tax_id": "redaction placeholder: customers has no tax_id column today; kept so a future column is redacted from its first write"},
}

// A Table, LabelFields or ParentFields name that is not a real column makes the
// old-value SELECT fail inside every update of that entity, and Postgres then
// aborts the whole business transaction. Pin every name to ent's generated schema.
func TestDefinitionsMatchEntSchema(t *testing.T) {
	for _, d := range Definitions() {
		sch, ok := entSchemas[d.EntType]
		if !ok {
			t.Errorf("%s: no ent schema listed in entSchemas; add it", d.EntType)
			continue
		}
		if d.EntType != sch.typ {
			t.Errorf("%s: EntType does not match ent mutation type %q", d.EntType, sch.typ)
		}
		if d.Table != sch.table {
			t.Errorf("%s: Table %q, ent table is %q", d.EntType, d.Table, sch.table)
		}
		cols := map[string]bool{}
		for _, c := range sch.columns {
			cols[c] = true
		}
		check := func(kind string, names []string) {
			for _, n := range names {
				if cols[n] {
					continue
				}
				if reason, inert := inertFields[d.EntType][n]; inert && kind != "LabelFields" && kind != "ParentFields" {
					t.Logf("%s: %s %q skipped: %s", d.EntType, kind, n, reason)
					continue
				}
				t.Errorf("%s: %s %q is not a column of %s", d.EntType, kind, n, sch.table)
			}
		}
		check("LabelFields", d.LabelFields)
		check("ParentFields", d.ParentFields)
		check("IgnoreFields", d.IgnoreFields)
		check("RedactFields", d.RedactFields)

		keys, listed := funcKeys[d.EntType]
		if !listed && (d.Label != nil || d.CustomerID != nil || d.CustomerLookup != nil) {
			t.Errorf("%s: has Label/CustomerID/CustomerLookup funcs but no funcKeys entry", d.EntType)
		}
		check("func key", keys)
	}
}

// The CustomerLookup helpers in lookup.go read customer_id from these parent
// tables by name; pin the table and column names to the generated schema.
func TestCustomerLookupTargetsMatchEntSchema(t *testing.T) {
	for _, tc := range []struct {
		table   string
		columns []string
	}{
		{"subscriptions", subscription.Columns},
		{"invoices", invoice.Columns},
	} {
		sch := map[string]bool{}
		for _, c := range tc.columns {
			sch[c] = true
		}
		if !sch["customer_id"] {
			t.Errorf("%s has no customer_id column", tc.table)
		}
	}
	if subscription.Table != "subscriptions" || invoice.Table != "invoices" {
		t.Errorf("lookup tables drifted: %s, %s", subscription.Table, invoice.Table)
	}
}
