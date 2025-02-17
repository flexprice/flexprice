package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/ent/wallettransaction"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type walletRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts WalletTransactionQueryOptions
}

func NewWalletRepository(client postgres.IClient, logger *logger.Logger) walletdomain.Repository {
	return &walletRepository{
		client: client,
		logger: logger,
	}
}

func (r *walletRepository) CreateWallet(ctx context.Context, w *walletdomain.Wallet) error {
	client := r.client.Querier(ctx)
	wallet, err := client.Wallet.Create().
		SetID(w.ID).
		SetTenantID(w.TenantID).
		SetCustomerID(w.CustomerID).
		SetName(w.Name).
		SetCurrency(w.Currency).
		SetDescription(w.Description).
		SetMetadata(w.Metadata).
		SetBalance(w.Balance).
		SetWalletStatus(string(w.WalletStatus)).
		SetAutoTopupTrigger(string(w.AutoTopupTrigger)).
		SetAutoTopupMinBalance(w.AutoTopupMinBalance).
		SetAutoTopupAmount(w.AutoTopupAmount).
		SetStatus(string(w.Status)).
		SetCreatedBy(w.CreatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	// Update the input wallet with created data
	*w = *toDomainWallet(wallet)
	return nil
}

func (r *walletRepository) GetWalletByID(ctx context.Context, id string) (*walletdomain.Wallet, error) {
	client := r.client.Querier(ctx)
	w, err := client.Wallet.Query().
		Where(
			wallet.ID(id),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("wallet not found")
		}
		return nil, fmt.Errorf("failed to query wallet: %w", err)
	}

	return toDomainWallet(w), nil
}

func (r *walletRepository) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*walletdomain.Wallet, error) {
	client := r.client.Querier(ctx)
	wallets, err := client.Wallet.Query().
		Where(
			wallet.CustomerID(customerID),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
			wallet.WalletStatusEQ(string(types.WalletStatusActive)),
		).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to query wallets: %w", err)
	}

	result := make([]*walletdomain.Wallet, len(wallets))
	for i, w := range wallets {
		result[i] = toDomainWallet(w)
	}
	return result, nil
}

func (r *walletRepository) UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error {
	client := r.client.Querier(ctx)
	count, err := client.Wallet.Update().
		Where(
			wallet.ID(id),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
		).
		SetWalletStatus(string(status)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to update wallet status: %w", err)
	}

	if count == 0 {
		return fmt.Errorf("wallet not found or already updated")
	}

	return nil
}

func (r *walletRepository) CreditWallet(ctx context.Context, req *walletdomain.WalletOperation) error {
	if req.Type != types.TransactionTypeCredit {
		return fmt.Errorf("invalid transaction type")
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be greater than 0")
	}

	return r.processWalletOperation(ctx, req)
}

func (r *walletRepository) DebitWallet(ctx context.Context, req *walletdomain.WalletOperation) error {
	if req.Type != types.TransactionTypeDebit {
		return fmt.Errorf("invalid transaction type")
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be greater than 0")
	}
	return r.processWalletOperation(ctx, req)
}

func (r *walletRepository) processWalletOperation(ctx context.Context, req *walletdomain.WalletOperation) error {
	if req.Type == "" {
		return fmt.Errorf("transaction type is required")
	}

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Get wallet
		w, err := r.client.Querier(ctx).Wallet.Query().
			Where(
				wallet.ID(req.WalletID),
				wallet.TenantID(types.GetTenantID(ctx)),
				wallet.StatusEQ(string(types.StatusPublished)),
			).
			Only(ctx)

		if err != nil {
			if ent.IsNotFound(err) {
				return fmt.Errorf("wallet not found: %w", err)
			}
			return fmt.Errorf("querying wallet: %w", err)
		}

		// Calculate new balance
		var newBalance decimal.Decimal
		if req.Type == types.TransactionTypeCredit {
			newBalance = w.Balance.Add(req.Amount)
		} else if req.Type == types.TransactionTypeDebit {
			newBalance = w.Balance.Sub(req.Amount)
			if newBalance.LessThan(decimal.Zero) {
				return fmt.Errorf("insufficient balance: current=%s, requested=%s", w.Balance, req.Amount)
			}
		} else {
			return fmt.Errorf("invalid transaction type")
		}

		// Create transaction record
		txn, err := r.client.Querier(ctx).WalletTransaction.Create().
			SetID(uuid.NewString()).
			SetTenantID(types.GetTenantID(ctx)).
			SetWalletID(req.WalletID).
			SetType(string(req.Type)).
			SetAmount(req.Amount).
			SetReferenceType(req.ReferenceType).
			SetReferenceID(req.ReferenceID).
			SetDescription(req.Description).
			SetMetadata(req.Metadata).
			SetStatus(string(types.StatusPublished)).
			SetTransactionStatus(string(types.TransactionStatusPending)).
			SetCreatedBy(types.GetUserID(ctx)).
			SetBalanceBefore(w.Balance).
			SetBalanceAfter(newBalance).
			Save(ctx)

		if err != nil {
			return fmt.Errorf("creating transaction: %w", err)
		}

		// Update wallet balance
		if err := r.client.Querier(ctx).Wallet.Update().
			Where(wallet.ID(req.WalletID)).
			SetBalance(newBalance).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now().UTC()).
			Exec(ctx); err != nil {
			return fmt.Errorf("updating wallet balance: %w", err)
		}

		// Update transaction status to completed
		if err := r.client.Querier(ctx).WalletTransaction.Update().
			Where(wallettransaction.ID(txn.ID)).
			SetTransactionStatus(string(types.TransactionStatusCompleted)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now().UTC()).
			Exec(ctx); err != nil {
			return fmt.Errorf("updating transaction status: %w", err)
		}

		return nil
	})
}

