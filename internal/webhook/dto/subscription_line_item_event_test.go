package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInternalSubscriptionLineItemEvent_JSON(t *testing.T) {
	ev := InternalSubscriptionLineItemEvent{
		SubscriptionID: "sub_123",
		LineItemID:     "li_456",
		CustomerID:     "cust_789",
		PriceType:      types.PRICE_TYPE_FIXED,
		TenantID:       "tenant_test",
		EnvironmentID:  "env_test",
	}

	raw, err := json.Marshal(ev)
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "sub_123", got["subscription_id"])
	require.Equal(t, "li_456", got["line_item_id"])
	require.Equal(t, "cust_789", got["customer_id"])
	require.Equal(t, string(types.PRICE_TYPE_FIXED), got["price_type"])
	require.Equal(t, "tenant_test", got["tenant_id"])
	require.Equal(t, "env_test", got["environment_id"])
}
