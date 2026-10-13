package checks

import (
	"strings"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

// knownIssueLegs lists, per gateway, legs that fail on an unfixed product bug.
// They are skipped unless E2EPROBE_PAYMENTS_ASSERT_KNOWN_ISSUES is set; drop an
// entry once its fix ships.
var knownIssueLegs = map[string]map[string]string{
	"chargebee": {
		"refund":  "refunds to the card fall back to the wallet while invoice sync is on",
		"decline": "a declined card returns a 500 instead of a 4xx",
	},
}

// legResults runs a probe's legs independently, so one failing leg does not hide
// the others, and folds every failure into a single report.
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

// err returns nil when every leg passed. Otherwise the first failure's attributes
// lead, and failing legs are listed grouped by their error.
func (l *legResults) err(f *paymentFlow) error {
	if len(l.errs) == 0 {
		return nil
	}
	attrs := map[string]string{}
	for k, v := range e2eprobe.AttributesFrom(l.errs[0]) {
		attrs[k] = v
	}
	attrs["failed_legs"] = strings.Join(l.failed, ",")
	var msgs []string
	legsByMsg := map[string][]string{}
	for i, err := range l.errs {
		if step := e2eprobe.AttributesFrom(err)["step"]; step != "" {
			attrs["leg."+l.failed[i]+".step"] = step
		}
		msg := e2eprobe.Brief(err)
		if _, seen := legsByMsg[msg]; !seen {
			msgs = append(msgs, msg)
		}
		legsByMsg[msg] = append(legsByMsg[msg], l.failed[i])
	}
	lines := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		lines = append(lines, strings.Join(legsByMsg[msg], ", ")+": "+msg)
	}
	if attrs["provider"] == "" {
		attrs["provider"] = f.provider()
	}
	return e2eprobe.Errorf(attrs, "%s", strings.Join(lines, "\n"))
}
