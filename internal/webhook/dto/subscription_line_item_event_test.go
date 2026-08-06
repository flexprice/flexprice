package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func TestInternalSubscriptionLineItemEvent_RoundTrip(t *testing.T) {
	ev := InternalSubscriptionLineItemEvent{
		SubscriptionID: "sub_1",
		LineItemID:     "li_1",
		CustomerID:     "cus_1",
		PriceType:      types.PRICE_TYPE_FIXED,
		TenantID:       "ten_1",
		EnvironmentID:  "env_1",
	}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var asMap map[string]string
	if err := json.Unmarshal(b, &asMap); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	want := map[string]string{
		"subscription_id": "sub_1",
		"line_item_id":    "li_1",
		"customer_id":     "cus_1",
		"price_type":      string(types.PRICE_TYPE_FIXED),
		"tenant_id":       "ten_1",
		"environment_id":  "env_1",
	}
	for key, wantVal := range want {
		if gotVal, ok := asMap[key]; !ok || gotVal != wantVal {
			t.Fatalf("json key %q: got %q (present=%v), want %q", key, gotVal, ok, wantVal)
		}
	}
	if len(asMap) != len(want) {
		t.Fatalf("unexpected extra JSON keys: got %d keys %+v, want %d keys %+v", len(asMap), asMap, len(want), want)
	}

	var out InternalSubscriptionLineItemEvent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != ev {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, ev)
	}
}
