package emaillog

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type emailLogBuilder struct {
	e *EmailLog
}

// NewEmailLogBuilder starts from a copy of e (or an empty email log when e is nil).
func NewEmailLogBuilder(e *EmailLog) *emailLogBuilder {
	if e == nil {
		return &emailLogBuilder{e: &EmailLog{Metadata: types.Metadata{}}}
	}

	cp := *e
	cp.ToAddresses = append([]string(nil), e.ToAddresses...)
	cp.CCAddresses = append([]string(nil), e.CCAddresses...)
	cp.Metadata = lo.Assign(types.Metadata{}, e.Metadata)

	return &emailLogBuilder{e: &cp}
}

func (b *emailLogBuilder) WithProviderMessageID(v string) *emailLogBuilder {
	b.e.ProviderMessageID = lo.ToPtr(v)
	return b
}

func (b *emailLogBuilder) WithDeliveryStatus(v types.EmailDeliveryStatus) *emailLogBuilder {
	b.e.DeliveryStatus = v
	return b
}

func (b *emailLogBuilder) WithStatusReason(v string) *emailLogBuilder {
	b.e.StatusReason = lo.ToPtr(v)
	return b
}

func (b *emailLogBuilder) WithSentAt(v time.Time) *emailLogBuilder {
	b.e.SentAt = lo.ToPtr(v)
	return b
}

func (b *emailLogBuilder) WithDeliveredAt(v time.Time) *emailLogBuilder {
	b.e.DeliveredAt = lo.ToPtr(v)
	return b
}

func (b *emailLogBuilder) WithMetadata(key, value string) *emailLogBuilder {
	b.e.Metadata[key] = value
	return b
}

func (b *emailLogBuilder) Build() *EmailLog {
	return b.e
}
