package payment

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// paymentBuilder copies an existing payment and applies field updates.
type paymentBuilder struct {
	p *Payment
}

// NewPaymentBuilder returns a builder seeded from an existing payment.
func NewPaymentBuilder(p *Payment) *paymentBuilder {
	if p == nil {
		return &paymentBuilder{p: &Payment{}}
	}

	copied := *p
	if p.Metadata != nil {
		copied.Metadata = lo.Assign(types.Metadata{}, p.Metadata)
	}
	if p.GatewayMetadata != nil {
		copied.GatewayMetadata = lo.Assign(types.Metadata{}, p.GatewayMetadata)
	}

	return &paymentBuilder{p: &copied}
}

func (b *paymentBuilder) WithPaymentStatus(status types.PaymentStatus) *paymentBuilder {
	if b == nil || b.p == nil {
		return b
	}
	b.p.PaymentStatus = status
	return b
}

func (b *paymentBuilder) WithSucceededAt(t *time.Time) *paymentBuilder {
	if b == nil || b.p == nil {
		return b
	}
	b.p.SucceededAt = t
	return b
}

func (b *paymentBuilder) WithGatewayPaymentID(id *string) *paymentBuilder {
	if b == nil || b.p == nil {
		return b
	}
	b.p.GatewayPaymentID = id
	return b
}

func (b *paymentBuilder) Build() *Payment {
	if b == nil {
		return nil
	}
	return b.p
}
