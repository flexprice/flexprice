package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/ent/wallettransaction"
	"github.com/flexprice/flexprice/internal/cache"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type walletRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts WalletTransactionQueryOptions
	cache     cache.Cache
}

func NewWalletRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) walletdomain.Repository {
	return &walletRepository{
		client: client,
		logger: logger,
		cache:  cache,
	}
}

func (r *walletRepository) CreateWallet(ctx context.Context, w *walletdomain.Wallet) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "create_wallet", map[string]interface{}{
		"wallet_id":   w.ID,
		"customer_id": w.CustomerID,
		"tenant_id":   w.TenantID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if w.EnvironmentID == "" {
		w.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	walletBuilder := client.Wallet.Create().
		SetID(w.ID).
		SetTenantID(w.TenantID).
		SetCustomerID(w.CustomerID).
		SetName(w.Name).
		SetCurrency(w.Currency).
		SetDescription(w.Description).
		SetMetadata(w.Metadata).
		SetBalance(w.Balance).
		SetCreditBalance(w.CreditBalance).
		SetWalletStatus(w.WalletStatus).
		SetWalletType(w.WalletType).
		SetConfig(w.Config).
		SetConversionRate(w.ConversionRate).
		SetStatus(string(w.Status)).
		SetCreatedBy(w.CreatedBy).
		SetCreatedAt(w.CreatedAt).
		SetUpdatedBy(w.UpdatedBy).
		SetUpdatedAt(w.UpdatedAt).
		SetEnvironmentID(w.EnvironmentID).
		SetAlertEnabled(w.AlertEnabled).
		SetNillableAlertConfig(w.AlertConfig).
		SetAlertState(types.AlertState(w.AlertState))

	if w.AutoTopup != nil {
		walletBuilder.SetAutoTopup(w.AutoTopup)
	}
	wallet, err := walletBuilder.Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create wallet").
			WithReportableDetails(map[string]interface{}{
				"customer_id": w.CustomerID,
				"currency":    w.Currency,
				"wallet_id":   w.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update the input wallet with created data
	*w = *walletdomain.FromEnt(wallet)
	return nil
}

func (r *walletRepository) GetWalletByID(ctx context.Context, id string) (*walletdomain.Wallet, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "get_wallet_by_id", map[string]interface{}{
		"wallet_id": id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedWallet := r.GetCache(ctx, id); cachedWallet != nil {
		return cachedWallet, nil
	}

	client := r.client.Reader(ctx)

	w, err := client.Wallet.Query().
		Where(
			wallet.ID(id),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
			wallet.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Wallet not found").
				WithReportableDetails(map[string]interface{}{
					"wallet_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	walletData := walletdomain.FromEnt(w)
	r.SetCache(ctx, walletData)
	return walletData, nil
}

func (r *walletRepository) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*walletdomain.Wallet, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "get_wallets_by_customer_id", map[string]interface{}{
		"customer_id": customerID,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	wallets, err := client.Wallet.Query().
		Where(
			wallet.CustomerID(customerID),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
			wallet.WalletStatusEQ(types.WalletStatusActive),
			wallet.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve customer wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := make([]*walletdomain.Wallet, len(wallets))
	for i, w := range wallets {
		result[i] = walletdomain.FromEnt(w)
	}
	return result, nil
}

func (r *walletRepository) UpdateWalletStatus(ctx context.Context, id string, status types.WalletStatus) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "update_wallet_status", map[string]interface{}{
		"wallet_id": id,
		"status":    status,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	count, err := client.Wallet.Update().
		Where(
			wallet.ID(id),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
			wallet.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetWalletStatus(status).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update wallet status").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
				"status":    status,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		// Create an error to pass to SetSpanError instead of using ErrorBuilder directly
		notFoundErr := fmt.Errorf("wallet not found or already updated")
		SetSpanError(span, notFoundErr)

		return ierr.NewError("wallet not found or already updated").
			WithHint("The wallet may not exist or has already been updated").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
				"status":    status,
			}).
			Mark(ierr.ErrNotFound)
	}

	r.DeleteCache(ctx, id)
	return nil
}

// FindEligibleCredits retrieves valid credits for debit operation with pagination
// the credits are sorted by priority (lower number first), expiry date, and then by credit amount
// this is to ensure that the highest priority credits are used first, then the oldest credits,
// and if there are multiple credits with the same priority and expiry date,
// the credits with the highest credit amount are used first
func (r *walletRepository) FindEligibleCredits(ctx context.Context, walletID string, requiredAmount decimal.Decimal, pageSize int, timeReference time.Time) ([]*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "find_eligible_credits", map[string]interface{}{
		"wallet_id":       walletID,
		"required_amount": requiredAmount.String(),
		"page_size":       pageSize,
		"time_reference":  timeReference.Format(time.RFC3339),
	})
	defer FinishSpan(span)

	var allCredits []*ent.WalletTransaction
	offset := 0

	for {
		credits, err := r.client.Writer(ctx).WalletTransaction.Query().
			Where(
				wallettransaction.WalletID(walletID),
				wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
				wallettransaction.Type(types.TransactionTypeCredit),
				wallettransaction.CreditsAvailableGT(decimal.Zero),
				wallettransaction.Or(
					wallettransaction.ExpiryDateIsNil(),
					wallettransaction.ExpiryDateGTE(timeReference),
				),
				wallettransaction.StatusEQ(string(types.StatusPublished)),
			).
			Order(
				ent.Asc(wallettransaction.FieldPriority), // Sort by priority first (nil values come last)
				ent.Asc(wallettransaction.FieldExpiryDate),
				ent.Desc(wallettransaction.FieldCreditAmount),
			).
			Offset(offset).
			Limit(pageSize).
			All(ctx)

		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to query eligible credits").
				WithReportableDetails(map[string]interface{}{
					"wallet_id":       walletID,
					"required_amount": requiredAmount,
				}).
				Mark(ierr.ErrDatabase)
		}

		if len(credits) == 0 {
			break
		}

		allCredits = append(allCredits, credits...)

		// Calculate total available so far
		var totalAvailable decimal.Decimal
		for _, c := range allCredits {
			totalAvailable = totalAvailable.Add(c.CreditsAvailable)
			if totalAvailable.GreaterThanOrEqual(requiredAmount) {
				return walletdomain.TransactionListFromEnt(allCredits), nil
			}
		}

		if len(credits) < pageSize {
			break
		}

		offset += pageSize
	}

	return walletdomain.TransactionListFromEnt(allCredits), nil
}

// ConsumeCredits processes debit operation across multiple credits
func (r *walletRepository) ConsumeCredits(ctx context.Context, credits []*walletdomain.Transaction, amount decimal.Decimal) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "consume_credits", map[string]interface{}{
		"credits_count": len(credits),
		"amount":        amount.String(),
	})
	defer FinishSpan(span)

	remainingAmount := amount

	for _, credit := range credits {
		if remainingAmount.IsZero() {
			break
		}

		toConsume := decimal.Min(remainingAmount, credit.CreditsAvailable)
		newAvailable := credit.CreditsAvailable.Sub(toConsume)

		// Update credit's available amount
		_, err := r.client.Writer(ctx).WalletTransaction.UpdateOne(&ent.WalletTransaction{
			ID: credit.ID,
		}).
			SetCreditsAvailable(newAvailable).
			SetUpdatedAt(time.Now().UTC()).
			SetUpdatedBy(types.GetUserID(ctx)).
			Save(ctx)

		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update credit available amount").
				WithReportableDetails(map[string]interface{}{
					"credit_id": credit.ID,
					"amount":    toConsume,
				}).
				Mark(ierr.ErrDatabase)
		}

		remainingAmount = remainingAmount.Sub(toConsume)
	}

	return nil
}

// CreateTransaction creates a new wallet transaction record
func (r *walletRepository) CreateTransaction(ctx context.Context, tx *walletdomain.Transaction) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "create_transaction", map[string]interface{}{
		"transaction_id": tx.ID,
		"wallet_id":      tx.WalletID,
		"type":           tx.Type,
		"amount":         tx.Amount.String(),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	// Set environment ID from context if not already set
	if tx.EnvironmentID == "" {
		tx.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	transaction, err := client.WalletTransaction.Create().
		SetID(tx.ID).
		SetTenantID(tx.TenantID).
		SetWalletID(tx.WalletID).
		SetNillableCustomerID(&tx.CustomerID).
		SetType(tx.Type).
		SetAmount(tx.Amount).
		SetCreditAmount(tx.CreditAmount).
		SetReferenceType(tx.ReferenceType).
		SetReferenceID(tx.ReferenceID).
		SetDescription(tx.Description).
		SetMetadata(tx.Metadata).
		SetStatus(string(tx.Status)).
		SetTransactionStatus(tx.TxStatus).
		SetTransactionReason(tx.TransactionReason).
		SetCreditsAvailable(tx.CreditsAvailable).
		SetNillableExpiryDate(tx.ExpiryDate).
		SetCreditBalanceBefore(tx.CreditBalanceBefore).
		SetCreditBalanceAfter(tx.CreditBalanceAfter).
		SetCurrency(tx.Currency).
		SetCreatedAt(tx.CreatedAt).
		SetCreatedBy(tx.CreatedBy).
		SetUpdatedAt(tx.UpdatedAt).
		SetUpdatedBy(tx.UpdatedBy).
		SetEnvironmentID(tx.EnvironmentID).
		SetIdempotencyKey(tx.IdempotencyKey).
		SetNillablePriority(tx.Priority).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create wallet transaction").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": tx.WalletID,
				"type":      tx.Type,
				"amount":    tx.Amount,
			}).
			Mark(ierr.ErrDatabase)
	}

	*tx = *walletdomain.TransactionFromEnt(transaction)
	return nil
}

