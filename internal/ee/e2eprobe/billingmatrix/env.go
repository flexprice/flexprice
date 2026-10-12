package billingmatrix

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// moneyTolerance absorbs per-line rounding: the API rounds each line to cents.
var moneyTolerance = decimal.RequireFromString("0.01")

// env is one scenario run: the API, shared fixtures, and the mismatches found so far.
type env struct {
	api        API
	fx         *fixtures
	runID      string
	scenario   string
	track      *tracker
	mismatches []string
}

func (e *env) fail(format string, args ...any) {
	e.mismatches = append(e.mismatches, fmt.Sprintf(format, args...))
}

func (e *env) money(label string, want, got decimal.Decimal) { e.moneyLines(label, want, got, 1) }

// moneyLines compares a total built from lines invoice lines, each rounded to cents on its own.
func (e *env) moneyLines(label string, want, got decimal.Decimal, lines int) {
	if money(want).Sub(money(got)).Abs().GreaterThan(moneyTolerance.Mul(decimal.NewFromInt(int64(max(lines, 1))))) {
		e.fail("%s: want %s, got %s", label, money(want).StringFixed(2), money(got).StringFixed(2))
	}
}

// quantity compares at full precision with a tiny tolerance, as entitlement quotas are not rounded.
func (e *env) quantity(label string, want, got decimal.Decimal) {
	if want.Sub(got).Abs().GreaterThan(decimal.RequireFromString("0.0001")) {
		e.fail("%s: want %s, got %s", label, want.String(), got.String())
	}
}

func (e *env) instant(label string, want, got time.Time) {
	if !want.Equal(got) {
		e.fail("%s: want %s, got %s", label, want.UTC().Format(time.RFC3339), got.UTC().Format(time.RFC3339))
	}
}

func (e *env) count(label string, want, got int) {
	if want != got {
		e.fail("%s: want %d, got %d", label, want, got)
	}
}

func (e *env) truth(label string, ok bool) {
	if !ok {
		e.fail("%s", label)
	}
}
