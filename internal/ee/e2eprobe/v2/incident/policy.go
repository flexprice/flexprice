// Package incident turns the stream of scenario results into incidents: one
// notification when a binding has been failing past its thresholds, reminders
// on a long interval, one notification on recovery.
package incident

import "time"

// Policy decides when a binding's failures or skips become an incident. A zero
// RenotifyEvery disables reminders.
type Policy struct {
	AfterFailures int
	AfterDuration time.Duration
	RenotifyEvery time.Duration
	SkipAfter     time.Duration
}

// Rollup opens one region-level incident when this many bindings see
// infrastructure errors inside the window.
type Rollup struct {
	MinBindings int
	Window      time.Duration
}

func DefaultRollup() Rollup {
	return Rollup{MinBindings: 3, Window: 15 * time.Minute}
}

// Key identifies a binding: a scenario on a persona, or a tenant-level scenario.
type Key struct {
	Scenario string `json:"scenario"`
	Persona  string `json:"persona,omitempty"`
}

func (k Key) String() string {
	if k.Persona == "" {
		return k.Scenario
	}
	return k.Scenario + " · " + k.Persona
}

// infraKey names the region-level rollup incident.
var infraKey = Key{Scenario: "api"}
