package scenario

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ledger"
)

// Status is the live view served at /status.
type Status struct {
	RunID     string              `json:"run_id"`
	StartedAt time.Time           `json:"started_at"`
	Bindings  []BindingStatus     `json:"bindings"`
	Open      []incident.Incident `json:"open_incidents"`
	Inbox     inbox.Stats         `json:"inbox"`
	Ledger    ledger.Stats        `json:"ledger"`
}

type BindingStatus struct {
	Scenario string          `json:"scenario"`
	Persona  string          `json:"persona,omitempty"`
	Every    time.Duration   `json:"every"`
	LastRun  time.Time       `json:"last_run,omitzero"`
	NextRun  time.Time       `json:"next_run,omitzero"`
	Last     e2eprobe.Result `json:"last"`
	Runs     int             `json:"runs"`
	Passes   int             `json:"passes"`
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	bindings := make([]BindingStatus, 0, len(r.status))
	for _, st := range r.status {
		bindings = append(bindings, *st)
	}
	started := r.started
	r.mu.Unlock()
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Scenario != bindings[j].Scenario {
			return bindings[i].Scenario < bindings[j].Scenario
		}
		return bindings[i].Persona < bindings[j].Persona
	})
	st := Status{RunID: r.deps.RunID, StartedAt: started, Bindings: bindings, Open: r.tracker.Open()}
	if r.deps.Inbox != nil {
		st.Inbox = r.deps.Inbox.Stats()
	}
	if r.deps.Ledger != nil {
		st.Ledger = r.deps.Ledger.Stats()
	}
	return st
}

// Handler serves GET /status as JSON.
func (r *Runner) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.Status())
	})
	return mux
}
