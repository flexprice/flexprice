package activity

import (
	"context"
	"encoding/json"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Record queues a declared domain action with its explicit snapshot on the current transaction.
func Record(ctx context.Context, reg *Registry, sink Sink, action Action, entityID string, state map[string]any) error {
	if ctx == nil || state == nil || reg == nil || sink == nil || entityID == "" || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return ierr.Validation("activity requires a scoped entity and configured recorder")
	}
	entity, _, _ := strings.Cut(string(action), ".")
	registration, ok := reg.ByType(types.SystemEntityType(entity))
	if !ok || !reg.IsAction(action) {
		return ierr.Validation("activity action is not registered")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return ierr.WithError(err).WithHint("Invalid activity snapshot").Mark(ierr.ErrValidation)
	}
	if _, ok := types.TxStoreLoadOrStore(ctx, "activity:explicit", func() any { return true }); !ok || !types.RegisterPostCommit(ctx, func() {}) {
		return ierr.NewError("explicit activity requires an active transaction").Mark(ierr.ErrInvalidOperation)
	}
	actor, _, present := types.GetActor(ctx)
	if !present {
		actor = types.Actor{Type: types.ActorTypeSystem, ID: "unknown"}
	}
	add(ctx, reg, sink, Entry{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ACTIVITY), EntityType: registration.EntityType, EntityID: entityID,
		Action: action, Actor: actor, State: data, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
	})
	return nil
}
