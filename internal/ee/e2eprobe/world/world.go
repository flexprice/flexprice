// Package world provisions and binds the probe's fixtures: shared catalog
// entities, long-lived personas and the ephemeral customers scenarios clone.
package world

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/logger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

const (
	EphemeralPrefix    = "e2eprobe-eph-"
	PersistentCohort   = "persistent"
	EphemeralCohort    = "ephemeral"
	MultiCadenceCohort = "multi_cadence_quarterly"
)

// World holds the live state behind a suite.
type World struct {
	client       e2eprobe.Client
	suite        *suite.Suite
	runID        string
	lg           *logger.Logger
	mu           sync.RWMutex
	fixtures     *Fixtures
	personas     map[string]*Persona
	eph          *Ephemerals
	sweepers     map[string]OrphanSweeper
	sweeperOrder []string
	now          func() time.Time
}

func New(c e2eprobe.Client, s *suite.Suite, runID string, lg *logger.Logger) *World {
	w := &World{client: c, suite: s, runID: runID, lg: lg, fixtures: newFixtures(), personas: map[string]*Persona{}, eph: NewEphemerals(), now: time.Now}
	for _, spec := range s.Personas {
		w.personas[spec.Name] = newPersona(spec)
	}
	return w
}

// Provision makes the live tenant match the suite: shared fixtures first, then
// every persona. A persona that fails keeps its last good state and is reported
// in the joined error; the others still provision.
func (w *World) Provision(ctx context.Context) error {
	if err := w.provisionFixtures(ctx); err != nil {
		return fmt.Errorf("fixtures: %w", err)
	}
	var errs []error
	for _, spec := range w.suite.Personas {
		p := w.personas[spec.Name]
		scratch := p.scratch()
		if err := w.provisionPersona(ctx, scratch); err != nil {
			// The published binding stands; a transient error must not silence every scenario on the persona.
			p.carryRecovery(scratch)
			errs = append(errs, fmt.Errorf("persona %s: %w", spec.Name, err))
			continue
		}
		p.adopt(scratch)
	}
	return errors.Join(errs...)
}

func (w *World) Ready() bool {
	return w.Fixtures().Ready()
}

func (w *World) Fixtures() *Fixtures {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.fixtures
}

func (w *World) Persona(name string) (*Persona, bool) {
	p, ok := w.personas[name]
	return p, ok
}

// Personas lists personas in suite order.
func (w *World) Personas() []*Persona {
	out := make([]*Persona, 0, len(w.suite.Personas))
	for _, spec := range w.suite.Personas {
		out = append(out, w.personas[spec.Name])
	}
	return out
}

func (w *World) Ephemerals() *Ephemerals {
	return w.eph
}

// SetClock replaces the clock that stamps ephemerals and activation dates.
func (w *World) SetClock(now func() time.Time) {
	w.now = now
}

func (w *World) info(ctx context.Context, msg string, kv ...any) {
	if w.lg != nil {
		w.lg.Info(ctx, msg, kv...)
	}
}
