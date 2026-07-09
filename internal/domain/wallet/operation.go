package wallet

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// WalletOperation represents the request to credit or debit a wallet
type WalletOperation struct {
	WalletID string                `json:"wallet_id"`
	Type     types.TransactionType `json:"type"`
	// Amount is the amount of the transaction in the wallet's currency (e.g., USD, EUR)
	// Priority: If Amount is provided along with CreditAmount, Amount takes precedence
	// The Type field (CREDIT or DEBIT) determines the operation direction
	Amount decimal.Decimal `json:"amount" swaggertype:"string"`

	// CreditAmount is the amount of credits to add/remove from the wallet
	// Used when you want to specify credits directly instead of currency amount
	// Priority: Only used if Amount is not provided
	CreditAmount decimal.Decimal `json:"credit_amount,omitempty" swaggertype:"string"`

	ReferenceType types.WalletTxReferenceType `json:"reference_type,omitempty"`
	ReferenceID   string                      `json:"reference_id,omitempty"`
	Description   string                      `json:"description,omitempty"`

	// InvoiceID is optional. When provided for debit operations, the invoice's period_end
	// is used as the time reference for finding eligible credits instead of the current time.
	// This ensures credits are selected based on their eligibility at the invoice period end,
	// which is important for accurate billing and credit consumption.
	InvoiceID *string `json:"invoice_id,omitempty"`

	Metadata       types.Metadata `json:"metadata,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	ExpiryDate     *int           `json:"expiry_date,omitempty"` // YYYYMMDD format (legacy/external-API path)
	// ExpiryDateTime is the full-precision expiry timestamp. Preferred over ExpiryDate
	// for internal callers (e.g. subscription credit grants) to avoid the YYYYMMDD truncation
	// that strips hours/timezone and shifts the wall-clock expiry by up to ~24h.
	ExpiryDateTime    *time.Time              `json:"expiry_date_time,omitempty"`
	Priority          *int                    `json:"priority,omitempty"` // lower number means higher priority
	TransactionReason types.TransactionReason `json:"transaction_reason,omitempty"`
	// For Expiry Credits, this is the ID of the parent credit transaction
	// so that we can use the same credits for the expiry debit transaction
	ParentCreditTxID string `json:"-"`
	// BonusCreditAmount, when set and greater than zero on a credit operation, creates a second
	// wallet_transaction row (reason PURCHASED_CREDIT_BONUS, parent_transaction_id = this
	// operation's transaction ID) crediting this many bonus credits in the same DB transaction.
	// nil for every caller except TopUpWallet's direct-purchase branch.
	BonusCreditAmount *decimal.Decimal `json:"-"`
}

func (w *WalletOperation) Validate() error {
	if err := w.Type.Validate(); err != nil {
		return ierr.NewError("invalid transaction type").
			WithHint("Transaction type must be either credit or debit").
			WithReportableDetails(map[string]interface{}{
				"type": w.Type,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if at least one amount field is provided
	if w.Amount.IsZero() && w.CreditAmount.IsZero() {
		return ierr.NewError("amount or credit_amount must be provided").
			WithHint("Either amount or credit_amount must be specified for the operation").
			Mark(ierr.ErrValidation)
	}

	// Validate negative values
	if w.Amount.LessThan(decimal.Zero) {
		return ierr.NewError("amount cannot be negative").
			WithHint("Amount must be zero or positive").
			WithReportableDetails(map[string]interface{}{
				"amount": w.Amount,
			}).
			Mark(ierr.ErrValidation)
	}

	if w.CreditAmount.LessThan(decimal.Zero) {
		return ierr.NewError("credit_amount cannot be negative").
			WithHint("Credit amount must be zero or positive").
			WithReportableDetails(map[string]interface{}{
				"credit_amount": w.CreditAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if w.BonusCreditAmount != nil && w.BonusCreditAmount.LessThan(decimal.Zero) {
		return ierr.NewError("bonus_credit_amount cannot be negative").
			WithHint("Bonus credit amount must be zero or positive").
			WithReportableDetails(map[string]interface{}{
				"bonus_credit_amount": w.BonusCreditAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := w.TransactionReason.Validate(); err != nil {
		return err
	}

	if err := w.ReferenceType.Validate(); err != nil {
		return err
	}

	if w.ExpiryDateTime != nil {
		if w.ExpiryDateTime.Before(time.Now().UTC()) {
			return ierr.NewError("expiry date cannot be in the past").
				WithHint("Expiry date must be in the future").
				WithReportableDetails(map[string]interface{}{
					"expiry_date": w.ExpiryDateTime,
				}).
				Mark(ierr.ErrValidation)
		}
	} else if w.ExpiryDate != nil {
		expiryTime := types.ParseYYYYMMDDToDate(w.ExpiryDate)
		if expiryTime == nil {
			return ierr.NewError("invalid expiry date").
				WithHint("Expiry date must be in YYYYMMDD format").
				WithReportableDetails(map[string]interface{}{
					"expiry_date": w.ExpiryDate,
				}).
				Mark(ierr.ErrValidation)
		}

		if expiryTime.Before(time.Now().UTC()) {
			return ierr.NewError("expiry date cannot be in the past").
				WithHint("Expiry date must be in the future").
				WithReportableDetails(map[string]interface{}{
					"expiry_date": expiryTime,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ResolvedExpiryDate returns the full-precision expiry timestamp regardless of
// which field the caller populated. Prefers ExpiryDateTime; falls back to the
// YYYYMMDD ExpiryDate (interpreted as midnight UTC per ParseYYYYMMDDToDate).
func (w *WalletOperation) ResolvedExpiryDate() *time.Time {
	if w.ExpiryDateTime != nil {
		return w.ExpiryDateTime
	}
	return types.ParseYYYYMMDDToDate(w.ExpiryDate)
}
