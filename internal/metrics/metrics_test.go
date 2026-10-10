package metrics_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/metrics/metricstest"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

const testName = metrics.CheckoutSessions

func ctxFor(tenantID, environmentID string) context.Context {
	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}
	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}
	return ctx
}

func TestUnknownNameIgnored(t *testing.T) {
	r := metricstest.Install(t)
	metrics.RecordCounter(context.Background(), "not.in.catalog", 1)
	assert.Empty(t, r.Series("not.in.catalog"))
}

func TestAddLabels(t *testing.T) {
	r := metricstest.Install(t)
	provider := metrics.L(metrics.KeyProvider, "labels-test")

	metrics.RecordCounter(ctxFor("t1", "e1"), testName, 2, provider)
	metrics.RecordCounter(context.Background(), testName, 1, provider)

	assert.Equal(t, int64(2), r.Sum(string(testName), map[string]string{"provider": "labels-test", "tenant_id": "t1", "environment_id": "e1"}))
	assert.Equal(t, int64(3), r.Sum(string(testName), map[string]string{"provider": "labels-test"}))
}

func TestTenantAllowlist(t *testing.T) {
	r := metricstest.Install(t)
	metrics.SetTenantAllowlist([]string{"a1:e1", "a2"})
	t.Cleanup(func() { metrics.SetTenantAllowlist(nil) })

	tests := []struct {
		name string
		ctx  context.Context
		want int64
	}{
		{"exact pair allowed", ctxFor("a1", "e1"), 1},
		{"other env blocked", ctxFor("a1", "e2"), 0},
		{"whole tenant allowed", ctxFor("a2", "e9"), 1},
		{"unlisted tenant blocked", ctxFor("a3", "e1"), 0},
		{"no tenant blocked", ctxFor("", ""), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.RecordCounter(tt.ctx, testName, 1, metrics.L(metrics.KeyProvider, "allowlist-"+tt.name))
			assert.Equal(t, tt.want, r.Sum(string(testName), map[string]string{"provider": "allowlist-" + tt.name}))
		})
	}
}

func TestCallerCannotOverrideTenantLabels(t *testing.T) {
	r := metricstest.Install(t)
	metrics.RecordCounter(ctxFor("real_tenant", "real_env"), testName, 1,
		metrics.L(metrics.KeyTenantID, "spoofed"), metrics.L(metrics.KeyEnvironmentID, "spoofed"), metrics.L(metrics.KeyProvider, "override-test"))

	assert.Equal(t, int64(1), r.Sum(string(testName), map[string]string{"provider": "override-test", "tenant_id": "real_tenant", "environment_id": "real_env"}))
}

func TestRecordedOnlyAfterCommit(t *testing.T) {
	r := metricstest.Install(t)
	committed := types.WithPostCommitHooks(context.Background())
	rolledBack := types.WithPostCommitHooks(context.Background())

	metrics.RecordCounter(committed, testName, 1, metrics.L(metrics.KeyProvider, "tx-commit"))
	metrics.RecordCounter(rolledBack, testName, 1, metrics.L(metrics.KeyProvider, "tx-rollback"))
	assert.Equal(t, int64(0), r.Sum(string(testName), map[string]string{"provider": "tx-commit"}), "not recorded before commit")

	types.RunPostCommitHooks(committed)
	types.DiscardPostCommitHooks(rolledBack)
	assert.Equal(t, int64(1), r.Sum(string(testName), map[string]string{"provider": "tx-commit"}))
	assert.Equal(t, int64(0), r.Sum(string(testName), map[string]string{"provider": "tx-rollback"}))
}
