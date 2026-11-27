package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
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
		{"per-key error", []string{"a", "b"}, nil, []string{"a"}, []bool{true, true}, []bool{false, false}, true, 1},
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

func newMiniredisLocker(t *testing.T) (*redisLocker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &redisLocker{client: client}, mr
}

func TestAcquireScriptRetryIdempotent(t *testing.T) {
	locker, mr := newMiniredisLocker(t)
	ctx := context.Background()
	run := func(token string) bool {
		ok, err := acquireScript.Run(ctx, locker.client, []string{"k"}, token, 60000).Bool()
		require.NoError(t, err)
		return ok
	}

	require.True(t, run("tok-a"))
	require.True(t, run("tok-a"), "retry with same token")
	require.False(t, run("tok-b"), "different token")
	got, err := mr.Get("k")
	require.NoError(t, err)
	require.Equal(t, "tok-a", got)
	require.Positive(t, mr.TTL("k"))
}

// replayHook re-sends each pipeline after it applied.
type replayHook struct{ sends int }

func (h *replayHook) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (h *replayHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *replayHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if cmds[0].Name() != "eval" {
			return next(ctx, cmds)
		}
		h.sends++
		if err := next(ctx, cmds); err != nil {
			return err
		}
		return next(ctx, cmds)
	}
}

func TestAcquireLocksTransportRetryRealScript(t *testing.T) {
	locker, mr := newMiniredisLocker(t)
	hook := &replayHook{}
	locker.client.AddHook(hook)

	locks, err := locker.AcquireLocks(context.Background(), []string{"a", "b", "a"}, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, hook.sends)
	require.Len(t, locks, 3)
	require.True(t, locks[0].AcquiredSuccessfully())
	require.True(t, locks[1].AcquiredSuccessfully())
	require.False(t, locks[2].AcquiredSuccessfully(), "duplicate key, other token")

	require.NoError(t, locks[0].Release(context.Background()))
	require.False(t, mr.Exists("a"))
	require.True(t, mr.Exists("b"))
}

// failReadHook fails the pipeline after Redis applied it.
type failReadHook struct{ replayHook }

func (h *failReadHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if err := next(ctx, cmds); err != nil {
			return err
		}
		err := errors.New("reply lost")
		for _, c := range cmds {
			c.SetErr(err)
		}
		return err
	}
}

func TestAcquireLocksKeepsTokensOnReplyReadFailure(t *testing.T) {
	locker, mr := newMiniredisLocker(t)
	locker.client.AddHook(&failReadHook{})

	locks, err := locker.AcquireLocks(context.Background(), []string{"a", "b"}, time.Minute)
	require.Error(t, err)
	require.Len(t, locks, 2)
	require.True(t, mr.Exists("a"))
	require.True(t, mr.Exists("b"))
	for i, l := range locks {
		require.NotNil(t, l, "key %d", i)
		require.NoError(t, l.Release(context.Background()))
	}
	require.False(t, mr.Exists("a"))
	require.False(t, mr.Exists("b"))

	mr.Set("c", "other-owner")
	locks, err = locker.AcquireLocks(context.Background(), []string{"c"}, time.Minute)
	require.Error(t, err)
	require.NoError(t, locks[0].Release(context.Background()))
	require.True(t, mr.Exists("c"), "foreign lock untouched")
}

// dropReplyProxy applies the first EVAL upstream, then drops the connection.
func dropReplyProxy(t *testing.T, upstream string) (addr string, drops, evals *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	drops, evals = &atomic.Int32{}, &atomic.Int32{}
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = client.Close()
				continue
			}
			var muted atomic.Bool
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := up.Read(buf)
					if err != nil {
						return
					}
					if !muted.Load() {
						_, _ = client.Write(buf[:n])
					}
				}
			}()
			go func() {
				defer client.Close()
				defer up.Close()
				buf := make([]byte, 65536)
				for {
					n, err := client.Read(buf)
					if err != nil {
						return
					}
					isEval := bytes.Contains(bytes.ToUpper(buf[:n]), []byte("EVAL"))
					drop := isEval && drops.CompareAndSwap(0, 1)
					// Mute before forwarding.
					if drop {
						muted.Store(true)
					}
					if isEval {
						evals.Add(1)
					}
					_, _ = up.Write(buf[:n])
					if drop {
						time.Sleep(100 * time.Millisecond)
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String(), drops, evals
}

func TestAcquireLocksLostReplyRealRetry(t *testing.T) {
	mr := miniredis.RunT(t)
	addr, drops, evals := dropReplyProxy(t, mr.Addr())
	client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: 3, MinRetryBackoff: time.Millisecond, MaxRetryBackoff: 5 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })
	locker := &redisLocker{client: client}

	locks, err := locker.AcquireLocks(context.Background(), []string{"a", "b"}, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, drops.Load(), "reply was dropped once")
	require.GreaterOrEqual(t, evals.Load(), int32(2), "retry reached upstream")
	for i, l := range locks {
		require.NotNil(t, l, "key %d", i)
		require.True(t, l.AcquiredSuccessfully(), "key %d", i)
	}
	require.True(t, mr.Exists("a"))
	require.True(t, mr.Exists("b"))
}

func TestReleaseLocksPipelinedTokenChecked(t *testing.T) {
	mr := miniredis.RunT(t)
	locker := &redisLocker{client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	locks, err := locker.AcquireLocks(context.Background(), []string{"a", "b"}, time.Minute)
	require.NoError(t, err)
	foreign := &redisLock{client: locker.client, key: "c", token: "not-ours", success: true}
	mr.Set("c", "other-owner")

	require.NoError(t, ReleaseLocks(context.Background(), append(locks, nil, foreign)))
	require.False(t, mr.Exists("a"))
	require.False(t, mr.Exists("b"))
	require.True(t, mr.Exists("c"), "foreign lock untouched")
}

func TestReleaseLocksOneDeadlineDuringOutage(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	prev := releaseTimeout
	releaseTimeout = 200 * time.Millisecond
	t.Cleanup(func() { releaseTimeout = prev })

	client := redis.NewClient(&redis.Options{Addr: ln.Addr().String(), MaxRetries: -1, ReadTimeout: time.Minute})
	t.Cleanup(func() { _ = client.Close() })
	locks := make([]Lock, 50)
	for i := range locks {
		locks[i] = &redisLock{client: client, key: fmt.Sprintf("k%d", i), token: "t", success: true}
	}

	start := time.Now()
	require.Error(t, ReleaseLocks(context.Background(), locks))
	require.Less(t, time.Since(start), 2*time.Second, "one deadline, not one per lock")
}
