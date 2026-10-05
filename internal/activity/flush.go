package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Execer is the write side of the mutation's connection.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ErrEmptyActor refuses a flush whose context carries no actor.
var ErrEmptyActor = errors.New("activity: actor not set in context")

const insertColumns = `id, tenant_id, environment_id, category, entity_type, entity_id, entity_label, action, actor_type, actor_id, actor_label, actor_user_id, source, customer_id, subscription_id, request_id, outcome, error_code, changes, snapshot, metadata, occurred_at`

const (
	columnsPerRow = 22
	maxLabelRunes = 255 // entity_label and actor_label are VARCHAR(255)
)

// Flush writes every pending entry of the context's collector in one insert.
func Flush(ctx context.Context, exec Execer, reg *Registry, log *logger.Logger) error {
	c := CollectorFrom(ctx)
	if c == nil {
		return nil
	}
	defer c.Close()
	return flush(ctx, exec, reg, log, c.Entries())
}

func flush(ctx context.Context, exec Execer, reg *Registry, log *logger.Logger, entries []Pending) error {
	if len(entries) == 0 {
		return nil
	}
	ctxActor := types.GetActor(ctx)
	// An entry keeps the actor captured when it was written, so a system write derived
	// inside a user's request is not flushed as that user.
	actors := make([]types.Actor, len(entries))
	for i, p := range entries {
		actors[i] = p.Actor
		if actors[i].Type == "" {
			actors[i] = ctxActor
		}
		if actors[i].Type == "" {
			if log != nil {
				log.Error(ctx, "activity flush refused: empty actor", "error", ErrEmptyActor, "request_id", types.GetRequestID(ctx))
			}
			return ErrEmptyActor
		}
	}
	source := types.GetSource(ctx)
	if source == "" {
		source = types.SourceAPI
	}
	now := time.Now().UTC()
	var sb strings.Builder
	sb.WriteString("INSERT INTO activity_logs (" + insertColumns + ") VALUES ")
	args := make([]any, 0, len(entries)*columnsPerRow)
	for i, p := range entries {
		actor := actors[i]
		def, _ := reg.ByEntityType(p.EntityType)
		var customerID, subscriptionID any // nil or string
		if def.CustomerID != nil && p.Fields != nil {
			if id := def.CustomerID(p.Fields); id != "" {
				customerID = id
			}
		}
		if id, _ := p.Fields["subscription_id"].(string); id != "" {
			subscriptionID = id
		} else if p.EntityType == types.SystemEntityTypeSubscription {
			subscriptionID = p.EntityID
		}
		// An entry whose op is still unset is a pure semantic action from
		// RecordAction: it keeps its own action and never carries a snapshot.
		action := p.Action
		if action == "" {
			action = fmt.Sprintf("%s.%s", p.EntityType, verb(p.Op))
		}
		meta := p.Metadata
		if p.Degraded != "" {
			merged := make(map[string]any, len(meta)+1)
			for k, v := range meta {
				merged[k] = v
			}
			merged["degraded"] = p.Degraded
			meta = merged
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * columnsPerRow
		sb.WriteString("(")
		for j := 1; j <= columnsPerRow; j++ {
			if j > 1 {
				sb.WriteString(", ")
			}
			sb.WriteString("$" + strconv.Itoa(base+j))
		}
		sb.WriteString(")")
		args = append(args,
			types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ACTIVITY_LOG),
			types.GetTenantID(ctx), types.GetEnvironmentID(ctx), "business",
			string(p.EntityType), p.EntityID, truncateRunes(p.Label, maxLabelRunes), action,
			string(actor.Type), actor.ID, truncateRunes(actor.Label, maxLabelRunes), nilIfEmpty(actor.UserID),
			string(source), customerID, subscriptionID, nilIfEmpty(types.GetRequestID(ctx)),
			"success", nil,
			jsonOrNil(p.Changes, p.Op == OpUpdate || len(p.Changes) > 0),
			jsonOrNil(p.Snapshot, p.Op == OpCreate),
			jsonOrNil(meta, meta != nil),
			now,
		)
	}
	_, err := exec.ExecContext(ctx, sb.String(), args...)
	return err
}

func verb(op Op) string {
	switch op {
	case OpCreate:
		return "created"
	case OpDelete:
		return "deleted"
	}
	return "updated"
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// jsonOrNil encodes v for a JSONB column; a nil map is SQL NULL, not 'null'.
func jsonOrNil(v any, include bool) any {
	if !include || v == nil {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		if x == nil {
			return nil
		}
	case map[string]Change:
		if x == nil {
			return nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}
