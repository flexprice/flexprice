package checks

import (
	"fmt"
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
	failed  []string
	errs    []error
	skipped []string
}

func (l *legResults) run(leg string, fn func() error) {
	if err := fn(); err != nil {
		l.failed = append(l.failed, leg)
		l.errs = append(l.errs, err)
	}
}

// runIf runs the leg only when its prerequisite succeeded; otherwise it is skipped.
func (l *legResults) runIf(ok bool, leg, needs string, fn func() error) {
	if !ok {
		l.skipped = append(l.skipped, fmt.Sprintf("%s (needs %s)", leg, needs))
		return
	}
	l.run(leg, fn)
}

// runKnown is runIf for a leg that skips while knownIssue names an unfixed bug.
func (l *legResults) runKnown(knownIssue string, ok bool, leg, needs string, fn func() error) {
	if knownIssue != "" {
		l.skipped = append(l.skipped, fmt.Sprintf("%s (known issue: %s)", leg, knownIssue))
		return
	}
	l.runIf(ok, leg, needs, fn)
}

// err returns nil when every leg passed. Otherwise the first failure's attributes
// lead, and each failing leg's step and message are listed alongside.
func (l *legResults) err(f *paymentFlow) error {
	if len(l.errs) == 0 {
		return nil
	}
	attrs := map[string]string{}
	for k, v := range e2eprobe.AttributesFrom(l.errs[0]) {
		attrs[k] = v
	}
	attrs["failed_legs"] = strings.Join(l.failed, ",")
	if len(l.skipped) > 0 {
		attrs["skipped_legs"] = strings.Join(l.skipped, ",")
	}
	msgs := make([]string, 0, len(l.errs))
	for i, err := range l.errs {
		if step := e2eprobe.AttributesFrom(err)["step"]; step != "" {
			attrs["leg."+l.failed[i]+".step"] = step
		}
		msgs = append(msgs, fmt.Sprintf("%s: %v", l.failed[i], err))
	}
	if attrs["provider"] == "" {
		attrs["provider"] = f.provider()
	}
	return e2eprobe.Errorf(attrs, "%d leg(s) failed: %s", len(l.errs), strings.Join(msgs, "; "))
}
