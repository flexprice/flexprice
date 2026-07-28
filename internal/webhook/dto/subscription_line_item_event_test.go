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

	var out InternalSubscriptionLineItemEvent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != ev {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, ev)
	}
}
