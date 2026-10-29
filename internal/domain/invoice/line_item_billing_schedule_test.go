package invoice

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestInvoiceLineItemBuilder_BillingScheduleItemID(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "replace source"
		if clear {
			name = "clear source"
		}
		t.Run(name, func(t *testing.T) {
			original := &InvoiceLineItem{ID: "line_original", BillingScheduleItemID: lo.ToPtr("bsi_original")}
			source := lo.ToPtr("bsi_replacement")
			if clear {
				source = nil
			}
			got := NewInvoiceLineItemBuilder(original).WithBillingScheduleItemID(source).Build()
			require.NotSame(t, original, got)
			require.Equal(t, original.ID, got.ID)
			require.Equal(t, "bsi_original", *original.BillingScheduleItemID)
			if clear {
				require.Nil(t, got.BillingScheduleItemID)
				return
			}
			require.NotSame(t, source, got.BillingScheduleItemID)
			*source = "bsi_changed_after_build"
			require.Equal(t, "bsi_replacement", *got.BillingScheduleItemID)
		})
	}

	t.Run("new line", func(t *testing.T) {
		got := NewInvoiceLineItemBuilder(nil).WithBillingScheduleItemID(lo.ToPtr("bsi_new")).Build()
		require.Equal(t, "bsi_new", *got.BillingScheduleItemID)
	})

	t.Run("nil builder", func(t *testing.T) {
		var builder *invoiceLineItemBuilder
		require.Nil(t, builder.WithBillingScheduleItemID(lo.ToPtr("bsi_new")).Build())
		require.Nil(t, (&invoiceLineItemBuilder{}).WithBillingScheduleItemID(nil).Build())
	})
}
