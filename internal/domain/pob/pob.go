package pob

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// PerformanceObligation is a continuing promise inside a contract, made of one or more subscription line items.
type PerformanceObligation struct {
	id                 string
	contractID         string
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

// Association links a performance obligation to an entity it covers, such as a subscription line item.
type Association struct {
	id         string
	pobID      string
	entityType types.RevRecEntityType
	entityID   string
	status     types.Status
	createdAt  time.Time
}

func (a *Association) ID() string {
	if a == nil {
		return ""
	}
	return a.id
}

func (a *Association) POBID() string {
	if a == nil {
		return ""
	}
	return a.pobID
}

func (a *Association) EntityType() types.RevRecEntityType {
	if a == nil {
		return ""
	}
	return a.entityType
}

func (a *Association) EntityID() string {
	if a == nil {
		return ""
	}
	return a.entityID
}

func (a *Association) Status() types.Status {
	if a == nil {
		return ""
	}
	return a.status
}

func (a *Association) CreatedAt() time.Time {
	if a == nil {
		return time.Time{}
	}
	return a.createdAt
}

// AssociationBuilder copies an existing association and applies field updates to the copy.
type AssociationBuilder struct {
	association Association
}

// NewAssociationBuilder returns a builder seeded from a; a nil a starts empty.
func NewAssociationBuilder(a *Association) *AssociationBuilder {
	b := &AssociationBuilder{}
	if a != nil {
		b.association = *a
	}
	return b
}

func (b *AssociationBuilder) WithID(id string) *AssociationBuilder {
	b.association.id = id
	return b
}

func (b *AssociationBuilder) WithPOBID(pobID string) *AssociationBuilder {
	b.association.pobID = pobID
	return b
}

func (b *AssociationBuilder) WithEntityType(entityType types.RevRecEntityType) *AssociationBuilder {
	b.association.entityType = entityType
	return b
}

func (b *AssociationBuilder) WithEntityID(entityID string) *AssociationBuilder {
	b.association.entityID = entityID
	return b
}

func (b *AssociationBuilder) WithStatus(status types.Status) *AssociationBuilder {
	b.association.status = status
	return b
}

func (b *AssociationBuilder) WithCreatedAt(createdAt time.Time) *AssociationBuilder {
	b.association.createdAt = createdAt
	return b
}

func (b *AssociationBuilder) Build() *Association {
	a := b.association
	return &a
}
