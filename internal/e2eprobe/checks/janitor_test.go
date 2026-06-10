package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
)

func TestJanitor_ArchivesOld(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	reg.RegisterEphemeral("customer", "old", time.Now().Add(-5*time.Hour))
	reg.RegisterEphemeral("customer", "fresh", time.Now().Add(-30*time.Minute))
	j := NewJanitor(fc, reg, 4*time.Hour, "run-1")
	if err := j.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := reg.Ephemerals("customer")
	if len(got) != 1 || got[0].ID != "fresh" {
		t.Errorf("after: %+v", got)
	}
}

func TestJanitor_NoOpOnEmpty(t *testing.T) {
	fc := newFakeClient()
	j := NewJanitor(fc, e2eprobe.NewRegistry(), time.Hour, "run-1")
	if err := j.Run(context.Background()); err != nil {
		t.Errorf("Run: %v", err)
	}
}
