package types

import (
	"context"
	"reflect"
	"strings"
	"sync"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

type invoiceLockAdmissionKey struct{}

type invoiceLockAdmission struct {
	mu            sync.Mutex
	transaction   any
	tenantID      string
	environmentID string
	invoiceIDs    map[string]struct{}
}

// HasInvoiceLockAdmission reports whether the context carries a sealed invoice set.
func HasInvoiceLockAdmission(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	admission, _ := ctx.Value(invoiceLockAdmissionKey{}).(*invoiceLockAdmission)
	return admission != nil
}

// WithInvoiceLockAdmission seals the existing invoice set before lifecycle guards are acquired.
// Re-entering an admitted transaction cannot add another existing invoice.
func WithInvoiceLockAdmission(ctx context.Context, invoiceIDs []string) (context.Context, error) {
	if ctx == nil || GetTenantID(ctx) == "" || GetEnvironmentID(ctx) == "" {
		return nil, ierr.Validation("invoice lock admission requires tenant and environment scope")
	}
	transaction := ctx.Value(CtxDBTransaction)
	value := reflect.ValueOf(transaction)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, ierr.NewError("invoice lock admission requires a transaction").Mark(ierr.ErrInvalidOperation)
	}
	if len(invoiceIDs) > 500 {
		return nil, ierr.Validation("invoice lock admission supports at most 500 existing invoices")
	}
	for _, id := range invoiceIDs {
		if strings.TrimSpace(id) == "" || len(id) > 50 {
			return nil, ierr.Validation("invoice lock admission requires valid invoice IDs")
		}
	}
	if admission, _ := ctx.Value(invoiceLockAdmissionKey{}).(*invoiceLockAdmission); admission != nil {
		if err := admission.validate(ctx); err != nil {
			return nil, err
		}
		for _, id := range invoiceIDs {
			if err := CheckInvoiceLockAdmission(ctx, id); err != nil {
				return nil, err
			}
		}
		return ctx, nil
	}
	admission := &invoiceLockAdmission{
		transaction: transaction, tenantID: GetTenantID(ctx), environmentID: GetEnvironmentID(ctx),
		invoiceIDs: make(map[string]struct{}, len(invoiceIDs)),
	}
	for _, id := range invoiceIDs {
		admission.invoiceIDs[id] = struct{}{}
	}
	return context.WithValue(ctx, invoiceLockAdmissionKey{}, admission), nil
}

// CheckInvoiceLockAdmission rejects late invoice locks and writes within a guarded lifecycle transaction.
func CheckInvoiceLockAdmission(ctx context.Context, invoiceID string) error {
	admission, _ := ctx.Value(invoiceLockAdmissionKey{}).(*invoiceLockAdmission)
	if admission == nil {
		return nil
	}
	if err := admission.validate(ctx); err != nil {
		return err
	}
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if _, exists := admission.invoiceIDs[invoiceID]; !exists {
		return ierr.NewError("invoice dependencies changed; retry the lifecycle operation").
			WithReportableDetails(map[string]any{"invoice_id": invoiceID}).Mark(ierr.ErrVersionConflict)
	}
	return nil
}

// ValidateInvoiceCreation checks the guarded transaction before an invoice insert can have side effects.
func ValidateInvoiceCreation(ctx context.Context, tenantID, environmentID string) error {
	admission, _ := ctx.Value(invoiceLockAdmissionKey{}).(*invoiceLockAdmission)
	if admission == nil {
		return nil
	}
	if err := admission.validate(ctx); err != nil {
		return err
	}
	if tenantID != admission.tenantID || environmentID != admission.environmentID {
		return ierr.Validation("created invoice must match the lifecycle transaction scope")
	}
	return nil
}

// AdmitCreatedInvoice records a successful insert in this transaction, never an idempotency lookup.
func AdmitCreatedInvoice(ctx context.Context, invoiceID string) error {
	admission, _ := ctx.Value(invoiceLockAdmissionKey{}).(*invoiceLockAdmission)
	if admission == nil {
		return nil
	}
	if err := admission.validate(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(invoiceID) == "" || len(invoiceID) > 50 {
		return ierr.Validation("created invoice requires a valid ID")
	}
	admission.mu.Lock()
	defer admission.mu.Unlock()
	admission.invoiceIDs[invoiceID] = struct{}{}
	return nil
}

func (a *invoiceLockAdmission) validate(ctx context.Context) error {
	transaction := ctx.Value(CtxDBTransaction)
	value := reflect.ValueOf(transaction)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || transaction != a.transaction || GetTenantID(ctx) != a.tenantID || GetEnvironmentID(ctx) != a.environmentID {
		return ierr.NewError("invoice lock admission belongs to another transaction or scope").Mark(ierr.ErrInvalidOperation)
	}
	return ctx.Err()
}

func withoutInvoiceLockAdmission(ctx context.Context) context.Context {
	if ctx.Value(invoiceLockAdmissionKey{}) == nil {
		return ctx
	}
	return context.WithValue(ctx, invoiceLockAdmissionKey{}, nil)
}
