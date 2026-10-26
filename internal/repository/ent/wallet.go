package ent

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
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
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type walletRepository struct {
	client     postgres.IClient
	logger     *logger.Logger
	queryOpts  WalletTransactionQueryOptions
	redisCache cache.RedisCache
}

func NewWalletRepository(client postgres.IClient, logger *logger.Logger, redisCache cache.RedisCache) walletdomain.Repository {
	return &walletRepository{
		client:     client,
		logger:     logger,
		redisCache: redisCache,
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
		SetTopupConversionRate(w.TopupConversionRate).
		SetStatus(string(w.Status)).
		SetCreatedBy(w.CreatedBy).
		SetCreatedAt(w.CreatedAt).
		SetUpdatedBy(w.UpdatedBy).
		SetUpdatedAt(w.UpdatedAt).
		SetEnvironmentID(w.EnvironmentID).
		SetAlertState(types.AlertState(w.AlertState))

	if w.AlertSettings != nil {
		walletBuilder.SetAlertSettings(*w.AlertSettings)
	}

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
		SaveReturning(ctx)

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

	if len(count) == 0 {
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
				wallettransaction.CreatedAtLT(timeReference),
				wallettransaction.StatusEQ(string(types.StatusPublished)),
				wallettransaction.TransactionStatusEQ(types.TransactionStatusCompleted),
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
func (r *walletRepository) ConsumeCredits(ctx context.Context, credits []*walletdomain.Transaction, amount decimal.Decimal) ([]*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "consume_credits", map[string]interface{}{
		"credits_count": len(credits),
		"amount":        amount.String(),
	})
	defer FinishSpan(span)

	remainingAmount := amount
	consumedCreditTransactions := make([]*walletdomain.Transaction, 0, len(credits))

	for _, credit := range credits {
		if remainingAmount.IsZero() {
			break
		}

		toConsume := decimal.Min(remainingAmount, credit.CreditsAvailable)
		newAvailable := credit.CreditsAvailable.Sub(toConsume)

		// Update credit's available amount
		updated, err := r.client.Writer(ctx).WalletTransaction.UpdateOne(&ent.WalletTransaction{
			ID: credit.ID,
		}).
			SetCreditsAvailable(newAvailable).
			SetUpdatedAt(time.Now().UTC()).
			SetUpdatedBy(types.GetUserID(ctx)).
			Save(ctx)

		if err != nil {
			return consumedCreditTransactions, ierr.WithError(err).
				WithHint("Failed to update credit available amount").
				WithReportableDetails(map[string]interface{}{
					"credit_id": credit.ID,
					"amount":    toConsume,
				}).
				Mark(ierr.ErrDatabase)
		}

		remainingAmount = remainingAmount.Sub(toConsume)
		consumedCreditTransactions = append(consumedCreditTransactions, walletdomain.TransactionFromEnt(updated))
	}

	return consumedCreditTransactions, nil
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

	var parentTransactionID *string
	if tx.ParentTransactionID != "" {
		parentTransactionID = &tx.ParentTransactionID
	}

	create := client.WalletTransaction.Create().
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
		SetNillableConversionRate(tx.ConversionRate).
		SetNillableTopupConversionRate(tx.TopupConversionRate).
		SetCreatedAt(tx.CreatedAt).
		SetCreatedBy(tx.CreatedBy).
		SetUpdatedAt(tx.UpdatedAt).
		SetUpdatedBy(tx.UpdatedBy).
		SetEnvironmentID(tx.EnvironmentID).
		SetIdempotencyKey(tx.IdempotencyKey).
		SetNillablePriority(tx.Priority).
		SetNillableParentTransactionID(parentTransactionID).
		SetNillableSourceType(lo.EmptyableToPtr(tx.SourceType)).
		SetNillableSourceID(lo.EmptyableToPtr(tx.SourceID))
	// Left unset when empty so the column is SQL NULL; ent would otherwise store JSON null.
	if len(tx.ConsumptionBreakdown) > 0 {
		create.SetConsumptionBreakdown(tx.ConsumptionBreakdown)
	}

	transaction, err := create.Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A wallet transaction with this idempotency key already exists").
				WithReportableDetails(map[string]interface{}{
					"wallet_id":       tx.WalletID,
					"type":            tx.Type,
					"amount":          tx.Amount,
					"idempotency_key": tx.IdempotencyKey,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
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

	_, err := r.client.Writer(ctx).Wallet.Update().
		Where(wallet.ID(walletID)).
		SetBalance(finalBalance).
		SetCreditBalance(newCreditBalance).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		SaveReturning(ctx)

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

// GetPendingTransactionByParent returns the pending bonus transaction whose
// parent_transaction_id is parentTransactionID, if one exists.
func (r *walletRepository) GetPendingTransactionByParent(ctx context.Context, parentTransactionID string) (*walletdomain.Transaction, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "get_pending_transaction_by_parent", map[string]interface{}{
		"parent_transaction_id": parentTransactionID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	t, err := client.WalletTransaction.Query().
		Where(
			wallettransaction.ParentTransactionID(parentTransactionID),
			wallettransaction.TransactionStatusEQ(types.TransactionStatusPending),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
			wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Pending bonus transaction not found").
				WithReportableDetails(map[string]interface{}{
					"parent_transaction_id": parentTransactionID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve pending bonus transaction").
			WithReportableDetails(map[string]interface{}{
				"parent_transaction_id": parentTransactionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return walletdomain.TransactionFromEnt(t), nil
}

// GetLastAutoTopupTransactionForWallet returns the most recent published wallet
// transaction with metadata.auto_topup=true for the wallet, or nil if none.
func (r *walletRepository) GetLastAutoTopupTransactionForWallet(ctx context.Context, walletID string) (*walletdomain.Transaction, error) {
	span := StartRepositorySpan(ctx, "wallet", "get_last_auto_topup_transaction_for_wallet", map[string]interface{}{
		"wallet_id": walletID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	t, err := client.WalletTransaction.Query().
		Where(
			wallettransaction.WalletID(walletID),
			wallettransaction.TenantID(types.GetTenantID(ctx)),
			wallettransaction.StatusEQ(string(types.StatusPublished)),
			wallettransaction.EnvironmentID(types.GetEnvironmentID(ctx)),
			predicate.WalletTransaction(func(s *sql.Selector) {
				s.Where(sqljson.ValueEQ(
					wallettransaction.FieldMetadata,
					"true",
					sqljson.Path(types.WalletMetadataKeyAutoTopup),
				))
			}),
		).
		Order(wallettransaction.ByCreatedAt(sql.OrderDesc())).
		First(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve last auto-topup wallet transaction").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": walletID,
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

func (r *walletRepository) SumCreditAmountsByFilter(ctx context.Context, f *types.WalletTransactionFilter) (decimal.Decimal, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "wallet", "sum_wallet_transactions_credit_amount", map[string]interface{}{
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
			return decimal.Zero, err
		}
	}

	// COALESCE guards the zero-rows case: SUM() over no rows is NULL, and decimal.Decimal
	// has no NULL representation to scan it into. The alias is required — the scanner maps
	// result columns onto struct fields by name, and an unaliased expression comes back as
	// "coalesce", matching nothing.
	sumCreditAmount := func(s *sql.Selector) string {
		return sql.As(fmt.Sprintf("COALESCE(%s, 0)", sql.Sum(s.C(wallettransaction.FieldCreditAmount))), "sum")
	}

	var result []struct {
		Sum decimal.Decimal `json:"sum"`
	}
	if err := query.Aggregate(sumCreditAmount).Scan(ctx, &result); err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHint("Failed to sum wallet transactions").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": f.WalletID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(result) == 0 {
		return decimal.Zero, nil
	}

	return result[0].Sum, nil
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
		SetNillableSourceType(lo.EmptyableToPtr(tx.SourceType)).
		SetNillableSourceID(lo.EmptyableToPtr(tx.SourceID)).
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
		return query.Where(wallettransaction.StatusIn(
			string(types.StatusPublished),
			string(types.StatusArchived),
		))
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

// GetFieldName returns the ent field name for wallet transaction; delegates to ent's ValidColumn so new schema fields are supported automatically.
func (o WalletTransactionQueryOptions) GetFieldName(field string) string {
	if wallettransaction.ValidColumn(field) {
		return field
	}
	return ""
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
	if w.AlertSettings != nil {
		update.SetAlertSettings(*w.AlertSettings)
	}
	if w.AlertState != "" {
		update.SetAlertState(types.AlertState(w.AlertState))
	}
	update.SetUpdatedAt(time.Now().UTC())
	update.SetUpdatedBy(types.GetUserID(ctx))

	count, err := update.SaveReturning(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update wallet").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(count) == 0 {
		return ierr.NewError("wallet not found or already updated").
			WithHint("The wallet may not exist or has already been updated").
			Mark(ierr.ErrNotFound)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *walletRepository) SetCache(ctx context.Context, wallet *walletdomain.Wallet) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "wallet", "set", map[string]interface{}{
		"wallet_id": wallet.ID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixWallet, wallet.ID)
	r.redisCache.Set(ctx, cacheKey, wallet, cache.ExpiryDefaultRedis)
}

func (r *walletRepository) GetCache(ctx context.Context, id string) *walletdomain.Wallet {
	span, ctx := cache.StartRedisCacheSpan(ctx, "wallet", "get", map[string]interface{}{
		"wallet_id": id,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixWallet, id)
	value, found := r.redisCache.Get(ctx, cacheKey)
	if !found {
		return nil
	}
	w, ok := cache.UnmarshalCacheValue[walletdomain.Wallet](value)
	if !ok {
		return nil
	}
	return w
}

func (r *walletRepository) DeleteCache(ctx context.Context, walletID string) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "wallet", "delete", map[string]interface{}{
		"wallet_id": walletID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixWallet, walletID)
	r.redisCache.Delete(ctx, cacheKey)
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
			wt.credit_amount,
			wt.currency,
			CASE WHEN wt.source_type = 'INVOICE' THEN wt.source_id END,
			i.period_start,
			i.period_end,
			` + amountPaidSQL("wt", "i") + `,
			wt.created_at
		FROM
			wallet_transactions wt
			INNER JOIN wallets w ON w.id = wt.wallet_id
			INNER JOIN customers c ON c.id = w.customer_id
			LEFT JOIN invoices i ON i.id = wt.source_id AND wt.source_type = 'INVOICE'
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
		var invoiceID *string
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
			&exportData.Credits,
			&exportData.Currency,
			&invoiceID,
			&exportData.InvoicePeriodStart,
			&exportData.InvoicePeriodEnd,
			&exportData.AmountPaid,
			&exportData.CreatedAt,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to read credit topup row").
				Mark(ierr.ErrDatabase)
		}
		exportData.InvoiceID = lo.FromPtr(invoiceID)

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

// amountPaidSQL is what a credit batch was bought for: the invoice after discount and before tax
// for an invoiced purchase, zero for free and bonus grants, and NULL for any other credit.
func amountPaidSQL(tx, inv string) string {
	return fmt.Sprintf(`CASE
				WHEN %[1]s.transaction_reason = '%[3]s' THEN %[2]s.total - %[2]s.total_tax
				WHEN %[1]s.transaction_reason IN ('%[4]s', '%[5]s') THEN 0
			END`, tx, inv,
		types.TransactionReasonPurchasedCreditInvoiced,
		types.TransactionReasonFreeCredit,
		types.TransactionReasonPurchasedCreditBonus)
}

// drawnSQL expands a debit's consumption_breakdown into one row per credit drawn from, with the
// credit and its invoice. A debit with no breakdown keeps a single row with no credit.
func drawnSQL(debit string) string {
	return drawnFromSQL(fmt.Sprintf(`CASE WHEN jsonb_typeof(%[1]s.consumption_breakdown) = 'array' THEN %[1]s.consumption_breakdown END`, debit))
}

// ledgerDrawnSQL is drawnSQL with one more row, with no credit, for what a manual debit took past
// the credits it drew from, so the debit's rows add up to its credit_amount.
func ledgerDrawnSQL(debit string) string {
	return drawnFromSQL(fmt.Sprintf(`CASE WHEN jsonb_typeof(%[1]s.consumption_breakdown) = 'array' THEN %[1]s.consumption_breakdown || COALESCE((
					SELECT jsonb_build_array(jsonb_build_object('credits', %[1]s.credit_amount - COALESCE(SUM((e->>'credits')::numeric), 0)))
					FROM jsonb_array_elements(%[1]s.consumption_breakdown) e
					HAVING %[1]s.credit_amount > COALESCE(SUM((e->>'credits')::numeric), 0)
				), '[]'::jsonb) END`, debit))
}

func drawnFromSQL(breakdown string) string {
	return `LEFT JOIN LATERAL ROWS FROM (
				jsonb_to_recordset(` + breakdown + `)
					AS (credit_transaction_id text, credits numeric)
			) WITH ORDINALITY AS drawn(credit_transaction_id, credits, ord) ON true
			LEFT JOIN wallet_transactions batch ON batch.id = drawn.credit_transaction_id
			LEFT JOIN invoices invoice ON invoice.id = batch.source_id AND batch.source_type = 'INVOICE'`
}

// drawnValueSQL is the share of what was paid for the credit drawn from that the drawn credits
// take, or 0 when the credit has no paid amount.
func drawnValueSQL() string {
	return `COALESCE(drawn.credits * ` + amountPaidSQL("batch", "invoice") + ` / NULLIF(batch.credit_amount, 0), 0)`
}

func (r *walletRepository) GetWalletDebitsForExport(ctx context.Context, tenantID, envID string, after walletdomain.WalletDebitsExportCursor, endTime time.Time, limit int) ([]*walletdomain.WalletDebitsExportData, error) {
	span := StartRepositorySpan(ctx, "wallet", "get_wallet_debits_for_export", map[string]interface{}{
		"tenant_id":        tenantID,
		"env_id":           envID,
		"after_created_at": after.CreatedAt,
		"after_debit_id":   after.DebitID,
		"end":              endTime,
		"limit":            limit,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	// Pages are keyed on the last debit seen rather than an offset, so each page is an index range
	// read from where the previous one stopped, and a debit's batch rows never straddle two pages.
	// Each debit expands to one row per credit batch in its consumption_breakdown; a debit without
	// one (written before breakdowns were recorded) keeps a single row with no batch.
	query := `
		WITH page AS (
			SELECT debit.*
			FROM wallet_transactions debit
			WHERE
				debit.tenant_id = $1
				AND debit.environment_id = $2
				AND debit.type = $3
				AND debit.transaction_status = $4
				AND debit.status = $5
				AND debit.created_at >= $6
				AND (debit.created_at, debit.id) > ($6, $7)
				AND debit.created_at < $8
			ORDER BY debit.created_at ASC, debit.id ASC
			LIMIT $9
		),
		-- The wallet's value after each step of its history up to the page: a credit adds what was
		-- paid for it, and each credit a debit draws from takes away its share. Credits come before
		-- debits made at the same time.
		wallet_value AS (
			SELECT
				step.debit_id,
				step.ord,
				SUM(step.change) OVER (PARTITION BY step.wallet_id ORDER BY step.created_at, step.kind, step.tx_id, step.ord) AS value_after
			FROM (
				SELECT
					credit.wallet_id,
					credit.created_at,
					0 AS kind,
					credit.id AS tx_id,
					0::bigint AS ord,
					NULL::text AS debit_id,
					COALESCE(` + amountPaidSQL("credit", "credit_invoice") + `, 0) AS change
				FROM
					wallet_transactions credit
					LEFT JOIN invoices credit_invoice ON credit_invoice.id = credit.source_id AND credit.source_type = 'INVOICE'
				WHERE
					credit.tenant_id = $1
					AND credit.environment_id = $2
					AND credit.wallet_id IN (SELECT wallet_id FROM page)
					AND credit.type = $10
					AND credit.transaction_status = $4
					AND credit.status = $5
					AND credit.created_at <= (SELECT MAX(created_at) FROM page)
				UNION ALL
				SELECT
					debit.wallet_id,
					debit.created_at,
					1,
					debit.id,
					COALESCE(drawn.ord, 0),
					debit.id,
					-` + drawnValueSQL() + `
				FROM
					wallet_transactions debit
					` + drawnSQL("debit") + `
				WHERE
					debit.tenant_id = $1
					AND debit.environment_id = $2
					AND debit.wallet_id IN (SELECT wallet_id FROM page)
					AND debit.type = $3
					AND debit.transaction_status = $4
					AND debit.status = $5
					AND debit.created_at <= (SELECT MAX(created_at) FROM page)
			) step
		)
		SELECT
			debit.id,
			customer.external_id,
			customer.name,
			debit.wallet_id,
			debit.currency,
			debit.transaction_reason,
			CASE WHEN debit.source_type = 'INVOICE' THEN debit.source_id END,
			debit_invoice.period_start,
			debit_invoice.period_end,
			drawn.credit_transaction_id,
			COALESCE(drawn.credits, debit.credit_amount),
			debit.credit_balance_before - COALESCE(SUM(drawn.credits) OVER drawn_so_far - drawn.credits, 0),
			debit.credit_balance_before - COALESCE(SUM(drawn.credits) OVER drawn_so_far, debit.credit_amount),
			debit.amount / NULLIF(debit.credit_amount, 0),
			value.value_after + ` + drawnValueSQL() + `,
			value.value_after,
			batch.credit_amount,
			` + amountPaidSQL("batch", "invoice") + `,
			debit.created_at
		FROM
			page debit
			INNER JOIN wallets wallet ON wallet.id = debit.wallet_id
			INNER JOIN customers customer ON customer.id = wallet.customer_id
			LEFT JOIN invoices debit_invoice ON debit_invoice.id = debit.source_id AND debit.source_type = 'INVOICE'
			` + drawnSQL("debit") + `
			LEFT JOIN wallet_value value ON value.debit_id = debit.id AND value.ord = COALESCE(drawn.ord, 0)
		WINDOW drawn_so_far AS (PARTITION BY debit.id ORDER BY drawn.ord)
		ORDER BY debit.created_at ASC, debit.id ASC, drawn.ord ASC
	`

	rows, err := client.QueryContext(ctx, query,
		tenantID,
		envID,
		string(types.TransactionTypeDebit),
		string(types.TransactionStatusCompleted),
		string(types.StatusPublished),
		after.CreatedAt,
		after.DebitID,
		endTime,
		limit,
		string(types.TransactionTypeCredit),
	)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch wallet debits for export").
			WithReportableDetails(map[string]interface{}{
				"tenant_id":      tenantID,
				"env_id":         envID,
				"after_debit_id": after.DebitID,
				"limit":          limit,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := make([]*walletdomain.WalletDebitsExportData, 0)
	for rows.Next() {
		exportData := &walletdomain.WalletDebitsExportData{}
		var invoiceID, creditTransactionID *string
		var conversionRate *decimal.Decimal
		err := rows.Scan(
			&exportData.DebitID,
			&exportData.ExternalID,
			&exportData.CustomerName,
			&exportData.WalletID,
			&exportData.Currency,
			&exportData.TransactionReason,
			&invoiceID,
			&exportData.InvoicePeriodStart,
			&exportData.InvoicePeriodEnd,
			&creditTransactionID,
			&exportData.Credits,
			&exportData.CreditBalanceBefore,
			&exportData.CreditBalanceAfter,
			&conversionRate,
			&exportData.TopUpAmountRemainingBefore,
			&exportData.TopUpAmountRemainingAfter,
			&exportData.WalletTxnCredits,
			&exportData.WalletTxnAmountPaid,
			&exportData.CreatedAt,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to read wallet debit row").
				Mark(ierr.ErrDatabase)
		}
		exportData.InvoiceID = lo.FromPtr(invoiceID)
		exportData.CreditTransactionID = lo.FromPtr(creditTransactionID)
		if conversionRate != nil {
			exportData.WalletBalanceBefore = exportData.CreditBalanceBefore.Mul(*conversionRate)
			exportData.WalletBalanceAfter = exportData.CreditBalanceAfter.Mul(*conversionRate)
		}

		result = append(result, exportData)
	}

	if err = rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to iterate wallet debit rows").
			Mark(ierr.ErrDatabase)
	}

	return result, nil
}

// effectiveAtSQL is when a transaction changed the balance. A purchase that waited on payment
// writes its balances when it completes, so it takes the invoice's paid time.
func effectiveAtSQL(tx, inv string) string {
	return fmt.Sprintf(`GREATEST(%[1]s.created_at, CASE WHEN %[1]s.transaction_reason IN ('%[3]s', '%[4]s') THEN %[2]s.paid_at END)`,
		tx, inv,
		types.TransactionReasonPurchasedCreditInvoiced,
		types.TransactionReasonPurchasedCreditBonus)
}

func (r *walletRepository) ListWalletIDsForLedgerExport(ctx context.Context, tenantID, envID string, startTime, endTime time.Time, customerExternalIDs []string) ([]string, error) {
	span := StartRepositorySpan(ctx, "wallet", "list_wallet_ids_for_ledger_export", map[string]interface{}{
		"tenant_id":          tenantID,
		"env_id":             envID,
		"start":              startTime,
		"end":                endTime,
		"customer_filter_on": len(customerExternalIDs) > 0,
	})
	defer FinishSpan(span)

	// A filter names at most 100 customers, so their wallets come straight from the customer index.
	// Without one, the wallets are those with a transaction in the window, from the created_at
	// index, plus those whose customer paid a one-off invoice in it, for a purchase paid late.
	query := `
		SELECT wallet.id
		FROM
			customers customer
			INNER JOIN wallets wallet ON wallet.tenant_id = customer.tenant_id
				AND wallet.environment_id = customer.environment_id
				AND wallet.customer_id = customer.id
		WHERE
			customer.tenant_id = $1
			AND customer.environment_id = $2
			AND customer.external_id = ANY($3::text[])
		ORDER BY wallet.id
	`
	args := []interface{}{tenantID, envID, pq.Array(customerExternalIDs)}
	if len(customerExternalIDs) == 0 {
		query = `
			SELECT tx.wallet_id
			FROM wallet_transactions tx
			WHERE
				tx.tenant_id = $1
				AND tx.environment_id = $2
				AND tx.created_at >= $3
				AND tx.created_at < $4
				AND tx.type IN ($5, $6)
				AND tx.transaction_status = $7
				AND tx.status = $8
			UNION
			SELECT wallet.id
			FROM
				invoices invoice
				INNER JOIN wallets wallet ON wallet.tenant_id = invoice.tenant_id
					AND wallet.environment_id = invoice.environment_id
					AND wallet.customer_id = invoice.customer_id
			WHERE
				invoice.tenant_id = $1
				AND invoice.environment_id = $2
				AND invoice.invoice_type = $9
				AND invoice.paid_at >= $3
				AND invoice.paid_at < $4
			ORDER BY 1
		`
		args = []interface{}{
			tenantID,
			envID,
			startTime,
			endTime,
			string(types.TransactionTypeCredit),
			string(types.TransactionTypeDebit),
			string(types.TransactionStatusCompleted),
			string(types.StatusPublished),
			string(types.InvoiceTypeOneOff),
		}
	}

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list wallets for the ledger export").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenantID,
				"env_id":    envID,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	walletIDs := make([]string, 0)
	for rows.Next() {
		var walletID string
		if err := rows.Scan(&walletID); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to read wallet id").
				Mark(ierr.ErrDatabase)
		}
		walletIDs = append(walletIDs, walletID)
	}
	if err = rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to iterate wallet ids").
			Mark(ierr.ErrDatabase)
	}
	return walletIDs, nil
}

func (r *walletRepository) GetWalletLedgerForExport(ctx context.Context, tenantID, envID string, walletIDs []string, startTime, endTime time.Time) ([]*walletdomain.WalletLedgerExportData, error) {
	span := StartRepositorySpan(ctx, "wallet", "get_wallet_ledger_for_export", map[string]interface{}{
		"tenant_id":    tenantID,
		"env_id":       envID,
		"wallet_count": len(walletIDs),
		"start":        startTime,
		"end":          endTime,
	})
	defer FinishSpan(span)

	if len(walletIDs) == 0 {
		return []*walletdomain.WalletLedgerExportData{}, nil
	}

	// history reads each wallet once, through the wallet index, up to the window's end: what was
	// paid for the credits before the window carries into the first row in it. A bonus shares its
	// purchase's time, so after_parent keeps it after the purchase.
	query := `
		WITH history AS (
			SELECT
				tx.id,
				tx.wallet_id,
				tx.type,
				tx.transaction_reason,
				tx.currency,
				tx.source_type,
				tx.source_id,
				tx.amount,
				tx.credit_amount,
				tx.credit_balance_before,
				tx.credit_balance_after,
				tx.created_at,
				` + effectiveAtSQL("tx", "tx_invoice") + ` AS effective_at,
				COALESCE(tx.parent_transaction_id, '') <> '' AS after_parent,
				tx_invoice.period_start AS invoice_period_start,
				tx_invoice.period_end AS invoice_period_end,
				CASE WHEN tx.type = $3 THEN ` + amountPaidSQL("tx", "tx_invoice") + ` END AS amount_paid,
				drawn.credit_transaction_id,
				drawn.credits AS drawn_credits,
				COALESCE(drawn.ord, 0) AS ord,
				batch.credit_amount AS batch_credits,
				` + amountPaidSQL("batch", "invoice") + ` AS batch_amount_paid,
				CASE WHEN tx.type = $3 THEN COALESCE(` + amountPaidSQL("tx", "tx_invoice") + `, 0)
					ELSE -` + drawnValueSQL() + ` END AS change
			FROM
				wallet_transactions tx
				LEFT JOIN invoices tx_invoice ON tx_invoice.id = tx.source_id AND tx.source_type = 'INVOICE'
				` + ledgerDrawnSQL("tx") + `
			WHERE
				tx.tenant_id = $1
				AND tx.environment_id = $2
				AND tx.wallet_id = ANY($7::text[])
				AND tx.type IN ($3, $4)
				AND tx.transaction_status = $5
				AND tx.status = $6
				AND tx.created_at < $9
		),
		valued AS (
			SELECT
				history.*,
				SUM(change) OVER (PARTITION BY wallet_id ORDER BY effective_at, after_parent, id, ord) AS value_after,
				SUM(drawn_credits) OVER (PARTITION BY id ORDER BY ord) AS drawn_so_far
			FROM history
		)
		SELECT
			tx.id,
			tx.type,
			tx.transaction_reason,
			customer.external_id,
			customer.name,
			tx.wallet_id,
			tx.currency,
			CASE WHEN tx.source_type = 'INVOICE' THEN tx.source_id END,
			tx.invoice_period_start,
			tx.invoice_period_end,
			tx.credit_transaction_id,
			COALESCE(tx.drawn_credits, tx.credit_amount),
			CASE WHEN tx.type = $3 THEN tx.credit_balance_before
				ELSE tx.credit_balance_before - COALESCE(tx.drawn_so_far - tx.drawn_credits, 0) END,
			CASE WHEN tx.type = $3 THEN tx.credit_balance_after
				ELSE tx.credit_balance_before - COALESCE(tx.drawn_so_far, tx.credit_amount) END,
			CASE WHEN tx.type = $3 THEN wallet.conversion_rate ELSE tx.amount / NULLIF(tx.credit_amount, 0) END,
			tx.value_after - tx.change,
			tx.value_after,
			tx.amount_paid,
			tx.batch_credits,
			tx.batch_amount_paid,
			tx.effective_at,
			tx.created_at
		FROM
			valued tx
			INNER JOIN wallets wallet ON wallet.id = tx.wallet_id
			INNER JOIN customers customer ON customer.id = wallet.customer_id
		WHERE
			tx.effective_at >= $8
			AND tx.effective_at < $9
		ORDER BY tx.wallet_id, tx.effective_at, tx.after_parent, tx.id, tx.ord
	`

	// The breakdown's jsonb_to_recordset is estimated at 100 rows per transaction, which pushes the
	// plan over the JIT threshold: compiling then costs more than running the query.
	tx, err := r.client.Reader(ctx).BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to start the wallet ledger read").
			Mark(ierr.ErrDatabase)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL jit = off"); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to start the wallet ledger read").
			Mark(ierr.ErrDatabase)
	}

	rows, err := tx.QueryContext(ctx, query,
		tenantID,
		envID,
		string(types.TransactionTypeCredit),
		string(types.TransactionTypeDebit),
		string(types.TransactionStatusCompleted),
		string(types.StatusPublished),
		pq.Array(walletIDs),
		startTime,
		endTime,
	)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch wallet ledger for export").
			WithReportableDetails(map[string]interface{}{
				"tenant_id":    tenantID,
				"env_id":       envID,
				"wallet_count": len(walletIDs),
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := make([]*walletdomain.WalletLedgerExportData, 0)
	for rows.Next() {
		row := &walletdomain.WalletLedgerExportData{}
		var invoiceID, creditTransactionID *string
		var conversionRate *decimal.Decimal
		err := rows.Scan(
			&row.TransactionID,
			&row.Type,
			&row.TransactionReason,
			&row.ExternalID,
			&row.CustomerName,
			&row.WalletID,
			&row.Currency,
			&invoiceID,
			&row.InvoicePeriodStart,
			&row.InvoicePeriodEnd,
			&creditTransactionID,
			&row.Credits,
			&row.CreditBalanceBefore,
			&row.CreditBalanceAfter,
			&conversionRate,
			&row.TopUpAmountRemainingBefore,
			&row.TopUpAmountRemainingAfter,
			&row.AmountPaid,
			&row.WalletTxnCredits,
			&row.WalletTxnAmountPaid,
			&row.EffectiveAt,
			&row.CreatedAt,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to read wallet ledger row").
				Mark(ierr.ErrDatabase)
		}
		row.InvoiceID = lo.FromPtr(invoiceID)
		row.CreditTransactionID = lo.FromPtr(creditTransactionID)
		if conversionRate != nil {
			row.WalletBalanceBefore = row.CreditBalanceBefore.Mul(*conversionRate)
			row.WalletBalanceAfter = row.CreditBalanceAfter.Mul(*conversionRate)
		}

		result = append(result, row)
	}

	if err = rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to iterate wallet ledger rows").
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

// GetCreditsAvailableBreakdown retrieves the breakdown of available credits by type (purchased, free)
func (r *walletRepository) GetCreditsAvailableBreakdown(ctx context.Context, walletID string) (*types.CreditBreakdown, error) {
	span := StartRepositorySpan(ctx, "wallet", "get_credits_available_breakdown", map[string]interface{}{
		"wallet_id": walletID,
	})
	defer FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	client := r.client.Reader(ctx)

	// Use raw SQL for complex aggregation query
	query := `
		SELECT 
			CASE 
				WHEN transaction_reason IN ('PURCHASED_CREDIT_INVOICED', 'PURCHASED_CREDIT_DIRECT') THEN 'PURCHASED'
				WHEN transaction_reason IN ('FREE_CREDIT_GRANT', 'SUBSCRIPTION_CREDIT_GRANT') THEN 'FREE'
			END AS credit_type,
			SUM(credits_available) AS total_credits_available
		FROM wallet_transactions
		WHERE tenant_id = $1
			AND environment_id = $2
			AND wallet_id = $3
			AND type = 'credit'
			AND transaction_status = 'completed'
			AND status = 'published'
		GROUP BY credit_type
		ORDER BY credit_type
	`

	rows, err := client.QueryContext(ctx, query, tenantID, envID, walletID)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch credits available breakdown").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": walletID,
				"tenant_id": tenantID,
				"env_id":    envID,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	breakdown := &types.CreditBreakdown{
		Purchased: decimal.Zero,
		Free:      decimal.Zero,
	}

	for rows.Next() {
		var creditType string
		var total decimal.Decimal

		err := rows.Scan(&creditType, &total)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan credit breakdown row").
				Mark(ierr.ErrDatabase)
		}

		switch creditType {
		case "PURCHASED":
			breakdown.Purchased = total
		case "FREE":
			breakdown.Free = total
		}
	}

	if err = rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to iterate credit breakdown rows").
			Mark(ierr.ErrDatabase)
	}

	return breakdown, nil
}
