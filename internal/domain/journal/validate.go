package journal

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// ValidateEntry checks known accounts, positive amounts, a debit and a credit, and Σ debit = Σ credit.
// e.g. Dr AR 118 / Cr deferred 100, Cr tax 18 is valid.
func ValidateEntry(lines []*Line) error {
	debit, credit := decimal.Zero, decimal.Zero
	var debits, credits int
	for _, l := range lines {
		if l.Account().Class() == "" {
			return ierr.NewErrorf("unknown ledger account %q", l.Account()).
				WithHint("Journal line account must be a known ledger account").
				Mark(ierr.ErrValidation)
		}
		if !l.Amount().IsPositive() {
			return ierr.NewErrorf("journal line amount %s on %s is not positive", l.Amount(), l.Account()).
				WithHint("Journal line amounts must be greater than zero").
				Mark(ierr.ErrValidation)
		}
		switch l.Direction() {
		case types.JournalDirectionDebit:
			debit = debit.Add(l.Amount())
			debits++
		case types.JournalDirectionCredit:
			credit = credit.Add(l.Amount())
			credits++
		default:
			return ierr.NewErrorf("invalid journal direction %q", l.Direction()).
				WithHint("Journal line direction must be debit or credit").
				Mark(ierr.ErrValidation)
		}
	}
	if debits == 0 || credits == 0 {
		return ierr.NewError("journal entry needs at least one debit and one credit").
			WithHint("Journal entry must have both debit and credit lines").
			Mark(ierr.ErrValidation)
	}
	if !debit.Equal(credit) {
		return ierr.NewErrorf("journal entry is unbalanced: debit %s, credit %s", debit, credit).
			WithHint("Journal entry debits must equal credits").
			Mark(ierr.ErrValidation)
	}
	return nil
}
