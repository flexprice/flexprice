package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StepStatus is the outcome of one step.
type StepStatus string

const (
	StatusPass StepStatus = "pass"
	StatusFail StepStatus = "fail"
	StatusSkip StepStatus = "skip"
)

// StepResult records one executed (or skipped) step.
type StepResult struct {
	Name       string
	ID         string
	Phase      string // "steps" or "teardown"
	Status     StepStatus
	Warned     bool // optional step that failed (reported, not fatal)
	SkipReason string
	Err        error
	Details    string
	Duration   time.Duration
	SDKCall    string // "Customers.CreateCustomer" for call steps
	RawHTTP    bool   // http: step (counted as an SDK coverage gap)
	Attempts   int    // poll attempts (1 for plain steps)
}

// JourneyResult aggregates a journey run.
type JourneyResult struct {
	Journey  *Journey
	RunID    string
	Steps    []*StepResult
	Duration time.Duration
}

// Tally returns (passed, failed, skipped, warned, teardownFailed).
// "failed" counts core failures only; teardown failures are separate.
func (r *JourneyResult) Tally() (passed, failed, skipped, warned, teardownFailed int) {
	for _, s := range r.Steps {
		switch s.Status {
		case StatusPass:
			passed++
			if s.Warned {
				warned++
			}
		case StatusSkip:
			skipped++
		case StatusFail:
			if s.Phase == "teardown" {
				teardownFailed++
			} else {
				failed++
			}
		}
	}
	return
}

// Failed reports whether the journey had core (non-teardown) failures.
func (r *JourneyResult) Failed() bool {
	_, failed, _, _, _ := r.Tally()
	return failed > 0
}

// Executor runs journeys against one target.
type Executor struct {
	Dispatcher *Dispatcher
	Raw        *RawClient
	TargetName string
	// StepTimeout bounds a single non-polling call (default 2m).
	StepTimeout time.Duration
	// PollTimeout / PollInterval are defaults for until: steps.
	PollTimeout  time.Duration
	PollInterval time.Duration
}

func (e *Executor) stepTimeout() time.Duration {
	if e.StepTimeout > 0 {
		return e.StepTimeout
	}
	return 2 * time.Minute
}

func (e *Executor) pollDefaults() (time.Duration, time.Duration) {
	t, i := e.PollTimeout, e.PollInterval
	if t == 0 {
		t = 60 * time.Second
	}
	if i == 0 {
		i = 3 * time.Second
	}
	return t, i
}

// RunJourney executes all steps then teardown, returning the full result.
func (e *Executor) RunJourney(ctx context.Context, j *Journey) *JourneyResult {
	start := time.Now()
	rc := NewRenderCtx(j.Vars, e.TargetName)
	res := &JourneyResult{Journey: j, RunID: rc.RunID()}

	failed := false
	for _, step := range j.Steps {
		if failed {
			res.Steps = append(res.Steps, &StepResult{
				Name: step.DisplayName(), ID: step.ID, Phase: "steps",
				Status: StatusSkip, SkipReason: "earlier step failed",
				SDKCall: step.Call, RawHTTP: step.HTTP != nil,
			})
			continue
		}
		sr := e.runStep(ctx, rc, step, "steps")
		res.Steps = append(res.Steps, sr)
		if sr.Status == StatusFail && !step.Optional {
			failed = true
		}
		if sr.Status == StatusFail && step.Optional {
			sr.Status = StatusPass
			sr.Warned = true
		}
	}

	// Teardown always runs; steps whose dependencies were never created are
	// skipped (ErrMissingDependency), the rest are attempted independently.
	for _, step := range j.Teardown {
		sr := e.runStep(ctx, rc, step, "teardown")
		if sr.Status == StatusFail && step.Optional {
			sr.Status = StatusPass
			sr.Warned = true
		}
		res.Steps = append(res.Steps, sr)
	}

	res.Duration = time.Since(start)
	return res
}

// runStep executes one step including repeat and polling semantics.
func (e *Executor) runStep(ctx context.Context, rc *RenderCtx, step *Step, phase string) *StepResult {
	sr := &StepResult{
		Name: step.DisplayName(), ID: step.ID, Phase: phase,
		SDKCall: step.Call, RawHTTP: step.HTTP != nil, Attempts: 1,
	}
	start := time.Now()
	defer func() { sr.Duration = time.Since(start) }()

	iterations := step.Repeat
	if iterations <= 0 {
		iterations = 1
	}

	var body any
	var status int
	for iter := 0; iter < iterations; iter++ {
		rc.SetIter(iter)
		var err error
		body, status, err = e.execOnce(ctx, rc, step, sr)
		if err != nil {
			if missing, ok := err.(*ErrMissingDependency); ok {
				sr.Status = StatusSkip
				sr.SkipReason = "dependency not available: " + shortMissingKey(missing.Error())
				return sr
			}
			sr.Status = StatusFail
			sr.Err = err
			return sr
		}
	}

	// Captures from the final response.
	caps := map[string]any{}
	var capErrs []string
	keys := make([]string, 0, len(step.Capture))
	for k := range step.Capture {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := step.Capture[key]
		if path == "$status" {
			caps[key] = status
			continue
		}
		v, found := GetPath(body, path)
		if !found {
			capErrs = append(capErrs, fmt.Sprintf("capture %q: path %q not found (body keys: %s)", key, path, topKeys(body)))
			continue
		}
		caps[key] = v
	}
	if step.ID != "" {
		rc.SetCaptures(step.ID, caps)
	}
	if len(capErrs) > 0 {
		sr.Status = StatusFail
		sr.Err = fmt.Errorf("%s", strings.Join(capErrs, "; "))
		return sr
	}

	sr.Status = StatusPass
	sr.Details = captureSummary(caps, status)
	return sr
}

