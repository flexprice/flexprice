package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// TargetReport aggregates all journey results for one target.
type TargetReport struct {
	Target   Target
	Results  []*JourneyResult
	Duration time.Duration
}

// Failed reports whether any journey on this target had core failures.
func (tr *TargetReport) Failed() bool {
	for _, r := range tr.Results {
		if r.Failed() {
			return true
		}
	}
	return false
}

// ---------- Console ----------

// PrintJourney renders one journey's step-by-step outcome.
func PrintJourney(r *JourneyResult) {
	passed, failed, skipped, warned, tdFailed := r.Tally()

	fmt.Printf("\n── %s ", r.Journey.Name)
	fmt.Println(strings.Repeat("─", max(0, 56-len(r.Journey.Name))))
	if r.Journey.Description != "" {
		fmt.Printf("   %s\n", r.Journey.Description)
	}
	fmt.Printf("   run_id=%s  tags=%s\n\n", r.RunID, strings.Join(r.Journey.Tags, ","))

	teardownHeaderPrinted := false
	for i, s := range r.Steps {
		if s.Phase == "teardown" && !teardownHeaderPrinted {
			fmt.Println("   teardown:")
			teardownHeaderPrinted = true
		}
		tag := "[PASS]"
		switch {
		case s.Status == StatusSkip:
			tag = "[SKIP]"
		case s.Status == StatusFail:
			tag = "[FAIL]"
		case s.Warned:
			tag = "[WARN]"
		}
		src := ""
		if s.RawHTTP {
			src = "  [RAW HTTP — SDK gap]"
		}
		attempts := ""
		if s.Attempts > 1 {
			attempts = fmt.Sprintf(" (%d polls)", s.Attempts)
		}
		fmt.Printf("%-6s %2d. %-42s %6dms%s%s\n", tag, i+1, s.Name, s.Duration.Milliseconds(), attempts, src)
		if s.Details != "" {
			fmt.Printf("        → %s\n", s.Details)
		}
		if s.Err != nil {
			fmt.Printf("        error: %v\n", s.Err)
		}
		if s.SkipReason != "" {
			fmt.Printf("        reason: %s\n", s.SkipReason)
		}
	}

	verdict := "PASS"
	if failed > 0 {
		verdict = "FAIL"
	}
	fmt.Printf("\n   %s: %d passed, %d failed, %d skipped", verdict, passed, failed, skipped)
	if warned > 0 {
		fmt.Printf(", %d warnings", warned)
	}
	if tdFailed > 0 {
		fmt.Printf(", %d teardown failures", tdFailed)
	}
	fmt.Printf("  (%.1fs)\n", r.Duration.Seconds())
}

// PrintTargetSummary renders the per-target rollup and SDK coverage notes.
func PrintTargetSummary(tr *TargetReport) {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 62))
	fmt.Printf("TARGET %s — %d journeys in %.1fs\n", tr.Target.label(), len(tr.Results), tr.Duration.Seconds())
	fmt.Println(strings.Repeat("═", 62))

	for _, r := range tr.Results {
		passed, failed, skipped, warned, tdFailed := r.Tally()
		status := "PASS"
		if failed > 0 {
			status = "FAIL"
		}
		extra := ""
		if warned > 0 {
			extra += fmt.Sprintf(" | %d warn", warned)
		}
		if tdFailed > 0 {
			extra += fmt.Sprintf(" | %d teardown-fail", tdFailed)
		}
		fmt.Printf("[%s] %-32s %d passed | %d failed | %d skipped%s | %.1fs\n",
			status, r.Journey.Name, passed, failed, skipped, extra, r.Duration.Seconds())
	}

	// SDK coverage: ops exercised vs raw-HTTP gaps.
	used := map[string]bool{}
	gaps := map[string]bool{}
	for _, r := range tr.Results {
		for _, s := range r.Steps {
			if s.Status == StatusSkip {
				continue
			}
			if s.SDKCall != "" {
				used[s.SDKCall] = true
			}
			if s.RawHTTP {
				gaps[s.Name] = true
			}
		}
	}
	fmt.Printf("\nSDK ops exercised: %d", len(used))
	if len(gaps) > 0 {
		fmt.Printf(" | raw-HTTP steps (SDK gaps): %s", strings.Join(sortedKeys(gaps), ", "))
	}
	fmt.Println()
}

