package fxrate

import (
	"time"

	"github.com/shopspring/decimal"
)

type fxRateBuilder struct {
	rate *FXRate
}

// NewFXRateBuilder returns a builder seeded from an existing FXRate. The source
// is deep-copied, so mutations through the builder never touch the original.
func NewFXRateBuilder(r *FXRate) *fxRateBuilder {
	if r == nil {
		return &fxRateBuilder{rate: &FXRate{}}
	}
	copied := *r
	copied.ValidFrom = copyTime(r.ValidFrom)
	copied.ValidTo = copyTime(r.ValidTo)
	if r.Metadata != nil {
		m := make(map[string]string, len(r.Metadata))
		for k, v := range r.Metadata {
			m[k] = v
		}
		copied.Metadata = m
	}
	return &fxRateBuilder{rate: &copied}
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func (b *fxRateBuilder) WithRate(rate decimal.Decimal) *fxRateBuilder {
	b.rate.Rate = rate
	return b
}

func (b *fxRateBuilder) WithValidFrom(t *time.Time) *fxRateBuilder {
	b.rate.ValidFrom = copyTime(t)
	return b
}

func (b *fxRateBuilder) WithValidTo(t *time.Time) *fxRateBuilder {
	b.rate.ValidTo = copyTime(t)
	return b
}

func (b *fxRateBuilder) WithMetadata(m map[string]string) *fxRateBuilder {
	b.rate.Metadata = m
	return b
}

func (b *fxRateBuilder) Build() *FXRate {
	return b.rate
}
