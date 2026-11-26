package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

type oneScenario struct{}

func (oneScenario) Scenario(name string) (suite.ScenarioInfo, bool) {
	if name != "usage" {
		return suite.ScenarioInfo{}, false
	}
	return suite.ScenarioInfo{Name: "usage", Kind: e2eprobe.KindProbe, Traits: []e2eprobe.Trait{e2eprobe.TraitEvents}, Every: time.Hour, Summary: "totals match", Areas: []e2eprobe.Area{e2eprobe.AreaUsage}}, true
}
func (oneScenario) Profile(string) (int, bool) { return 27, true }
func (oneScenario) Names() []string            { return []string{"usage"} }

const tinySuite = `version: 1
personas:
  - { name: solo, traits: [subscription], events: standard }
scenarios:
  - { name: usage, every: 1h }
`

func newBuilder(t *testing.T) *Builder {
	t.Helper()
	s, err := suite.Parse([]byte(tinySuite))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	return New(Sources{
		Version:    "abc1234",
		SuitePath:  "/etc/e2eprobe/suite.yaml",
		SuiteBytes: []byte(tinySuite),
		Suite:      s,
		Catalog:    oneScenario{},
		Status:     func() scenario.Status { return scenario.Status{RunID: "run-1", StartedAt: now} },
		State:      func() incident.State { return incident.State{SavedAt: now} },
		Logs:       func() []string { return []string{`{"msg":"one"}`} },
		Now:        func() time.Time { return now.Add(time.Minute) },
	})
}

func TestBuild_CollectsBuildSuiteStatusStateAndLogs(t *testing.T) {
	b, err := newBuilder(t).Build()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(tinySuite))
	if b.Version != "abc1234" || b.Suite.Source != "/etc/e2eprobe/suite.yaml" || b.Suite.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("identity = %+v", b.Suite)
	}
	if !strings.Contains(b.Suite.Describe, "solo") || !strings.Contains(b.Suite.Describe, "totals match") {
		t.Errorf("the bundle must carry the rendered suite:\n%s", b.Suite.Describe)
	}
	if b.Status.RunID != "run-1" || b.State.SavedAt.IsZero() || len(b.RecentLogs) != 1 || b.GeneratedAt.Minute() != 1 {
		t.Errorf("bundle = %+v", b)
	}
}

func TestHandler_ServesTheBundleAsADownload(t *testing.T) {
	rec := httptest.NewRecorder()
	newBuilder(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bundle", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="e2eprobe-bundle-`) {
		t.Fatalf("status %d headers %v", rec.Code, rec.Header())
	}
	var b Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || b.Version != "abc1234" {
		t.Fatalf("body = %s (%v)", rec.Body.String(), err)
	}
}
