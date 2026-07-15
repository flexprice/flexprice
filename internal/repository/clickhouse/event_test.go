package clickhouse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/stretchr/testify/require"
)

type eventTestStore struct {
	conn driver.Conn
}

func (s *eventTestStore) GetConn() driver.Conn {
	return s.conn
}

type eventTestConn struct {
	driver.Conn
	asyncInsertCalls  int
	prepareBatchCalls int
	wait              bool
	err               error
}

func (c *eventTestConn) AsyncInsert(_ context.Context, _ string, wait bool, _ ...any) error {
	c.asyncInsertCalls++
	c.wait = wait
	return c.err
}

func (c *eventTestConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	c.prepareBatchCalls++
	return nil, errors.New("unexpected batch insert")
}

func TestBulkInsertEventsUsesWaitingAsyncInsertForSingleton(t *testing.T) {
	insertErr := errors.New("flush failed")
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "flush error", err: insertErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &eventTestConn{err: test.err}
			repo := &EventRepository{store: &eventTestStore{conn: conn}}
			event := events.NewEvent("api_call", "tenant-1", "customer-1", nil, time.Now(), "event-1", "", "test", "environment-1")

			err := repo.BulkInsertEvents(context.Background(), []*events.Event{event})

			if test.err == nil {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.err.Error())
			}
			require.Equal(t, 1, conn.asyncInsertCalls)
			require.True(t, conn.wait)
			require.Zero(t, conn.prepareBatchCalls)
		})
	}
}
