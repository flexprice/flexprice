package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	domainMembership "github.com/flexprice/flexprice/internal/domain/tenantmembership"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *userService) createMembership(ctx context.Context, userID, tenantID string, roles []string) error {
	if s.membershipRepo == nil {
		return ierr.NewError("tenant membership repository not configured").
			WithHint("Multi-tenant membership requires a membership repository").
			Mark(ierr.ErrValidation)
	}
	m := domainMembership.New(userID, tenantID, roles)
	actor := types.GetUserID(ctx)
	if actor != "" {
		m.CreatedBy = actor
		m.UpdatedBy = actor
	}
	return s.membershipRepo.Create(ctx, m)
}

func (s *userService) requireMembership(ctx context.Context, userID, tenantID string) (*domainMembership.Membership, error) {
	if s.membershipRepo == nil {
		return nil, ierr.NewError("tenant membership repository not configured").
			WithHint("Multi-tenant membership requires a membership repository").
			Mark(ierr.ErrValidation)
	}
	return s.membershipRepo.GetByUserAndTenant(ctx, userID, tenantID)
}

func (s *userService) overlayMembershipRoles(u *user.User, m *domainMembership.Membership) *user.User {
	if u == nil || m == nil {
		return u
	}
	cp := *u
	cp.Roles = m.Roles
	cp.TenantID = m.TenantID
	return &cp
}

func (s *userService) loadUserForTenant(ctx context.Context, userID, tenantID string) (*user.User, error) {
	u, err := s.userRepo.GetByIDGlobal(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Archived identities are returned so callers (e.g. role updates) can reject
	// them with a validation error; deleted users stay not-found.
	if u.Status != types.StatusPublished && u.Status != types.StatusArchived {
		return nil, ierr.NewError("user not found").
			WithHint("User not found").
			Mark(ierr.ErrNotFound)
	}
	if u.Type == types.UserTypeServiceAccount {
		if u.TenantID != tenantID {
			return nil, ierr.NewError("user not found").
				WithHint("User not found").
				Mark(ierr.ErrNotFound)
		}
		return u, nil
	}
	if s.membershipRepo == nil {
		if u.TenantID != tenantID {
			return nil, ierr.NewError("user not found").
				WithHint("User not found").
				Mark(ierr.ErrNotFound)
		}
		return u, nil
	}
	m, err := s.membershipRepo.GetByUserAndTenant(ctx, userID, tenantID)
	if err != nil {
		if ierr.IsNotFound(err) && u.TenantID == tenantID {
			return u, nil
		}
		return nil, err
	}
	return s.overlayMembershipRoles(u, m), nil
}

// removeMembershipFromTenant archives the membership and, when it was the user's
// last membership, archives the identity and deletes the auth-provider login.
func (s *userService) removeMembershipFromTenant(ctx context.Context, target *user.User, tenantID string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.db.LockWithWait(ctx, postgres.LockRequest{Key: "user_removal:" + tenantID}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to acquire tenant lock for user removal").
				Mark(ierr.ErrInternal)
		}

		if s.membershipRepo == nil {
			_, totalHumanUsers, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				Type:        lo.ToPtr(types.UserTypeUser),
			})
			if err != nil {
				return err
			}
			if totalHumanUsers <= 1 {
				return ierr.NewError("cannot remove the last user in the tenant").
					WithHint("At least one human user must remain in the tenant").
					Mark(ierr.ErrValidation)
			}
			if target.TenantID != tenantID {
				return ierr.NewError("user not found").
					WithHint("User does not belong to your tenant").
					Mark(ierr.ErrNotFound)
			}
			if s.cfg != nil {
				provider := authProvider.NewProvider(s.cfg)
				if err := provider.RemoveUser(ctx, target.ID); err != nil {
					return err
				}
			}
			return s.userRepo.Delete(ctx, target.ID)
		}

		_, memErr := s.membershipRepo.GetByUserAndTenant(ctx, target.ID, tenantID)
		if ierr.IsNotFound(memErr) && target.TenantID == tenantID {
			_, totalHumanUsers, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				Type:        lo.ToPtr(types.UserTypeUser),
			})
			if err != nil {
				return err
			}
			if totalHumanUsers <= 1 {
				return ierr.NewError("cannot remove the last user in the tenant").
					WithHint("At least one human user must remain in the tenant").
					Mark(ierr.ErrValidation)
			}
			if s.cfg != nil {
				provider := authProvider.NewProvider(s.cfg)
				if err := provider.RemoveUser(ctx, target.ID); err != nil {
					return err
				}
			}
			return s.userRepo.Delete(ctx, target.ID)
		}
		if memErr != nil && !ierr.IsNotFound(memErr) {
			return memErr
		}
		if ierr.IsNotFound(memErr) {
			return ierr.NewError("user not found").
				WithHint("User does not belong to your tenant").
				Mark(ierr.ErrNotFound)
		}

		count, err := s.membershipRepo.CountByTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ierr.NewError("cannot remove the last user in the tenant").
				WithHint("At least one human user must remain in the tenant").
				Mark(ierr.ErrValidation)
		}

		if err := s.membershipRepo.Delete(ctx, target.ID, tenantID); err != nil {
			return err
		}

		remaining, err := s.membershipRepo.CountByUser(ctx, target.ID)
		if err != nil {
			return err
		}
		if remaining > 0 {
			return nil
		}

		if s.cfg != nil {
			provider := authProvider.NewProvider(s.cfg)
			if err := provider.RemoveUser(ctx, target.ID); err != nil {
				return err
			}
		}
		return s.userRepo.ArchiveByID(ctx, target.ID)
	})
}

