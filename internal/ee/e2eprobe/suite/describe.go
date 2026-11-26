package suite

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
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

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
