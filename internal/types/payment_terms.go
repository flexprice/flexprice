package types

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// PaymentTerms sets when an invoice falls due, counted from its issue date.
// @Description Deprecated: use payment_terms_type and payment_terms_days. "<days> NET" (days 1–365), DUE_ON_RECEIPT, END_OF_MONTH or END_OF_NEXT_MONTH.
type PaymentTerms string

const (
	PaymentTerms15Net PaymentTerms = "15 NET"
	PaymentTerms30Net PaymentTerms = "30 NET"
	PaymentTerms45Net PaymentTerms = "45 NET"
	PaymentTerms60Net PaymentTerms = "60 NET"
	PaymentTerms75Net PaymentTerms = "75 NET"
	PaymentTerms90Net PaymentTerms = "90 NET"

	PaymentTermsDueOnReceipt   PaymentTerms = "DUE_ON_RECEIPT"
	PaymentTermsEndOfMonth     PaymentTerms = "END_OF_MONTH"
	PaymentTermsEndOfNextMonth PaymentTerms = "END_OF_NEXT_MONTH"
)

const (
	paymentTermsNetSuffix  = " NET"
	paymentTermsMinNetDays = 1
	paymentTermsMaxNetDays = 365
)

func (p PaymentTerms) String() string {
	return string(p)
}

// Validate returns an error if the payment terms value is not allowed.
func (p PaymentTerms) Validate() error {
	if p == "" || p == PaymentTermsEndOfMonth || p == PaymentTermsEndOfNextMonth {
		return nil
	}
	if _, ok := PaymentTermsToDueDateDays(p); ok {
		return nil
	}
	return ierr.NewError("invalid payment_terms").
		WithHint(fmt.Sprintf("Payment terms must be \"<days> NET\" with days from %d to %d (e.g. \"45 NET\"), DUE_ON_RECEIPT, END_OF_MONTH or END_OF_NEXT_MONTH", paymentTermsMinNetDays, paymentTermsMaxNetDays)).
		WithReportableDetails(map[string]any{
			"payment_terms": p,
		}).
		Mark(ierr.ErrValidation)
}

// PaymentTermsToDueDateDays returns the number of days after the issue date for the given payment terms.
// Returns (0, false) for end of month terms and for invalid or empty values.
func PaymentTermsToDueDateDays(p PaymentTerms) (int, bool) {
	if p == PaymentTermsDueOnReceipt {
		return 0, true
	}
	digits, ok := strings.CutSuffix(string(p), paymentTermsNetSuffix)
	if !ok {
		return 0, false
	}
	days, err := strconv.Atoi(digits)
	// Rejects non-canonical forms such as "07 NET".
	if err != nil || strconv.Itoa(days) != digits || days < paymentTermsMinNetDays || days > paymentTermsMaxNetDays {
		return 0, false
	}
	return days, true
}

// PaymentTermsType is the kind of payment terms; DAYS takes a day count.
// @Description DAYS (due payment_terms_days after the issue date), DUE_ON_RECEIPT, END_OF_MONTH or END_OF_NEXT_MONTH.
type PaymentTermsType string

const (
	PaymentTermsTypeDays           PaymentTermsType = "DAYS"
	PaymentTermsTypeDueOnReceipt   PaymentTermsType = "DUE_ON_RECEIPT"
	PaymentTermsTypeEndOfMonth     PaymentTermsType = "END_OF_MONTH"
	PaymentTermsTypeEndOfNextMonth PaymentTermsType = "END_OF_NEXT_MONTH"
)

// NewPaymentTerms builds payment terms from a type and, for DAYS only, a day count.
func NewPaymentTerms(termsType PaymentTermsType, days *int) (PaymentTerms, error) {
	switch termsType {
	case PaymentTermsTypeDays:
		terms := PaymentTerms(fmt.Sprintf("%d%s", lo.FromPtr(days), paymentTermsNetSuffix))
		if _, ok := PaymentTermsToDueDateDays(terms); days == nil || !ok {
			return "", ierr.NewError("invalid payment_terms_days").
				WithHint(fmt.Sprintf("payment_terms_days must be from %d to %d when payment_terms_type is DAYS", paymentTermsMinNetDays, paymentTermsMaxNetDays)).
				WithReportableDetails(map[string]any{"payment_terms_days": days}).
				Mark(ierr.ErrValidation)
		}
		return terms, nil
	case PaymentTermsTypeDueOnReceipt, PaymentTermsTypeEndOfMonth, PaymentTermsTypeEndOfNextMonth:
		if days != nil {
			return "", ierr.NewError("payment_terms_days is only allowed with payment_terms_type DAYS").
				WithHint("Remove payment_terms_days or set payment_terms_type to DAYS").
				WithReportableDetails(map[string]any{"payment_terms_type": termsType}).
				Mark(ierr.ErrValidation)
		}
		return PaymentTerms(termsType), nil
	default:
		return "", ierr.NewError("invalid payment_terms_type").
			WithHint("payment_terms_type must be DAYS, DUE_ON_RECEIPT, END_OF_MONTH or END_OF_NEXT_MONTH").
			WithReportableDetails(map[string]any{"payment_terms_type": termsType}).
			Mark(ierr.ErrValidation)
	}
}

// Parts splits payment terms into their type and, for DAYS, day count; both nil when empty or invalid.
func (p PaymentTerms) Parts() (*PaymentTermsType, *int) {
	switch p {
	case PaymentTermsDueOnReceipt, PaymentTermsEndOfMonth, PaymentTermsEndOfNextMonth:
		return lo.ToPtr(PaymentTermsType(p)), nil
	}
	if days, ok := PaymentTermsToDueDateDays(p); ok {
		return lo.ToPtr(PaymentTermsTypeDays), &days
	}
	return nil, nil
}

// ResolvePaymentTerms returns the terms a request sets: payment_terms_type and payment_terms_days when sent, else
// payment_terms. Nil when none is sent; "" clears.
func ResolvePaymentTerms(legacy *PaymentTerms, termsType *PaymentTermsType, days *int) (*PaymentTerms, error) {
	if termsType == nil && days == nil {
		if legacy == nil {
			return nil, nil
		}
		if err := legacy.Validate(); err != nil {
			return nil, err
		}
		return legacy, nil
	}
	if lo.FromPtr(termsType) == "" && days == nil {
		return lo.ToPtr(PaymentTerms("")), nil
	}
	terms, err := NewPaymentTerms(lo.FromPtr(termsType), days)
	if err != nil {
		return nil, err
	}
	return &terms, nil
}

// DueDate returns the invoice due date for terms of this type, counted from the issue date; days applies to DAYS only.
// End of month terms take the last day of the issue date's month (or the next) in issueDate's location.
// Returns false if the type is empty or invalid, or DAYS has no valid day count.
func (t PaymentTermsType) DueDate(days int, issueDate time.Time) (time.Time, bool) {
	switch t {
	case PaymentTermsTypeDays:
		if days < paymentTermsMinNetDays || days > paymentTermsMaxNetDays {
			return time.Time{}, false
		}
		return issueDate.AddDate(0, 0, days), true
	case PaymentTermsTypeDueOnReceipt:
		return issueDate, true
	case PaymentTermsTypeEndOfMonth:
		return endOfMonth(issueDate, 0), true
	case PaymentTermsTypeEndOfNextMonth:
		return endOfMonth(issueDate, 1), true
	}
	return time.Time{}, false
}

func endOfMonth(t time.Time, monthsAhead int) time.Time {
	return time.Date(t.Year(), t.Month()+time.Month(monthsAhead)+1, 0, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}
