package billingterm

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestNewInstalmentBuilder_CopiesInput(t *testing.T) {
	billOn := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	original := &Instalment{
		ID:               "binst_1",
		OwnerType:        types.BillingTermOwnerCommitment,
		OwnerID:          "cmt_1",
		Amount:           decimal.NewFromInt(100),
		BillOn:           billOn,
		InstalmentStatus: types.InstalmentStatusScheduled,
		Metadata:         types.Metadata{"k": "v"},
	}

	built := NewInstalmentBuilder(original).Build()
	built.Metadata["k"] = "changed"
	built.Metadata["extra"] = "added"
	built.Amount = decimal.NewFromInt(1)

	require.Equal(t, types.Metadata{"k": "v"}, original.Metadata)
	require.True(t, original.Amount.Equal(decimal.NewFromInt(100)))
	require.NotSame(t, original, built)
}

func TestNewInstalmentBuilder_Nil(t *testing.T) {
	require.Equal(t, &Instalment{}, NewInstalmentBuilder(nil).Build())
}

func TestInstalmentBuilder_WithSetsOnlyItsField(t *testing.T) {
	newAmount := decimal.NewFromInt(250)
	newBillOn := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	newTriggeredAt := time.Date(2027, 5, 1, 12, 0, 0, 0, time.UTC)

	base := func() *Instalment {
		return &Instalment{
			ID:               "binst_1",
			OwnerType:        types.BillingTermOwnerCommitment,
			OwnerID:          "cmt_1",
			Amount:           decimal.NewFromInt(100),
			BillOn:           time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			InstalmentStatus: types.InstalmentStatusScheduled,
			Metadata:         types.Metadata{"k": "v"},
		}
	}

	tests := []struct {
		name   string
		apply  func(b *instalmentBuilder) *instalmentBuilder
		expect func(i *Instalment)
	}{
		{
			name:   "amount",
			apply:  func(b *instalmentBuilder) *instalmentBuilder { return b.WithAmount(newAmount) },
			expect: func(i *Instalment) { i.Amount = newAmount },
		},
		{
			name:   "bill_on",
			apply:  func(b *instalmentBuilder) *instalmentBuilder { return b.WithBillOn(newBillOn) },
			expect: func(i *Instalment) { i.BillOn = newBillOn },
		},
		{
			name:   "triggered_at",
			apply:  func(b *instalmentBuilder) *instalmentBuilder { return b.WithTriggeredAt(newTriggeredAt) },
			expect: func(i *Instalment) { i.TriggeredAt = lo.ToPtr(newTriggeredAt) },
		},
		{
			name: "status",
			apply: func(b *instalmentBuilder) *instalmentBuilder {
				return b.WithStatus(types.InstalmentStatusCancelled)
			},
			expect: func(i *Instalment) { i.InstalmentStatus = types.InstalmentStatusCancelled },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := base()
			tt.expect(want)

			got := tt.apply(NewInstalmentBuilder(base())).Build()

			require.Equal(t, want, got)
		})
	}
}
