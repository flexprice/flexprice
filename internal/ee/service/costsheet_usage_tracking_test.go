package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func costPrice(id, meterID, currency string) *dto.PriceResponse {
	return &dto.PriceResponse{Price: &price.Price{ID: id, MeterID: meterID, Currency: currency, Type: types.PRICE_TYPE_USAGE}}
}

func TestCostPricesByMeter(t *testing.T) {
	bandwidthEUR := costPrice("price_bw_eur", "meter_bw", "eur")
	bandwidthUSD := costPrice("price_bw_usd", "meter_bw", "usd")
	smsUSD := costPrice("price_sms_usd", "meter_sms", "usd")
	fixedUSD := &dto.PriceResponse{Price: &price.Price{ID: "price_fixed", Currency: "usd", Type: types.PRICE_TYPE_FIXED}}

	tests := []struct {
		name     string
		prices   []*dto.PriceResponse
		currency string
		want     map[string]string // meter ID -> price ID
		wantErr  bool
	}{
		{
			name:     "picks the customer's currency even when another currency is listed first",
			prices:   []*dto.PriceResponse{bandwidthEUR, bandwidthUSD, smsUSD},
			currency: "usd",
			want:     map[string]string{"meter_bw": "price_bw_usd", "meter_sms": "price_sms_usd"},
		},
		{
			name:     "matches currency case-insensitively",
			prices:   []*dto.PriceResponse{bandwidthEUR, bandwidthUSD},
			currency: "USD",
			want:     map[string]string{"meter_bw": "price_bw_usd"},
		},
		{
			name:     "skips non-usage prices",
			prices:   []*dto.PriceResponse{fixedUSD, smsUSD},
			currency: "usd",
			want:     map[string]string{"meter_sms": "price_sms_usd"},
		},
		{
			name:     "meter without a price in the customer's currency fails",
			prices:   []*dto.PriceResponse{bandwidthEUR, smsUSD},
			currency: "usd",
			wantErr:  true,
		},
		{
			name:     "no currency and one currency per meter uses that price",
			prices:   []*dto.PriceResponse{bandwidthEUR, smsUSD},
			currency: "",
			want:     map[string]string{"meter_bw": "price_bw_eur", "meter_sms": "price_sms_usd"},
		},
		{
			name:     "no currency and a meter in several currencies fails",
			prices:   []*dto.PriceResponse{bandwidthEUR, bandwidthUSD},
			currency: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := costPricesByMeter(tt.prices, tt.currency)
			if tt.wantErr {
				require.Error(t, err)
				require.True(t, ierr.IsValidation(err), "got %v", err)
				return
			}
			require.NoError(t, err)
			gotIDs := make(map[string]string, len(got))
			for meterID, p := range got {
				gotIDs[meterID] = p.ID
			}
			require.Equal(t, tt.want, gotIDs)
		})
	}
}
