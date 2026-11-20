package suite

import (
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

// Binding is one scenario scheduled against one persona ("" for tenant-level).
type Binding struct {
	Scenario string
	Persona  string
	Every    time.Duration
	Kind     e2eprobe.Kind
}

// names lists the catalog names a suite entry stands for: itself, or one per family.
func (sc Scenario) names() []string {
	if len(sc.Families) == 0 {
		return []string{sc.Name}
	}
	out := make([]string, 0, len(sc.Families))
	for _, f := range sc.Families {
		out = append(out, sc.Name+"-"+f)
	}
	return out
}

// personasFor picks the personas a scenario runs on: the explicit list, or
// every persona carrying all required traits.
func (s *Suite) personasFor(sc Scenario, info ScenarioInfo) []string {
	if len(sc.Personas) > 0 {
		return append([]string(nil), sc.Personas...)
	}
	var out []string
	for _, p := range s.Personas {
		if hasAll(p, info.Traits) {
			out = append(out, p.Name)
		}
	}
	return out
}

func hasAll(p Persona, traits []e2eprobe.Trait) bool {
	for _, t := range traits {
		if !p.HasTrait(t) {
			return false
		}
	}
	return true
}

// Bindings resolves every enabled scenario entry against the catalog. A zero
// Every is kept when the catalog has none, so the runner can use the tick.
func (s *Suite) Bindings(cat Catalog) ([]Binding, error) {
	var out []Binding
	for _, sc := range s.Scenarios {
		if !sc.IsEnabled() {
			continue
		}
		for _, name := range sc.names() {
			info, ok := cat.Scenario(name)
			if !ok {
				return nil, fmt.Errorf("suite: unknown scenario %q", name)
			}
			every := sc.Every.Duration
			if every == 0 {
				every = info.Every
			}
			if len(info.Traits) == 0 {
				out = append(out, Binding{Scenario: name, Every: every, Kind: info.Kind})
				continue
			}
			for _, persona := range s.personasFor(sc, info) {
				out = append(out, Binding{Scenario: name, Persona: persona, Every: every, Kind: info.Kind})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scenario != out[j].Scenario {
			return out[i].Scenario < out[j].Scenario
		}
		return out[i].Persona < out[j].Persona
	})
	return out, nil
}
