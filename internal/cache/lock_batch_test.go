package cache

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type pipelineStub struct {
	held      map[string]bool
	failKeys  map[string]bool
	pipelines int
}

func (p *pipelineStub) DialHook(next redis.DialHook) redis.DialHook { return next }

func (p *pipelineStub) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }

func (p *pipelineStub) ProcessPipelineHook(_ redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(_ context.Context, cmds []redis.Cmder) error {
		p.pipelines++
		var first error
		for _, c := range cmds {
			key := c.Args()[1].(string)
			cmd := c.(*redis.BoolCmd)
			switch {
			case p.failKeys[key]:
				cmd.SetErr(errors.New("boom"))
				if first == nil {
					first = cmd.Err()
				}
			case p.held[key]:
				cmd.SetVal(false)
			default:
				p.held[key] = true
				cmd.SetVal(true)
			}
		}
		return first
	}
}

func newStubLocker(stub *pipelineStub) *redisLocker {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(stub)
	return &redisLocker{client: client}
}

func TestRedisLockerAcquireLocks(t *testing.T) {
	tests := []struct {
		name         string
		keys         []string
		held         []string
		fail         []string
		wantAcquired []bool
		wantNil      []bool
		wantErr      bool
		wantPipes    int
	}{
		{"empty", nil, nil, nil, nil, nil, false, 0},
		{"all free", []string{"a", "b"}, nil, nil, []bool{true, true}, []bool{false, false}, false, 1},
		{"one held", []string{"a", "b", "c"}, []string{"b"}, nil, []bool{true, false, true}, []bool{false, false, false}, false, 1},
		{"in-batch duplicate", []string{"a", "a"}, nil, nil, []bool{true, false}, []bool{false, false}, false, 1},
		{"per-key error", []string{"a", "b"}, nil, []string{"a"}, []bool{false, true}, []bool{true, false}, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &pipelineStub{held: map[string]bool{}, failKeys: map[string]bool{}}
			for _, k := range tt.held {
				stub.held[k] = true
			}
			for _, k := range tt.fail {
				stub.failKeys[k] = true
			}
			locks, err := newStubLocker(stub).AcquireLocks(context.Background(), tt.keys, time.Minute)
			require.Equal(t, tt.wantErr, err != nil)
			require.Len(t, locks, len(tt.keys))
			for i := range tt.keys {
				if tt.wantNil[i] {
					require.Nil(t, locks[i])
					continue
				}
				require.NotNil(t, locks[i])
				require.Equal(t, tt.wantAcquired[i], locks[i].AcquiredSuccessfully())
			}
			require.Equal(t, tt.wantPipes, stub.pipelines)
		})
	}
}

func TestRedisLockerAcquireLocksChunksPipelines(t *testing.T) {
	stub := &pipelineStub{held: map[string]bool{}, failKeys: map[string]bool{}}
	keys := make([]string, 0, 2*batchLockChunk+5)
	for i := 0; i < 2*batchLockChunk+5; i++ {
		keys = append(keys, fmt.Sprintf("k%d", i))
	}
	locks, err := newStubLocker(stub).AcquireLocks(context.Background(), keys, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 3, stub.pipelines)
	for i, l := range locks {
		require.NotNil(t, l, "key %d", i)
		require.True(t, l.AcquiredSuccessfully())
	}
}

func TestAcquireLocksUsesBatchLocker(t *testing.T) {
	stub := &pipelineStub{held: map[string]bool{}, failKeys: map[string]bool{}}
	locks, err := AcquireLocks(context.Background(), newStubLocker(stub), []string{"a", "b"}, time.Minute)
	require.NoError(t, err)
	require.Len(t, locks, 2)
	require.Equal(t, 1, stub.pipelines)
}
