package wallet

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Wallet represents a credit wallet for a customer
type Wallet struct {
	ID                  string                 `db:"id" json:"id"`
	CustomerID          string                 `db:"customer_id" json:"customer_id"`
	Currency            string                 `db:"currency" json:"currency"`
	Balance             decimal.Decimal        `db:"balance" json:"balance"`
	CreditBalance       decimal.Decimal        `db:"credit_balance" json:"credit_balance"`
	WalletStatus        types.WalletStatus     `db:"wallet_status" json:"wallet_status"`
	Name                string                 `db:"name" json:"name,omitempty"`
	Description         string                 `db:"description" json:"description"`
	Metadata            types.Metadata         `db:"metadata" json:"metadata"`
	AutoTopupTrigger    types.AutoTopupTrigger `db:"auto_topup_trigger" json:"auto_topup_trigger"`
	AutoTopupMinBalance decimal.Decimal        `db:"auto_topup_min_balance" json:"auto_topup_min_balance"`
	AutoTopupAmount     decimal.Decimal        `db:"auto_topup_amount" json:"auto_topup_amount"`
	WalletType          types.WalletType       `db:"wallet_type" json:"wallet_type"`
	Config              types.WalletConfig     `db:"config" json:"config"`
	ConversionRate      decimal.Decimal        `db:"conversion_rate" json:"conversion_rate"`
	EnvironmentID       string                 `db:"environment_id" json:"environment_id"`
	AlertEnabled        bool                   `db:"alert_enabled" json:"alert_enabled"`
	AlertConfig         *types.AlertConfig     `db:"alert_config" json:"alert_config,omitempty"`
	AlertState          string                 `db:"alert_state" json:"alert_state"`
	types.BaseModel
}

func (w *Wallet) TableName() string {
	return "wallets"
}

func (w *Wallet) Validate() error {
	if w.ConversionRate.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("conversion rate must be greater than 0").
			WithHint("Conversion rate must be a positive value").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": w.ConversionRate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Verify balance = credit_balance * conversion_rate
	expectedBalance := w.CreditBalance.Mul(w.ConversionRate)
	if !w.Balance.Equal(expectedBalance) {
		return ierr.NewError("balance and credit balance do not match").
			WithHint("Wallet Balance and Credit Balance must be equal after applying conversion rate").
			WithReportableDetails(map[string]interface{}{
				"balance":         w.Balance,
				"credit_balance":  w.CreditBalance,
				"conversion_rate": w.ConversionRate,
				"expected":        expectedBalance,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ApplyConversionRate applies the conversion rate to the wallet
// so for conversion rate of 2 means 1 credit = 2 dollars (assuming USD)
// and similarly for conversion rate of 0.5 means 1 dollar = 0.5 credits
func (w *Wallet) ApplyConversionRate(rate decimal.Decimal) *Wallet {
	w.Balance = w.CreditBalance.Mul(rate)
	return w
}

// FromEnt converts an ent wallet to a domain wallet
func FromEnt(e *ent.Wallet) *Wallet {
	if e == nil {
		return nil
	}

	// Create a copy of AlertConfig if it has a threshold set
	var alertConfig *types.AlertConfig
	if e.AlertConfig.Threshold != nil {
		alertConfigCopy := e.AlertConfig
		alertConfig = &alertConfigCopy
	}

	return &Wallet{
		ID:                  e.ID,
		CustomerID:          e.CustomerID,
		Currency:            e.Currency,
		Balance:             e.Balance,
		CreditBalance:       e.CreditBalance,
		WalletStatus:        types.WalletStatus(e.WalletStatus),
		Name:                e.Name,
		Description:         e.Description,
		Metadata:            e.Metadata,
		AutoTopupTrigger:    types.AutoTopupTrigger(lo.FromPtr(e.AutoTopupTrigger)),
		AutoTopupMinBalance: lo.FromPtr(e.AutoTopupMinBalance),
		AutoTopupAmount:     lo.FromPtr(e.AutoTopupAmount),
		WalletType:          types.WalletType(e.WalletType),
		Config:              e.Config,
		ConversionRate:      e.ConversionRate,
		EnvironmentID:       e.EnvironmentID,
		AlertEnabled:        e.AlertEnabled,
		AlertConfig:         alertConfig,
		AlertState:          e.AlertState,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of ent wallets to domain wallets
func FromEntList(es []*ent.Wallet) []*Wallet {
	if es == nil {
		return nil
	}

	wallets := make([]*Wallet, len(es))
	for i, e := range es {
		wallets[i] = FromEnt(e)
	}
	return wallets
}
