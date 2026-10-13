package activity

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Entry is one logged change to one entity.
type Entry struct {
	ID            string
	EntityType    types.SystemEntityType
	EntityID      string
	Action        Action
	Actor         types.Actor
	State         json.RawMessage
	TenantID      string
	EnvironmentID string
}

// Batch is the entries of one tenant and environment committed together.
type Batch struct {
	TenantID      string
	EnvironmentID string
	CommittedAt   time.Time
	RequestID     string
	Source        types.Source
	IP            string
	UserAgent     string
	Entries       []Entry
}

// Sink receives committed batches; Publish must not block.
type Sink interface {
	Publish(b Batch)
}

type requestInfo struct {
	requestID string
	source    types.Source
	ip        string
	userAgent string
}

func requestInfoFrom(ctx context.Context) requestInfo {
	_, source, _ := types.GetActor(ctx)
	ip, userAgent := types.GetRequestMeta(ctx)
	return requestInfo{requestID: types.GetRequestID(ctx), source: source, ip: ip, userAgent: userAgent}
}

func (r requestInfo) batch(tenantID, environmentID string, entries []Entry) Batch {
	return Batch{
		TenantID: tenantID, EnvironmentID: environmentID, CommittedAt: time.Now().UTC(),
		RequestID: r.requestID, Source: r.source, IP: r.ip, UserAgent: r.userAgent, Entries: entries,
	}
}

const collectorKey = "activity"

// collector gathers the entries of one transaction until it commits.
type collector struct {
	mu      sync.Mutex
	reg     *Registry
	sink    Sink
	info    requestInfo
	entries []Entry
}

// add queues e on the transaction's collector, or publishes it at once outside a transaction.
func add(ctx context.Context, reg *Registry, sink Sink, e Entry) {
	created := false
	v, inTx := types.TxStoreLoadOrStore(ctx, collectorKey, func() any {
		created = true
		return &collector{reg: reg, sink: sink, info: requestInfoFrom(ctx)}
	})
	if !inTx {
		info := requestInfoFrom(ctx)
		sink.Publish(info.batch(e.TenantID, e.EnvironmentID, []Entry{e}))
		return
	}
	c := v.(*collector)
	c.mu.Lock()
	c.entries = append(c.entries, e)
	c.mu.Unlock()
	if created && !types.RegisterPostCommit(ctx, c.flush) {
		c.flush()
	}
}

func (c *collector) flush() {
	c.mu.Lock()
	entries := collapseByEntity(c.reg, c.entries)
	c.entries = nil
	c.mu.Unlock()

	type scope struct{ tenant, env string }
	var order []scope
	groups := map[scope][]Entry{}
	for _, e := range entries {
		s := scope{e.TenantID, e.EnvironmentID}
		if _, ok := groups[s]; !ok {
			order = append(order, s)
		}
		groups[s] = append(groups[s], e)
	}
	for _, s := range order {
		c.sink.Publish(c.info.batch(s.tenant, s.env, groups[s]))
	}
}

// collapseByEntity keeps one entry per entity: the winning action and actor, with the last state written.
func collapseByEntity(reg *Registry, entries []Entry) []Entry {
	type key struct {
		t  types.SystemEntityType
		id string
	}
	var order []key
	best := map[key]Entry{}
	for _, e := range entries {
		k := key{e.EntityType, e.EntityID}
		cur, seen := best[k]
		if !seen {
			order = append(order, k)
			best[k] = e
			continue
		}
		r, _ := reg.ByType(e.EntityType)
		winner := cur
		if r.rank(e.Action) > r.rank(cur.Action) {
			winner = e
		}
		winner.State = e.State
		best[k] = winner
	}
	out := make([]Entry, 0, len(order))
	for _, k := range order {
		out = append(out, best[k])
	}
	return out
}
