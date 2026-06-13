package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Journey is one self-contained customer workflow: a sequence of SDK calls
// with captures, assertions, and teardown. Journeys are independent of each
// other and safe to run in parallel.
type Journey struct {
	Name        string         `yaml:"journey"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Vars        map[string]any `yaml:"vars"`
	Steps       []*Step        `yaml:"steps"`
	Teardown    []*Step        `yaml:"teardown"`

	// File is the path the journey was loaded from (set by the loader).
	File string `yaml:"-"`
}

// Step is a single action within a journey. Exactly one of Call or HTTP must
// be set.
type Step struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`

	// Call invokes an SDK operation, e.g. "Customers.CreateCustomer".
	Call string `yaml:"call"`
	// With is the single argument for the call (struct fields as a map, or a
	// scalar for id-style params). Sugar for Args with one element.
	With any `yaml:"with"`
	// Args supplies all arguments positionally for multi-arg operations.
	// Use null for optional pointer params you want to omit.
	Args []any `yaml:"args"`

	// HTTP performs a raw HTTP request for endpoints not exposed by the SDK.
	HTTP *HTTPRequest `yaml:"http"`

	// Capture extracts values from the response body into .steps.<id>.<key>.
	// Values are dotted paths ("subscription.id", "items.0.id"). The special
	// path "$status" captures the HTTP status code.
	Capture map[string]string `yaml:"capture"`

	// Expect asserts on the response body after the call succeeds.
	Expect []*Expectation `yaml:"expect"`

	// ExpectError makes this a negative test: the call MUST fail, and the
	// error must match. The step fails if the call succeeds.
	ExpectError *ErrorExpectation `yaml:"expect_error"`

	// Until turns the step into a poll: the call is retried every Interval
	// until all Until expectations pass or Timeout elapses.
	Until    []*Expectation `yaml:"until"`
	Timeout  string         `yaml:"timeout"`
	Interval string         `yaml:"interval"`

	// Repeat re-executes the step N times ({{ .iter }} is the 0-based index).
	Repeat int `yaml:"repeat"`

	// Optional downgrades a failure to a warning: reported, but the journey
	// continues and the run still exits 0.
	Optional bool `yaml:"optional"`
}

// HTTPRequest describes a raw HTTP step.
type HTTPRequest struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Query   map[string]string `yaml:"query"`
	Headers map[string]string `yaml:"headers"`
	Body    any               `yaml:"body"`
	// Status is the expected status code; 0 means "any 2xx/3xx".
	Status int `yaml:"status"`
}

// Expectation is one declarative assertion against a dotted path in the
// response body. Exactly one operator should be set.
type Expectation struct {
	Path string `yaml:"path"`

	Equals    any     `yaml:"equals"`
	NotEquals any     `yaml:"not_equals"`
	Exists    *bool   `yaml:"exists"`
	Contains  any     `yaml:"contains"`
	NotEmpty  *bool   `yaml:"not_empty"`
	Matches   string  `yaml:"matches"`
	Gt        any     `yaml:"gt"`
	Gte       any     `yaml:"gte"`
	Lt        any     `yaml:"lt"`
	Lte       any     `yaml:"lte"`
	LenEq     *int    `yaml:"len_eq"`
	LenGte    *int    `yaml:"len_gte"`
	Approx    *Approx `yaml:"approx"`
	// AnyEq / AnyGt apply to wildcard paths (items.*.field): pass when at
	// least one element matches.
	AnyEq any `yaml:"any_eq"`
	AnyGt any `yaml:"any_gt"`
}

// Approx asserts numeric closeness, for billing amounts subject to rounding.
type Approx struct {
	Value   any     `yaml:"value"`
	Epsilon float64 `yaml:"epsilon"`
}

// ErrorExpectation matches an expected failure.
type ErrorExpectation struct {
	Contains string `yaml:"contains"`
	Status   int    `yaml:"status"`
}

// DisplayName returns the human-readable step label.
func (s *Step) DisplayName() string {
	if s.Name != "" {
		return s.Name
	}
	if s.ID != "" {
		return s.ID
	}
	if s.Call != "" {
		return s.Call
	}
	if s.HTTP != nil {
		return fmt.Sprintf("%s %s", s.HTTP.Method, s.HTTP.Path)
	}
	return "(unnamed)"
}

// timeoutOr parses the step timeout, with a default.
func (s *Step) timeoutOr(d time.Duration) time.Duration {
	return parseDurationOr(s.Timeout, d)
}

// intervalOr parses the poll interval, with a default.
func (s *Step) intervalOr(d time.Duration) time.Duration {
	return parseDurationOr(s.Interval, d)
}

func parseDurationOr(s string, d time.Duration) time.Duration {
	if s == "" {
		return d
	}
	if parsed, err := time.ParseDuration(s); err == nil && parsed > 0 {
		return parsed
	}
	return d
}

// HasTag reports whether the journey carries the given tag.
func (j *Journey) HasTag(tag string) bool {
	for _, t := range j.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// ---------- Loading ----------

// LoadJourneys reads every *.yaml/*.yml under dir (recursively), sorted by
// path for deterministic ordering.
func LoadJourneys(dir string) ([]*Journey, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan journeys dir %s: %w", dir, err)
	}
	sort.Strings(files)

	var journeys []*Journey
	seen := map[string]string{}
	for _, f := range files {
		j, err := loadJourneyFile(f)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[j.Name]; dup {
			return nil, fmt.Errorf("duplicate journey name %q in %s (already defined in %s)", j.Name, f, prev)
		}
		seen[j.Name] = f
		journeys = append(journeys, j)
	}
	return journeys, nil
}