func (s *userService) ListMyTenants(ctx context.Context) (*dto.ListMyTenantsResponse, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("User ID is required").
			Mark(ierr.ErrValidation)
	}
	if s.membershipRepo == nil {
		return nil, ierr.NewError("tenant membership repository not configured").
			WithHint("Multi-tenant membership requires a membership repository").
			Mark(ierr.ErrValidation)
	}

	memberships, err := s.membershipRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	activeTenantID := types.GetTenantID(ctx)
	items := make([]*dto.TenantMembershipResponse, 0, len(memberships))
	for _, m := range memberships {
		t, err := s.tenantRepo.GetByID(ctx, m.TenantID)
		if err != nil {
			return nil, err
		}
		items = append(items, &dto.TenantMembershipResponse{
			TenantID:   m.TenantID,
			TenantName: t.Name,
			Roles:      m.Roles,
			Active:     m.TenantID == activeTenantID,
		})
	}
	return &dto.ListMyTenantsResponse{Items: items}, nil
}

func (s *userService) LeaveTenant(ctx context.Context) error {
	userID := types.GetUserID(ctx)
	tenantID := types.GetTenantID(ctx)
	if userID == "" || tenantID == "" {
		return ierr.NewError("user and tenant are required").
			WithHint("Authentication context is incomplete").
			Mark(ierr.ErrValidation)
	}

	target, err := s.userRepo.GetByIDGlobal(ctx, userID)
	if err != nil {
		return err
	}
	if target.Type != types.UserTypeUser {
		return ierr.NewError("only human users can leave a tenant").
			WithHint("Service accounts cannot leave a tenant").
			Mark(ierr.ErrValidation)
	}
	if _, err := s.requireMembership(ctx, userID, tenantID); err != nil {
		return err
	}

	if err := s.removeMembershipFromTenant(ctx, target, tenantID); err != nil {
		return err
	}

	s.logger.Info(ctx, "user left tenant",
		"user_id", userID,
		"tenant_id", tenantID,
	)
	return nil
}

func (s *userService) SwitchTenant(ctx context.Context, req *dto.SwitchTenantRequest) (*dto.AuthResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("User ID is required").
			Mark(ierr.ErrValidation)
	}
	if s.cfg == nil {
		return nil, ierr.NewError("auth configuration missing").
			WithHint("Tenant switch requires auth provider configuration").
			Mark(ierr.ErrValidation)
	}

	u, err := s.userRepo.GetByIDGlobal(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Type != types.UserTypeUser {
		return nil, ierr.NewError("only human users can switch tenants").
			WithHint("Service accounts are scoped to a single tenant").
			Mark(ierr.ErrValidation)
	}
	if _, err := s.requireMembership(ctx, userID, req.TenantID); err != nil {
		return nil, err
	}

	provider := authProvider.NewProvider(s.cfg)
	if err := provider.AssignUserToTenant(ctx, userID, req.TenantID); err != nil {
		return nil, err
	}

	token, _, err := provider.GenerateDevToken(req.TenantID, types.GetEnvironmentID(ctx), userID, u.Email, 30*24)
	if err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "user switched tenant",
		"user_id", userID,
		"tenant_id", req.TenantID,
	)

	return &dto.AuthResponse{
		Token:    token,
		UserID:   userID,
		TenantID: req.TenantID,
	}, nil
}
