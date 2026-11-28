package meterusagecheckpoint

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type checkpointBuilder struct {
	checkpoint MeterUsageCheckpoint
}

// NewMeterUsageCheckpointBuilder copies a checkpoint before applying updates.
func NewMeterUsageCheckpointBuilder(checkpoint *MeterUsageCheckpoint) *checkpointBuilder {
	b := &checkpointBuilder{}
	if checkpoint != nil {
		b.checkpoint = *checkpoint
		b.WithInvoiceID(checkpoint.InvoiceID)
	}
	return b
}

func (b *checkpointBuilder) WithAggregationType(aggregation types.AggregationType) *checkpointBuilder {
	b.checkpoint.AggregationType = aggregation
	return b
}

func (b *checkpointBuilder) WithTotal(total decimal.Decimal) *checkpointBuilder {
	b.checkpoint.Total = total
	return b
}

func (b *checkpointBuilder) WithInvoiceID(invoiceID *string) *checkpointBuilder {
	b.checkpoint.InvoiceID = nil
	if invoiceID != nil {
		copied := *invoiceID
		b.checkpoint.InvoiceID = &copied
	}
	return b
}

func (b *checkpointBuilder) WithUpdatedAt(at time.Time) *checkpointBuilder {
	b.checkpoint.UpdatedAt = at
	return b
}

func (b *checkpointBuilder) Build() *MeterUsageCheckpoint {
	copied := b.checkpoint
	if copied.InvoiceID != nil {
		invoiceID := *copied.InvoiceID
		copied.InvoiceID = &invoiceID
	}
	return &copied
}
