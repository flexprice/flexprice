package activity

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRecord_CommitAndRollback(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			sink := &recordingSink{}
			ctx := types.SetEnvironmentID(txCtx(), "env_1")
			require.NoError(t, Record(ctx, Registrations(), sink, CommitmentBillingTermsReplaced, "comm_1", map[string]any{"id": "comm_1", "customer_id": "customer_1", "reason": "Correct configured dates"}))
			require.Empty(t, sink.entries())
			if !commit {
				types.DiscardPostCommitHooks(ctx)
				require.Empty(t, sink.entries())
				return
			}
			types.RunPostCommitHooks(ctx)
			require.Len(t, sink.entries(), 1)
			entry := sink.entries()[0]
			require.Equal(t, CommitmentBillingTermsReplaced, entry.Action)
			require.Equal(t, "comm_1", entry.EntityID)
			require.Equal(t, types.GetTenantID(ctx), entry.TenantID)
			require.Equal(t, "env_1", entry.EnvironmentID)
			require.Equal(t, "user_1", entry.Actor.ID)
			var snapshot map[string]any
			require.NoError(t, json.Unmarshal(entry.State, &snapshot))
			require.Equal(t, "Correct configured dates", snapshot["reason"])
		})
	}
}

func TestRecord_RejectsUnscopedOrUncommittedActions(t *testing.T) {
	scoped := types.SetEnvironmentID(userCtx(), "env_1")
	for _, ctx := range []context.Context{nil, context.Background(), scoped} {
		require.Error(t, Record(ctx, Registrations(), &recordingSink{}, CommitmentBillingTermsReplaced, "comm_1", map[string]any{"reason": "correct"}))
	}
	ctx := types.SetEnvironmentID(txCtx(), "env_1")
	require.Error(t, Record(ctx, Registrations(), &recordingSink{}, "commitment.unknown", "comm_1", map[string]any{}))
	require.Error(t, Record(ctx, Registrations(), nil, CommitmentBillingTermsReplaced, "comm_1", map[string]any{}))
	require.Error(t, Record(ctx, Registrations(), &recordingSink{}, CommitmentBillingTermsReplaced, "comm_1", nil))
}
