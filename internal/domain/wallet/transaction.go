package wallet

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Transaction represents a wallet transaction
type Transaction struct {
	ID                  string                      `json:"id"`
	WalletID            string                      `json:"wallet_id"`
	CustomerID          string                      `json:"customer_id"`
	Type                types.TransactionType       `json:"type"`
	Amount              decimal.Decimal             `json:"amount" swaggertype:"string"`
	CreditAmount        decimal.Decimal             `json:"credit_amount" swaggertype:"string"`
	CreditBalanceBefore decimal.Decimal             `json:"credit_balance_before" swaggertype:"string"`
	CreditBalanceAfter  decimal.Decimal             `json:"credit_balance_after" swaggertype:"string"`
	TxStatus            types.TransactionStatus     `json:"transaction_status"`
	ReferenceType       types.WalletTxReferenceType `json:"reference_type"`
	ReferenceID         string                      `json:"reference_id"`
	Description         string                      `json:"description"`
	Metadata            types.Metadata              `json:"metadata"`
	ExpiryDate          *time.Time                  `json:"expiry_date"`
	CreditsAvailable    decimal.Decimal             `json:"credits_available" swaggertype:"string"`
	TransactionReason   types.TransactionReason     `json:"transaction_reason"`
	Priority            *int                        `json:"priority"`
	Currency            string                      `json:"currency"`

	// ParentTransactionID is the ID of the parent wallet_transaction this row was earned from
	// (the purchase tx, for a bonus grant). Empty for ordinary transactions.
	ParentTransactionID string `json:"parent_transaction_id,omitempty"`

	// SourceType/SourceID identify the entity this transaction's value comes from or goes to
	// (e.g. an invoice). Empty when there is none.
	SourceType types.WalletTxSourceType `json:"source_type,omitempty"`
	SourceID   string                   `json:"source_id,omitempty"`

	// ConsumptionBreakdown is set on debits: the credits drawn from each credit batch.
	ConsumptionBreakdown []*types.WalletTxConsumption `json:"consumption_breakdown,omitempty"`

	// conversion_rate is the conversion rate for the transaction to the currency
	ConversionRate *decimal.Decimal `json:"conversion_rate,omitempty" swaggertype:"string"`

	// topup_conversion_rate is the conversion rate for the topup to the currency
	TopupConversionRate *decimal.Decimal `json:"topup_conversion_rate,omitempty" swaggertype:"string"`

	IdempotencyKey string `json:"idempotency_key"`
	EnvironmentID  string `json:"environment_id"`
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

// ComputeCreditsAvailable computes the credits available for a transaction
func (t *Transaction) ComputeCreditsAvailable() (decimal.Decimal, error) {
	if t.Type == types.TransactionTypeCredit {
		if t.CreditBalanceBefore.LessThan(decimal.Zero) {
			return decimal.Max(decimal.Zero, t.CreditBalanceAfter), nil
		} else {
			return t.CreditAmount, nil
		}
	} else if t.Type == types.TransactionTypeDebit {
		return decimal.Zero, nil
	}
	// return error for the unknown transaction type
	return decimal.Zero, ierr.NewError("invalid transaction type").
		WithHint("Invalid transaction type").
		WithReportableDetails(map[string]interface{}{
			"transaction_type": t.Type,
		}).
		Mark(ierr.ErrInvalidOperation)
}

// ToEnt converts a domain transaction to an ent transaction
func (t *Transaction) ToEnt() *ent.WalletTransaction {
	return &ent.WalletTransaction{
		ID:                   t.ID,
		WalletID:             t.WalletID,
		CustomerID:           t.CustomerID,
		Type:                 t.Type,
		Amount:               t.Amount,
		CreditAmount:         t.CreditAmount,
		CreditBalanceBefore:  t.CreditBalanceBefore,
		CreditBalanceAfter:   t.CreditBalanceAfter,
		TransactionStatus:    t.TxStatus,
		ReferenceType:        t.ReferenceType,
		ReferenceID:          t.ReferenceID,
		Description:          t.Description,
		Metadata:             t.Metadata,
		ExpiryDate:           t.ExpiryDate,
		CreditsAvailable:     t.CreditsAvailable,
		TransactionReason:    t.TransactionReason,
		Priority:             t.Priority,
		Currency:             t.Currency,
		ConversionRate:       t.ConversionRate,
		TopupConversionRate:  t.TopupConversionRate,
		ParentTransactionID:  t.ParentTransactionID,
		SourceType:           lo.EmptyableToPtr(t.SourceType),
		SourceID:             lo.EmptyableToPtr(t.SourceID),
		ConsumptionBreakdown: t.ConsumptionBreakdown,
		EnvironmentID:        t.EnvironmentID,
		TenantID:             t.TenantID,
		Status:               string(t.Status),
		CreatedBy:            t.CreatedBy,
		UpdatedBy:            t.UpdatedBy,
		CreatedAt:            t.CreatedAt,
		UpdatedAt:            t.UpdatedAt,
	}
}

// FromEnt converts an ent transaction to a domain transaction
func TransactionFromEnt(e *ent.WalletTransaction) *Transaction {
	if e == nil {
		return nil
	}

	return &Transaction{
		ID:                   e.ID,
		WalletID:             e.WalletID,
		CustomerID:           e.CustomerID,
		Type:                 e.Type,
		Amount:               e.Amount,
		CreditAmount:         e.CreditAmount,
		TxStatus:             e.TransactionStatus,
		ReferenceType:        e.ReferenceType,
		ReferenceID:          e.ReferenceID,
		IdempotencyKey:       lo.FromPtr(e.IdempotencyKey),
		Description:          e.Description,
		Metadata:             types.Metadata(e.Metadata),
		ExpiryDate:           e.ExpiryDate,
		CreditsAvailable:     e.CreditsAvailable,
		CreditBalanceBefore:  e.CreditBalanceBefore,
		CreditBalanceAfter:   e.CreditBalanceAfter,
		Currency:             e.Currency,
		TransactionReason:    e.TransactionReason,
		Priority:             e.Priority,
		ConversionRate:       e.ConversionRate,
		TopupConversionRate:  e.TopupConversionRate,
		ParentTransactionID:  e.ParentTransactionID,
		SourceType:           lo.FromPtr(e.SourceType),
		SourceID:             lo.FromPtr(e.SourceID),
		ConsumptionBreakdown: e.ConsumptionBreakdown,
		EnvironmentID:        e.EnvironmentID,
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

// CreditTopupsExportData represents the joined data for credit topup export
type CreditTopupsExportData struct {
	TopupID             string
	ExternalID          string
	CustomerName        string
	WalletID            string
	Amount              decimal.Decimal
	CreditBalanceBefore decimal.Decimal
	CreditBalanceAfter  decimal.Decimal
	ReferenceID         string
	TransactionReason   types.TransactionReason
	Credits             decimal.Decimal
	Currency            string
	InvoiceID           string
	// The billing period of the linked invoice. Empty when there is no invoice or it has no period,
	// as with one-off top-up invoices.
	InvoicePeriodStart *time.Time
	InvoicePeriodEnd   *time.Time
	// AmountPaid is what the credits were bought for: the invoice after discount, before tax, for
	// an invoiced purchase, and zero for free and bonus grants. Not set for other credits.
	AmountPaid *decimal.Decimal
	CreatedAt  time.Time
}

// WalletDebitsExportData is one credit batch a debit drew from. A debit with no recorded
// breakdown is a single row with no batch.
type WalletDebitsExportData struct {
	DebitID             string
	ExternalID          string
	CustomerName        string
	WalletID            string
	Currency            string
	TransactionReason   types.TransactionReason
	InvoiceID           string
	InvoicePeriodStart  *time.Time
	InvoicePeriodEnd    *time.Time
	CreditTransactionID string
	Credits             decimal.Decimal
	// The wallet balance around this row. A debit drawing from several credits steps down row by row.
	CreditBalanceBefore decimal.Decimal
	CreditBalanceAfter  decimal.Decimal
	// The credit balance in the wallet currency, at the conversion rate the debit used.
	WalletBalanceBefore decimal.Decimal
	WalletBalanceAfter  decimal.Decimal
	// What is left of what was paid for the wallet's credits around this row. Credits with no
	// paid amount, such as refunds and adjustments, add nothing. Unrounded, so rounding for
	// display never builds up from row to row.
	TopUpAmountRemainingBefore decimal.Decimal
	TopUpAmountRemainingAfter  decimal.Decimal
	// The size and paid amount of the credit drawn from, for spreading the paid amount over the
	// credits drawn.
	WalletTxnCredits    *decimal.Decimal
	WalletTxnAmountPaid *decimal.Decimal
	CreatedAt           time.Time
}

// WalletDebitsExportCursor is the last debit of the previous page. The first page starts from
// the export window's start time with an empty DebitID.
type WalletDebitsExportCursor struct {
	CreatedAt time.Time
	DebitID   string
}

// CreditUsageCSVHeaders represents the column header strings for the credit usage CSV export
type CreditUsageCSVHeaders string

const (
	CreditUsageCSVHeadersCustomerName       CreditUsageCSVHeaders = "name"
	CreditUsageCSVHeadersCustomerExternalID CreditUsageCSVHeaders = "customer_external_id"
	CreditUsageCSVHeadersCustomerID         CreditUsageCSVHeaders = "customer_id"
	CreditUsageCSVHeadersCurrentBalance     CreditUsageCSVHeaders = "current_balance"
	CreditUsageCSVHeadersRealtimeBalance    CreditUsageCSVHeaders = "real_time_balance"
	CreditUsageCSVHeadersNumberOfWallets    CreditUsageCSVHeaders = "number_of_wallets"
)
