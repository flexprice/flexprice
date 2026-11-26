package suite

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

// line writes one formatted line; the logger is not the destination here.
func line(w io.Writer, format string, args ...any) {
	_, _ = io.WriteString(w, fmt.Sprintf(format, args...))
}

// Describe prints the coverage matrix: personas, bindings, tenant expectations
// and the event budget.
func Describe(w io.Writer, s *Suite, cat Catalog) error {
	bindings, err := s.Bindings(cat)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	line(tw, "e2eprobe suite v%d\n\n", s.Version)

	line(tw, "Personas (%d)\n", len(s.Personas))
	line(tw, "NAME\tEXTERNAL ID\tTRAITS\tEVENTS"+"\n")
	for _, p := range s.Personas {
		traits := make([]string, 0, len(p.Traits))
		for _, t := range p.Traits {
			traits = append(traits, string(t))
		}
		events := "-"
		if p.Events != "" {
			size, _ := cat.Profile(p.Events)
			events = fmt.Sprintf("%s (%d/tick)", p.Events, size)
		}
		line(tw, "%s\t%s\t%s\t%s\n", p.Name, p.ExternalIDOrDefault(), strings.Join(traits, ", "), events)
	}

	line(tw, "\nScenarios (%d bindings)\n", len(bindings))
	line(tw, "SCENARIO\tKIND\tPERSONA\tEVERY"+"\n")
	for _, b := range bindings {
		persona := b.Persona
		if persona == "" {
			persona = "(tenant)"
		}
		every := b.Every.String()
		if b.Every == 0 {
			every = "tick (" + s.Defaults.Tick.String() + ")"
		}
		line(tw, "%s\t%s\t%s\t%s\n", b.Scenario, b.Kind, persona, every)
	}

	line(tw, "\nCoverage"+"\n")
	line(tw, "SCENARIO\tCHECKS\tON"+"\n")
	var order []string
	personasBy := map[string][]string{}
	for _, b := range bindings {
		if _, seen := personasBy[b.Scenario]; !seen {
			order = append(order, b.Scenario)
		}
		on := "(tenant)"
		if p, ok := s.Persona(b.Persona); ok {
			on = fmt.Sprintf("%s (%s)", p.Name, p.ExternalIDOrDefault())
		}
		personasBy[b.Scenario] = append(personasBy[b.Scenario], on)
	}
	for _, name := range order {
		info, _ := cat.Scenario(name)
		line(tw, "%s\t%s\t%s\n", name, orNone(info.Summary), strings.Join(personasBy[name], ", "))
	}
	for _, sc := range s.Scenarios {
		if sc.IsEnabled() {
			continue
		}
		families := ""
		if len(sc.Families) > 0 {
			families = " (families " + strings.Join(sc.Families, ", ") + ")"
		}
		line(tw, "disabled: %s%s\n", sc.Name, families)
	}

	line(tw, "\nAreas"+"\n")
	for _, area := range e2eprobe.Areas() {
		line(tw, "%s\t%s\n", area, s.describeArea(cat, area, bindings))
	}

	line(tw, "\nTenant"+"\n")
	line(tw, "connections: %s\n", orNone(strings.Join(s.Tenant.Connections, ", ")))
	keys := make([]string, 0, len(s.Tenant.Settings))
	for k := range s.Tenant.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	line(tw, "settings: %s\n", orNone(strings.Join(keys, ", ")))
	line(tw, "webhook silence: %s\n", s.Tenant.Webhooks.SilenceAfter)

	line(tw, "\nLoad"+"\n")
	line(tw, "tick: %s\n", s.Defaults.Tick)
	profiles, flows := s.Ingest(cat)
	line(tw, "events/day: %d of max %d (profiles %d, flows %d)\n", profiles+flows, s.Defaults.Load.MaxEventsPerDay, profiles, flows)
	return tw.Flush()
}

// describeArea names the bindings that exercise an area, or why none does.
func (s *Suite) describeArea(cat Catalog, area e2eprobe.Area, bindings []Binding) string {
	counts := map[string]int{}
	for _, b := range bindings {
		counts[b.Scenario]++
	}
	var covered, unbound []string
	for _, name := range cat.Names() {
		info, _ := cat.Scenario(name)
		if !slices.Contains(info.Areas, area) {
			continue
		}
		if n := counts[name]; n > 0 {
			covered = append(covered, fmt.Sprintf("%s ×%d", name, n))
		} else {
			unbound = append(unbound, name)
		}
	}
	switch {
	case len(covered) > 0:
		return "covered by: " + strings.Join(covered, ", ")
	case len(unbound) > 0:
		return "not covered: " + s.whyUnbound(cat, unbound)
	}
	return "no scenario in the catalog"
}

// whyUnbound groups unbound scenarios by what keeps the suite from running
// them: an entry switched off, a trait no persona carries, or no entry at all.
func (s *Suite) whyUnbound(cat Catalog, names []string) string {
	reasons := map[string][]string{}
	var order []string
	note := func(reason, name string) {
		if _, seen := reasons[reason]; !seen {
			order = append(order, reason)
		}
		reasons[reason] = append(reasons[reason], name)
	}
	for _, name := range names {
		entry, listed := s.entryFor(name)
		if listed && !entry.IsEnabled() {
			note(entry.Name+" disabled", name)
			continue
		}
		info, _ := cat.Scenario(name)
		var why []string
		if !listed {
			why = append(why, "not in the suite")
		}
		for _, t := range info.Traits {
			if !s.anyPersonaHas(t) {
				why = append(why, "no persona has trait "+string(t))
				break
			}
		}
		if len(why) == 0 {
			why = append(why, "not bound")
		}
		note(strings.Join(why, " and "), name)
	}
	parts := make([]string, 0, len(order))
	for _, reason := range order {
		if strings.HasSuffix(reason, " disabled") {
			parts = append(parts, reason)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", reason, strings.Join(reasons[reason], ", ")))
	}
	return strings.Join(parts, "; ")
}

// entryFor finds the suite entry that stands for a catalog scenario, by name
// or as one of its families.
func (s *Suite) entryFor(name string) (Scenario, bool) {
	for _, sc := range s.Scenarios {
		if slices.Contains(sc.names(), name) {
			return sc, true
		}
	}
	return Scenario{}, false
}

func (s *Suite) anyPersonaHas(t e2eprobe.Trait) bool {
	for _, p := range s.Personas {
		if p.HasTrait(t) {
			return true
		}
	}
	return false
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
