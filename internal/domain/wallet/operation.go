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
	// Amount is the amount of the transaction in the wallet's currency
	Amount decimal.Decimal `json:"amount"`
	// CreditAmount is the amount of the transaction in the wallet's credit currency
	// If both are provided, Amount is used
	CreditAmount      decimal.Decimal             `json:"credit_amount"`
	ReferenceType     types.WalletTxReferenceType `json:"reference_type,omitempty"`
	ReferenceID       string                      `json:"reference_id,omitempty"`
	Description       string                      `json:"description,omitempty"`
	Metadata          types.Metadata              `json:"metadata,omitempty"`
	IdempotencyKey    string                      `json:"idempotency_key,omitempty"`
	ExpiryDate        *int                        `json:"expiry_date,omitempty"` // YYYYMMDD format
	Priority          *int                        `json:"priority,omitempty"`    // lower number means higher priority
	TransactionReason types.TransactionReason     `json:"transaction_reason,omitempty"`
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

	if w.Amount.IsZero() && w.CreditAmount.IsZero() {
		return ierr.NewError("amount or credit_amount must be provided").
			WithHint("Either amount or credit amount must be specified for the operation").
			Mark(ierr.ErrValidation)
	}

	if err := w.TransactionReason.Validate(); err != nil {
		return err
	}

	if err := w.ReferenceType.Validate(); err != nil {
		return err
	}

	if w.ExpiryDate != nil {
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
