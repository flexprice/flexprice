package pob

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PerformanceObligation is a promise inside a contract: one per Product and recognition method.
type PerformanceObligation struct {
	id                 string
	contractID         string
	productID          string
	allocatedAmount    decimal.Decimal
	name               string
	recognitionMethod  types.RecognitionMethod
	amortizationMethod types.AmortizationMethod
	serviceStart       time.Time
	serviceEnd         *time.Time
	pobStatus          types.PerformanceObligationStatus
	status             types.Status
	createdAt          time.Time
	updatedAt          time.Time
}

func (p *PerformanceObligation) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *PerformanceObligation) ContractID() string {
	if p == nil {
		return ""
	}
	return p.contractID
}

func (p *PerformanceObligation) ProductID() string {
	if p == nil {
		return ""
	}
	return p.productID
}

// AllocatedAmount is the fixed consideration allocated to the POB; 0 for a variable-only POB.
func (p *PerformanceObligation) AllocatedAmount() decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	return p.allocatedAmount
}

func (p *PerformanceObligation) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *PerformanceObligation) RecognitionMethod() types.RecognitionMethod {
	if p == nil {
		return ""
	}
	return p.recognitionMethod
}

func (p *PerformanceObligation) AmortizationMethod() types.AmortizationMethod {
	if p == nil {
		return ""
	}
	return p.amortizationMethod
}

func (p *PerformanceObligation) ServiceStart() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.serviceStart
}

func (p *PerformanceObligation) ServiceEnd() *time.Time {
	if p == nil {
		return nil
	}
	return p.serviceEnd
}

func (p *PerformanceObligation) POBStatus() types.PerformanceObligationStatus {
	if p == nil {
		return ""
	}
	return p.pobStatus
}

func (p *PerformanceObligation) Status() types.Status {
	if p == nil {
		return ""
	}
	return p.status
}

func (p *PerformanceObligation) CreatedAt() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.createdAt
}

func (p *PerformanceObligation) UpdatedAt() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.updatedAt
}

// Builder copies an existing performance obligation and applies field updates to the copy.
type Builder struct {
	pob PerformanceObligation
}

// NewBuilder returns a builder seeded from p; a nil p starts empty.
func NewBuilder(p *PerformanceObligation) *Builder {
	b := &Builder{}
	if p != nil {
		b.pob = *p
	}
	return b
}

func (b *Builder) WithID(id string) *Builder {
	b.pob.id = id
	return b
}

func (b *Builder) WithContractID(contractID string) *Builder {
	b.pob.contractID = contractID
	return b
}

func (b *Builder) WithProductID(productID string) *Builder {
	b.pob.productID = productID
	return b
}

func (b *Builder) WithAllocatedAmount(allocatedAmount decimal.Decimal) *Builder {
	b.pob.allocatedAmount = allocatedAmount
	return b
}

func (b *Builder) WithName(name string) *Builder {
	b.pob.name = name
	return b
}

func (b *Builder) WithRecognitionMethod(recognitionMethod types.RecognitionMethod) *Builder {
	b.pob.recognitionMethod = recognitionMethod
	return b
}

func (b *Builder) WithAmortizationMethod(amortizationMethod types.AmortizationMethod) *Builder {
	b.pob.amortizationMethod = amortizationMethod
	return b
}

func (b *Builder) WithServiceStart(serviceStart time.Time) *Builder {
	b.pob.serviceStart = serviceStart
	return b
}

func (b *Builder) WithServiceEnd(serviceEnd *time.Time) *Builder {
	b.pob.serviceEnd = serviceEnd
	return b
}

func (b *Builder) WithPOBStatus(pobStatus types.PerformanceObligationStatus) *Builder {
	b.pob.pobStatus = pobStatus
	return b
}

func (b *Builder) WithStatus(status types.Status) *Builder {
	b.pob.status = status
	return b
}

func (b *Builder) WithCreatedAt(createdAt time.Time) *Builder {
	b.pob.createdAt = createdAt
	return b
}

func (b *Builder) WithUpdatedAt(updatedAt time.Time) *Builder {
	b.pob.updatedAt = updatedAt
	return b
}

func (b *Builder) Build() *PerformanceObligation {
	p := b.pob
	return &p
}
