package product

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Product is a reusable commercial identity for what is sold.
type Product struct {
	id          string
	name        string
	description string
	lookupKey   string
	status      types.Status
	createdAt   time.Time
	updatedAt   time.Time
}

func (p *Product) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *Product) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *Product) Description() string {
	if p == nil {
		return ""
	}
	return p.description
}

func (p *Product) LookupKey() string {
	if p == nil {
		return ""
	}
	return p.lookupKey
}

func (p *Product) Status() types.Status {
	if p == nil {
		return ""
	}
	return p.status
}

func (p *Product) CreatedAt() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.createdAt
}

func (p *Product) UpdatedAt() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.updatedAt
}

// Builder copies an existing product and applies field updates to the copy.
type Builder struct {
	product Product
}

// NewBuilder returns a builder seeded from p; a nil p starts empty.
func NewBuilder(p *Product) *Builder {
	b := &Builder{}
	if p != nil {
		b.product = *p
	}
	return b
}

func (b *Builder) WithID(id string) *Builder {
	b.product.id = id
	return b
}

func (b *Builder) WithName(name string) *Builder {
	b.product.name = name
	return b
}

func (b *Builder) WithDescription(description string) *Builder {
	b.product.description = description
	return b
}

func (b *Builder) WithLookupKey(lookupKey string) *Builder {
	b.product.lookupKey = lookupKey
	return b
}

func (b *Builder) WithStatus(status types.Status) *Builder {
	b.product.status = status
	return b
}

func (b *Builder) WithCreatedAt(createdAt time.Time) *Builder {
	b.product.createdAt = createdAt
	return b
}

func (b *Builder) WithUpdatedAt(updatedAt time.Time) *Builder {
	b.product.updatedAt = updatedAt
	return b
}

func (b *Builder) Build() *Product {
	p := b.product
	return &p
}
