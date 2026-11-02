package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/ent"
	entcustomer "github.com/flexprice/flexprice/ent/customer"
)

func TestTruncateTimeFields_CutsNanosecondsOnWrite(t *testing.T) {
	ns := time.Date(2026, 1, 10, 12, 0, 0, 123456789, time.UTC)
	whole := ns.Truncate(time.Microsecond)

	m := ent.NewClient().Customer.Create().
		SetCreatedAt(ns).
		SetUpdatedAt(whole).
		Mutation()

	var created, updated time.Time
	next := ent.MutateFunc(func(_ context.Context, m ent.Mutation) (ent.Value, error) {
		v, ok := m.Field(entcustomer.FieldCreatedAt)
		require.True(t, ok)
		created = v.(time.Time)
		v, ok = m.Field(entcustomer.FieldUpdatedAt)
		require.True(t, ok)
		updated = v.(time.Time)
		return nil, nil
	})

	_, err := truncateTimeFields(next).Mutate(context.Background(), m)
	require.NoError(t, err)
	require.Equal(t, whole, created, "nanosecond field must be stored cut to microseconds")
	require.Equal(t, whole, updated, "already-whole field must pass through unchanged")
}
