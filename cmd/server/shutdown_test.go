package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx/fxtest"
)

// freeAddr reserves a loopback port and releases it for the server to rebind.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func newTestServer(t *testing.T, shutdownTimeout time.Duration, h gin.HandlerFunc) (*fxtest.Lifecycle, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	addr := freeAddr(t)
	r := gin.New()
	r.GET("/slow", h)

	cfg := &config.Configuration{
		Server: config.ServerConfig{
			Address:         addr,
			ShutdownTimeout: shutdownTimeout,
		},
	}

	lc := fxtest.NewLifecycle(t)
	startAPIServer(lc, r, cfg, logger.NewNoopLogger())
	return lc, addr
}

// waitForServer blocks until the listener accepts, so requests are not sent early.
func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s never became reachable", addr)
}

// An in-flight request shorter than the drain window must finish, not be severed.
func TestAPIServerDrainsInFlightRequest(t *testing.T) {
	entered := make(chan struct{})
	lc, addr := newTestServer(t, 10*time.Second, func(c *gin.Context) {
		close(entered)
		time.Sleep(300 * time.Millisecond)
		c.String(http.StatusOK, "completed")
	})

	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForServer(t, addr)

	type result struct {
		status int
		body   string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://%s/slow", addr))
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		done <- result{status: resp.StatusCode, body: string(body), err: err}
	}()

	// Shutdown must not begin until the request is provably inside the handler.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}

	if err := lc.Stop(context.Background()); err != nil {
		t.Fatalf("expected clean drain, got %v", err)
	}

	got := <-done
	if got.err != nil {
		t.Fatalf("in-flight request was severed during drain: %v", got.err)
	}
	if got.status != http.StatusOK || got.body != "completed" {
		t.Fatalf("expected 200 completed, got %d %q", got.status, got.body)
	}
}

// A request outlasting the drain window must be cut so the hook cannot hang.
func TestAPIServerClosesConnectionsPastDrainDeadline(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	entered := make(chan struct{})
	lc, addr := newTestServer(t, 100*time.Millisecond, func(c *gin.Context) {
		close(entered)
		<-release
		c.String(http.StatusOK, "never delivered")
	})

	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForServer(t, addr)

	reqErr := make(chan error, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://%s/slow", addr))
		if err == nil {
			resp.Body.Close()
		}
		reqErr <- err
	}()

	// The handler must be blocking before the drain deadline starts.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- lc.Stop(context.Background()) }()

	select {
	case err := <-stopped:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected drain deadline to be exceeded, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop hook hung past the drain deadline instead of closing connections")
	}

	// srv.Close() must sever the stuck connection rather than leave it open.
	select {
	case err := <-reqErr:
		if err == nil {
			t.Fatal("expected the stuck request to be severed, but it succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stuck connection was not closed after the drain deadline")
	}
}

// The listener must be released so a replacement task can bind the same port.
func TestAPIServerReleasesListenerOnStop(t *testing.T) {
	lc, addr := newTestServer(t, time.Second, func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForServer(t, addr)

	if err := lc.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s still bound after stop: %v", addr, err)
	}
	l.Close()
}
