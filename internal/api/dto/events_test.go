package dto

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestGetEventsRequest_ValidateEventIDRequiresCustomer(t *testing.T) {
	tests := []struct {
		name    string
		req     GetEventsRequest
		wantErr bool
	}{
		{name: "event id without customer", req: GetEventsRequest{EventID: "evt_1"}, wantErr: true},
		{name: "event id with customer", req: GetEventsRequest{EventID: "evt_1", ExternalCustomerID: "cust_1"}},
		{name: "customer only", req: GetEventsRequest{ExternalCustomerID: "cust_1"}},
		{name: "no filters", req: GetEventsRequest{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.True(t, ierr.IsValidation(err))
		})
	}
}
