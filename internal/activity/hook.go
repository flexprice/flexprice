package activity

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent"
	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type mutation interface {
	ent.Mutation
	IDs(context.Context) ([]string, error)
	ID() (string, bool)
}

// clientMutation is satisfied by every generated ent mutation.
type clientMutation interface {
	Client() *flexent.Client
}

// oldValuesFunc fetches the current column values for ids before an update.
type oldValuesFunc func(ctx context.Context, def Definition, ids []string, cols []string) (map[string]map[string]any, error)

type hook struct {
	reg *Registry
	log *logger.Logger
	old oldValuesFunc
}

// Hook returns the ent hook that observes every mutation on registered types.
func Hook(reg *Registry, log *logger.Logger) ent.Hook {
	h := &hook{reg: reg, log: log}
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return h.mutate(ctx, next, m)
		})
	}
}

func (h *hook) mutate(ctx context.Context, next ent.Mutator, m ent.Mutation) (ent.Value, error) {
	def, ok := h.reg.ByEntType(m.Type())
	if !ok || IsSuppressed(ctx) {
		return next.Mutate(ctx, m)
	}
	mm, ok := m.(mutation)
	if !ok {
		return next.Mutate(ctx, m)
	}
	// Outside WithTx no querier is installed; reads and the direct write then
	// go through the mutation's own client.
	if ctx.Value(ctxQuerier) == nil {
		if cm, ok := m.(clientMutation); ok {
			ctx = WithQuerier(ctx, cm.Client())
		}
	}
	switch {
	case m.Op().Is(ent.OpCreate):
		return h.create(ctx, next, mm, def)
	case m.Op().Is(ent.OpDelete) || m.Op().Is(ent.OpDeleteOne):
		return h.delete(ctx, next, mm, def)
	default:
		return h.update(ctx, next, mm, def)
	}
}

func fieldValues(m ent.Mutation) map[string]any {
	out := map[string]any{}
	for _, f := range m.Fields() {
		if v, ok := m.Field(f); ok {
			out[f] = v
		}
	}
	return out
}

func (h *hook) create(ctx context.Context, next ent.Mutator, m mutation, def Definition) (ent.Value, error) {
	v, err := next.Mutate(ctx, m)
	if err != nil {
		return v, err
	}
	id, _ := m.ID()
	fields := fieldValues(m)
	// The id is not one of the mutation's fields; the label ladder needs it.
	if _, has := fields["id"]; !has && id != "" {
		fields["id"] = id
	}
	h.resolveCustomer(ctx, def, fields)
	rec := Record{EntityType: def.EntityType, EntityID: id, Op: OpCreate, Fields: fields, Label: label(def, fields)}
	if def.SnapshotMode == SnapshotFull {
		rec.Snapshot = snapshot(def, fields)
	}
	h.emit(ctx, rec)
	return v, nil
}

// resolveCustomer fills fields["customer_id"] through the definition's parent
// lookup when the row itself has none. Failures leave it empty; the row still logs.
func (h *hook) resolveCustomer(ctx context.Context, def Definition, fields map[string]any) {
	if def.CustomerLookup == nil {
		return
	}
	if def.CustomerID != nil && def.CustomerID(fields) != "" {
		return
	}
	q, ok := ctx.Value(ctxQuerier).(Querier)
	if !ok {
		return
	}
	id, err := def.CustomerLookup(ctx, q, fields)
	if err != nil {
		if h.log != nil {
			h.log.Error(ctx, "activity customer lookup failed", "error", err, "entity_type", string(def.EntityType))
		}
		return
	}
	if id != "" {
		fields["customer_id"] = id
	}
}

// label runs the entity's descriptor ladder; the short id is the last rung.
func label(def Definition, fields map[string]any) string {
	if def.Label != nil {
		if l := def.Label(fields); l != "" {
			return l
		}
	}
	if id, _ := fields["id"].(string); id != "" {
		return id
	}
	return ""
}

