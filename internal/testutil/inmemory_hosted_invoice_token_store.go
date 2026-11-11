package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/hostedinvoicetoken"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryHostedInvoiceTokenStore implements hostedinvoicetoken.Repository for tests.
type InMemoryHostedInvoiceTokenStore struct {
	*InMemoryStore[*hostedinvoicetoken.HostedInvoiceToken]
}

func NewInMemoryHostedInvoiceTokenStore() *InMemoryHostedInvoiceTokenStore {
	return &InMemoryHostedInvoiceTokenStore{
		InMemoryStore: NewInMemoryStore[*hostedinvoicetoken.HostedInvoiceToken](),
	}
}

// currentTokensFilterFn keeps the scoped, unrevoked tokens of the invoice ids in filter.
func currentTokensFilterFn(ctx context.Context, t *hostedinvoicetoken.HostedInvoiceToken, filter interface{}) bool {
	invoiceIDs, _ := filter.([]string)
	if t == nil || t.RevokedAt != nil || !lo.Contains(invoiceIDs, t.InvoiceID) {
		return false
	}
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok && t.TenantID != tenantID {
		return false
	}
	return CheckEnvironmentFilter(ctx, t.EnvironmentID)
}

func (s *InMemoryHostedInvoiceTokenStore) Create(ctx context.Context, t *hostedinvoicetoken.HostedInvoiceToken) error {
	if current, _ := s.ListCurrentByInvoiceIDs(ctx, []string{t.InvoiceID}); len(current) > 0 {
		return ierr.NewError("hosted invoice token already exists").Mark(ierr.ErrAlreadyExists)
	}
	return s.InMemoryStore.Create(ctx, t.ID, t)
}

func (s *InMemoryHostedInvoiceTokenStore) GetUnscoped(ctx context.Context, id string) (*hostedinvoicetoken.HostedInvoiceToken, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryHostedInvoiceTokenStore) GetCurrentByInvoiceID(ctx context.Context, invoiceID string) (*hostedinvoicetoken.HostedInvoiceToken, error) {
	current, err := s.ListCurrentByInvoiceIDs(ctx, []string{invoiceID})
	if err != nil {
		return nil, err
	}
	if len(current) == 0 {
		return nil, ierr.NewError("hosted invoice token not found").Mark(ierr.ErrNotFound)
	}
	return current[0], nil
}

func (s *InMemoryHostedInvoiceTokenStore) ListCurrentByInvoiceIDs(ctx context.Context, invoiceIDs []string) ([]*hostedinvoicetoken.HostedInvoiceToken, error) {
	return s.InMemoryStore.List(ctx, invoiceIDs, currentTokensFilterFn, nil)
}

func (s *InMemoryHostedInvoiceTokenStore) UpdateExpiresAt(ctx context.Context, invoiceID string, expiresAt time.Time) error {
	t, err := s.GetCurrentByInvoiceID(ctx, invoiceID)
	if err != nil {
		return err
	}
	t.ExpiresAt = expiresAt
	return s.InMemoryStore.Update(ctx, t.ID, t)
}

func (s *InMemoryHostedInvoiceTokenStore) Revoke(ctx context.Context, id string) error {
	t, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}
	if t.RevokedAt != nil {
		return nil
	}
	t.RevokedAt = lo.ToPtr(time.Now().UTC())
	return s.InMemoryStore.Update(ctx, t.ID, t)
}

func (s *InMemoryHostedInvoiceTokenStore) RevokeByInvoiceID(ctx context.Context, invoiceID string) error {
	t, err := s.GetCurrentByInvoiceID(ctx, invoiceID)
	if ierr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Revoke(ctx, t.ID)
}
