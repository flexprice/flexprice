package ent

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

func TestActivityCursorRoundTrip(t *testing.T) {
	in := types.ActivityCursor{OccurredAt: time.Date(2026, 10, 3, 1, 2, 3, 4000, time.UTC), ID: "act_1"}
	enc := encodeActivityCursor(in)
	out, err := decodeActivityCursor(enc)
	if err != nil || !out.OccurredAt.Equal(in.OccurredAt) || out.ID != in.ID {
		t.Fatalf("round trip failed: %+v %v", out, err)
	}
	if _, err := decodeActivityCursor("not-base64!"); err == nil {
		t.Fatal("garbage cursor must error")
	}
}
