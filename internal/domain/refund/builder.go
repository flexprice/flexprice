package refund

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type refundBuilder struct {
	refund *Refund
}

func NewRefundBuilder(r *Refund) *refundBuilder {
	if r == nil {
		return &refundBuilder{refund: &Refund{}}
	}
	copied := *r
	copied.PaymentID = copyString(r.PaymentID)
	copied.CreditNoteID = copyString(r.CreditNoteID)
	copied.PaymentGateway = copyString(r.PaymentGateway)
	copied.GatewayRefundID = copyString(r.GatewayRefundID)
	copied.GatewayTrackingID = copyString(r.GatewayTrackingID)
	copied.RefundDestinationID = copyString(r.RefundDestinationID)
	copied.GatewayIdempotencyToken = copyString(r.GatewayIdempotencyToken)
	copied.FailureReason = copyString(r.FailureReason)
	copied.InitiatedAt = copyTime(r.InitiatedAt)
	copied.SucceededAt = copyTime(r.SucceededAt)
	copied.FailedAt = copyTime(r.FailedAt)
	copied.CancelledAt = copyTime(r.CancelledAt)
	if r.Metadata != nil {
		m := make(types.Metadata, len(r.Metadata))
		for k, v := range r.Metadata {
			m[k] = v
		}
		copied.Metadata = m
	}
	if r.GatewayMetadata != nil {
		m := make(map[string]interface{}, len(r.GatewayMetadata))
		for k, v := range r.GatewayMetadata {
			m[k] = v
		}
		copied.GatewayMetadata = m
	}
	return &refundBuilder{refund: &copied}
}

func copyString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func (b *refundBuilder) WithID(id string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.ID = id
	return b
}

func (b *refundBuilder) WithInvoiceID(id string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.InvoiceID = id
	return b
}

func (b *refundBuilder) WithPaymentID(id *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.PaymentID = id
	return b
}

func (b *refundBuilder) WithCreditNoteID(id *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.CreditNoteID = id
	return b
}

func (b *refundBuilder) WithPaymentGateway(g *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.PaymentGateway = g
	return b
}

func (b *refundBuilder) WithGatewayRefundID(id *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.GatewayRefundID = id
	return b
}

func (b *refundBuilder) WithGatewayTrackingID(id *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.GatewayTrackingID = id
	return b
}

func (b *refundBuilder) WithAmount(a decimal.Decimal) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.Amount = a
	return b
}

func (b *refundBuilder) WithSettledAmount(a decimal.Decimal) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.SettledAmount = a
	return b
}

func (b *refundBuilder) WithCurrency(c string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.Currency = c
	return b
}

func (b *refundBuilder) WithStatus(s types.RefundStatus) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.RefundStatus = s
	return b
}

func (b *refundBuilder) WithRefundReason(r types.RefundReason) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.RefundReason = r
	return b
}

func (b *refundBuilder) WithDestination(d types.RefundDestination) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.RefundDestination = d
	return b
}

func (b *refundBuilder) WithDestinationID(id *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.RefundDestinationID = id
	return b
}

func (b *refundBuilder) WithAttempt(n int) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.Attempt = n
	return b
}

func (b *refundBuilder) WithIdempotencyKey(k string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.IdempotencyKey = k
	return b
}

func (b *refundBuilder) WithGatewayIdempotencyToken(t *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.GatewayIdempotencyToken = t
	return b
}

func (b *refundBuilder) WithFailureReason(r *string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.FailureReason = r
	return b
}

func (b *refundBuilder) WithMetadata(m types.Metadata) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.Metadata = m
	return b
}

func (b *refundBuilder) WithGatewayMetadata(m map[string]interface{}) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.GatewayMetadata = m
	return b
}

func (b *refundBuilder) WithInitiatedAt(t *time.Time) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.InitiatedAt = t
	return b
}

func (b *refundBuilder) WithSucceededAt(t *time.Time) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.SucceededAt = t
	return b
}

func (b *refundBuilder) WithFailedAt(t *time.Time) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.FailedAt = t
	return b
}

func (b *refundBuilder) WithCancelledAt(t *time.Time) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.CancelledAt = t
	return b
}

func (b *refundBuilder) WithEnvironmentID(id string) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.EnvironmentID = id
	return b
}

func (b *refundBuilder) WithBaseModel(m types.BaseModel) *refundBuilder {
	if b == nil || b.refund == nil {
		return b
	}
	b.refund.BaseModel = m
	return b
}

func (b *refundBuilder) Build() *Refund {
	if b == nil {
		return nil
	}
	return b.refund
}
