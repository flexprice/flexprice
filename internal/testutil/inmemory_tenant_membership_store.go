package testutil

import (
	"context"
	"sync"
	"time"

	domainMembership "github.com/flexprice/flexprice/internal/domain/tenantmembership"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryTenantMembershipStore is an in-memory tenant membership repository.
type InMemoryTenantMembershipStore struct {
	mu   sync.Mutex
	rows map[string]*domainMembership.Membership
}

// NewInMemoryTenantMembershipStore creates an empty membership store.
func NewInMemoryTenantMembershipStore() *InMemoryTenantMembershipStore {
	return &InMemoryTenantMembershipStore{rows: make(map[string]*domainMembership.Membership)}
}

func (s *InMemoryTenantMembershipStore) key(userID, tenantID string) string {
	return userID + "|" + tenantID
}

func (s *InMemoryTenantMembershipStore) Create(_ context.Context, m *domainMembership.Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(m.UserID, m.TenantID)
	if existing, ok := s.rows[k]; ok && existing.Status == types.StatusPublished {
		return ierr.NewError("tenant membership already exists").Mark(ierr.ErrAlreadyExists)
	}
	cp := *m
	if cp.Roles == nil {
		cp.Roles = []string{}
	}
	s.rows[k] = &cp
	return nil
}

func (s *InMemoryTenantMembershipStore) GetByUserAndTenant(_ context.Context, userID, tenantID string) (*domainMembership.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.rows[s.key(userID, tenantID)]
	if !ok || m.Status != types.StatusPublished {
		return nil, ierr.NewError("tenant membership not found").Mark(ierr.ErrNotFound)
	}
	cp := *m
	return &cp, nil
}

func (s *InMemoryTenantMembershipStore) ListByUser(_ context.Context, userID string) ([]*domainMembership.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domainMembership.Membership
	for _, m := range s.rows {
		if m.UserID == userID && m.Status == types.StatusPublished {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *InMemoryTenantMembershipStore) ListByTenant(_ context.Context, tenantID string) ([]*domainMembership.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domainMembership.Membership
	for _, m := range s.rows {
		if m.TenantID == tenantID && m.Status == types.StatusPublished {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *InMemoryTenantMembershipStore) UpdateRoles(ctx context.Context, userID, tenantID string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.rows[s.key(userID, tenantID)]
	if !ok || m.Status != types.StatusPublished {
		return ierr.NewError("tenant membership not found").Mark(ierr.ErrNotFound)
	}
	m.Roles = roles
	m.UpdatedBy = types.GetUserID(ctx)
	m.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *InMemoryTenantMembershipStore) Delete(ctx context.Context, userID, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.rows[s.key(userID, tenantID)]
	if !ok || m.Status != types.StatusPublished {
		return ierr.NewError("tenant membership not found").Mark(ierr.ErrNotFound)
	}
	m.Status = types.StatusArchived
	m.UpdatedBy = types.GetUserID(ctx)
	m.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *InMemoryTenantMembershipStore) CountByTenant(_ context.Context, tenantID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, m := range s.rows {
		if m.TenantID == tenantID && m.Status == types.StatusPublished {
			n++
		}
	}
	return n, nil
}

func (s *InMemoryTenantMembershipStore) CountByUser(_ context.Context, userID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, m := range s.rows {
		if m.UserID == userID && m.Status == types.StatusPublished {
			n++
		}
	}
	return n, nil
}

// Clear removes all memberships.
func (s *InMemoryTenantMembershipStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = make(map[string]*domainMembership.Membership)
}
