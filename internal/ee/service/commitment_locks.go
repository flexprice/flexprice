package service

import (
	"context"
	"slices"
	"strings"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type commitmentGuardKey struct{}

type commitmentGuard struct {
	transaction   any
	tenantID      string
	environmentID string
}

func (s *commitmentService) admitTerminationInvoice(ctx context.Context, c *commitment.Commitment, fee *decimal.Decimal) (context.Context, error) {
	var invoiceIDs []string
	if fee != nil && fee.IsPositive() {
		if s.InvoiceRepo == nil {
			return nil, ierr.NewError("termination invoices are unavailable").Mark(ierr.ErrServiceUnavailable)
		}
		inv, err := s.InvoiceRepo.GetByIdempotencyKey(ctx, "commitment_termination_"+c.ID)
		if err != nil && !ierr.IsNotFound(err) {
			return nil, err
		}
		if inv != nil {
			locked, err := s.InvoiceRepo.GetForUpdateNowait(ctx, inv.ID)
			if err != nil {
				return nil, err
			}
			if locked == nil || locked.TenantID != types.GetTenantID(ctx) || locked.EnvironmentID != types.GetEnvironmentID(ctx) || !strings.EqualFold(locked.Currency, c.Currency) || pointerString(locked.SubscriptionID) != c.SubscriptionID {
				return nil, ierr.Validation("termination fee invoice has inconsistent attribution")
			}
			invoiceIDs = append(invoiceIDs, locked.ID)
		}
	}
	return types.WithInvoiceLockAdmission(ctx, invoiceIDs)
}

func (s *commitmentService) lockCollectionOwner(ctx context.Context, subscriptionID string) (context.Context, error) {
	if s == nil || s.DB == nil || s.SubRepo == nil || s.BillingTermsStore == nil {
		return nil, ierr.NewError("commitment collection locking is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	if ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || s.DB.TxFromContext(ctx) == nil {
		return nil, ierr.NewError("commitment collection locking requires a scoped transaction").Mark(ierr.ErrInvalidOperation)
	}
	adapter := &commitmentTermsOwner{subscriptions: s.SubRepo}
	sub, err := adapter.subscription(types.WithForceWriter(ctx), subscriptionID)
	if err != nil {
		return nil, err
	}
	scopes, err := commitmentEligibilityScopes(ctx, adapter, sub)
	if err != nil {
		return nil, err
	}
	store, ok := s.BillingTermsStore.(domain.NonblockingEligibilityStore)
	if !ok {
		return nil, ierr.NewError("commitment collection requires nonblocking eligibility locks").Mark(ierr.ErrServiceUnavailable)
	}
	if err := store.TryLockEligibility(ctx, scopes); err != nil {
		return nil, err
	}
	fresh, err := adapter.subscription(types.WithForceWriter(ctx), subscriptionID)
	if err != nil {
		return nil, err
	}
	freshScopes, err := commitmentEligibilityScopes(ctx, adapter, fresh)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(scopes, freshScopes) || fresh.SubscriptionType != sub.SubscriptionType || pointerString(fresh.ParentSubscriptionID) != pointerString(sub.ParentSubscriptionID) {
		return nil, lifecycleMembershipChanged()
	}
	guard := &commitmentGuard{transaction: ctx.Value(types.CtxDBTransaction), tenantID: types.GetTenantID(ctx), environmentID: types.GetEnvironmentID(ctx)}
	return context.WithValue(ctx, commitmentGuardKey{}, guard), nil
}

func commitmentEligibilityScopes(ctx context.Context, adapter *commitmentTermsOwner, sub *subscription.Subscription) ([]domain.EligibilityScope, error) {
	scopes := []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}
	if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
		if sub.ParentSubscriptionID == nil || strings.TrimSpace(*sub.ParentSubscriptionID) == "" || *sub.ParentSubscriptionID == sub.ID {
			return nil, ierr.Validation("grouped collection requires its parent subscription")
		}
		parent, err := adapter.subscription(ctx, *sub.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
		if parent.SubscriptionType != types.SubscriptionTypeParent || parent.ParentSubscriptionID != nil {
			return nil, ierr.Validation("grouped collection has an invalid parent")
		}
		scopes = append(scopes, domain.EligibilityScope{Type: "subscription", ID: parent.ID})
	}
	slices.SortFunc(scopes, func(a, b domain.EligibilityScope) int { return strings.Compare(a.ID, b.ID) })
	return scopes, nil
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *commitmentService) lockCommitmentKey(ctx context.Context, key string) error {
	guard, _ := ctx.Value(commitmentGuardKey{}).(*commitmentGuard)
	if guard != nil {
		if guard.transaction != ctx.Value(types.CtxDBTransaction) || guard.tenantID != types.GetTenantID(ctx) || guard.environmentID != types.GetEnvironmentID(ctx) {
			return ierr.NewError("commitment guard belongs to another transaction or scope").Mark(ierr.ErrInvalidOperation)
		}
	}
	if guard != nil || types.HasInvoiceLockAdmission(ctx) {
		locker, ok := s.DB.(postgres.NonblockingLocker)
		if !ok {
			return ierr.NewError("guarded commitment writes require nonblocking advisory locks").Mark(ierr.ErrServiceUnavailable)
		}
		return locker.TryLock(ctx, postgres.LockRequest{Key: key})
	}
	return s.DB.LockWithWait(ctx, postgres.LockRequest{Key: key})
}
