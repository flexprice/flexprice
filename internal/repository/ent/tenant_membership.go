package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entMembership "github.com/flexprice/flexprice/ent/tenantmembership"
	domainMembership "github.com/flexprice/flexprice/internal/domain/tenantmembership"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type tenantMembershipRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewTenantMembershipRepository creates a tenant membership repository.
func NewTenantMembershipRepository(client postgres.IClient, logger *logger.Logger) domainMembership.Repository {
	return &tenantMembershipRepository{client: client, logger: logger}
}

func (r *tenantMembershipRepository) Create(ctx context.Context, m *domainMembership.Membership) error {
	span := StartRepositorySpan(ctx, "tenant_membership", "create", map[string]interface{}{
		"membership_id": m.ID,
		"user_id":       m.UserID,
		"tenant_id":     m.TenantID,
	})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).TenantMembership.
		Create().
		SetID(m.ID).
		SetUserID(m.UserID).
		SetTenantID(m.TenantID).
		SetRoles(m.Roles).
		SetStatus(string(m.Status)).
		SetCreatedBy(m.CreatedBy).
		SetUpdatedBy(m.UpdatedBy).
		SetCreatedAt(m.CreatedAt).
		SetUpdatedAt(m.UpdatedAt).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create tenant membership").
			WithReportableDetails(map[string]interface{}{
				"user_id":   m.UserID,
				"tenant_id": m.TenantID,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *tenantMembershipRepository) GetByUserAndTenant(ctx context.Context, userID, tenantID string) (*domainMembership.Membership, error) {
	span := StartRepositorySpan(ctx, "tenant_membership", "get_by_user_and_tenant", map[string]interface{}{
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	defer FinishSpan(span)

	row, err := r.client.Reader(ctx).TenantMembership.
		Query().
		Where(
			entMembership.UserID(userID),
			entMembership.TenantID(tenantID),
			entMembership.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Tenant membership not found").
				WithReportableDetails(map[string]interface{}{
					"user_id":   userID,
					"tenant_id": tenantID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve tenant membership").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainMembership.FromEnt(row), nil
}

func (r *tenantMembershipRepository) ListByUser(ctx context.Context, userID string) ([]*domainMembership.Membership, error) {
	span := StartRepositorySpan(ctx, "tenant_membership", "list_by_user", map[string]interface{}{
		"user_id": userID,
	})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).TenantMembership.
		Query().
		Where(
			entMembership.UserID(userID),
			entMembership.Status(string(types.StatusPublished)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tenant memberships for user").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainMembership.FromEntList(rows), nil
}

func (r *tenantMembershipRepository) ListByTenant(ctx context.Context, tenantID string) ([]*domainMembership.Membership, error) {
	span := StartRepositorySpan(ctx, "tenant_membership", "list_by_tenant", map[string]interface{}{
		"tenant_id": tenantID,
	})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).TenantMembership.
		Query().
		Where(
			entMembership.TenantID(tenantID),
			entMembership.Status(string(types.StatusPublished)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tenant memberships").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainMembership.FromEntList(rows), nil
}

func (r *tenantMembershipRepository) UpdateRoles(ctx context.Context, userID, tenantID string, roles []string) error {
	span := StartRepositorySpan(ctx, "tenant_membership", "update_roles", map[string]interface{}{
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	defer FinishSpan(span)

	affected, err := r.client.Writer(ctx).TenantMembership.
		Update().
		Where(
			entMembership.UserID(userID),
			entMembership.TenantID(tenantID),
			entMembership.Status(string(types.StatusPublished)),
		).
		SetRoles(roles).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update membership roles").
			Mark(ierr.ErrDatabase)
	}
	if affected == 0 {
		return ierr.NewError("tenant membership not found").
			WithHint("Tenant membership not found").
			WithReportableDetails(map[string]interface{}{
				"user_id":   userID,
				"tenant_id": tenantID,
			}).
			Mark(ierr.ErrNotFound)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *tenantMembershipRepository) Delete(ctx context.Context, userID, tenantID string) error {
	span := StartRepositorySpan(ctx, "tenant_membership", "delete", map[string]interface{}{
		"user_id":   userID,
		"tenant_id": tenantID,
	})
	defer FinishSpan(span)

	affected, err := r.client.Writer(ctx).TenantMembership.
		Update().
		Where(
			entMembership.UserID(userID),
			entMembership.TenantID(tenantID),
			entMembership.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to archive tenant membership").
			Mark(ierr.ErrDatabase)
	}
	if affected == 0 {
		return ierr.NewError("tenant membership not found").
			WithHint("Tenant membership not found or already archived").
			WithReportableDetails(map[string]interface{}{
				"user_id":   userID,
				"tenant_id": tenantID,
			}).
			Mark(ierr.ErrNotFound)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *tenantMembershipRepository) CountByTenant(ctx context.Context, tenantID string) (int64, error) {
	count, err := r.client.Reader(ctx).TenantMembership.
		Query().
		Where(
			entMembership.TenantID(tenantID),
			entMembership.Status(string(types.StatusPublished)),
		).
		Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count tenant memberships").
			Mark(ierr.ErrDatabase)
	}
	return int64(count), nil
}

func (r *tenantMembershipRepository) CountByUser(ctx context.Context, userID string) (int64, error) {
	count, err := r.client.Reader(ctx).TenantMembership.
		Query().
		Where(
			entMembership.UserID(userID),
			entMembership.Status(string(types.StatusPublished)),
		).
		Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count user memberships").
			Mark(ierr.ErrDatabase)
	}
	return int64(count), nil
}
