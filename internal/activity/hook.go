package activity

import (
	"context"
	"encoding/json"
	"reflect"

	entgo "entgo.io/ent"
	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/types"
)

// Hook records an entry for every captured write to a registered entity. It never fails a write.
func Hook(reg *Registry, sink Sink) entgo.Hook {
	return func(next entgo.Mutator) entgo.Mutator {
		return entgo.MutateFunc(func(ctx context.Context, m entgo.Mutation) (entgo.Value, error) {
			r, ok := reg.ByEntName(m.Type())
			if !ok {
				return next.Mutate(ctx, m)
			}
			v, err := next.Mutate(ctx, m)
			if err != nil {
				return v, err
			}
			rows, counted := rowsOf(v)
			if counted {
				metrics.RecordCounter(ctx, metrics.ActivityUncaptured, 1, metrics.L(metrics.KeyEntity, string(r.EntityType)))
				return v, nil
			}
			for _, row := range rows {
				buildEntry(ctx, reg, sink, r, m.Op(), row)
			}
			return v, nil
		})
	}
}

// rowsOf extracts written rows from a mutation result; counted is true when only a row count came back.
func rowsOf(v entgo.Value) (rows []any, counted bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int:
		return nil, true
	case reflect.Ptr:
		if !rv.IsNil() {
			return []any{v}, false
		}
	case reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			rows = append(rows, rv.Index(i).Interface())
		}
	}
	return rows, false
}

// buildEntry snapshots one written row and queues it; any failure skips the entry and is counted.
func buildEntry(ctx context.Context, reg *Registry, sink Sink, r *Registration, op entgo.Op, row any) {
	defer func() {
		if recover() != nil {
			metrics.RecordCounter(ctx, metrics.ActivityCaptureFailed, 1, metrics.L(metrics.KeyEntity, string(r.EntityType)))
		}
	}()
	entry, ok := newEntry(ctx, r, op, row)
	if !ok {
		metrics.RecordCounter(ctx, metrics.ActivityCaptureFailed, 1, metrics.L(metrics.KeyEntity, string(r.EntityType)))
		return
	}
	add(ctx, reg, sink, entry)
}

func newEntry(ctx context.Context, r *Registration, op entgo.Op, row any) (Entry, bool) {
	fields, ok := r.Snapshot.state(row)
	if !ok {
		return Entry{}, false
	}
	entityID, tenantID, envID := stringField(fields, "id"), stringField(fields, "tenant_id"), stringField(fields, "environment_id")
	if tenantID == "" {
		tenantID = types.GetTenantID(ctx)
	}
	if envID == "" {
		envID = types.GetEnvironmentID(ctx)
	}
	if entityID == "" || (tenantID == "" && envID == "") {
		return Entry{}, false
	}
	state, err := json.Marshal(fields)
	if err != nil {
		return Entry{}, false
	}

	actor, _, hasActor := types.GetActor(ctx)
	if !hasActor {
		actor = types.Actor{Type: types.ActorTypeSystem, ID: "unknown"}
		metrics.RecordCounter(ctx, metrics.ActivityUnknownActor, 1)
	}
	return Entry{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ACTIVITY), EntityType: r.EntityType, EntityID: entityID,
		Action: resolveAction(ctx, r, op, entityID), Actor: actor, State: state, TenantID: tenantID, EnvironmentID: envID,
	}, true
}

// resolveAction is the entry's action: the declared action for the entity, else created, updated or deleted by the write.
func resolveAction(ctx context.Context, r *Registration, op entgo.Op, entityID string) Action {
	if declared, ok := declaredAction(ctx, r, entityID); ok {
		return declared
	}
	switch {
	case op.Is(entgo.OpCreate):
		return r.action("created")
	case op.Is(entgo.OpDelete | entgo.OpDeleteOne):
		return r.action("deleted")
	}
	return r.action("updated")
}
