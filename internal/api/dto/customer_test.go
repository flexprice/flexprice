package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestCustomerResponse_ToWebhookPayload(t *testing.T) {
	t.Run("returns an untrimmed copy", func(t *testing.T) {
		cust := &CustomerResponse{Customer: &customer.Customer{ID: "cust_1"}}
		out := cust.ToWebhookPayload(types.WebhookEventCustomerUpdated)
		assert.Equal(t, "cust_1", out.ID)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var cust *CustomerResponse
		assert.Nil(t, cust.ToWebhookPayload(types.WebhookEventCustomerUpdated))
	})
}