func snapshot(def Definition, fields map[string]any) map[string]any {
	redact := map[string]bool{}
	for _, f := range def.RedactFields {
		redact[f] = true
	}
	out := map[string]any{}
	for f, v := range fields {
		if bookkeeping[f] {
			continue
		}
		if redact[f] {
			out[f] = "[redacted]"
			continue
		}
		out[f] = display(v)
	}
	return out
}

// update reads the old values of every matched row before the write, because
// ent update mutations are bulk-shaped, then records one diff per row.
func (h *hook) update(ctx context.Context, next ent.Mutator, m mutation, def Definition) (ent.Value, error) {
	newVals := fieldValues(m)
	if len(newVals) == 0 {
		return next.Mutate(ctx, m)
	}
	ids, err := m.IDs(ctx)
	if err != nil || len(ids) == 0 {
		return next.Mutate(ctx, m)
	}
	cols := make([]string, 0, len(newVals)+len(def.LabelFields)+len(def.ParentFields))
	for c := range newVals {
		cols = append(cols, c)
	}
	for _, c := range append(append([]string{}, def.LabelFields...), def.ParentFields...) {
		if _, set := newVals[c]; !set && !containsStr(cols, c) {
			cols = append(cols, c)
		}
	}
	oldFn := h.old
	if oldFn == nil {
		oldFn = h.selectOld
	}
	olds, oldErr := oldFn(ctx, def, ids, cols)
	v, err := next.Mutate(ctx, m)
	if err != nil {
		return v, err
	}
	for _, id := range ids {
		merged := map[string]any{"id": id}
		for k, v := range olds[id] {
			merged[k] = v
		}
		for k, v := range newVals {
			merged[k] = v
		}
		h.resolveCustomer(ctx, def, merged)
		rec := Record{EntityType: def.EntityType, EntityID: id, Op: OpUpdate, Fields: merged, Label: label(def, merged)}
		if oldErr != nil {
			rec.Degraded = "diff_error"
			rec.Changes = map[string]Change{}
			h.emit(ctx, rec)
			continue
		}
		changes, changed := Diff(def, olds[id], newVals)
		if !changed {
			continue
		}
		rec.Changes = changes
		if st, ok := changes["status"]; ok && fmt.Sprint(st.To) == string(types.StatusDeleted) {
			rec.Op = OpDelete
		}
		h.emit(ctx, rec)
	}
	return v, nil
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func (h *hook) delete(ctx context.Context, next ent.Mutator, m mutation, def Definition) (ent.Value, error) {
	ids, _ := m.IDs(ctx)
	v, err := next.Mutate(ctx, m)
	if err != nil {
		return v, err
	}
	for _, id := range ids {
		h.emit(ctx, Record{EntityType: def.EntityType, EntityID: id, Op: OpDelete, Changes: map[string]Change{}})
	}
	return v, nil
}

// selectOld reads the columns about to change, inside the mutation's transaction.
func (h *hook) selectOld(ctx context.Context, def Definition, ids []string, cols []string) (map[string]map[string]any, error) {
	q, ok := ctx.Value(ctxQuerier).(Querier)
	if !ok {
		return nil, fmt.Errorf("no querier in context")
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pq.QuoteIdentifier(c)
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT id, %s FROM %s WHERE id = ANY($1)`,
		strings.Join(quoted, ", "), pq.QuoteIdentifier(def.Table)), pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var id string
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols)+1)
		ptrs[0] = &id
		for i := range vals {
			ptrs[i+1] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		out[id] = row
	}
	return out, rows.Err()
}

// emit appends to the transaction collector, or writes directly through the
// mutation's own client when the mutation runs outside WithTx. A direct-write
// failure is logged and never fails the mutation.
func (h *hook) emit(ctx context.Context, rec Record) {
	if c := CollectorFrom(ctx); c != nil {
		c.Add(rec)
		return
	}
	exec, ok := ctx.Value(ctxQuerier).(Execer)
	if !ok {
		return
	}
	tmp := &Collector{pending: map[key]*Pending{}}
	tmp.Add(rec)
	if err := flush(ctx, exec, h.reg, h.log, tmp.Entries()); err != nil && h.log != nil {
		h.log.Error(ctx, "activity direct write failed", "error", err, "entity_id", rec.EntityID)
	}
}
