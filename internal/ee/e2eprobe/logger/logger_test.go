package logger

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestLogger_KeepsRecentLines(t *testing.T) {
	lg := newWith(io.Discard, slog.LevelInfo, 2)
	ctx := context.Background()
	lg.Info(ctx, "one")
	lg.Info(ctx, "two", "k", "v")
	lg.Debug(ctx, "below the level")
	lg.Error(ctx, "three", "error", errors.New("boom"))
	got := lg.Recent()
	if len(got) != 2 || !strings.Contains(got[0], `"msg":"two"`) || !strings.Contains(got[1], `"msg":"three"`) || !strings.Contains(got[1], `"error":"boom"`) {
		t.Fatalf("recent lines = %q, want the last two emitted lines", got)
	}
	if Noop().Recent() != nil || (*Logger)(nil).Recent() != nil {
		t.Fatal("a noop or nil logger remembers nothing")
	}
}
