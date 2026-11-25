// changedlines reads `file:line:col: message` diagnostics on stdin and keeps
// the ones on lines added or edited since the merge base with -base, plus
// every line of untracked files. It exits 1 when anything is kept.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	diagnosticPattern = regexp.MustCompile(`^([^:\s]+):(\d+):\d+: (.*)$`)
	fileHeaderPattern = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
	hunkPattern       = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
)

type lineRange struct {
	start int
	end   int
}

type changes struct {
	ranges    map[string][]lineRange
	untracked map[string]bool
}

func (c changes) contains(path string, line int) bool {
	if c.untracked[path] {
		return true
	}
	for _, r := range c.ranges[path] {
		if line >= r.start && line <= r.end {
			return true
		}
	}
	return false
}

func main() {
	base := flag.String("base", "origin/develop", "ref the branch is compared against")
	flag.Parse()

	changed, err := changesSince(*base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "changedlines:", err)
		os.Exit(2)
	}

	cwd, _ := os.Getwd()
	kept := 0
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "warning:") || strings.TrimSpace(line) == "" {
			continue
		}
		match := diagnosticPattern.FindStringSubmatch(line)
		if match == nil {
			// Build failures and tool errors are not diagnostics; never hide them.
			fmt.Println(line)
			kept++
			continue
		}
		lineNo, _ := strconv.Atoi(match[2])
		if changed.contains(relative(cwd, match[1]), lineNo) {
			fmt.Println(line)
			kept++
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "changedlines: reading diagnostics:", err)
		os.Exit(2)
	}
	if kept > 0 {
		fmt.Fprintf(os.Stderr, "%d finding(s) on lines changed since %s\n", kept, *base)
		os.Exit(1)
	}
}

func changesSince(base string) (changes, error) {
	mergeBase, err := git("merge-base", base, "HEAD")
	if err != nil {
		return changes{}, fmt.Errorf("merge-base with %s: %w", base, err)
	}
	// Explicit prefixes and unquoted paths keep the output parseable whatever
	// diff.noprefix, diff.mnemonicPrefix or core.quotePath the developer set.
	// Paths with spaces or quotes are out of scope: Go files here are snake_case
	// and go vet's own file:line:col output could not carry them either.
	diff, err := git("-c", "core.quotePath=false", "diff", "-U0", "--no-color", "--no-ext-diff",
		"--src-prefix=a/", "--dst-prefix=b/", strings.TrimSpace(mergeBase))
	if err != nil {
		return changes{}, fmt.Errorf("diff: %w", err)
	}
	untracked, err := git("-c", "core.quotePath=false", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return changes{}, fmt.Errorf("ls-files: %w", err)
	}

	result := changes{ranges: map[string][]lineRange{}, untracked: map[string]bool{}}
	current := ""
	for _, line := range strings.Split(diff, "\n") {
		if m := fileHeaderPattern.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := hunkPattern.FindStringSubmatch(line)
		if m == nil || current == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		if count == 0 {
			continue
		}
		result.ranges[current] = append(result.ranges[current], lineRange{start: start, end: start + count - 1})
	}
	for _, path := range strings.Split(strings.TrimSpace(untracked), "\n") {
		if path != "" {
			result.untracked[path] = true
		}
	}
	return result, nil
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// relative maps a diagnostic path onto the repo-relative form git diff prints.
func relative(cwd, path string) string {
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(cwd, path); err == nil {
			path = rel
		}
	}
	return filepath.ToSlash(strings.TrimPrefix(path, "./"))
}
