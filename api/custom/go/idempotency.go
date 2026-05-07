package flexprice

import (
	"github.com/flexprice/go-sdk/v2/models/dtos"
)

// WithIdempotencyKey sets the Idempotency-Key request header for a single API call.
// Use this on POST requests that create or mutate resources to safely retry
// requests without risk of duplicate operations.
//
//	resp, err := client.Customers.CreateCustomer(ctx, req,
//	    flexprice.WithIdempotencyKey("create-customer-acme-001"))
func WithIdempotencyKey(key string) dtos.Option {
	return dtos.WithSetHeaders(map[string]string{
		"Idempotency-Key": key,
	})
}
