// Package e2eprobe is the shared vocabulary of the synthetic monitor: what a
// scenario is for, which traits a persona carries and what one run reports.
package e2eprobe

import (
	"fmt"
	"time"
)

// Kind classifies a scenario for routing and the coverage matrix.
type Kind string

const (
	KindPreflight   Kind = "preflight"
	KindProbe       Kind = "probe"
	KindFlow        Kind = "flow"
	KindMaintenance Kind = "maintenance"
)

// Trait is a capability a persona carries; scenarios bind to personas by trait.
type Trait string

const (
	TraitSubscription  Trait = "subscription"
	TraitEvents        Trait = "events"
	TraitTax           Trait = "tax"
	TraitCoupon        Trait = "coupon"
	TraitCommitment    Trait = "commitment"
	TraitWallet        Trait = "wallet"
	TraitAlerts        Trait = "alerts"
	TraitSoftLimit     Trait = "soft_limit"
	TraitAdditiveGrant Trait = "additive_grant"
	TraitBucketed      Trait = "bucketed"
	TraitMultiCadence  Trait = "multi_cadence"
	TraitPayment       Trait = "payment"
)

var allTraits = []Trait{
	TraitSubscription,
	TraitEvents,
	TraitTax,
	TraitCoupon,
	TraitCommitment,
	TraitWallet,
	TraitAlerts,
	TraitSoftLimit,
	TraitAdditiveGrant,
	TraitBucketed,
	TraitMultiCadence,
	TraitPayment,
}

// Traits lists every known trait in catalog order.
func Traits() []Trait {
	return append([]Trait(nil), allTraits...)
}

func (t Trait) Valid() bool {
	for _, known := range allTraits {
		if t == known {
			return true
		}
	}
	return false
}

// Status is the outcome of one scenario run.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Result is what one scenario run reports. Infra marks a failure caused by the
// API being unreachable or erroring, as opposed to a wrong answer.
type Result struct {
	Status     Status            `json:"status"`
	Step       string            `json:"step,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Mismatches []string          `json:"mismatches,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
	Infra      bool              `json:"infra,omitempty"`
	Duration   time.Duration     `json:"duration,omitempty"`
	Err        error             `json:"-"`
}

func Pass() Result {
	return Result{Status: StatusPass}
}

func Skip(step, reason string) Result {
	return Result{Status: StatusSkip, Step: step, Reason: reason}
}

// Fail reports an error at step. Attributes carried by the error chain are
// merged under attrs, so a caller's value wins on conflict.
func Fail(step string, err error, attrs map[string]string) Result {
	merged := map[string]string{}
	for k, v := range AttributesFrom(err) {
		merged[k] = v
	}
	for k, v := range attrs {
		merged[k] = v
	}
	reason := ""
	if err != nil {
		reason = Brief(err)
	}
	return Result{
		Status: StatusFail,
		Step:   step,
		Reason: reason,
		Attrs:  merged,
		Infra:  IsInfra(err),
		Err:    err,
	}
}

// Mismatch reports assertion failures at step; the first line is the reason.
func Mismatch(step string, mismatches []string, attrs map[string]string) Result {
	r := Result{
		Status:     StatusFail,
		Step:       step,
		Mismatches: append([]string(nil), mismatches...),
		Attrs:      attrs,
	}
	if len(mismatches) > 0 {
		r.Reason = mismatches[0]
	}
	if r.Attrs == nil {
		r.Attrs = map[string]string{}
	}
	return r
}

func (r Result) Failed() bool {
	return r.Status == StatusFail
}

// Summary renders the result on one line for logs and alerts.
func (r Result) Summary() string {
	switch r.Status {
	case StatusPass:
		return "pass"
	case StatusSkip:
		return fmt.Sprintf("skip %s: %s", r.Step, r.Reason)
	}
	s := fmt.Sprintf("%s: %s", r.Step, r.Reason)
	if extra := len(r.Mismatches) - 1; extra > 0 {
		s += fmt.Sprintf(" (+%d more)", extra)
	}
	return s
}
