package wallet

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Transaction represents a wallet transaction
type Transaction struct {
	ID                  string                      `db:"id" json:"id"`
	WalletID            string                      `db:"wallet_id" json:"wallet_id"`
	Type                types.TransactionType       `db:"type" json:"type"`
	Amount              decimal.Decimal             `db:"amount" json:"amount"`
	CreditAmount        decimal.Decimal             `db:"credit_amount" json:"credit_amount"`
	CreditBalanceBefore decimal.Decimal             `db:"credit_balance_before" json:"credit_balance_before"`
	CreditBalanceAfter  decimal.Decimal             `db:"credit_balance_after" json:"credit_balance_after"`
	TxStatus            types.TransactionStatus     `db:"transaction_status" json:"transaction_status"`
	ReferenceType       types.WalletTxReferenceType `db:"reference_type" json:"reference_type"`
	ReferenceID         string                      `db:"reference_id" json:"reference_id"`
	Description         string                      `db:"description" json:"description"`
	Metadata            types.Metadata              `db:"metadata" json:"metadata"`
	ExpiryDate          *time.Time                  `db:"expiry_date" json:"expiry_date"`
	CreditsAvailable    decimal.Decimal             `db:"credits_available" json:"credits_available"`
	TransactionReason   types.TransactionReason     `db:"transaction_reason" json:"transaction_reason"`
	Priority            *int                        `db:"priority" json:"priority"`
	IdempotencyKey      string                      `db:"idempotency_key" json:"idempotency_key"`
	EnvironmentID       string                      `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

func (t *Transaction) TableName() string {
	return "wallet_transactions"
}

func (t *Transaction) Validate() error {
	return nil
}

// ApplyConversionRate applies the conversion rate to the transaction
// so for conversion rate of 2 means 1 credit = 2 dollars (assuming USD)
// and similarly for conversion rate of 0.5 means 1 dollar = 0.5 credits
func (t Transaction) ApplyConversionRate(rate decimal.Decimal) Transaction {
	t.Amount = t.CreditAmount.Mul(rate)
	return t
}

// ToEnt converts a domain transaction to an ent transaction
func (t *Transaction) ToEnt() *ent.WalletTransaction {
	return &ent.WalletTransaction{
		ID:                  t.ID,
		WalletID:            t.WalletID,
		Type:                string(t.Type),
		Amount:              t.Amount,
		CreditAmount:        t.CreditAmount,
		CreditBalanceBefore: t.CreditBalanceBefore,
		CreditBalanceAfter:  t.CreditBalanceAfter,
		TransactionStatus:   string(t.TxStatus),
		ReferenceType:       string(t.ReferenceType),
		ReferenceID:         t.ReferenceID,
		Description:         t.Description,
		Metadata:            t.Metadata,
		ExpiryDate:          t.ExpiryDate,
		CreditsAvailable:    t.CreditsAvailable,
		TransactionReason:   string(t.TransactionReason),
		Priority:            t.Priority,
		EnvironmentID:       t.EnvironmentID,
		TenantID:            t.TenantID,
		Status:              string(t.Status),
		CreatedBy:           t.CreatedBy,
		UpdatedBy:           t.UpdatedBy,
		CreatedAt:           t.CreatedAt,
		UpdatedAt:           t.UpdatedAt,
	}
}

// FromEnt converts an ent transaction to a domain transaction
func TransactionFromEnt(e *ent.WalletTransaction) *Transaction {
	if e == nil {
		return nil
	}

	return &Transaction{
		ID:                  e.ID,
		WalletID:            e.WalletID,
		Type:                types.TransactionType(e.Type),
		Amount:              e.Amount,
		CreditAmount:        e.CreditAmount,
		TxStatus:            types.TransactionStatus(e.TransactionStatus),
		ReferenceType:       types.WalletTxReferenceType(e.ReferenceType),
		ReferenceID:         e.ReferenceID,
		Description:         e.Description,
		Metadata:            types.Metadata(e.Metadata),
		ExpiryDate:          e.ExpiryDate,
		CreditsAvailable:    e.CreditsAvailable,
		CreditBalanceBefore: e.CreditBalanceBefore,
		CreditBalanceAfter:  e.CreditBalanceAfter,
		TransactionReason:   types.TransactionReason(e.TransactionReason),
		Priority:            e.Priority,
		EnvironmentID:       e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// TransactionListFromEnt converts a slice of ent.WalletTransaction pointers to domain transactions
func TransactionListFromEnt(dataList []*ent.WalletTransaction) []*Transaction {
	if dataList == nil {
		return nil
	}

	transactions := make([]*Transaction, len(dataList))
	for i, data := range dataList {
		transactions[i] = TransactionFromEnt(data)
	}
	return transactions
}
