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

// pipelineStub emulates acquireScript semantics on EVAL commands.
type pipelineStub struct {
	held      map[string]string
	failKeys  map[string]bool
	pipelines int
	// replays re-send each pipeline once after it applied.
	replays bool
	tokens  map[string][]string
}

func (p *pipelineStub) DialHook(next redis.DialHook) redis.DialHook { return next }

func (p *pipelineStub) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }

func (p *pipelineStub) apply(cmds []redis.Cmder) error {
	var first error
	for _, c := range cmds {
		args := c.Args()
		key, token := args[3].(string), args[4].(string)
		p.tokens[key] = append(p.tokens[key], token)
		cmd := c.(*redis.Cmd)
		switch {
		case p.failKeys[key]:
			cmd.SetErr(errors.New("boom"))
			if first == nil {
				first = cmd.Err()
			}
		case p.held[key] == "":
			p.held[key] = token
			cmd.SetVal(int64(1))
		case p.held[key] == token:
			cmd.SetVal(int64(1))
		default:
			cmd.SetVal(int64(0))
		}
	}
	return first
}

func (p *pipelineStub) ProcessPipelineHook(_ redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(_ context.Context, cmds []redis.Cmder) error {
		p.pipelines++
		err := p.apply(cmds)
		if p.replays {
			err = p.apply(cmds)
		}
		return err
	}
}

func newPipelineStub() *pipelineStub {
	return &pipelineStub{held: map[string]string{}, failKeys: map[string]bool{}, tokens: map[string][]string{}}
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
			stub := newPipelineStub()
			for _, k := range tt.held {
				stub.held[k] = "other-token"
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
	stub := newPipelineStub()
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
	stub := newPipelineStub()
	locks, err := AcquireLocks(context.Background(), newStubLocker(stub), []string{"a", "b"}, time.Minute)
	require.NoError(t, err)
	require.Len(t, locks, 2)
	require.Equal(t, 1, stub.pipelines)
}

func TestRedisLockerAcquireLocksRetryKeepsTokens(t *testing.T) {
	stub := newPipelineStub()
	stub.replays = true
	locks, err := newStubLocker(stub).AcquireLocks(context.Background(), []string{"a", "b"}, time.Minute)
	require.NoError(t, err)
	for i, l := range locks {
		require.NotNil(t, l, "key %d", i)
		require.True(t, l.AcquiredSuccessfully(), "key %d", i)
	}
	for key, sent := range stub.tokens {
		require.Len(t, sent, 2)
		require.Equal(t, sent[0], sent[1], key)
	}
}

func TestRedisLockerAcquireLocksDuplicateKeysDistinctTokens(t *testing.T) {
	stub := newPipelineStub()
	_, err := newStubLocker(stub).AcquireLocks(context.Background(), []string{"a", "a"}, time.Minute)
	require.NoError(t, err)
	require.Len(t, stub.tokens["a"], 2)
	require.NotEqual(t, stub.tokens["a"][0], stub.tokens["a"][1])
}

func TestRedisLockerAcquireLocksHeldByOtherToken(t *testing.T) {
	stub := newPipelineStub()
	stub.held["a"] = "other-token"
	locks, err := newStubLocker(stub).AcquireLocks(context.Background(), []string{"a"}, time.Minute)
	require.NoError(t, err)
	require.False(t, locks[0].AcquiredSuccessfully())
}

type singleStub struct {
	pipelineStub
	sent []string
}

func (s *singleStub) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		s.sent = append(s.sent, cmd.Args()[4].(string))
		return s.apply([]redis.Cmder{cmd})
	}
}

func TestRedisLockerAcquireLockTokenMatch(t *testing.T) {
	stub := &singleStub{pipelineStub: *newPipelineStub()}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(stub)
	locker := &redisLocker{client: client}
	ctx := context.Background()

	lock, err := locker.AcquireLock(ctx, "k", time.Minute)
	require.NoError(t, err)
	require.True(t, lock.AcquiredSuccessfully())

	other, err := locker.AcquireLock(ctx, "k", time.Minute)
	require.NoError(t, err)
	require.False(t, other.AcquiredSuccessfully())

	replayed := redis.NewCmd(ctx, "evalsha", "sha", 1, "k", stub.sent[0], 1)
	require.NoError(t, stub.apply([]redis.Cmder{replayed}))
	v, err := replayed.Int64()
	require.NoError(t, err)
	require.Equal(t, int64(1), v)
}
