package main

import (
	"fmt"
	"strings"
	"time"

	flexprice "github.com/flexprice/go-sdk/v2"
)

// StepResult captures the outcome of a single orchestration step.
type StepResult struct {
	StepNumber int
	StepName   string
	Phase      string
	Passed     bool
	Skipped    bool
	Duration   time.Duration
	EntityID   string // ID of created entity (for debugging)
	Error      error
	Details    string // Additional context (e.g. expected vs actual)
	SDKMethod   string // The SDK method that SHOULD be used (for coverage report)
	RawHTTP     bool   // true = raw HTTP because SDK does not expose this resource
	SDKFallback bool   // true = SDK was tried first but failed, fell back to raw HTTP
	SkipReason  string
}

// SanityRunner orchestrates the end-to-end sanity test.
type SanityRunner struct {
	client  *flexprice.Flexprice
	raw     *RawClient // fallback for any endpoints not in SDK
	results []StepResult
	step    int
	phase   string

	// Tracks SDK coverage.
	sdkCovered []string // API calls that the SDK DOES cover
	sdkMissing []string // API calls that the SDK does NOT cover
	sdkBroken  []string // API calls where SDK failed and we fell back to raw HTTP

	// Entity IDs collected during the run.
	featureGroupID string
	priceGroupID   string
	featureAID     string
	featureBID     string
	meterAID       string
	meterBID       string
	eventNameA     string
	eventNameB     string
	planID         string
	priceRecurr1   string
	priceRecurr2   string
	priceUsageA    string
	priceUsageB    string
	entitlementID  string
	taxRateID      string
	taxRateCode    string
	couponID       string
	customerID     string
	externalCustID string
	subscriptionID        string
	subscriptionCancelled bool
	invoiceID      string
	walletID       string

	// Running totals for usage verification.
	totalTokensIngested  float64
	totalGBHoursIngested float64
}

// setPhase sets the current phase label for subsequent steps.
func (r *SanityRunner) setPhase(name string) {
	r.phase = name
}

// run executes a single named step, recording its result.
// sdkMethod is the SDK method name (e.g. "client.Features.CreateFeature") for coverage tracking.
// missingFromSDK flags that this resource is not in the generated Speakeasy SDK.
func (r *SanityRunner) run(name string, sdkMethod string, missingFromSDK bool, fn func() error) {
	r.step++
	start := time.Now()

	// Append first so that lastResult() works inside fn().
	r.results = append(r.results, StepResult{
		StepNumber: r.step,
		StepName:   name,
		Phase:      r.phase,
		SDKMethod:  sdkMethod,
		RawHTTP:    missingFromSDK,
	})
	idx := len(r.results) - 1

	err := fn()
	r.results[idx].Duration = time.Since(start)

	if err != nil {
		r.results[idx].Passed = false
		r.results[idx].Error = err
	} else {
		r.results[idx].Passed = true
	}

	// Track SDK coverage.
	if missingFromSDK {
		r.sdkMissing = append(r.sdkMissing, sdkMethod)
	} else {
		r.sdkCovered = append(r.sdkCovered, sdkMethod)
	}

	r.printStep(r.results[idx])
}

// skip records a step that was skipped due to a prior dependency failure.
func (r *SanityRunner) skip(name string, reason string) {
	r.step++
	result := StepResult{
		StepNumber: r.step,
		StepName:   name,
		Phase:      r.phase,
		Skipped:    true,
		SkipReason: reason,
	}
	r.results = append(r.results, result)
	r.printStep(result)
}

// require checks whether a dependency ID is non-empty. Returns false and
// records a skip if the dependency is missing.
func (r *SanityRunner) require(depID string, depName string, stepName string) bool {
	if depID != "" {
		return true
	}
	r.skip(stepName, fmt.Sprintf("depends on %s which failed", depName))
	return false
}

// lastResult returns a pointer to the most recently appended result (for in-step mutation).
func (r *SanityRunner) lastResult() *StepResult {
	return &r.results[len(r.results)-1]
}

// markSDKFallback flags the current step as having fallen back from SDK to raw HTTP.
// Call this inside a step's fn() when the SDK call fails and you retry with raw HTTP.
func (r *SanityRunner) markSDKFallback(sdkMethod string, sdkErr error) {
	res := r.lastResult()
	res.SDKFallback = true
	r.sdkBroken = append(r.sdkBroken, sdkMethod)
	// Prepend the SDK failure info to details.
	failNote := fmt.Sprintf("SDK %s FAILED: %v → retrying via raw HTTP", sdkMethod, sdkErr)
	if res.Details != "" {
		res.Details = failNote + "\n        " + res.Details
	} else {
		res.Details = failNote
	}
}

// ---------- Printing helpers ----------

func (r *SanityRunner) printPhaseHeader(name string) {
	pad := 60 - len(name) - 4
	if pad < 0 {
		pad = 0
	}
	fmt.Printf("\n── %s %s\n\n", name, strings.Repeat("─", pad))
}

