package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
)

func TestBulkIngestEventRequestValidate(t *testing.T) {
	mk := func(n int) *dto.BulkIngestEventRequest {
		ev := make([]*dto.IngestEventRequest, n)
		for i := range ev {
			ev[i] = &dto.IngestEventRequest{
				EventName:          "api_call",
				ExternalCustomerID: "cust_1",
			}
		}
		return &dto.BulkIngestEventRequest{Events: ev}
	}
	cases := []struct {
		name    string
		n       int
		wantErr bool
	}{
		{"empty rejected", 0, true},
		{"one ok", 1, false},
		{"at cap ok", 1000, false},
		{"over cap rejected", 1001, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mk(c.n).Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("n=%d err=%v, wantErr=%v", c.n, err, c.wantErr)
			}
		})
	}
}