// PrintCrossTargetSummary prints the final multi-target rollup.
func PrintCrossTargetSummary(reports []*TargetReport) {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 62))
	fmt.Println("CROSS-TARGET SUMMARY")
	fmt.Println(strings.Repeat("═", 62))
	allPassed := true
	for _, tr := range reports {
		status := "PASS"
		if tr.Failed() {
			status = "FAIL"
			allPassed = false
		}
		fmt.Printf("[%s] %-20s %d journeys | %.1fs\n", status, tr.Target.label(), len(tr.Results), tr.Duration.Seconds())
	}
	fmt.Println()
	if allPassed {
		fmt.Println("ALL TARGETS PASSED ✓")
	} else {
		fmt.Println("ONE OR MORE TARGETS FAILED ✗")
	}
}

// ---------- JSON report ----------

type jsonReport struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Targets     []jsonTargetReport `json:"targets"`
}

type jsonTargetReport struct {
	Target   string            `json:"target"`
	Host     string            `json:"host"`
	Failed   bool              `json:"failed"`
	Duration float64           `json:"duration_seconds"`
	Journeys []jsonJourneyItem `json:"journeys"`
}

type jsonJourneyItem struct {
	Journey  string         `json:"journey"`
	File     string         `json:"file"`
	Tags     []string       `json:"tags"`
	RunID    string         `json:"run_id"`
	Failed   bool           `json:"failed"`
	Duration float64        `json:"duration_seconds"`
	Steps    []jsonStepItem `json:"steps"`
}

type jsonStepItem struct {
	Name       string `json:"name"`
	ID         string `json:"id,omitempty"`
	Phase      string `json:"phase"`
	Status     string `json:"status"`
	Warned     bool   `json:"warned,omitempty"`
	SDKCall    string `json:"sdk_call,omitempty"`
	RawHTTP    bool   `json:"raw_http,omitempty"`
	Error      string `json:"error,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
	Details    string `json:"details,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Attempts   int    `json:"attempts,omitempty"`
}

// WriteJSONReport writes the machine-readable run record.
func WriteJSONReport(path string, reports []*TargetReport) error {
	doc := jsonReport{GeneratedAt: time.Now().UTC()}
	for _, tr := range reports {
		jt := jsonTargetReport{
			Target:   tr.Target.label(),
			Host:     tr.Target.host(),
			Failed:   tr.Failed(),
			Duration: tr.Duration.Seconds(),
		}
		for _, r := range tr.Results {
			jj := jsonJourneyItem{
				Journey: r.Journey.Name, File: r.Journey.File, Tags: r.Journey.Tags,
				RunID: r.RunID, Failed: r.Failed(), Duration: r.Duration.Seconds(),
			}
			for _, s := range r.Steps {
				item := jsonStepItem{
					Name: s.Name, ID: s.ID, Phase: s.Phase, Status: string(s.Status),
					Warned: s.Warned, SDKCall: s.SDKCall, RawHTTP: s.RawHTTP,
					SkipReason: s.SkipReason, Details: s.Details,
					DurationMs: s.Duration.Milliseconds(), Attempts: s.Attempts,
				}
				if s.Err != nil {
					item.Error = s.Err.Error()
				}
				jj.Steps = append(jj.Steps, item)
			}
			jt.Journeys = append(jt.Journeys, jj)
		}
		doc.Targets = append(doc.Targets, jt)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ---------- JUnit ----------

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     float64     `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitMessage `xml:"failure,omitempty"`
	Skipped   *junitMessage `xml:"skipped,omitempty"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
}

// WriteJUnitReport writes a JUnit XML file (one suite per target+journey)
// for CI test visualization.
func WriteJUnitReport(path string, reports []*TargetReport) error {
	var suites junitSuites
	for _, tr := range reports {
		for _, r := range tr.Results {
			suite := junitSuite{
				Name: fmt.Sprintf("%s/%s", tr.Target.label(), r.Journey.Name),
				Time: r.Duration.Seconds(),
			}
			for _, s := range r.Steps {
				c := junitCase{
					Name:      s.Name,
					ClassName: r.Journey.Name + "." + s.Phase,
					Time:      s.Duration.Seconds(),
				}
				suite.Tests++
				switch s.Status {
				case StatusFail:
					suite.Failures++
					msg := "step failed"
					if s.Err != nil {
						msg = s.Err.Error()
					}
					c.Failure = &junitMessage{Message: msg}
				case StatusSkip:
					suite.Skipped++
					c.Skipped = &junitMessage{Message: s.SkipReason}
				}
				suite.Cases = append(suite.Cases, c)
			}
			suites.Suites = append(suites.Suites, suite)
		}
	}
	data, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0o644)
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
