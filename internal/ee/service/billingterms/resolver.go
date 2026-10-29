package billingterms

import (
	"context"
	"reflect"
	"sort"
	"strings"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// OwnerResolver resolves registered owners without a mutable service registry.
type OwnerResolver interface {
	Snapshot(context.Context, domain.OwnerRef) (*domain.OwnerSnapshot, error)
}

type ownerResolver struct {
	adapters map[types.BillingTermOwnerType]domain.OwnerAdapter
}

// NewOwnerResolver validates and copies the owner adapter registry.
func NewOwnerResolver(adapters ...domain.OwnerAdapter) (OwnerResolver, error) {
	r := &ownerResolver{adapters: make(map[types.BillingTermOwnerType]domain.OwnerAdapter, len(adapters))}
	for _, adapter := range adapters {
		if nilInterface(adapter) {
			return nil, ierr.Validation("billing owner adapter is required")
		}
		kind := adapter.Type()
		if err := kind.Validate(); err != nil {
			return nil, err
		}
		if _, exists := r.adapters[kind]; exists {
			return nil, ierr.Validation("billing owner adapter is already registered")
		}
		r.adapters[kind] = adapter
	}
	return r, nil
}

func (r *ownerResolver) Snapshot(ctx context.Context, owner domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	if err := requestScope(ctx, owner); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, unavailable("billing owner resolver is unavailable")
	}
	adapter, ok := r.adapters[owner.Type]
	if !ok {
		return nil, ierr.Validation("billing owner type is not supported")
	}
	snapshot, err := adapter.Snapshot(ctx, owner)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.Owner != owner || snapshot.TenantID != types.GetTenantID(ctx) || snapshot.EnvironmentID != types.GetEnvironmentID(ctx) {
		return nil, integrity("billing owner snapshot does not match its scoped source")
	}
	return cloneOwner(snapshot), nil
}

func requestScope(ctx context.Context, owner domain.OwnerRef) error {
	if nilInterface(ctx) {
		return ierr.Validation("billing context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(types.GetTenantID(ctx)) == "" || strings.TrimSpace(types.GetEnvironmentID(ctx)) == "" {
		return ierr.Validation("tenant and environment context are required")
	}
	if err := owner.Type.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(owner.ID) == "" || len(owner.ID) > 50 {
		return ierr.Validation("billing owner id is required and must fit 50 bytes")
	}
	return nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func cloneOwner(owner *domain.OwnerSnapshot) *domain.OwnerSnapshot {
	if owner == nil {
		return nil
	}
	copy := *owner
	copy.SubscriptionID = clonePointer(owner.SubscriptionID)
	copy.EligibilityScopes = append([]domain.EligibilityScope{}, owner.EligibilityScopes...)
	return &copy
}

func integrity(message string) error { return ierr.NewError(message).Mark(ierr.ErrInternal) }
func unavailable(message string) error {
	return ierr.NewError(message).Mark(ierr.ErrServiceUnavailable)
}

func (s *service) snapshot(ctx context.Context, owner domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	snapshot, err := s.resolver.Snapshot(ctx, owner)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.Owner != owner || snapshot.TenantID != types.GetTenantID(ctx) || snapshot.EnvironmentID != types.GetEnvironmentID(ctx) {
		return nil, integrity("billing owner snapshot scope does not match context")
	}
	snapshot = cloneOwner(snapshot)
	snapshot.EligibilityScopes, err = canonicalEligibilityScopes(snapshot.EligibilityScopes)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func canonicalEligibilityScopes(scopes []domain.EligibilityScope) ([]domain.EligibilityScope, error) {
	if len(scopes) > domain.MaxEligibilityScopes {
		return nil, ierr.Validationf("billing eligibility cannot exceed %d scopes", domain.MaxEligibilityScopes)
	}
	seen := make(map[domain.EligibilityScope]bool, len(scopes))
	canonical := make([]domain.EligibilityScope, 0, len(scopes))
	for _, scope := range scopes {
		if strings.TrimSpace(scope.Type) == "" || len(scope.Type) > 50 || strings.TrimSpace(scope.ID) == "" || len(scope.ID) > 50 {
			return nil, ierr.Validation("eligibility scope type and id must be nonempty and fit 50 bytes")
		}
		if !seen[scope] {
			seen[scope] = true
			canonical = append(canonical, scope)
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Type != canonical[j].Type {
			return canonical[i].Type < canonical[j].Type
		}
		return canonical[i].ID < canonical[j].ID
	})
	return canonical, nil
}
