package dto

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecalculateInvoiceSelection(t *testing.T) {
	for _, test := range []struct {
		name  string
		ids   []string
		valid bool
	}{
		{"bodyless", nil, true},
		{"one selection", []string{"item"}, true},
		{"explicit empty", []string{}, false},
		{"duplicates", []string{"item", "item"}, false},
		{"blank", []string{" "}, false},
		{"long", []string{strings.Repeat("x", 51)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := (RecalculateInvoiceRequest{BillingScheduleItemIDs: test.ids}).Validate()
			require.Equal(t, test.valid, err == nil, "%v", err)
		})
	}
	ids := make([]string, 501)
	for i := range ids {
		ids[i] = fmt.Sprintf("item_%d", i)
	}
	require.NoError(t, (RecalculateInvoiceRequest{BillingScheduleItemIDs: ids[:500]}).Validate())
	require.Error(t, (RecalculateInvoiceRequest{BillingScheduleItemIDs: ids}).Validate())
}
