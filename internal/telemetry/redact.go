package telemetry

import (
	"bytes"
	"encoding/json"
)

// redactedFieldSet returns the keys removed from system_event payloads at every nesting depth.
func redactedFieldSet() map[string]bool {
	return map[string]bool{
		// customer PII
		"name":                true,
		"email":               true,
		"address_line1":       true,
		"address_line2":       true,
		"address_city":        true,
		"address_state":       true,
		"address_postal_code": true,
		"address_country":     true,
		"phone":               true,
		"metadata":            true,

		// financial amounts and balances
		"amount":                        true,
		"amount_due":                    true,
		"amount_paid":                   true,
		"amount_remaining":              true,
		"subtotal":                      true,
		"total":                         true,
		"total_tax":                     true,
		"total_discount":                true,
		"total_prepaid_credits_applied": true,
		"settled_amount":                true,
		"total_amount":                  true,
		"balance":                       true,
		"credit_balance":                true,
		"credit_balance_before":         true,
		"credit_balance_after":          true,
		"credit_amount":                 true,
		"current_balance":               true,
		"current_spend":                 true,
		"threshold":                     true,
		"conversion_rate":               true,
		"currency":                      true,
		"value_at_time":                 true,

		// payment/gateway details and freeform event properties
		"payment_gateway":    true,
		"gateway_refund_id":  true,
		"hosted_invoice_url": true,
		"properties":         true,
		"error":              true,
	}
}

// redactPayload walks a parsed JSON value and deletes keys in fields at every nesting depth.
func redactPayload(v any, fields map[string]bool) {
	switch val := v.(type) {
	case map[string]any:
		for k := range val {
			if fields[k] {
				delete(val, k)
				continue
			}
			redactPayload(val[k], fields)
		}
	case []any:
		for _, item := range val {
			redactPayload(item, fields)
		}
	}
}

// redactEventPayload returns raw with sensitive fields stripped at any depth.
// Decodes with UseNumber so large integers aren't rounded through float64.
func redactEventPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return raw
	}
	redactPayload(parsed, redactedFieldSet())
	b, err := json.Marshal(parsed)
	if err != nil {
		return raw
	}
	return json.RawMessage(b)
}
