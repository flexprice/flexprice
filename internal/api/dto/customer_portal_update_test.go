package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortalUpdateCustomerRequestIgnoresForbiddenFields(t *testing.T) {
	body := `{"name":"New","email":"a@b.co","timezone":"UTC",
		"external_id":"hijack","billing_currency":"eur","metadata":{"k":"v"}}`

	var req PortalUpdateCustomerRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req))
	got := req.ToUpdateCustomerRequest()

	assert.Equal(t, "New", *got.Name)
	assert.Equal(t, "a@b.co", *got.Email)
	assert.Equal(t, "UTC", *got.Timezone)
	assert.Nil(t, got.ExternalID)
	assert.Nil(t, got.BillingCurrency)
	assert.Nil(t, got.Metadata)
}
