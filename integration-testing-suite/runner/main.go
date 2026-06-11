// Command runner executes YAML-defined integration journeys against live
// Flexprice API targets through the official Go SDK.
//
// Modes:
//
//	runner -dir ../journeys                      run all journeys (needs a target)
//	runner -dir ../journeys -tags sanity         run journeys tagged "sanity"
//	runner -dir ../journeys -validate            static validation, no network
//	runner -list-ops [-verbose]                  dump the SDK operation catalog
//	runner -dir ../journeys -coverage            SDK coverage gap report
//
// Targets come from FLEXPRICE_TARGETS_FILE / FLEXPRICE_TARGETS /
// FLEXPRICE_API_KEY+FLEXPRICE_API_HOST (see targets.go).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	flexprice "github.com/flexprice/go-sdk/v2"
)

func main() {
	var (
		dir         = flag.String("dir", "../journeys", "directory containing journey YAML files (recursive)")
		tagsFlag    = flag.String("tags", "", "comma-separated tags filter (journey runs if it has ANY of these)")
		namesFlag   = flag.String("journey", "", "comma-separated journey names to run")
		parallel    = flag.Int("parallel", 4, "max journeys running concurrently per target")
		validate    = flag.Bool("validate", false, "validate journeys statically and exit (no network, no secrets)")
		listOps     = flag.Bool("list-ops", false, "print the SDK operation catalog and exit")
		coverage    = flag.Bool("coverage", false, "print SDK operation coverage across journeys and exit")
		verbose     = flag.Bool("verbose", false, "with -list-ops: include request field names")
		reportJSON  = flag.String("report-json", "", "write a machine-readable JSON report to this path")
		junitPath   = flag.String("junit", "", "write a JUnit XML report to this path")
		stepTimeout = flag.Duration("step-timeout", 2*time.Minute, "timeout for a single API call")
		jTimeout    = flag.Duration("journey-timeout", 15*time.Minute, "timeout for one full journey")
	)
	flag.Parse()

	// A throwaway client is enough to reflect over the SDK surface.
	dispatcher := NewDispatcher(flexprice.New())

	if *listOps {
		PrintOps(dispatcher, *verbose)
		return
	}

	journeys, err := LoadJourneys(*dir)
	if err != nil {
		fatal(err)
	}
	if len(journeys) == 0 {
		fatal(fmt.Errorf("no journey YAML files found under %s", *dir))
	}

	// Static validation always runs first; it is cheap and catches authoring
	// mistakes before any API call is made.
	validationFailed := false
	for _, j := range journeys {
		for _, e := range ValidateJourney(j, dispatcher) {
			fmt.Fprintf(os.Stderr, "VALIDATION %s: %v\n", j.File, e)
			validationFailed = true
		}
	}
	if validationFailed {
		os.Exit(2)
	}

	if *coverage {
		BuildCoverage(dispatcher, journeys).Print()
		return
	}

	if *validate {
		fmt.Printf("OK: %d journeys validated (%s)\n", len(journeys), *dir)
		return
	}

	selected := FilterJourneys(journeys, splitCSV(*namesFlag), splitCSV(*tagsFlag))
	if len(selected) == 0 {
		fatal(fmt.Errorf("no journeys match filters (tags=%q, journey=%q); %d journeys loaded", *tagsFlag, *namesFlag, len(journeys)))
	}

	targets, err := loadTargets()
	if err != nil {
		fatal(err)
	}

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║            FLEXPRICE INTEGRATION JOURNEY RUNNER              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nJourneys: %d selected (of %d loaded)  |  Targets: %d  |  Parallelism: %d\n",
		len(selected), len(journeys), len(targets), *parallel)

	var reports []*TargetReport
	for ti, target := range targets {
		fmt.Printf("\n%s\nTARGET %d/%d: %s (%s, key %s)\n%s\n",
			strings.Repeat("█", 62), ti+1, len(targets), target.label(), target.host(), target.maskedKey(),
			strings.Repeat("█", 62))

		reports = append(reports, runTarget(target, selected, dispatcher, *parallel, *stepTimeout, *jTimeout))
	}

	if *reportJSON != "" {
		if err := WriteJSONReport(*reportJSON, reports); err != nil {
			fmt.Fprintf(os.Stderr, "write JSON report: %v\n", err)
		}
	}
	if *junitPath != "" {
		if err := WriteJUnitReport(*junitPath, reports); err != nil {
			fmt.Fprintf(os.Stderr, "write JUnit report: %v\n", err)
		}
	}

	if len(reports) > 1 {
		PrintCrossTargetSummary(reports)
	}
	for _, tr := range reports {
		if tr.Failed() {
			os.Exit(1)
		}
	}
}

// runTarget executes the selected journeys (in parallel) against one target.
func runTarget(target Target, journeys []*Journey, dispatcher *Dispatcher, parallelism int, stepTimeout, journeyTimeout time.Duration) *TargetReport {
	serverURL := target.serverURL()
	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(target.APIKey),
	)
	// Each target needs its own dispatcher bound to its authenticated client.
	targetDispatcher := NewDispatcher(client)
	_ = dispatcher

	exec := &Executor{
		Dispatcher:  targetDispatcher,
		Raw:         NewRawClient(serverURL, target.APIKey),
		TargetName:  target.label(),
		StepTimeout: stepTimeout,
	}

	start := time.Now()
	results := make([]*JourneyResult, len(journeys))

	if parallelism < 1 {
		parallelism = 1
	}
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var printMu sync.Mutex

	for i, j := range journeys {
		wg.Add(1)
		go func(idx int, journey *Journey) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), journeyTimeout)
			defer cancel()

			res := exec.RunJourney(ctx, journey)
			results[idx] = res

			// Print each journey's block atomically to avoid interleaving.
			printMu.Lock()
			PrintJourney(res)
			printMu.Unlock()
		}(i, j)
	}
	wg.Wait()

	tr := &TargetReport{Target: target, Results: results, Duration: time.Since(start)}
	PrintTargetSummary(tr)
	return tr
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
