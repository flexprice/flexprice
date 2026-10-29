package ent

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/ent"
	invoicedb "github.com/flexprice/flexprice/ent/invoice"
	linedb "github.com/flexprice/flexprice/ent/invoicelineitem"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type billingTermsExecutionStore struct {
	client postgres.IClient
}

// NewBillingTermsExecutionStore shares billing transactions, locks and financial source reads.
func NewBillingTermsExecutionStore(client postgres.IClient) domain.ExecutionStore {
	if client == nil {
		return nil
	}
	return &billingTermsExecutionStore{client: client}
}

func (s *billingTermsExecutionStore) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if _, _, err := billingScope(ctx); err != nil {
		return err
	}
	if fn == nil {
		return ierr.Validation("billing transaction callback is required")
	}
	return s.client.WithTx(ctx, fn)
}

func (s *billingTermsExecutionStore) InTransaction(ctx context.Context) bool {
	if nilBillingContext(ctx) || s == nil || s.client == nil {
		return false
	}
	return s.client.TxFromContext(ctx) != nil
}

func (s *billingTermsExecutionStore) LockOwner(ctx context.Context, owner domain.OwnerRef) error {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return err
	}
	if err := billingOwner(owner); err != nil {
		return err
	}
	if s.client.TxFromContext(ctx) == nil {
		return ierr.Validation("billing owner locking requires a transaction")
	}
	if err := s.client.LockWithWait(ctx, postgres.LockRequest{Key: billingOwnerLockKey(tenantID, environmentID, owner)}); err != nil {
		return billingPersistenceError(err, "lock billing owner")
	}
	return nil
}

func (s *billingTermsExecutionStore) LockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	keys, err := s.eligibilityKeys(ctx, scopes)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.client.LockWithWait(ctx, postgres.LockRequest{Key: key}); err != nil {
			return billingPersistenceError(err, "lock billing eligibility")
		}
	}
	return nil
}

func (s *billingTermsExecutionStore) TryLockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	keys, err := s.eligibilityKeys(ctx, scopes)
	if err != nil {
		return err
	}
	for _, key := range keys {
		rows, err := s.client.Writer(ctx).QueryContext(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", key)
		if err != nil {
			return billingPersistenceError(err, "try locking billing eligibility")
		}
		var acquired bool
		if rows.Next() {
			err = rows.Scan(&acquired)
		} else {
			err = rows.Err()
		}
		rowErr := rows.Err()
		rows.Close() // #nosec G104 -- best-effort close, error non-fatal
		if err != nil {
			return billingPersistenceError(err, "read billing eligibility lock result")
		}
		if rowErr != nil {
			return billingPersistenceError(rowErr, "read billing eligibility lock result")
		}
		if !acquired {
			return ierr.NewError("subscription eligibility is being changed; retry the request").Mark(ierr.ErrVersionConflict)
		}
	}
	return nil
}

func (s *billingTermsExecutionStore) eligibilityKeys(ctx context.Context, scopes []domain.EligibilityScope) ([]string, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return nil, err
	}
	if !s.InTransaction(ctx) {
		return nil, ierr.Validation("billing eligibility locking requires a transaction")
	}
	if len(scopes) > domain.MaxEligibilityScopes {
		return nil, ierr.Validationf("eligibility scopes cannot exceed %d entries", domain.MaxEligibilityScopes)
	}
	ordered := make([]domain.EligibilityScope, 0, len(scopes))
	seen := make(map[domain.EligibilityScope]bool, len(scopes))
	for _, scope := range scopes {
		if err := billingIdentifier("eligibility scope type", scope.Type); err != nil {
			return nil, err
		}
		if err := billingIdentifier("eligibility scope id", scope.ID); err != nil {
			return nil, err
		}
		if !seen[scope] {
			ordered = append(ordered, scope)
			seen[scope] = true
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Type != ordered[j].Type {
			return ordered[i].Type < ordered[j].Type
		}
		return ordered[i].ID < ordered[j].ID
	})
	keys := make([]string, 0, len(ordered))
	for _, scope := range ordered {
		keys = append(keys, billingScopedLockKey("eligibility", tenantID, environmentID, scope.Type, scope.ID))
	}
	return keys, nil
}

func billingOwnerLockKey(tenantID, environmentID string, owner domain.OwnerRef) string {
	return billingScopedLockKey("owner", tenantID, environmentID, string(owner.Type), owner.ID)
}

func billingScopedLockKey(namespace string, parts ...string) string {
	var key strings.Builder
	key.WriteString("billingterms:")
	key.WriteString(namespace)
	key.WriteByte(':')
	for _, part := range parts {
		key.WriteString(strconv.Itoa(len(part)))
		key.WriteByte(':')
		key.WriteString(part)
	}
	return key.String()
}

