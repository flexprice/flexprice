package activity

import (
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
