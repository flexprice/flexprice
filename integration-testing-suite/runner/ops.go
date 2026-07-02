package main

import (
	"fmt"
	"sort"
	"strings"
)

// PrintOps dumps the full SDK operation catalog. With verbose, the JSON
// fields of every struct argument are included so journeys can be authored
// without reading SDK source.
func PrintOps(d *Dispatcher, verbose bool) {
	ops := d.Ops()
	fmt.Printf("# %d operations available on the compiled-in Flexprice Go SDK\n", len(ops))
	fmt.Println("# Use these as `call:` values in journey YAML.")
	fmt.Println()
	current := ""
	for _, op := range ops {
		if op.Service != current {
			current = op.Service
			fmt.Printf("\n## %s\n", current)
		}
		fmt.Printf("  %s\n", op.Signature())
		if verbose {
			for _, f := range op.RequestFields() {
				fmt.Printf("      %s\n", f)
			}
		}
	}
}

// CoverageReport compares the SDK op catalog against the ops referenced by
// the given journeys.
type CoverageReport struct {
	Total     int
	Covered   []string
	Uncovered []string
	// PerService maps service → "covered/total".
	PerService map[string][2]int
}

// BuildCoverage computes journey coverage of the SDK surface.
func BuildCoverage(d *Dispatcher, journeys []*Journey) *CoverageReport {
	used := map[string]bool{}
	for _, j := range journeys {
		for _, s := range append(append([]*Step{}, j.Steps...), j.Teardown...) {
			if s.Call == "" {
				continue
			}
			if op, err := d.Resolve(s.Call); err == nil {
				used[op.Name] = true
			}
		}
	}

	rep := &CoverageReport{PerService: map[string][2]int{}}
	for _, op := range d.Ops() {
		rep.Total++
		counts := rep.PerService[op.Service]
		counts[1]++
		if used[op.Name] {
			counts[0]++
			rep.Covered = append(rep.Covered, op.Name)
		} else {
			rep.Uncovered = append(rep.Uncovered, op.Name)
		}
		rep.PerService[op.Service] = counts
	}
	return rep
}

// Print renders the coverage report (markdown-friendly, used in CI summaries).
func (c *CoverageReport) Print() {
	fmt.Printf("## SDK operation coverage: %d/%d (%.0f%%)\n\n",
		len(c.Covered), c.Total, 100*float64(len(c.Covered))/float64(max(c.Total, 1)))

	services := make([]string, 0, len(c.PerService))
	for svc := range c.PerService {
		services = append(services, svc)
	}
	sort.Strings(services)
	for _, svc := range services {
		counts := c.PerService[svc]
		marker := " "
		if counts[0] == 0 {
			marker = "!"
		}
		fmt.Printf("%s %-20s %d/%d\n", marker, svc, counts[0], counts[1])
	}

	if len(c.Uncovered) > 0 {
		fmt.Printf("\nUncovered operations (%d) — candidates for new journey steps:\n", len(c.Uncovered))
		fmt.Println("  " + strings.Join(c.Uncovered, "\n  "))
	}
}