func (r *SanityRunner) printStep(s StepResult) {
	tag := "[PASS]"
	if s.Skipped {
		tag = "[SKIP]"
	} else if !s.Passed {
		tag = "[FAIL]"
	}

	rawTag := ""
	if s.SDKFallback {
		rawTag = "  [SDK BROKEN → RAW HTTP]"
	} else if s.RawHTTP {
		rawTag = "  [RAW HTTP]"
	}

	fmt.Printf("%-6s %2d. %-38s %6dms%s\n",
		tag, s.StepNumber, s.StepName, s.Duration.Milliseconds(), rawTag)

	if s.Details != "" {
		fmt.Printf("        → %s\n", s.Details)
	}

	if s.Error != nil && !s.Passed {
		fmt.Printf("       Error: %v\n", s.Error)
	}

	if s.Skipped && s.SkipReason != "" {
		fmt.Printf("       Reason: %s\n", s.SkipReason)
	}
}

// printReport prints the final summary.
func (r *SanityRunner) printReport(totalDuration time.Duration) {
	passed, failed, skipped := 0, 0, 0
	cleanupFailed := 0
	var coreFailures []StepResult
	var cleanupFailures []StepResult
	for _, s := range r.results {
		isCleanup := strings.HasPrefix(s.Phase, "PHASE 7")
		switch {
		case s.Skipped:
			skipped++
		case s.Passed:
			passed++
		default:
			failed++
			if isCleanup {
				cleanupFailed++
				cleanupFailures = append(cleanupFailures, s)
			} else {
				coreFailures = append(coreFailures, s)
			}
		}
	}

	coreFailed := failed - cleanupFailed

	fmt.Println()
	fmt.Println(strings.Repeat("═", 62))
	fmt.Println()
	fmt.Printf("RESULTS: %d/%d passed | %d failed | %d skipped\n",
		passed, len(r.results), failed, skipped)
	if cleanupFailed > 0 {
		fmt.Printf("         (%d core failures, %d cleanup failures)\n", coreFailed, cleanupFailed)
	}
	fmt.Printf("Duration: %.1fs\n", totalDuration.Seconds())

	if len(coreFailures) > 0 {
		fmt.Println()
		fmt.Println("FAILED STEPS:")
		for _, f := range coreFailures {
			fmt.Printf("  Step %d: %s\n", f.StepNumber, f.StepName)
			if f.Error != nil {
				fmt.Printf("    Error: %v\n", f.Error)
			}
			if f.Details != "" {
				fmt.Printf("    Details: %s\n", f.Details)
			}
			if f.SDKMethod != "" {
				fmt.Printf("    SDK Method: %s\n", f.SDKMethod)
			}
		}
	}

	if len(cleanupFailures) > 0 {
		fmt.Println()
		fmt.Println("CLEANUP FAILURES:")
		for _, f := range cleanupFailures {
			fmt.Printf("  Step %d: %s\n", f.StepNumber, f.StepName)
			if f.Error != nil {
				fmt.Printf("    Error: %v\n", f.Error)
			}
		}
	}

	// SDK coverage summary.
	fmt.Println()
	fmt.Println("SDK COVERAGE:")
	coveredUniq := uniqueStrings(r.sdkCovered)
	missingUniq := uniqueStrings(r.sdkMissing)
	fmt.Printf("  Via SDK:          %d unique methods (%d calls)\n", len(coveredUniq), len(r.sdkCovered))
	fmt.Printf("  Via Raw HTTP:     %d unique methods (%d calls)\n", len(missingUniq), len(r.sdkMissing))

	if len(coveredUniq) > 0 {
		fmt.Printf("  SDK-covered:      %s\n", strings.Join(coveredUniq, ", "))
	}
	if len(missingUniq) > 0 {
		fmt.Printf("  Missing from SDK: %s\n", strings.Join(missingUniq, ", "))
	}
	brokenUniq := uniqueStrings(r.sdkBroken)
	if len(brokenUniq) > 0 {
		fmt.Printf("  SDK BROKEN:       %s\n", strings.Join(brokenUniq, ", "))
		fmt.Printf("                    (SDK was tried first, returned bad data/error, fell back to raw HTTP)\n")
	}

	fmt.Println()
	fmt.Println(strings.Repeat("═", 62))
}

// ---------- Utility helpers ----------

func strPtr(s string) *string { return &s }

func int64Ptr(n int64) *int64 { return &n }

func boolPtr(b bool) *bool { return &b }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func uniqueStrings(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// getString extracts a string from a generic map (used for raw HTTP responses only).
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getFloat extracts a float64 from a generic map.
func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

// getMap extracts a sub-map from a generic map.
func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if sub, ok := v.(map[string]interface{}); ok {
			return sub
		}
	}
	return nil
}

// getSlice extracts a slice from a generic map.
func getSlice(m map[string]interface{}, key string) []interface{} {
	if v, ok := m[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			return arr
		}
	}
	return nil
}
