package contract

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Contract is the accounting agreement a subscription establishes.
type Contract struct {
	id             string
	customerID     string
	currency       string
	termStart      *time.Time
	termEnd        *time.Time
	endedAt        *time.Time
	contractStatus types.ContractStatus
	status         types.Status
	createdAt      time.Time
	updatedAt      time.Time
}

func (c *Contract) ID() string {
	if c == nil {
		return ""
	}
	return c.id
}

func (c *Contract) CustomerID() string {
	if c == nil {
		return ""
	}
	return c.customerID
}

func (c *Contract) Currency() string {
	if c == nil {
		return ""
	}
	return c.currency
}

func (c *Contract) TermStart() *time.Time {
	if c == nil {
		return nil
	}
	return c.termStart
}

func (c *Contract) TermEnd() *time.Time {
	if c == nil {
		return nil
	}
	return c.termEnd
}

func (c *Contract) EndedAt() *time.Time {
	if c == nil {
		return nil
	}
	return c.endedAt
}

func (c *Contract) ContractStatus() types.ContractStatus {
	if c == nil {
		return ""
	}
	return c.contractStatus
}

func (c *Contract) Status() types.Status {
	if c == nil {
		return ""
	}
	return c.status
}

func (c *Contract) CreatedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.createdAt
}

func (c *Contract) UpdatedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.updatedAt
}

// Builder copies an existing contract and applies field updates to the copy.
type Builder struct {
	contract Contract
}

// NewBuilder returns a builder seeded from c; a nil c starts empty.
func NewBuilder(c *Contract) *Builder {
	b := &Builder{}
	if c != nil {
		b.contract = *c
	}
	return b
}

func (b *Builder) WithID(id string) *Builder {
	b.contract.id = id
	return b
}

func (b *Builder) WithCustomerID(customerID string) *Builder {
	b.contract.customerID = customerID
	return b
}

func (b *Builder) WithCurrency(currency string) *Builder {
	b.contract.currency = currency
	return b
}

func (b *Builder) WithTermStart(termStart *time.Time) *Builder {
	b.contract.termStart = termStart
	return b
}

func (b *Builder) WithTermEnd(termEnd *time.Time) *Builder {
	b.contract.termEnd = termEnd
	return b
}

func (b *Builder) WithEndedAt(endedAt *time.Time) *Builder {
	b.contract.endedAt = endedAt
	return b
}

func (b *Builder) WithContractStatus(contractStatus types.ContractStatus) *Builder {
	b.contract.contractStatus = contractStatus
	return b
}

func (b *Builder) WithStatus(status types.Status) *Builder {
	b.contract.status = status
	return b
}

func (b *Builder) WithCreatedAt(createdAt time.Time) *Builder {
	b.contract.createdAt = createdAt
	return b
}

func (b *Builder) WithUpdatedAt(updatedAt time.Time) *Builder {
	b.contract.updatedAt = updatedAt
	return b
}

func (b *Builder) Build() *Contract {
	c := b.contract
	return &c
}

// Association links a contract to an entity it covers, such as a subscription.
type Association struct {
	id         string
	contractID string
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

func (a *Association) ContractID() string {
	if a == nil {
		return ""
	}
	return a.contractID
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

func (b *AssociationBuilder) WithContractID(contractID string) *AssociationBuilder {
	b.association.contractID = contractID
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
