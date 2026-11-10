package telemetry

import (
	"encoding/json"
	"testing"
)

func TestRedactEventPayload(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantKey   []string // keys that must survive
		noKey     []string // keys that must be gone, anywhere in the tree
		wantExact string   // if set, output must equal this exactly (pass-through cases)
	}{
		{
			name: "customer.created carries PII",
			in: `{
				"event_type": "customer.created",
				"customer": {
					"id": "cust_01", "external_id": "ext_01",
					"name": "Jane Doe", "email": "jane@example.com",
					"address_line1": "123 Main St", "address_city": "Bangalore",
					"address_country": "IN", "timezone": "Asia/Kolkata",
					"metadata": {"key": "val"}
				}
			}`,
			wantKey: []string{"event_type", "customer", "id", "external_id", "timezone"},
			noKey:   []string{"name", "email", "address_line1", "address_city", "address_country", "metadata"},
		},
		{
			name: "invoice embeds customer and financial amounts",
			in: `{
				"id": "inv_01", "status": "finalized",
				"amount_due": "100.00", "amount_paid": "0.00", "total": "100.00",
				"currency": "usd",
				"customer": {"id": "cust_01", "name": "Jane Doe", "email": "jane@example.com"}
			}`,
			wantKey: []string{"id", "status"},
			noKey:   []string{"amount_due", "amount_paid", "total", "currency", "name", "email"},
		},
		{
			name: "payment amount redacted, gateway id kept",
			in: `{
				"id": "pay_01", "status": "succeeded",
				"amount": "50.00", "payment_gateway": "stripe", "destination_id": "inv_01"
			}`,
			wantKey: []string{"id", "status", "destination_id"},
			noKey:   []string{"amount", "payment_gateway"},
		},
		{
			name:    "feature event has nothing sensitive, passes through",
			in:      `{"id": "feat_01", "name": "api_calls", "type": "metered"}`,
			wantKey: []string{"id", "type"},
			noKey:   []string{"name"},
		},
		{
			name: "event.rejected freeform properties stripped",
			in: `{
				"customer_id": "cust_01",
				"properties": {"anything": "goes here, could be PII"}
			}`,
			wantKey: []string{"customer_id"},
			noKey:   []string{"properties"},
		},
		{
			name: "invoice.sync.failed provider error stripped",
			in: `{
				"invoice_id": "inv_01", "tenant_id": "tenant_01",
				"provider": "stripe", "error": "card ending in 4242 declined: insufficient_funds"
			}`,
			wantKey: []string{"invoice_id", "tenant_id", "provider"},
			noKey:   []string{"error"},
		},
		{
			name:      "large integers survive redaction without precision loss",
			in:        `{"id": "evt_01", "sequence": 9007199254740993}`,
			wantExact: `{"id":"evt_01","sequence":9007199254740993}`,
		},
		{
			name: "alert value_at_time stripped from nested alert_info",
			in: `{
				"wallet_id": "wallet_01", "alert_type": "low_balance",
				"alert_info": {"value_at_time": "42.50", "timestamp": "2026-01-01T00:00:00Z"}
			}`,
			wantKey: []string{"wallet_id", "alert_type", "alert_info", "timestamp"},
			noKey:   []string{"value_at_time"},
		},
		{
			name:      "non-object payload passes through unchanged",
			in:        `"just a string"`,
			wantExact: `"just a string"`,
		},
		{
			name:      "invalid json passes through unchanged",
			in:        `{not valid json`,
			wantExact: `{not valid json`,
		},
		{
			name:      "empty payload passes through unchanged",
			in:        ``,
			wantExact: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := redactEventPayload(json.RawMessage(tt.in))
			outStr := string(out)

			if tt.wantKey == nil && tt.noKey == nil {
				if outStr != tt.wantExact {
					t.Errorf("expected exact output %q, got %q", tt.wantExact, outStr)
				}
				return
			}

			for _, k := range tt.wantKey {
				if !containsKey(outStr, k) {
					t.Errorf("expected key %q to survive redaction, got: %s", k, outStr)
				}
			}
			for _, k := range tt.noKey {
				if containsKey(outStr, k) {
					t.Errorf("expected key %q to be redacted, got: %s", k, outStr)
				}
			}
		})
	}
}

// containsKey does a cheap substring check for `"key":` in the marshaled
// output, sufficient for this test since field names here don't collide
// with string values.
func containsKey(s, key string) bool {
	needle := `"` + key + `":`
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
