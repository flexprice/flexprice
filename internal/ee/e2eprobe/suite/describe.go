package suite

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Describe prints the coverage matrix: personas, bindings, tenant expectations
// and the event budget.
func Describe(w io.Writer, s *Suite, cat Catalog) error {
	bindings, err := s.Bindings(cat)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "e2eprobe suite v%d\n\n", s.Version)

	fmt.Fprintf(tw, "Personas (%d)\n", len(s.Personas))
	fmt.Fprintln(tw, "NAME\tEXTERNAL ID\tTRAITS\tEVENTS")
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
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, p.ExternalIDOrDefault(), strings.Join(traits, ", "), events)
	}

	fmt.Fprintf(tw, "\nScenarios (%d bindings)\n", len(bindings))
	fmt.Fprintln(tw, "SCENARIO\tKIND\tPERSONA\tEVERY")
	for _, b := range bindings {
		persona := b.Persona
		if persona == "" {
			persona = "(tenant)"
		}
		every := b.Every.String()
		if b.Every == 0 {
			every = "tick (" + s.Defaults.Tick.String() + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", b.Scenario, b.Kind, persona, every)
	}

	fmt.Fprintln(tw, "\nTenant")
	fmt.Fprintf(tw, "connections: %s\n", orNone(strings.Join(s.Tenant.Connections, ", ")))
	keys := make([]string, 0, len(s.Tenant.Settings))
	for k := range s.Tenant.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(tw, "settings: %s\n", orNone(strings.Join(keys, ", ")))
	fmt.Fprintf(tw, "webhook silence: %s\n", s.Tenant.Webhooks.SilenceAfter)

	fmt.Fprintln(tw, "\nLoad")
	fmt.Fprintf(tw, "tick: %s\n", s.Defaults.Tick)
	fmt.Fprintf(tw, "events/day: %d of max %d\n", s.EventsPerDay(cat), s.Defaults.Load.MaxEventsPerDay)
	return tw.Flush()
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
