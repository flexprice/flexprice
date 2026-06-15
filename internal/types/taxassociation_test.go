package types

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestTaxAssociationFilter_Validate_DateRange(t *testing.T) {
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		filter  *TaxAssociationFilter
		wantErr bool
	}{
		{
			name:    "no dates",
			filter:  &TaxAssociationFilter{},
			wantErr: false,
		},
		{
			name: "only start date",
			filter: &TaxAssociationFilter{
				StartDate: lo.ToPtr(start),
			},
			wantErr: false,
		},
		{
			name: "valid range",
			filter: &TaxAssociationFilter{
				StartDate: lo.ToPtr(start),
				EndDate:   lo.ToPtr(end),
			},
			wantErr: false,
		},
		{
			name: "equal dates",
			filter: &TaxAssociationFilter{
				StartDate: lo.ToPtr(start),
				EndDate:   lo.ToPtr(start),
			},
			wantErr: false,
		},
		{
			name: "inverted range",
			filter: &TaxAssociationFilter{
				StartDate: lo.ToPtr(end),
				EndDate:   lo.ToPtr(start),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
