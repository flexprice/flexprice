package tenant

import "github.com/flexprice/flexprice/internal/types"

// Builder changes a copy of a Tenant; the original is never mutated.
type Builder struct {
	t Tenant
}

// NewBuilder starts a builder from a copy of t, including its metadata map.
func NewBuilder(t *Tenant) *Builder {
	b := &Builder{t: *t}
	b.t.Metadata = make(types.Metadata, len(t.Metadata))
	for k, v := range t.Metadata {
		b.t.Metadata[k] = v
	}
	return b
}

// WithInternalStatus sets the tenant's internal status.
func (b *Builder) WithInternalStatus(s types.TenantInternalStatus) *Builder {
	b.t.InternalStatus = s
	return b
}

// Build returns the resulting tenant copy.
func (b *Builder) Build() *Tenant {
	out := b.t
	return &out
}
