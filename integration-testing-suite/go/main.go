package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	flexprice "github.com/flexprice/go-sdk/v2"
)

const defaultAPIHost = "api.cloud.flexprice.io/v1"

// ts returns a unique-ish timestamp suffix for entity names.
func ts() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

// targetOutcome captures the aggregate result of running the suite against one target.
type targetOutcome struct {
	target      Target
	coreFailed  int
	cleanupFail int
	passed      int
	failed      int
	skipped     int
	total       int
	duration    time.Duration
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              FLEXPRICE ORCHESTRATED SANITY TEST              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// ── Resolve targets (one or many base URL + API key pairs) ──────────

	targets, err := loadTargets()
	if err != nil {
		log.Fatal(err)
	}

	if len(targets) > 1 {
		fmt.Printf("Running suite against %d targets:\n", len(targets))
		for i, t := range targets {
			fmt.Printf("  %d. %-16s %s\n", i+1, t.label(), t.host())
		}
		fmt.Println()
	}

	// ── Run the suite once per target ───────────────────────────────────

	outcomes := make([]targetOutcome, 0, len(targets))
	for i, t := range targets {
		if len(targets) > 1 {
			fmt.Println(strings.Repeat("█", 62))
			fmt.Printf("█ TARGET %d/%d: %s\n", i+1, len(targets), t.label())
			fmt.Println(strings.Repeat("█", 62))
			fmt.Println()
		}
		outcomes = append(outcomes, runTarget(t))
	}

	// ── Cross-target summary (only when running multiple targets) ────────

	overallFailed := false
	for _, o := range outcomes {
		if o.coreFailed > 0 {
			overallFailed = true
		}
	}

	if len(targets) > 1 {
		printMultiTargetSummary(outcomes)
	}

	if overallFailed {
		os.Exit(1)
	}
}

// runTarget executes the full sanity suite against a single target and returns
// its aggregate outcome.
func runTarget(t Target) targetOutcome {
	serverURL := t.serverURL()
	insecure := t.skipTLSVerify()

	fmt.Printf("API Host: %s\n", t.host())
	fmt.Printf("API Key:  %s\n", t.maskedKey())
	if insecure {
		fmt.Printf("TLS:      INSECURE (certificate verification disabled)\n")
	}
	fmt.Printf("Started:  %s\n", time.Now().Format(time.RFC3339))

	// ── Initialize SDK client ───────────────────────────────────────────

	// currentStep is an atomic shared between SanityRunner and TrafficLogger so
	// every HTTP call is tagged with the step number it belongs to.
	var currentStep atomic.Int32

	// Transport chain: RoutingCapture → TrafficLogger → real transport.
	// RoutingCapture enriches the request (adds X-Debug-DB-Routing / X-Pin-To-Writer),
	// then TrafficLogger logs the enriched request + full response for the HTML report.
	base := newHTTPClient(insecure)
	inner := base.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	trafficLogger := NewTrafficLogger(inner, &currentStep)
	capture := NewRoutingCapture(trafficLogger)
	base.Transport = capture
	httpClient := base

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(t.APIKey),
		flexprice.WithClient(httpClient),
	)

	// Also keep a raw HTTP client as fallback for any edge cases.
	raw := NewRawClient(serverURL, t.APIKey, httpClient)

	// ── Run orchestrated sanity test ────────────────────────────────────

	runner := &SanityRunner{
		client:         client,
		raw:            raw,
		routingCapture: capture,
		trafficLogger:  trafficLogger,
		currentStep:    &currentStep, // shared pointer — runner.run() updates it, trafficLogger reads it
	}
	ctx := contextWithTimeout()
	start := time.Now()

	// Phase 0: routing validation — must run first so lagProbeOK is set
	// before any routing assertions in subsequent phases.
	runner.runRoutingSteps(ctx)

	// Phases 1-7: Full billing lifecycle.
	runner.runCatalogSteps(ctx)
	runner.runBillingSteps(ctx)
	runner.runSubscriptionSteps(ctx)
	runner.runWalletSteps(ctx)
	runner.runUsageSteps(ctx)
	runner.runInvoiceSteps(ctx)
	runner.runCleanupSteps(ctx)

	totalDuration := time.Since(start)

	// ── Print per-target report ─────────────────────────────────────────

	runner.printReport(totalDuration)

	// ── Generate HTML report ─────────────────────────────────────────────
	reportPath := fmt.Sprintf("sanity-report-%s.html", time.Now().Format("20060102-150405"))
	if runner.trafficLogger != nil {
		if err := generateHTMLReport(
			runner.results,
			runner.trafficLogger.Calls(),
			totalDuration,
			reportPath,
		); err != nil {
			fmt.Printf("\n⚠  Failed to write HTML report: %v\n", err)
		} else {
			fmt.Printf("\nHTML report written → %s\n", reportPath)
		}
	}

	return runner.outcome(t, totalDuration)
}

// outcome tallies the runner's results into a targetOutcome.
func (r *SanityRunner) outcome(t Target, duration time.Duration) targetOutcome {
	o := targetOutcome{target: t, total: len(r.results), duration: duration}
	for _, s := range r.results {
		switch {
		case s.Skipped:
			o.skipped++
		case s.Passed:
			o.passed++
		default:
			o.failed++
			if strings.HasPrefix(s.Phase, "PHASE 7") {
				o.cleanupFail++
			} else {
				o.coreFailed++
			}
		}
	}
	return o
}

// printMultiTargetSummary prints a one-line-per-target roll-up across all targets.
func printMultiTargetSummary(outcomes []targetOutcome) {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 62))
	fmt.Println("CROSS-TARGET SUMMARY")
	fmt.Println(strings.Repeat("═", 62))
	fmt.Println()

	allPassed := true
	for _, o := range outcomes {
		status := "PASS"
		if o.coreFailed > 0 {
			status = "FAIL"
			allPassed = false
		}
		fmt.Printf("[%s] %-18s %d/%d passed | %d failed | %d skipped | %.1fs\n",
			status, o.target.label(), o.passed, o.total, o.failed, o.skipped, o.duration.Seconds())
		if o.cleanupFail > 0 {
			fmt.Printf("       (%d core failures, %d cleanup failures)\n", o.coreFailed, o.cleanupFail)
		}
	}

	fmt.Println()
	if allPassed {
		fmt.Println("ALL TARGETS PASSED ✓")
	} else {
		fmt.Println("ONE OR MORE TARGETS FAILED ✗")
	}
	fmt.Println(strings.Repeat("═", 62))
}

func contextWithTimeout() context.Context {
	return context.Background()
}
