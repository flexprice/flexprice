//go:build e2eprobe_integration

package billingmatrix

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

// TestLiveMatrix runs the whole matrix once against a live API:
//
//	E2EPROBE_API_HOST=http://localhost:8080/v1 E2EPROBE_API_KEY=sk_... \
//	BM_FAMILIES=opening,change BM_FILTER=quarterly BM_ASSERT_KNOWN_ISSUES=true \
//	go test -tags e2eprobe_integration ./internal/ee/e2eprobe/billingmatrix -run TestLiveMatrix -v -timeout 2h
func TestLiveMatrix(t *testing.T) {
	host, key := os.Getenv("E2EPROBE_API_HOST"), os.Getenv("E2EPROBE_API_KEY")
	if host == "" || key == "" {
		t.Skip("E2EPROBE_API_HOST and E2EPROBE_API_KEY must be set")
	}
	families := Families
	if v := os.Getenv("BM_FAMILIES"); v != "" {
		families = nil
		for _, f := range strings.Split(v, ",") {
			families = append(families, Family(strings.TrimSpace(f)))
		}
	}
	runID := fmt.Sprintf("bm-live-%d", time.Now().Unix())
	engine := NewEngine(e2eprobe.NewSDKClient(host, key).Raw(), runID, os.Getenv("BM_ASSERT_KNOWN_ISSUES") == "true")

	ctx := context.Background()
	for _, f := range families {
		results := engine.RunAll(ctx, f, os.Getenv("BM_FILTER"))
		passed, skipped := 0, 0
		for _, r := range results {
			for _, f := range r.CleanupFailures {
				t.Logf("CLEANUP %s: %s", r.Name, f)
			}
			switch {
			case r.Skipped != "":
				skipped++
				t.Logf("SKIP %s (known issue: %s)", r.Name, r.Skipped)
			case r.Failed():
				_, detail := Summary([]Result{r})
				t.Errorf("FAIL %s", strings.TrimSpace(detail))
			default:
				passed++
			}
		}
		t.Logf("family %s: %d passed, %d failed, %d skipped", f, passed, len(results)-passed-skipped, skipped)
	}
	if os.Getenv("BM_SWEEP") == "true" {
		removed, failed := engine.SweepOrphans(ctx, time.Now())
		t.Logf("orphan sweep: removed %d, %d failed", removed, len(failed))
		for _, f := range failed {
			t.Logf("SWEEP %s", f)
		}
	}
}
