package scenarios

import (
	"strings"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
)

// knownIssueLegs lists, per gateway, legs that fail on an unfixed product bug; they
// skip unless the persona sets assert_known_issues. Drop an entry once its fix ships.
var knownIssueLegs = map[string]map[string]string{
	"chargebee": {
		"refund":  "refunds to the card fall back to the wallet while invoice sync is on",
		"decline": "a declined card returns a 500 instead of a 4xx",
	},
}

// legResults runs a scenario's legs independently, so one failing leg does not
// hide the others, and folds every failure into a single result.
type legResults struct {
	failed []string
	errs   []error
}

func (l *legResults) run(leg string, fn func() error) {
	if err := fn(); err != nil {
		l.failed = append(l.failed, leg)
		l.errs = append(l.errs, err)
	}
}

// runIf runs the leg only when its prerequisite succeeded; otherwise it is skipped.
func (l *legResults) runIf(ok bool, leg string, fn func() error) {
	if !ok {
		return
	}
	l.run(leg, fn)
}

// runKnown is runIf for a leg that skips while knownIssue names an unfixed bug.
func (l *legResults) runKnown(knownIssue string, ok bool, leg string, fn func() error) {
	l.runIf(knownIssue == "" && ok, leg, fn)
}

// result passes when every leg did. Otherwise the first failure's attributes
// lead, and failing legs become mismatch lines grouped by their error.
func (l *legResults) result(sc *scenario.Context, step string) e2eprobe.Result {
	if len(l.errs) == 0 {
		return sc.Result(step)
	}
	for k, v := range e2eprobe.AttributesFrom(l.errs[0]) {
		sc.Attr(k, v)
	}
	sc.Attr("failed_legs", strings.Join(l.failed, ","))
	var msgs []string
	legsByMsg := map[string][]string{}
	for i, err := range l.errs {
		if s := e2eprobe.AttributesFrom(err)["step"]; s != "" {
			sc.Attr("leg."+l.failed[i]+".step", s)
		}
		msg := e2eprobe.Brief(err)
		if _, seen := legsByMsg[msg]; !seen {
			msgs = append(msgs, msg)
		}
		legsByMsg[msg] = append(legsByMsg[msg], l.failed[i])
	}
	for _, msg := range msgs {
		sc.Truth(strings.Join(legsByMsg[msg], ", ")+": "+msg, false)
	}
	return sc.Result(step)
}

// stepFrom is the step a flow error was raised at, or fallback for a plain error.
func stepFrom(err error, fallback string) string {
	if s := e2eprobe.AttributesFrom(err)["step"]; s != "" {
		return s
	}
	return fallback
}
