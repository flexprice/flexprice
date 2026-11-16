package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSkipDistinctMeterPrefilterGate(t *testing.T) {
	sumMeter := &meter.Meter{ID: "m1", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	maxMeter := &meter.Meter{ID: "m2", Aggregation: meter.Aggregation{Type: types.AggregationMax, BucketSize: types.WindowSizeHour}}
	usage := &SubscriptionMeterUsage{
		MeterMap: map[string]*meter.Meter{"m1": sumMeter, "m2": maxMeter},
		PriceMap: map[string]*price.Price{"p1": {ID: "p1", MeterID: "m1"}, "p2": {ID: "p2", MeterID: "m2"}},
	}
	plain := []*subscription.SubscriptionLineItem{
		{PriceType: types.PRICE_TYPE_FIXED},
		{PriceType: types.PRICE_TYPE_USAGE},
		{PriceType: types.PRICE_TYPE_USAGE, MeterID: "m1", PriceID: "p1"},
	}
	bucketed := append([]*subscription.SubscriptionLineItem{}, plain...)
	bucketed = append(bucketed, &subscription.SubscriptionLineItem{PriceType: types.PRICE_TYPE_USAGE, MeterID: "m2", PriceID: "p2"})

	tests := []struct {
		name      string
		cfg       *config.Configuration
		analytics bool
		items     []*subscription.SubscriptionLineItem
		want      bool
	}{
		{"nil config", nil, false, plain, false},
		{"flag off", &config.Configuration{}, false, plain, false},
		{"flag on", flagOn(), false, plain, true},
		{"analytics query", flagOn(), true, plain, false},
		{"bucketed line", flagOn(), false, bucketed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &meterUsageService{ServiceParams: ServiceParams{Config: tt.cfg}}
			assert.Equal(t, tt.want, svc.skipDistinctMeterPrefilter(tt.analytics, tt.items, usage))
		})
	}
}

func flagOn() *config.Configuration {
	return &config.Configuration{MeterUsage: config.MeterUsageConfig{SkipDistinctMeterPrefilter: true}}
}