func (r *walletRepository) GetTransactionByID(ctx context.Context, id string) (*walletdomain.Transaction, error) {
	client := r.client.Querier(ctx)
	t, err := client.WalletTransaction.Query().
		Where(
			wallettransaction.ID(id),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("transaction not found")
		}
		return nil, fmt.Errorf("failed to query transaction: %w", err)
	}

	return toDomainTransaction(t), nil
}

func (r *walletRepository) ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*walletdomain.Transaction, error) {
	client := r.client.Querier(ctx)
	query := client.WalletTransaction.Query()
	query = ApplyQueryOptions(ctx, query, f, r.queryOpts)
	if f != nil {
		query = r.queryOpts.applyEntityQueryOptions(ctx, f, query)
	}

	result, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}

	transactions := make([]*walletdomain.Transaction, len(result))
	for i, t := range result {
		transactions[i] = toDomainTransaction(t)
	}

	return transactions, nil
}

func (r *walletRepository) ListAllWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*walletdomain.Transaction, error) {
	if f == nil {
		f = types.NewNoLimitWalletTransactionFilter()
	}

	client := r.client.Querier(ctx)
	query := client.WalletTransaction.Query()
	query = ApplyBaseFilters(ctx, query, f, r.queryOpts)
	if f != nil {
		query = r.queryOpts.applyEntityQueryOptions(ctx, f, query)
	}

	result, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}

	transactions := make([]*walletdomain.Transaction, len(result))
	for i, t := range result {
		transactions[i] = toDomainTransaction(t)
	}

	return transactions, nil
}

func (r *walletRepository) CountWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) (int, error) {
	if f == nil {
		f = types.NewNoLimitWalletTransactionFilter()
	}

	client := r.client.Querier(ctx)
	query := client.WalletTransaction.Query()
	query = ApplyBaseFilters(ctx, query, f, r.queryOpts)
	if f != nil {
		query = r.queryOpts.applyEntityQueryOptions(ctx, f, query)
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count transactions: %w", err)
	}

	return count, nil
}

// UpdateTransactionStatus updates the status of a transaction
func (r *walletRepository) UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error {
	client := r.client.Querier(ctx)
	count, err := client.WalletTransaction.Update().
		Where(
			wallettransaction.ID(id),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
		).
		SetTransactionStatus(string(status)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to update transaction status: %w", err)
	}

	if count == 0 {
		return fmt.Errorf("transaction not found or already updated")
	}

	return nil
}

// Helper function to convert Ent wallet to domain wallet
func toDomainWallet(w *ent.Wallet) *walletdomain.Wallet {
	// Set default values for nullable fields
	autoTopupTrigger := types.AutoTopupTriggerDisabled
	if w.AutoTopupTrigger != nil {
		autoTopupTrigger = types.AutoTopupTrigger(*w.AutoTopupTrigger)
	}

	autoTopupMinBalance := decimal.Zero
	if w.AutoTopupMinBalance != nil {
		autoTopupMinBalance = *w.AutoTopupMinBalance
	}

	autoTopupAmount := decimal.Zero
	if w.AutoTopupAmount != nil && !w.AutoTopupAmount.IsZero() {
		autoTopupAmount = *w.AutoTopupAmount
	}

	return &walletdomain.Wallet{
		ID:                  w.ID,
		Name:                w.Name,
		CustomerID:          w.CustomerID,
		Currency:            w.Currency,
		Description:         w.Description,
		Metadata:            w.Metadata,
		Balance:             w.Balance,
		WalletStatus:        types.WalletStatus(w.WalletStatus),
		AutoTopupTrigger:    autoTopupTrigger,
		AutoTopupMinBalance: autoTopupMinBalance,
		AutoTopupAmount:     autoTopupAmount,
		BaseModel: types.BaseModel{
			TenantID:  w.TenantID,
			Status:    types.Status(w.Status),
			CreatedAt: w.CreatedAt,
			CreatedBy: w.CreatedBy,
			UpdatedAt: w.UpdatedAt,
			UpdatedBy: w.UpdatedBy,
		},
	}
}

