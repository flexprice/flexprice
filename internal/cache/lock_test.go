package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeLock struct {
	acquired bool
}

func (l fakeLock) AcquiredSuccessfully() bool      { return l.acquired }
func (l fakeLock) Release(_ context.Context) error { return nil }

type fakeLocker struct {
	// acquiredOnCall is the attempt at which the lock becomes free; 0 means never.
	acquiredOnCall int
	err            error
	calls          int
}

func (l *fakeLocker) AcquireLock(_ context.Context, _ string, _ time.Duration) (Lock, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return fakeLock{acquired: l.acquiredOnCall > 0 && l.calls >= l.acquiredOnCall}, nil
}

func TestAcquireLockWithRetry(t *testing.T) {
	tests := []struct {
		name           string
		locker         *fakeLocker
		attempts       int
		wantAcquired   bool
		wantNilLock    bool
		wantErr        bool
		wantLockerCall int
	}{
		{
			name:           "free on the first attempt",
			locker:         &fakeLocker{acquiredOnCall: 1},
			attempts:       3,
			wantAcquired:   true,
			wantLockerCall: 1,
		},
		{
			name:           "held then released is retried",
			locker:         &fakeLocker{acquiredOnCall: 3},
			attempts:       3,
			wantAcquired:   true,
			wantLockerCall: 3,
		},
		{
			name:           "never released exhausts the attempts",
			locker:         &fakeLocker{},
			attempts:       3,
			wantLockerCall: 3,
		},
		{
			name:           "locker failure returns immediately",
			locker:         &fakeLocker{err: errors.New("redis down")},
			attempts:       3,
			wantNilLock:    true,
			wantErr:        true,
			wantLockerCall: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock, err := AcquireLockWithRetry(
				context.Background(), tt.locker, "key", time.Second, tt.attempts, time.Millisecond,
			)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.wantNilLock {
				require.Nil(t, lock)
			} else {
				require.NotNil(t, lock)
				require.Equal(t, tt.wantAcquired, lock.AcquiredSuccessfully())
			}
			require.Equal(t, tt.wantLockerCall, tt.locker.calls)
		})
	}
}

func TestAcquireLockWithRetryWithNilLocker(t *testing.T) {
	lock, err := AcquireLockWithRetry(
		context.Background(), nil, "key", time.Second, 3, time.Millisecond,
	)

	require.NoError(t, err)
	require.Nil(t, lock)
}

func TestAcquireLockWithRetryStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	locker := &fakeLocker{}
	lock, err := AcquireLockWithRetry(ctx, locker, "key", time.Second, 3, time.Minute)

	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, lock)
	require.False(t, lock.AcquiredSuccessfully())
	require.Equal(t, 1, locker.calls)
}
