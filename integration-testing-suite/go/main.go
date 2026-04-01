// Flexprice Integration Test Suite
//
// Runs all unit tests in internal/... via `go test -json` and reports results
// per individual test with full error output on failure.
//
// Usage:
//
//	make test-suite
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TestEvent is a single line from `go test -json` output.
type TestEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test,omitempty"`
	Output  string  `json:"Output,omitempty"`
	Elapsed float64 `json:"Elapsed,omitempty"`
}

// TestResult holds the outcome of a single test function.
type TestResult struct {
	Name     string
	Package  string
	Passed   bool
	Err      string
	Duration time.Duration
}

func main() {
	repoRoot := findRepoRoot()
	start := time.Now()

	printBanner()

	// ── Unit Tests ──────────────────────────────────────────────────────────
	printSection("UNIT TESTS", "go test -json -v -race ./internal/...")
	unitResults, buildErr := runUnitTests(repoRoot)

	// ── Summary ──────────────────────────────────────────────────────────────
	printSummary(unitResults, buildErr, time.Since(start))

	// Exit non-zero if anything failed
	anyFailed := buildErr != nil
	for _, r := range unitResults {
		if !r.Passed {
			anyFailed = true
			break
		}
	}
	if anyFailed {
		os.Exit(1)
	}
}

// ── Unit test runner ─────────────────────────────────────────────────────────

func runUnitTests(repoRoot string) ([]TestResult, error) {
	cmd := exec.Command("go", "test", "-json", "-v", "-race", "./internal/...")
	cmd.Dir = repoRoot

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	// Capture stderr (build errors, race detector output)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start go test: %w", err)
	}

	// pending maps testKey -> accumulated output lines (for error display)
	pending := make(map[string][]string)
	var results []TestResult

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024) // 4 MB line buffer

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var ev TestEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Not JSON (e.g. build errors printed to stdout) — pass through
			fmt.Println(line)
			continue
		}

		// Handle package-level events (build/package failures can have no Test name)
		if ev.Test == "" {
			if ev.Action == "build-fail" {
				fmt.Printf("  ❌ BUILD FAILED [%s]\n", shortPkg(ev.Package))
				results = append(results, TestResult{
					Name:    "(build)",
					Package: shortPkg(ev.Package),
					Passed:  false,
					Err:     "package build failed",
				})
			}
			if ev.Action == "fail" {
				fmt.Printf("  ❌ PACKAGE FAILED [%s]\n", shortPkg(ev.Package))
				results = append(results, TestResult{
					Name:    "(package)",
					Package: shortPkg(ev.Package),
					Passed:  false,
					Err:     "package failed (see output above)",
				})
			}
			continue
		}

		key := ev.Package + "|" + ev.Test

		switch ev.Action {
		case "run":
			pending[key] = nil

		case "output":
			// Strip noisy go test header/footer lines; keep error/log lines
			out := strings.TrimRight(ev.Output, "\n")
			if isUsefulOutput(out) {
				pending[key] = append(pending[key], out)
			}

		case "pass":
			d := toDuration(ev.Elapsed)
			fmt.Printf("  ✓  PASS  %-65s %7s   %s\n", ev.Test, formatDur(d), shortPkg(ev.Package))
			results = append(results, TestResult{
				Name:     ev.Test,
				Package:  shortPkg(ev.Package),
				Passed:   true,
				Duration: d,
			})
			delete(pending, key)

		case "fail":
			d := toDuration(ev.Elapsed)
			errLines := pending[key]
			errMsg := strings.Join(errLines, "\n")
			fmt.Printf("  ❌ FAIL  %-65s %7s   %s\n", ev.Test, formatDur(d), shortPkg(ev.Package))
			for _, l := range errLines {
				fmt.Printf("       %s\n", l)
			}
			results = append(results, TestResult{
				Name:     ev.Test,
				Package:  shortPkg(ev.Package),
				Passed:   false,
				Err:      errMsg,
				Duration: d,
			})
			delete(pending, key)

		case "skip":
			d := toDuration(ev.Elapsed)
			fmt.Printf("  ⊘  SKIP  %-65s %7s   %s\n", ev.Test, formatDur(d), shortPkg(ev.Package))
			delete(pending, key)
		}
	}

	waitErr := cmd.Wait()

	// If there was stderr output (build errors, race output), show it
	if stderrBuf.Len() > 0 {
		fmt.Fprintln(os.Stderr, "\n--- stderr ---")
		fmt.Fprint(os.Stderr, stderrBuf.String())
	}

	if waitErr != nil {
		fmt.Printf("  ❌ go test command failed: %v\n", waitErr)
		return results, waitErr
	}

	return results, nil
}

// isUsefulOutput returns true for lines worth showing in error context.
func isUsefulOutput(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	skip := []string{
		"=== RUN", "=== PAUSE", "=== CONT",
		"--- PASS", "--- FAIL", "--- SKIP",
		"PASS", "FAIL", "ok  ", "?   ",
	}
	for _, prefix := range skip {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	return true
}

// ── Summary ───────────────────────────────────────────────────────────────────

func printSummary(results []TestResult, buildErr error, total time.Duration) {
	passed, failed := 0, 0
	var failures []TestResult
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
			failures = append(failures, r)
		}
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("  SUMMARY")
	fmt.Println("========================================")

	if buildErr != nil {
		fmt.Printf("  Unit Tests:  ❌ BUILD FAILED\n")
	} else {
		fmt.Printf("  Unit Tests:  %d passed  |  %d failed\n", passed, failed)
	}

	if len(failures) > 0 {
		fmt.Println()
		fmt.Println("  FAILED TESTS:")
		for _, r := range failures {
			fmt.Printf("    ❌ FAIL  %s  [%s]\n", r.Name, r.Package)
			if r.Err != "" {
				for _, line := range strings.Split(r.Err, "\n") {
					if strings.TrimSpace(line) != "" {
						fmt.Printf("         %s\n", line)
					}
				}
			}
		}
	}

	fmt.Printf("\n  Total: %d tests  |  %d ✓  |  %d ❌  |  %s\n",
		passed+failed, passed, failed, formatDur(total))
	fmt.Println("========================================")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// findRepoRoot walks up from cwd looking for the main go.mod.
func findRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("getwd: %v", err))
	}
	dir := wd
	for {
		if isRepoRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: assume we're running from integration-testing-suite/go/
	return filepath.Join(wd, "..", "..")
}

func isRepoRoot(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/flexprice/flexprice\n")
}

// shortPkg trims the module prefix from a package path.
func shortPkg(pkg string) string {
	if i := strings.Index(pkg, "/internal/"); i >= 0 {
		return pkg[i+1:]
	}
	return pkg
}

func toDuration(elapsed float64) time.Duration {
	return time.Duration(elapsed * float64(time.Second))
}

func formatDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func printBanner() {
	fmt.Println("========================================")
	fmt.Println("  Flexprice Integration Test Suite")
	fmt.Println("========================================")
}

func printSection(title, detail string) {
	fmt.Printf("\n▶ %s\n", title)
	if detail != "" {
		fmt.Printf("  %s\n", detail)
	}
	fmt.Println(strings.Repeat("─", 72))
}