// UpdateWalletBalance updates the wallet's balance and credit balance
func (r *walletRepository) UpdateWalletBalance(ctx context.Context, walletID string, finalBalance, newCreditBalance decimal.Decimal) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "update_wallet_balance", map[string]interface{}{
		"wallet_id":          walletID,
		"final_balance":      finalBalance.String(),
		"new_credit_balance": newCreditBalance.String(),
	})
	defer FinishSpan(span)

	err := r.client.Writer(ctx).Wallet.Update().
		Where(wallet.ID(walletID)).
		SetBalance(finalBalance).
		SetCreditBalance(newCreditBalance).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update wallet balance").
			WithReportableDetails(map[string]interface{}{
				"wallet_id":      walletID,
				"balance":        finalBalance,
				"credit_balance": newCreditBalance,
			}).
			Mark(ierr.ErrDatabase)
	}
	r.DeleteCache(ctx, walletID)
	return nil
}

func (r *walletRepository) GetTransactionByID(ctx context.Context, id string) (*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "get_transaction_by_id", map[string]interface{}{
		"transaction_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	t, err := client.WalletTransaction.Query().
		Where(
			wallettransaction.ID(id),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
			wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Transaction not found").
				WithReportableDetails(map[string]interface{}{
					"transaction_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve transaction").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return walletdomain.TransactionFromEnt(t), nil
}

func (r *walletRepository) GetTransactionByIdempotencyKey(ctx context.Context, idempotencyKey string) (*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "get_transaction_by_idempotency_key", map[string]interface{}{
		"idempotency_key": idempotencyKey,
		"tenant_id":       types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	t, err := client.WalletTransaction.Query().
		Where(
			wallettransaction.IdempotencyKey(idempotencyKey),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
			wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Transaction not found").
				WithReportableDetails(map[string]interface{}{
					"idempotency_key": idempotencyKey,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve transaction by idempotency key").
			WithReportableDetails(map[string]interface{}{
				"idempotency_key": idempotencyKey,
			}).
			Mark(ierr.ErrDatabase)
	}

	return walletdomain.TransactionFromEnt(t), nil
}

func (r *walletRepository) ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "list_wallet_transactions", map[string]interface{}{
		"filter": f,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.WalletTransaction.Query()
	var err error

	// Apply entity-specific filters
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, f, query)
	if err != nil {
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, f, r.queryOpts)

	transactions, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list wallet transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return walletdomain.TransactionListFromEnt(transactions), nil
}

func (r *walletRepository) ListAllWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "list_all_wallet_transactions", map[string]interface{}{
		"filter": f,
	})
	defer FinishSpan(span)

	if f == nil {
		f = types.NewNoLimitWalletTransactionFilter()
	}

	client := r.client.Reader(ctx)
	query := client.WalletTransaction.Query()
	query = ApplyBaseFilters(ctx, query, f, r.queryOpts)
	var err error
	if f != nil {
		query, err = r.queryOpts.applyEntityQueryOptions(ctx, f, query)
		if err != nil {
			return nil, err
		}
	}

	result, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return walletdomain.TransactionListFromEnt(result), nil
}

func (r *walletRepository) CountWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "count_wallet_transactions", map[string]interface{}{
		"filter": f,
	})
	defer FinishSpan(span)

	if f == nil {
		f = types.NewNoLimitWalletTransactionFilter()
	}

	client := r.client.Reader(ctx)
	query := client.WalletTransaction.Query()
	query = ApplyBaseFilters(ctx, query, f, r.queryOpts)
	var err error
	if f != nil {
		query, err = r.queryOpts.applyEntityQueryOptions(ctx, f, query)
		if err != nil {
			return 0, err
		}
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count wallet transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *walletRepository) UpdateTransactionStatus(ctx context.Context, id string, status types.TransactionStatus) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "update_transaction_status", map[string]interface{}{
		"transaction_id": id,
		"status":         status,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	count, err := client.WalletTransaction.Update().
		Where(
			wallettransaction.ID(id),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
			wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetTransactionStatus(status).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update transaction status").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": id,
				"status":         status,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		return ierr.NewError("transaction not found").
			WithHint("The transaction may not exist").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	return nil
}

// UpdateTransaction updates a wallet transaction with all fields
func (r *walletRepository) UpdateTransaction(ctx context.Context, tx *walletdomain.Transaction) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "update_transaction", map[string]interface{}{
		"transaction_id": tx.ID,
		"wallet_id":      tx.WalletID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	count, err := client.WalletTransaction.Update().
		Where(
			wallettransaction.ID(tx.ID),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
			wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetTransactionStatus(tx.TxStatus).
		SetCreditBalanceBefore(tx.CreditBalanceBefore).
		SetCreditBalanceAfter(tx.CreditBalanceAfter).
		SetCreditsAvailable(tx.CreditsAvailable).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(tx.UpdatedAt).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update transaction").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": tx.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		return ierr.NewError("transaction not found").
			WithHint("The transaction may not exist").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": tx.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return nil
}

// WalletTransactionQuery type alias for better readability
type WalletTransactionQuery = *ent.WalletTransactionQuery

// WalletTransactionQueryOptions implements BaseQueryOptions for wallet queries
type WalletTransactionQueryOptions struct{}

func (o WalletTransactionQueryOptions) ApplyTenantFilter(ctx context.Context, query WalletTransactionQuery) WalletTransactionQuery {
	return query.Where(wallettransaction.TenantID(types.GetTenantID(ctx)))
}

func (o WalletTransactionQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query WalletTransactionQuery) WalletTransactionQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(wallettransaction.EnvironmentID(environmentID))
	}
	return query
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
	case "created_by":
		return wallettransaction.FieldCreatedBy
	case "updated_at":
		return wallettransaction.FieldUpdatedAt
	case "amount":
		return wallettransaction.FieldAmount
	default:
		return field
	}
}

func (o WalletTransactionQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.WalletTransactionFilter, query WalletTransactionQuery) (WalletTransactionQuery, error) {
	if f == nil {
		return query, nil
	}

	var err error

	if f.WalletID != nil {
		query = query.Where(wallettransaction.WalletID(*f.WalletID))
	}

	if f.Type != nil {
		query = query.Where(wallettransaction.Type(*f.Type))
	}

	if f.TransactionStatus != nil {
		query = query.Where(wallettransaction.TransactionStatus(*f.TransactionStatus))
	}

	if f.ReferenceType != nil && f.ReferenceID != nil {
		query = query.Where(
			wallettransaction.ReferenceType(types.WalletTxReferenceType(*f.ReferenceType)),
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

	if f.CreditsAvailableGT != nil {
		query = query.Where(wallettransaction.CreditsAvailableGT(*f.CreditsAvailableGT))
	}

	if f.ExpiryDateBefore != nil {
		query = query.Where(wallettransaction.ExpiryDateLTE(*f.ExpiryDateBefore))
	}

	if f.ExpiryDateAfter != nil {
		query = query.Where(wallettransaction.ExpiryDateGTE(*f.ExpiryDateAfter))
	}

	if f.TransactionReason != nil {
		query = query.Where(wallettransaction.TransactionReason(types.TransactionReason(*f.TransactionReason)))
	}

	if f.Priority != nil {
		query = query.Where(wallettransaction.Priority(*f.Priority))
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[WalletTransactionQuery, predicate.WalletTransaction](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.WalletTransaction { return predicate.WalletTransaction(p) },
		)
		if err != nil {
			return query, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[WalletTransactionQuery, wallettransaction.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) wallettransaction.OrderOption { return wallettransaction.OrderOption(o) },
		)
		if err != nil {
			return query, err
		}
	}

	return query, nil
}

func (r *walletRepository) UpdateWallet(ctx context.Context, id string, w *walletdomain.Wallet) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "update_wallet", map[string]interface{}{
		"wallet_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	update := client.Wallet.Update().
		Where(
			wallet.ID(id),
			wallet.TenantID(types.GetTenantID(ctx)),
			wallet.StatusEQ(string(types.StatusPublished)),
			wallet.EnvironmentID(types.GetEnvironmentID(ctx)),
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
	if w.AutoTopup != nil {
		update.SetAutoTopup(w.AutoTopup)
	}
	// Check if Config has any non-nil fields
	if w.Config.AllowedPriceTypes != nil {
		update.SetConfig(w.Config)
	}
	if w.AlertConfig != nil {
		update.SetNillableAlertConfig(w.AlertConfig)
	}
	if w.AlertState != "" {
		update.SetAlertState(types.AlertState(w.AlertState))
	}
	update.SetAlertEnabled(w.AlertEnabled)
	update.SetUpdatedAt(time.Now().UTC())
	update.SetUpdatedBy(types.GetUserID(ctx))

	count, err := update.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		return ierr.NewError("wallet not found or already updated").
			WithHint("The wallet may not exist or has already been updated").
			Mark(ierr.ErrNotFound)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *walletRepository) SetCache(ctx context.Context, wallet *walletdomain.Wallet) {
	span := cache.StartCacheSpan(ctx, "wallet", "set", map[string]interface{}{
		"wallet_id": wallet.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixWallet, tenantID, environmentID, wallet.ID)
	r.cache.Set(ctx, cacheKey, wallet, cache.ExpiryDefaultInMemory)
}

func (r *walletRepository) GetCache(ctx context.Context, key string) *walletdomain.Wallet {
	span := cache.StartCacheSpan(ctx, "wallet", "get", map[string]interface{}{
		"wallet_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixWallet, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*walletdomain.Wallet)
	}
	return nil
}

func (r *walletRepository) DeleteCache(ctx context.Context, walletID string) {
	span := cache.StartCacheSpan(ctx, "wallet", "delete", map[string]interface{}{
		"wallet_id": walletID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixWallet, tenantID, environmentID, walletID)
	r.cache.Delete(ctx, cacheKey)
}

func (r *walletRepository) GetWalletsByFilter(ctx context.Context, filter *types.WalletFilter) ([]*walletdomain.Wallet, error) {
	client := r.client.Reader(ctx)
	query := client.Wallet.Query().
		Where(
			wallet.StatusEQ(string(types.StatusPublished)),
			wallet.EnvironmentID(types.GetEnvironmentID(ctx)),
			wallet.TenantID(types.GetTenantID(ctx)),
		)

	// Apply status filter
	if filter.Status != nil {
		query = query.Where(wallet.WalletStatusEQ(*filter.Status))
	}

	// Apply alert enabled filter
	if filter.AlertEnabled != nil {
		query = query.Where(wallet.AlertEnabledEQ(*filter.AlertEnabled))
	}

	// Apply wallet IDs filter
	if len(filter.WalletIDs) > 0 {
		query = query.Where(wallet.IDIn(filter.WalletIDs...))
	}

	wallets, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get wallets by filter").
			Mark(ierr.ErrDatabase)
	}

	return walletdomain.FromEntList(wallets), nil
}

// GetCreditTopupsForExport retrieves wallet transactions joined with wallet and customer data for export
func (r *walletRepository) GetCreditTopupsForExport(ctx context.Context, tenantID, envID string, startTime, endTime time.Time, limit, offset int) ([]*walletdomain.CreditTopupsExportData, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "get_credit_topups_for_export", map[string]interface{}{
		"tenant_id": tenantID,
		"env_id":    envID,
		"start":     startTime,
		"end":       endTime,
		"limit":     limit,
		"offset":    offset,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	// Use raw SQL for complex join query
	query := `
		SELECT
			wt.id AS topup_id,
			c.external_id,
			c.name AS customer_name,
			wt.wallet_id,
			wt.amount,
			wt.credit_balance_before,
			wt.credit_balance_after,
			wt.reference_id,
			wt.transaction_reason,
			wt.created_at
		FROM
			wallet_transactions wt
			INNER JOIN wallets w ON w.id = wt.wallet_id
			INNER JOIN customers c ON c.id = w.customer_id
		WHERE
			wt.tenant_id = $1
			AND wt.environment_id = $2
			AND wt.type = $3
			AND wt.transaction_status = $4
			AND wt.status = $5
			AND wt.created_at >= $6
			AND wt.created_at < $7
		ORDER BY wt.created_at ASC
		LIMIT $8 OFFSET $9
	`

	rows, err := client.QueryContext(ctx, query,
		tenantID,
		envID,
		string(types.TransactionTypeCredit),
		string(types.TransactionStatusCompleted),
		string(types.StatusPublished),
		startTime,
		endTime,
		limit,
		offset,
	)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch credit topups for export").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenantID,
				"env_id":    envID,
				"limit":     limit,
				"offset":    offset,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := make([]*walletdomain.CreditTopupsExportData, 0)
	for rows.Next() {
		exportData := &walletdomain.CreditTopupsExportData{}
		err := rows.Scan(
			&exportData.TopupID,
			&exportData.ExternalID,
			&exportData.CustomerName,
			&exportData.WalletID,
			&exportData.Amount,
			&exportData.CreditBalanceBefore,
			&exportData.CreditBalanceAfter,
			&exportData.ReferenceID,
			&exportData.TransactionReason,
			&exportData.CreatedAt,
		)
		if err != nil {
			r.logger.Errorw("failed to scan credit topup export data", "error", err)
			continue
		}

		result = append(result, exportData)
	}

	if err = rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to iterate credit topup rows").
			Mark(ierr.ErrDatabase)
	}

	return result, nil
}

func (o WalletTransactionQueryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in wallet transaction query", st).
			WithHintf("Unknown field name '%s' in wallet transaction query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}
