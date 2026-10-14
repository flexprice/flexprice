package testutil

import (
	"context"
	"testing"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemorySelfBillingStore_WindowIsHalfOpen(t *testing.T) {
	s := NewInMemorySelfBillingStore()
	h := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	s.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: h, Count: 1})
	s.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: h.Add(time.Hour), Count: 1})
	s.AddInvoice(&domainsb.FinalizedInvoice{ID: "i1", TenantID: "t1", Total: decimal.NewFromInt(1), FinalizedAt: h})
	s.AddInvoice(&domainsb.FinalizedInvoice{ID: "i2", TenantID: "t1", Total: decimal.NewFromInt(1), FinalizedAt: h.Add(time.Hour)})

	counts, err := s.CountByTenantHour(context.Background(), h, h.Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, counts, 1)
	invs, err := s.ListFinalized(context.Background(), h, h.Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, invs, 1)
}
