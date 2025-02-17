package wallet

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Wallet represents a credit wallet for a customer
type Wallet struct {
	ID                  string                 `db:"id" json:"id"`
	CustomerID          string                 `db:"customer_id" json:"customer_id"`
	Currency            string                 `db:"currency" json:"currency"`
	Balance             decimal.Decimal        `db:"balance" json:"balance"`
	WalletStatus        types.WalletStatus     `db:"wallet_status" json:"wallet_status"`
	Name                string                 `json:"name,omitempty"`
	Description         string                 `db:"description" json:"description"`
	Metadata            types.Metadata         `db:"metadata" json:"metadata"`
	AutoTopupTrigger    types.AutoTopupTrigger `db:"auto_topup_trigger" json:"auto_topup_trigger"`
	AutoTopupMinBalance decimal.Decimal        `db:"auto_topup_min_balance" json:"auto_topup_min_balance"`
	AutoTopupAmount     decimal.Decimal        `db:"auto_topup_amount" json:"auto_topup_amount"`
	types.BaseModel
}

func (w *Wallet) TableName() string {
	return "wallets"
}