// execOnce performs one logical execution of the step: a single call, a
// negative test, or a poll-until loop.
func (e *Executor) execOnce(ctx context.Context, rc *RenderCtx, step *Step, sr *StepResult) (any, int, error) {
	// Negative test: the call must fail and the error must match.
	if step.ExpectError != nil {
		body, status, callErr, err := e.invoke(ctx, rc, step)
		if err != nil {
			return nil, 0, err
		}
		if callErr == nil {
			return nil, 0, fmt.Errorf("expected the call to fail (%+v), but it succeeded with status %d (body keys: %s)",
				*step.ExpectError, status, topKeys(body))
		}
		if matchErr := step.ExpectError.MatchError(callErr, status, rc); matchErr != nil {
			return nil, 0, matchErr
		}
		return map[string]any{"error": callErr.Error(), "status": status}, status, nil
	}

	// Poll-until loop.
	if len(step.Until) > 0 {
		defTimeout, defInterval := e.pollDefaults()
		timeout := step.timeoutOr(defTimeout)
		interval := step.intervalOr(defInterval)
		deadline := time.Now().Add(timeout)
		attempts := 0
		var lastErr error
		for {
			attempts++
			sr.Attempts = attempts
			body, status, callErr, err := e.invoke(ctx, rc, step)
			if err != nil {
				return nil, 0, err
			}
			if callErr != nil {
				lastErr = callErr
			} else {
				lastErr = evalAll(step.Until, body, rc)
				if lastErr == nil {
					if err := evalAll(step.Expect, body, rc); err != nil {
						return nil, 0, err
					}
					return body, status, nil
				}
			}
			if time.Now().After(deadline) {
				return nil, 0, fmt.Errorf("poll timed out after %s (%d attempts): %v", timeout, attempts, lastErr)
			}
			select {
			case <-ctx.Done():
				return nil, 0, fmt.Errorf("cancelled while polling: %w", ctx.Err())
			case <-time.After(interval):
			}
		}
	}

	// Plain call.
	body, status, callErr, err := e.invoke(ctx, rc, step)
	if err != nil {
		return nil, 0, err
	}
	if callErr != nil {
		return nil, 0, callErr
	}
	if err := evalAll(step.Expect, body, rc); err != nil {
		return nil, 0, err
	}
	return body, status, nil
}

// invoke renders the step inputs and performs a single SDK or HTTP call.
// Returns (body, status, callError, fatalError) where callError is an API
// failure (matchable by expect_error) and fatalError aborts the step
// (template/arg-construction problems, missing dependencies).
func (e *Executor) invoke(ctx context.Context, rc *RenderCtx, step *Step) (any, int, error, error) {
	callCtx, cancel := context.WithTimeout(ctx, e.stepTimeout())
	defer cancel()

	if step.Call != "" {
		op, err := e.Dispatcher.Resolve(step.Call)
		if err != nil {
			return nil, 0, nil, err
		}
		rawArgs := step.Args
		if step.With != nil {
			rawArgs = []any{step.With}
		}
		rendered := make([]any, len(rawArgs))
		for i, a := range rawArgs {
			r, err := rc.Render(a)
			if err != nil {
				return nil, 0, nil, err
			}
			rendered[i] = r
		}
		body, status, callErr, buildErr := op.Invoke(callCtx, rendered)
		if buildErr != nil {
			return nil, 0, nil, buildErr
		}
		return body, status, callErr, nil
	}

	// http: step
	h := step.HTTP
	pathV, err := rc.RenderString(h.Path)
	if err != nil {
		return nil, 0, nil, err
	}
	query, err := rc.RenderStringMap(h.Query)
	if err != nil {
		return nil, 0, nil, err
	}
	headers, err := rc.RenderStringMap(h.Headers)
	if err != nil {
		return nil, 0, nil, err
	}
	var bodyDoc any
	if h.Body != nil {
		bodyDoc, err = rc.Render(h.Body)
		if err != nil {
			return nil, 0, nil, err
		}
	}
	respBody, status, callErr := e.Raw.Do(callCtx, h.Method, fmt.Sprintf("%v", pathV), query, headers, bodyDoc, h.Status)
	return respBody, status, callErr, nil
}

func evalAll(exps []*Expectation, body any, rc *RenderCtx) error {
	for _, exp := range exps {
		if err := exp.Eval(body, rc); err != nil {
			return err
		}
	}
	return nil
}

func captureSummary(caps map[string]any, status int) string {
	if len(caps) == 0 {
		if status != 0 {
			return fmt.Sprintf("status=%d", status)
		}
		return ""
	}
	keys := make([]string, 0, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, truncateStr(fmt.Sprintf("%v", caps[k]), 48)))
	}
	return strings.Join(parts, ", ")
}

// shortMissingKey trims Go template noise from missing-key errors, leaving
// the useful part: which reference was unavailable.
func shortMissingKey(msg string) string {
	if i := strings.Index(msg, "map has no entry for key"); i >= 0 {
		return strings.TrimSpace(msg[i:])
	}
	return msg
}
