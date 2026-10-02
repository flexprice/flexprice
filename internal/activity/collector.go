package activity

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/types"
)

type ctxKey string

const (
	ctxCollector ctxKey = "activity_collector"
	ctxSuppress  ctxKey = "activity_suppress"
)

type key struct {
	entityType types.SystemEntityType
	entityID   string
}

// Pending is one entity's merged state for a transaction.
type Pending struct {
	EntityType types.SystemEntityType
	EntityID   string
	Label      string
	Op         Op
	Action     string
	Changes    map[string]Change
	Snapshot   map[string]any
	Fields     map[string]any
	Metadata   map[string]any
	Degraded   string
}

type Collector struct {
	mu      sync.Mutex
	order   []key
	pending map[key]*Pending
	closed  bool
}

func WithCollector(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxCollector, &Collector{pending: map[key]*Pending{}})
}

func CollectorFrom(ctx context.Context) *Collector {
	c, _ := ctx.Value(ctxCollector).(*Collector)
	return c
}

func (c *Collector) get(k key) *Pending {
	p, ok := c.pending[k]
	if !ok {
		p = &Pending{EntityType: k.entityType, EntityID: k.entityID, Changes: map[string]Change{}}
		c.pending[k] = p
		c.order = append(c.order, k)
	}
	return p
}

// Add merges a hook observation: first from wins, last to wins, a create
// stays a create and carries the final field values as its snapshot.
func (c *Collector) Add(r Record) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	p := c.get(key{r.EntityType, r.EntityID})
	switch {
	case p.Op == opUnknown:
		p.Op = r.Op
		if r.Op == OpCreate {
			p.Snapshot = r.Snapshot
		}
	case r.Op == OpCreate:
		p.Op = OpCreate
		p.Snapshot = r.Snapshot
	case p.Op == OpCreate && r.Op == OpUpdate:
		// The created row carries only the final snapshot; the intermediate
		// values were never visible outside this transaction. A nil snapshot
		// means the entity opted out of snapshots, so nothing is folded in.
		if p.Snapshot != nil {
			for f, ch := range r.Changes {
				if ch.Redacted {
					p.Snapshot[f] = "[redacted]"
					continue
				}
				p.Snapshot[f] = ch.To
			}
		}
		r.Changes = nil
	case r.Op == OpDelete:
		p.Op = OpDelete
	default:
		p.Op = OpUpdate
	}
	for f, ch := range r.Changes {
		prev, had := p.Changes[f]
		if had {
			ch.From = prev.From
		}
		p.Changes[f] = ch
	}
	if r.Fields != nil {
		p.Fields = r.Fields
	}
	if r.Label != "" {
		p.Label = r.Label
	}
	if r.Degraded != "" {
		p.Degraded = r.Degraded
	}
}

func (c *Collector) Name(e Entry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	p := c.get(key{types.SystemEntityType(e.EntityType), e.EntityID})
	p.Action = e.Action
	if e.Metadata != nil {
		p.Metadata = e.Metadata
	}
	for f, ch := range e.Changes {
		p.Changes[f] = ch
	}
	if p.Op == opUnknown && len(e.Changes) > 0 {
		p.Op = OpUpdate
	}
}

func (c *Collector) Entries() []Pending {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Pending, 0, len(c.order))
	for _, k := range c.order {
		p := c.pending[k]
		if len(p.Changes) == 0 && p.Op == OpUpdate && p.Action == "" && p.Degraded == "" {
			continue
		}
		out = append(out, *p)
	}
	return out
}

func (c *Collector) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}

func Suppress(ctx context.Context, reason string) context.Context {
	if reason == "" {
		panic("activity.Suppress requires a reason")
	}
	return context.WithValue(ctx, ctxSuppress, reason)
}

func IsSuppressed(ctx context.Context) bool {
	r, _ := ctx.Value(ctxSuppress).(string)
	return r != ""
}

func SuppressReason(ctx context.Context) string {
	r, _ := ctx.Value(ctxSuppress).(string)
	return r
}

const ctxQuerier ctxKey = "activity_querier"

// WithQuerier exposes the connection the hook must use for old-value reads
// and direct writes. The postgres client installs it per transaction.
func WithQuerier(ctx context.Context, q any) context.Context {
	return context.WithValue(ctx, ctxQuerier, q)
}