func loadJourneyFile(path string) (*Journey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var j Journey
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&j); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	j.File = path
	if j.Name == "" {
		return nil, fmt.Errorf("%s: missing required field 'journey' (the journey name)", path)
	}
	return &j, nil
}

// FilterJourneys applies name and tag filters. Empty filters select all.
func FilterJourneys(journeys []*Journey, names, tags []string) []*Journey {
	var out []*Journey
	for _, j := range journeys {
		if len(names) > 0 && !containsString(names, j.Name) {
			continue
		}
		if len(tags) > 0 {
			match := false
			for _, t := range tags {
				if j.HasTag(t) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, j)
	}
	return out
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ---------- Static validation ----------

// stepRefRe finds {{ ... .steps.<id> ... }} references inside templates.
var stepRefRe = regexp.MustCompile(`\.steps\.([A-Za-z0-9_]+)`)

// ValidateJourney performs static (no-network) validation: structure, call
// resolution against the compiled-in SDK, arg shapes, template syntax, and
// step-reference ordering. Returns all problems found.
func ValidateJourney(j *Journey, dispatcher *Dispatcher) []error {
	var errs []error
	addf := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if len(j.Steps) == 0 {
		addf("%s: journey has no steps", j.Name)
	}

	declared := map[string]bool{}
	validateStep := func(s *Step, where string, teardown bool) {
		label := fmt.Sprintf("%s: %s step %q", j.Name, where, s.DisplayName())

		// Exactly one action.
		hasCall := s.Call != ""
		hasHTTP := s.HTTP != nil
		if hasCall == hasHTTP {
			addf("%s: must set exactly one of 'call' or 'http'", label)
			return
		}
		if s.With != nil && len(s.Args) > 0 {
			addf("%s: set 'with' or 'args', not both", label)
		}
		if s.ExpectError != nil && (len(s.Expect) > 0 || len(s.Until) > 0) {
			addf("%s: 'expect_error' cannot be combined with 'expect'/'until'", label)
		}
		if s.Repeat < 0 {
			addf("%s: 'repeat' must be >= 0", label)
		}
		if len(s.Capture) > 0 && s.ID == "" {
			addf("%s: 'capture' requires an 'id' — captured values are only reachable via .steps.<id>.<name>", label)
		}
		for _, d := range []struct{ field, val string }{{"timeout", s.Timeout}, {"interval", s.Interval}} {
			if d.val == "" {
				continue
			}
			if parsed, err := time.ParseDuration(d.val); err != nil || parsed <= 0 {
				addf("%s: %s %q is not a positive Go duration (e.g. 30s, 2m)", label, d.field, d.val)
			}
		}

		// Resolve the SDK call and check arg shapes.
		if hasCall {
			op, err := dispatcher.Resolve(s.Call)
			if err != nil {
				addf("%s: %v", label, err)
			} else {
				args := s.Args
				if s.With != nil {
					args = []any{s.With}
				}
				if err := op.CheckArgs(args); err != nil {
					addf("%s: %v", label, err)
				}
			}
		}
		if hasHTTP {
			if s.HTTP.Method == "" || s.HTTP.Path == "" {
				addf("%s: http step requires 'method' and 'path'", label)
			}
		}

		// Expectation operators well-formed.
		for i, e := range append(append([]*Expectation{}, s.Expect...), s.Until...) {
			if err := e.Validate(); err != nil {
				addf("%s: expectation #%d: %v", label, i+1, err)
			}
		}

		// Template syntax + step references resolve to earlier steps.
		walkStrings(stepAsDoc(s), func(str string) {
			if _, err := parseTemplate(str); err != nil {
				addf("%s: template error in %q: %v", label, truncateStr(str, 60), err)
			}
			for _, m := range stepRefRe.FindAllStringSubmatch(str, -1) {
				ref := m[1]
				if !declared[ref] {
					if teardown {
						// Teardown may reference any step in the journey.
						return
					}
					addf("%s: references .steps.%s which is not declared by an earlier step", label, ref)
				}
			}
		})
	}

	ids := map[string]bool{}
	for _, s := range j.Steps {
		if s.ID != "" {
			if ids[s.ID] {
				addf("%s: duplicate step id %q", j.Name, s.ID)
			}
			ids[s.ID] = true
		}
		validateStep(s, "steps", false)
		if s.ID != "" {
			declared[s.ID] = true
		}
	}
	// All step ids are in `declared` now, so teardown steps may reference any
	// of them (and additionally skip the ordering check via the teardown flag).
	for _, s := range j.Teardown {
		validateStep(s, "teardown", true)
	}
	return errs
}

// stepAsDoc converts a step to a generic document for template scanning.
func stepAsDoc(s *Step) any {
	raw, err := yaml.Marshal(s)
	if err != nil {
		return nil
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return doc
}

// walkStrings visits every string in a nested document.
func walkStrings(doc any, fn func(string)) {
	switch v := doc.(type) {
	case string:
		fn(v)
	case map[string]any:
		for _, val := range v {
			walkStrings(val, fn)
		}
	case []any:
		for _, val := range v {
			walkStrings(val, fn)
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
