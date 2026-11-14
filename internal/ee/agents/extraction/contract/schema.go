package contract

import (
	"encoding/json"
	"sort"
	"sync"
)

var (
	schemaOnce sync.Once
	schemaJSON json.RawMessage
)

func ResponseSchema() json.RawMessage {
	schemaOnce.Do(func() {
		b, err := json.Marshal(buildSchema())
		if err != nil {
			panic(err)
		}
		schemaJSON = b
	})
	return schemaJSON
}

func buildSchema() map[string]any {
	return object(map[string]any{
		"documents": array(object(map[string]any{
			"type":       enumString(documentTypes...),
			"title":      str(),
			"page_start": integer(),
			"page_end":   integer(),
		})),
		"contract_type": term(contractTypes...),
		"customer": object(map[string]any{
			"legal_name":      term(),
			"billing_address": term(),
			"primary_contact": term(),
			"billing_email":   term(),
		}),
		"term": object(map[string]any{
			"signed_date":          term(),
			"effective_date":       term(),
			"term_months":          term(),
			"auto_renew":           term("true", "false"),
			"renewal_term_months":  term(),
			"notice_days":          term(),
			"renewal_price_change": term(),
			"cancellation":         term(),
		}),
		"billing": object(map[string]any{
			"structure":     term(),
			"currency":      term(),
			"invoice_day":   term(),
			"payment_terms": term(),
			"late_fee":      term(),
			"tax":           term(),
		}),
		"charges": array(object(map[string]any{
			"kind":              enumString(chargeKinds...),
			"name":              term(),
			"amount":            term(),
			"amount_period":     term(billingPeriods...),
			"billing_period":    term(billingPeriods...),
			"billing_timing":    term(billingTimings...),
			"included_quantity": term(),
			"unit":              term(),
			"tier_mode":         term(tierModes...),
			"tiers": array(object(map[string]any{
				"up_to":       str(),
				"unit_amount": str(),
				"flat_amount": str(),
			})),
			"trigger":             term(),
			"revenue_recognition": term(),
		})),
		"commitments": array(object(map[string]any{
			"amount":    term(),
			"period":    term(billingPeriods...),
			"covers":    term(),
			"shortfall": term(),
		})),
		"credits": array(object(map[string]any{
			"amount": term(),
			"kind":   term(),
			"expiry": term(),
		})),
		"service_levels": array(object(map[string]any{
			"description": term(),
		})),
		"doubts": array(str()),
	})
}

func term(allowed ...string) map[string]any {
	value := str()
	if len(allowed) > 0 {
		value = enumString(append([]string{""}, allowed...)...)
	}
	return object(map[string]any{
		"value":      value,
		"confidence": integer(),
		"page":       integer(),
		"quote":      str(),
		"options":    array(str()),
		"note":       str(),
	})
}

func object(props map[string]any) map[string]any {
	required := make([]string, 0, len(props))
	for k := range props {
		required = append(required, k)
	}
	sort.Strings(required)
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func str() map[string]any { return map[string]any{"type": "string"} }

func integer() map[string]any { return map[string]any{"type": "integer"} }

func enumString(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
