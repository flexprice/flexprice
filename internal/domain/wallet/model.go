package wallet

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Wallet represents a credit wallet for a customer
type Wallet struct {
	ID           string             `db:"id" json:"id"`
	CustomerID   string             `db:"customer_id" json:"customer_id"`
	Currency     string             `db:"currency" json:"currency"`
	Balance      decimal.Decimal    `db:"balance" json:"balance"`
	WalletStatus types.WalletStatus `db:"wallet_status" json:"wallet_status"`
	Description  string             `db:"description" json:"description"`
	Metadata     types.Metadata     `db:"metadata" json:"metadata"`
	types.BaseModel
}

func (w *Wallet) TableName() string {
	return "wallets"
}