// Helper function to convert Ent transaction to domain transaction
func toDomainTransaction(t *ent.WalletTransaction) *walletdomain.Transaction {
	return &walletdomain.Transaction{
		ID:            t.ID,
		WalletID:      t.WalletID,
		Type:          types.TransactionType(t.Type),
		Amount:        t.Amount,
		ReferenceType: t.ReferenceType,
		ReferenceID:   t.ReferenceID,
		Description:   t.Description,
		Metadata:      t.Metadata,
		TxStatus:      types.TransactionStatus(t.TransactionStatus),
		BalanceBefore: t.BalanceBefore,
		BalanceAfter:  t.BalanceAfter,
		BaseModel: types.BaseModel{
			TenantID:  t.TenantID,
			Status:    types.Status(t.Status),
			CreatedAt: t.CreatedAt,
			CreatedBy: t.CreatedBy,
			UpdatedAt: t.UpdatedAt,
			UpdatedBy: t.UpdatedBy,
		},
	}
}

// WalletTransactionQuery type alias for better readability
type WalletTransactionQuery = *ent.WalletTransactionQuery

// WalletTransactionQueryOptions implements BaseQueryOptions for wallet queries
type WalletTransactionQueryOptions struct{}

func (o WalletTransactionQueryOptions) ApplyTenantFilter(ctx context.Context, query WalletTransactionQuery) WalletTransactionQuery {
	return query.Where(wallettransaction.TenantID(types.GetTenantID(ctx)))
}

func (o WalletTransactionQueryOptions) ApplyStatusFilter(query WalletTransactionQuery, status string) WalletTransactionQuery {
	if status == "" {
		return query.Where(wallettransaction.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(wallettransaction.Status(status))
}

func (o WalletTransactionQueryOptions) ApplySortFilter(query WalletTransactionQuery, field string, order string) WalletTransactionQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o WalletTransactionQueryOptions) ApplyPaginationFilter(query WalletTransactionQuery, limit int, offset int) WalletTransactionQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o WalletTransactionQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return wallettransaction.FieldCreatedAt
	case "updated_at":
		return wallettransaction.FieldUpdatedAt
	case "amount":
		return wallettransaction.FieldAmount
	default:
		return field
	}
}

func (o WalletTransactionQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.WalletTransactionFilter, query WalletTransactionQuery) WalletTransactionQuery {
	if f == nil {
		return query
	}

	if f.WalletID != nil {
		query = query.Where(wallettransaction.WalletID(*f.WalletID))
	}

	if f.Type != nil {
		query = query.Where(wallettransaction.Type(string(*f.Type)))
	}

	if f.TransactionStatus != nil {
		query = query.Where(wallettransaction.TransactionStatus(string(*f.TransactionStatus)))
	}

	if f.ReferenceType != nil && f.ReferenceID != nil {
		query = query.Where(
			wallettransaction.ReferenceType(*f.ReferenceType),
			wallettransaction.ReferenceID(*f.ReferenceID),
		)
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(wallettransaction.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(wallettransaction.CreatedAtLTE(*f.EndTime))
		}
	}

	return query
}

func (r *walletRepository) UpdateWallet(ctx context.Context, id string, w *walletdomain.Wallet) error {
	client := r.client.Querier(ctx)
	update := client.Wallet.Update().
		Where(
			wallet.ID(id),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
		)

	if w.Name != "" {
		update.SetName(w.Name)
	}
	if w.Description != "" {
		update.SetDescription(w.Description)
	}
	if w.Metadata != nil {
		update.SetMetadata(w.Metadata)
	}
	if w.AutoTopupTrigger != "" {
		if w.AutoTopupTrigger == types.AutoTopupTriggerDisabled {
			// When disabling auto top-up, set all related fields to NULL
			update.SetAutoTopupTrigger(string(types.AutoTopupTriggerDisabled))
			update.ClearAutoTopupMinBalance()
			update.ClearAutoTopupAmount()
		} else {
			// When enabling auto top-up, set all required fields
			update.SetAutoTopupTrigger(string(w.AutoTopupTrigger))
			update.SetAutoTopupMinBalance(w.AutoTopupMinBalance)
			update.SetAutoTopupAmount(w.AutoTopupAmount)
		}
	}

	update.SetUpdatedBy(types.GetUserID(ctx))
	update.SetUpdatedAt(time.Now().UTC())

	count, err := update.Save(ctx)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError, "update wallet")
	}

	if count == 0 {
		return errors.New(errors.ErrCodeNotFound, "wallet not found or already updated")
	}

	return nil
}
