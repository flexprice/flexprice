// Package bundle assembles what a support engineer needs from a running probe
// into one JSON document: build, suite, live status, tracker state and recent
// log lines. It carries ids and reasons, never credentials.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

// Sources is where a Builder reads each part of the bundle from.
type Sources struct {
	Version    string
	SuitePath  string // empty means the embedded default
	SuiteBytes []byte
	Suite      *suite.Suite
	Catalog    suite.Catalog
	Status     func() scenario.Status
	State      func() incident.State
	Logs       func() []string
	Now        func() time.Time
}

// Bundle is the document a support engineer receives.
type Bundle struct {
	Version     string          `json:"version"`
	GeneratedAt time.Time       `json:"generated_at"`
	Suite       SuiteInfo       `json:"suite"`
	Status      scenario.Status `json:"status"`
	State       incident.State  `json:"state"`
	RecentLogs  []string        `json:"recent_logs"`
}

// SuiteInfo identifies the suite the probe runs and renders it as describe does.
type SuiteInfo struct {
	Source   string `json:"source"`
	SHA256   string `json:"sha256"`
	Describe string `json:"describe"`
}

// Builder assembles bundles from its sources.
type Builder struct {
	src Sources
}

func New(src Sources) *Builder {
	if src.Now == nil {
		src.Now = time.Now
	}
	return &Builder{src: src}
}

// Build reads every source once.
func (b *Builder) Build() (Bundle, error) {
	var describe bytes.Buffer
	if err := suite.Describe(&describe, b.src.Suite, b.src.Catalog); err != nil {
		return Bundle{}, fmt.Errorf("describe suite: %w", err)
	}
	sum := sha256.Sum256(b.src.SuiteBytes)
	source := b.src.SuitePath
	if source == "" {
		source = "embedded"
	}
	out := Bundle{
		Version:     b.src.Version,
		GeneratedAt: b.src.Now().UTC(),
		Suite:       SuiteInfo{Source: source, SHA256: hex.EncodeToString(sum[:]), Describe: describe.String()},
	}
	if b.src.Status != nil {
		out.Status = b.src.Status()
	}
	if b.src.State != nil {
		out.State = b.src.State()
	}
	if b.src.Logs != nil {
		out.RecentLogs = b.src.Logs()
	}
	return out, nil
}

// Handler serves the bundle as a JSON download.
func (b *Builder) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		out, err := b.Build()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="e2eprobe-bundle-%s.json"`, out.GeneratedAt.Format("20060102T150405Z")))
		_ = json.NewEncoder(w).Encode(out)
	})
}
