package e2eprobe

import (
	"errors"
	"testing"

	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

func TestFail_EnrichesFromCheckError(t *testing.T) {
	inner := Errorf(map[string]string{"subscription_id": "sub_1", "step": "inner"}, "preview: %w", errors.New("boom"))
	r := Fail("preview", inner, map[string]string{"step": "outer"})

	if r.Status != StatusFail {
		t.Fatalf("status = %q, want fail", r.Status)
	}
	if r.Step != "preview" {
		t.Errorf("step = %q, want preview", r.Step)
	}
	if r.Attrs["subscription_id"] != "sub_1" {
		t.Errorf("attrs from the error chain lost: %v", r.Attrs)
	}
	if r.Attrs["step"] != "outer" {
		t.Errorf("caller attr must win, got %q", r.Attrs["step"])
	}
	if r.Reason != Brief(inner) {
		t.Errorf("reason = %q, want %q", r.Reason, Brief(inner))
	}
	if r.Err == nil || !r.Failed() {
		t.Error("Fail must keep the error and report Failed()")
	}
}

func TestFail_FlagsInfraOn5xx(t *testing.T) {
	infra := Fail("get", sdkerrors.NewAPIError("down", 503, "", nil), nil)
	if !infra.Infra {
		t.Error("503 must be flagged as infrastructure")
	}
	user := Fail("get", sdkerrors.NewAPIError("missing", 404, "", nil), nil)
	if user.Infra {
		t.Error("404 must not be flagged as infrastructure")
	}
	if user.Attrs == nil {
		t.Error("Fail must always return a non-nil attrs map")
	}
}

func TestMismatch_ReasonIsFirstLine(t *testing.T) {
	r := Mismatch("assert_usage", []string{"sum: want 30, got 20", "count: want 5, got 4"}, map[string]string{"persona": "taxed"})
	if r.Status != StatusFail || !r.Failed() {
		t.Fatalf("status = %q, want fail", r.Status)
	}
	if r.Reason != "sum: want 30, got 20" {
		t.Errorf("reason = %q", r.Reason)
	}
	if len(r.Mismatches) != 2 || r.Attrs["persona"] != "taxed" {
		t.Errorf("mismatches or attrs lost: %+v", r)
	}
}

func TestSummary_CountsExtraMismatches(t *testing.T) {
	cases := []struct {
		name string
		r    Result
		want string
	}{
		{"pass", Pass(), "pass"},
		{"skip", Skip("window", "partial hour"), "skip window: partial hour"},
		{"fail", Fail("get", errors.New("boom"), nil), "get: boom"},
		{"mismatch", Mismatch("assert", []string{"a: want 1, got 2", "b: want 3, got 4", "c: want 5, got 6"}, nil), "assert: a: want 1, got 2 (+2 more)"},
	}
	for _, tc := range cases {
		if got := tc.r.Summary(); got != tc.want {
			t.Errorf("%s: Summary() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSkip_IsNeitherPassNorFailed(t *testing.T) {
	r := Skip("persona", "not provisioned")
	if r.Status != StatusSkip || r.Failed() || r.Step != "persona" || r.Reason != "not provisioned" {
		t.Errorf("unexpected skip result: %+v", r)
	}
}

func TestTrait_Valid(t *testing.T) {
	for _, tr := range Traits() {
		if !tr.Valid() {
			t.Errorf("%q should be valid", tr)
		}
	}
	if Trait("nope").Valid() {
		t.Error("unknown trait must be invalid")
	}
	if len(Traits()) != 12 {
		t.Errorf("expected 12 traits, got %d", len(Traits()))
	}
}
