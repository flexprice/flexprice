package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/ent/wallettransaction"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
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
}

func NewWalletRepository(client postgres.IClient, logger *logger.Logger) walletdomain.Repository {
	return &walletRepository{
		client: client,
		logger: logger,
	}
}

func (r *walletRepository) CreateWallet(ctx context.Context, w *walletdomain.Wallet) error {
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if w.EnvironmentID == "" {
		w.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	wallet, err := client.Wallet.Create().
		SetID(w.ID).
		SetTenantID(w.TenantID).
		SetCustomerID(w.CustomerID).
		SetName(w.Name).
		SetCurrency(w.Currency).
		SetDescription(w.Description).
		SetMetadata(w.Metadata).
		SetBalance(w.Balance).
		SetCreditBalance(w.CreditBalance).
		SetWalletStatus(string(w.WalletStatus)).
		SetAutoTopupTrigger(string(w.AutoTopupTrigger)).
		SetAutoTopupMinBalance(w.AutoTopupMinBalance).
		SetAutoTopupAmount(w.AutoTopupAmount).
		SetWalletType(string(w.WalletType)).
		SetConfig(w.Config).
		SetConversionRate(w.ConversionRate).
		SetStatus(string(w.Status)).
		SetCreatedBy(w.CreatedBy).
		SetCreatedAt(w.CreatedAt).
		SetUpdatedBy(w.UpdatedBy).
		SetUpdatedAt(w.UpdatedAt).
		SetEnvironmentID(w.EnvironmentID).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create wallet").
			WithReportableDetails(map[string]interface{}{
				"customer_id": w.CustomerID,
				"currency":    w.Currency,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update the input wallet with created data
	*w = *walletdomain.FromEnt(wallet)
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

	return walletdomain.FromEnt(w), nil
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
		return ierr.WithError(err).
			WithHint("Failed to update wallet status").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
				"status":    status,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		return ierr.NewError("wallet not found or already updated").
			WithHint("The wallet may not exist or has already been updated").
			WithReportableDetails(map[string]interface{}{
				"wallet_id": id,
				"status":    status,
			}).
			Mark(ierr.ErrNotFound)
	}

	return nil
}

// FindEligibleCredits retrieves valid credits for debit operation with pagination
// the credits are sorted by expiry date and then by credit amount
// this is to ensure that the oldest credits are used first and if there are
// multiple credits with the same expiry date, the credits with the highest credit amount are used first
func (r *walletRepository) FindEligibleCredits(ctx context.Context, walletID string, requiredAmount decimal.Decimal, pageSize int) ([]*walletdomain.Transaction, error) {
	var allCredits []*ent.WalletTransaction
	offset := 0

	for {
		credits, err := r.client.Querier(ctx).WalletTransaction.Query().
			Where(
				wallettransaction.WalletID(walletID),
				wallettransaction.Type(string(types.TransactionTypeCredit)),
				wallettransaction.CreditsAvailableGT(decimal.Zero),
				wallettransaction.Or(
					wallettransaction.ExpiryDateIsNil(),
					wallettransaction.ExpiryDateGTE(time.Now().UTC()),
				),
				wallettransaction.StatusEQ(string(types.StatusPublished)),
			).
			Order(ent.Asc(wallettransaction.FieldExpiryDate)).
			Order(ent.Desc(wallettransaction.FieldCreditAmount)).
			Offset(offset).
			Limit(pageSize).
			All(ctx)

		if err != nil {
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
	remainingAmount := amount

	for _, credit := range credits {
		if remainingAmount.IsZero() {
			break
		}

		toConsume := decimal.Min(remainingAmount, credit.CreditsAvailable)
		newAvailable := credit.CreditsAvailable.Sub(toConsume)

		// Update credit's available amount
		_, err := r.client.Querier(ctx).WalletTransaction.UpdateOne(&ent.WalletTransaction{
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
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if tx.EnvironmentID == "" {
		tx.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	transaction, err := client.WalletTransaction.Create().
		SetID(tx.ID).
		SetTenantID(tx.TenantID).
		SetWalletID(tx.WalletID).
		SetType(string(tx.Type)).
		SetAmount(tx.Amount).
		SetCreditAmount(tx.CreditAmount).
		SetReferenceType(string(tx.ReferenceType)).
		SetReferenceID(tx.ReferenceID).
		SetDescription(tx.Description).
		SetMetadata(tx.Metadata).
		SetStatus(string(tx.Status)).
		SetTransactionStatus(string(tx.TxStatus)).
		SetTransactionReason(string(tx.TransactionReason)).
		SetCreditsAvailable(tx.CreditsAvailable).
		SetNillableExpiryDate(tx.ExpiryDate).
		SetCreditBalanceBefore(tx.CreditBalanceBefore).
		SetCreditBalanceAfter(tx.CreditBalanceAfter).
		SetCreatedAt(tx.CreatedAt).
		SetCreatedBy(tx.CreatedBy).
		SetUpdatedAt(tx.UpdatedAt).
		SetUpdatedBy(tx.UpdatedBy).
		SetEnvironmentID(tx.EnvironmentID).
		SetIdempotencyKey(tx.IdempotencyKey).
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
	err := r.client.Querier(ctx).Wallet.Update().
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

	return nil
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

func (r *walletRepository) ListWalletTransactions(ctx context.Context, f *types.WalletTransactionFilter) ([]*walletdomain.Transaction, error) {
	client := r.client.Querier(ctx)
	query := client.WalletTransaction.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, f, query)

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
	case "updated_at":
		return wallettransaction.FieldUpdatedAt
	case "amount":
		return wallettransaction.FieldAmount
	default:
		return field
	}
}

func (o WalletTransactionQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.WalletTransactionFilter, query WalletTransactionQuery) WalletTransactionQuery {
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
		query = query.Where(wallettransaction.TransactionReason(string(*f.TransactionReason)))
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
		).
		SetName(w.Name).
		SetDescription(w.Description).
		SetMetadata(w.Metadata).
		SetConfig(w.Config).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC())

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

	return nil
}