func (s *billingTermsExecutionStore) BindingStates(ctx context.Context, items []*domain.ScheduleItem) ([]domain.BindingState, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) > billingMaximumPage {
		return nil, ierr.Validationf("binding state reads cannot exceed %d items", billingMaximumPage)
	}
	if len(items) == 0 {
		return []domain.BindingState{}, nil
	}
	ids := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item == nil || item.InvoiceID == nil || item.InvoiceLineItemID == nil {
			return nil, ierr.Validation("binding state requires an item with both invoice references")
		}
		if err := billingModelScope(item.TenantID, item.EnvironmentID, tenantID, environmentID); err != nil {
			return nil, err
		}
		if err := billingOwner(domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}); err != nil {
			return nil, err
		}
		for label, id := range map[string]string{"schedule item id": item.ID, "invoice id": *item.InvoiceID, "invoice line id": *item.InvoiceLineItemID} {
			if err := billingIdentifier(label, id); err != nil {
				return nil, err
			}
		}
		if seen[*item.InvoiceLineItemID] {
			return nil, ierr.Validation("binding state contains duplicate invoice line references")
		}
		seen[*item.InvoiceLineItemID] = true
		ids = append(ids, *item.InvoiceLineItemID)
	}
	rows, err := s.client.Writer(ctx).InvoiceLineItem.Query().Where(
		linedb.TenantID(tenantID), linedb.EnvironmentID(environmentID), linedb.IDIn(ids...),
		linedb.StatusIn(string(types.StatusPublished), string(types.StatusArchived)),
	).Select(
		linedb.FieldID, linedb.FieldTenantID, linedb.FieldEnvironmentID, linedb.FieldStatus,
		linedb.FieldInvoiceID, linedb.FieldBillingScheduleItemID, linedb.FieldCustomerID,
		linedb.FieldSubscriptionID, linedb.FieldCommitmentID, linedb.FieldAmount, linedb.FieldQuantity,
		linedb.FieldCurrency, linedb.FieldPeriodStart, linedb.FieldPeriodEnd, linedb.FieldFxConversion, linedb.FieldCustomCurrency,
	).WithInvoice(func(q *ent.InvoiceQuery) {
		q.Where(invoicedb.TenantID(tenantID), invoicedb.EnvironmentID(environmentID), invoicedb.StatusIn(string(types.StatusPublished), string(types.StatusArchived))).
			Select(invoicedb.FieldID, invoicedb.FieldTenantID, invoicedb.FieldEnvironmentID, invoicedb.FieldStatus,
				invoicedb.FieldCustomerID, invoicedb.FieldCurrency, invoicedb.FieldInvoiceStatus, invoicedb.FieldFxConversion, invoicedb.FieldCustomCurrency)
	}).Limit(len(ids)).All(ctx)
	if err != nil {
		return nil, billingPersistenceError(err, "read billing invoice bindings")
	}
	byID := make(map[string]*ent.InvoiceLineItem, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	states := make([]domain.BindingState, 0, len(items))
	for _, item := range items {
		line := byID[*item.InvoiceLineItemID]
		if err := billingValidateBinding(item, line); err != nil {
			return nil, err
		}
		invoice := line.Edges.Invoice
		states = append(states, domain.BindingState{
			ItemID: item.ID, InvoiceID: invoice.ID, InvoiceLineItemID: line.ID,
			TenantID: tenantID, EnvironmentID: environmentID,
			InvoiceStatus: invoice.InvoiceStatus, InvoiceArchived: invoice.Status == string(types.StatusArchived),
		})
	}
	return states, nil
}

func billingValidateBinding(item *domain.ScheduleItem, line *ent.InvoiceLineItem) error {
	if line == nil || line.Edges.Invoice == nil {
		return billingBindingIntegrity("scoped invoice or source line is missing")
	}
	invoice := line.Edges.Invoice
	if line.InvoiceID != *item.InvoiceID || invoice.ID != *item.InvoiceID || line.BillingScheduleItemID == nil || *line.BillingScheduleItemID != item.ID {
		return billingBindingIntegrity("invoice and line do not match the scheduled source")
	}
	if invoice.CustomerID != item.CustomerID || line.CustomerID != item.CustomerID || !billingSameOptionalString(line.SubscriptionID, item.SubscriptionID) {
		return billingBindingIntegrity("source recipient or subscription does not match")
	}
	if line.PeriodStart == nil || line.PeriodEnd == nil || !line.PeriodStart.Equal(item.ServicePeriodStart) || !line.PeriodEnd.Equal(item.ServicePeriodEnd) {
		return billingBindingIntegrity("source service period does not match")
	}
	if item.OwnerType == types.BillingTermOwnerCommitment {
		if line.CommitmentID == nil || *line.CommitmentID != item.OwnerID {
			return billingBindingIntegrity("source commitment does not match")
		}
	} else if line.CommitmentID != nil {
		return billingBindingIntegrity("generic source has unrelated commitment attribution")
	}
	if invoice.InvoiceStatus == "" || invoice.InvoiceStatus == types.InvoiceStatusSkipped || invoice.InvoiceStatus.Validate() != nil {
		return billingBindingIntegrity("assigned principal has an invalid invoice state")
	}
	if line.Status == string(types.StatusArchived) && invoice.InvoiceStatus != types.InvoiceStatusVoided {
		return billingBindingIntegrity("a live invoice has an archived source line")
	}
	if invoice.CustomCurrency != nil || line.CustomCurrency != nil {
		return billingBindingIntegrity("custom-currency scheduled sources are not supported")
	}
	amount, currency := line.Amount, line.Currency
	if line.FxConversion != nil {
		if invoice.FxConversion == nil || !strings.EqualFold(invoice.FxConversion.ChargeCurrency, line.FxConversion.ChargeCurrency) || !strings.EqualFold(invoice.FxConversion.BillingCurrency, invoice.Currency) {
			return billingBindingIntegrity("original-currency conversion source is inconsistent")
		}
		amount, currency = line.FxConversion.Source.Subtotal, line.FxConversion.ChargeCurrency
	} else if invoice.FxConversion != nil {
		return billingBindingIntegrity("converted invoice is missing the line source snapshot")
	}
	if !amount.Equal(item.Amount) || !line.Quantity.Equal(decimal.NewFromInt(1)) || !strings.EqualFold(currency, item.Currency) || !strings.EqualFold(line.Currency, invoice.Currency) {
		return billingBindingIntegrity("source principal or currency does not match")
	}
	return nil
}

func billingSameOptionalString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func billingBindingIntegrity(hint string) error {
	return ierr.NewError("billing invoice source integrity failed").WithHint(hint).Mark(ierr.ErrInvalidOperation)
}
