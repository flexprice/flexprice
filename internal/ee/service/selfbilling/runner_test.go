package selfbilling

import (
	"context"
	"errors"
	"testing"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingCounts struct{}

func (failingCounts) CountByTenantHour(context.Context, time.Time, time.Time) ([]*domainsb.TenantEnvHourCount, error) {
	return nil, errors.New("clickhouse down")
}

func TestRunHourly_SyncRunsEvenIfPushFails(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "paid", types.TenantInternalStatusTrialing)
	fc.subs["us:paid"] = &remoteSubscription{id: "s1", status: "active"}
	s.events = failingCounts{}

	require.Error(t, s.RunHourly(context.Background()))
	got, _ := d.tenants.GetByID(context.Background(), "paid")
	assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus)
}
