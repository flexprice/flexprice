package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	domainMembership "github.com/flexprice/flexprice/internal/domain/tenantmembership"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type UserService interface {
	GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
	CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error)
	UpdateUser(ctx context.Context, req *dto.UpdateUserRequest) (*dto.UpdateUserResponse, error)
	UpdateServiceAccount(ctx context.Context, id string, req *dto.UpdateServiceAccountRequest) (*dto.UpdateServiceAccountResponse, error)
	UpdateUserRoles(ctx context.Context, id string, req *dto.UpdateUserRolesRequest) (*dto.UpdateUserRolesResponse, error)
	DeleteUser(ctx context.Context, id string) error
	RemoveUser(ctx context.Context, id string) error
	LeaveTenant(ctx context.Context) error
	ListMyTenants(ctx context.Context) (*dto.ListMyTenantsResponse, error)
	SwitchTenant(ctx context.Context, req *dto.SwitchTenantRequest) (*dto.AuthResponse, error)
	ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error)
	CreateSupportChatToken(ctx context.Context) (*dto.SupportChatTokenResponse, error)
}

type userService struct {
	userRepo        user.Repository
	membershipRepo  domainMembership.Repository
	tenantRepo      tenant.Repository
	authRepo        domainAuth.Repository
	secretRepo      domainSecret.Repository
	environmentRepo domainEnvironment.Repository
	db              postgres.IClient
	cfg             *config.Configuration
	rbacService     *rbac.RBACService
	settingsService SettingsService
	logger          *logger.Logger
}

func NewUserService(
	userRepo user.Repository,
	membershipRepo domainMembership.Repository,
	tenantRepo tenant.Repository,
	authRepo domainAuth.Repository,
	secretRepo domainSecret.Repository,
	environmentRepo domainEnvironment.Repository,
	db postgres.IClient,
	cfg *config.Configuration,
	rbacService *rbac.RBACService,
	settingsService SettingsService,
	logger *logger.Logger,
) UserService {
	return &userService{
		userRepo:        userRepo,
		membershipRepo:  membershipRepo,
		tenantRepo:      tenantRepo,
		authRepo:        authRepo,
		secretRepo:      secretRepo,
		environmentRepo: environmentRepo,
		db:              db,
		cfg:             cfg,
		rbacService:     rbacService,
		settingsService: settingsService,
		logger:          logger,
	}
}

func (s *userService) GetUserInfo(ctx context.Context) (*dto.UserResponse, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("User ID is required").
			Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	user, err := s.loadUserForTenant(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return dto.NewUserResponse(user, tenant), nil
}

func (s *userService) CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required in context").
			Mark(ierr.ErrValidation)
	}

	// Verify tenant exists
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Get current user ID for audit fields
	currentUserID := types.GetUserID(ctx)
	if currentUserID == "" {
		currentUserID = "system"
	}

	var newUser *user.User
	var password *string

	switch req.Type {
	case types.UserTypeUser:
		// invite user to the tenant
		newUser, password, err = s.InviteUser(ctx, req, currentUserID)
		if err != nil {
			return nil, err
		}

	case types.UserTypeServiceAccount:
		if s.rbacService == nil {
			return nil, ierr.NewError("RBAC not configured").
				WithHint("Service accounts require RBAC for role validation; provide a non-nil RBAC service.").
				Mark(ierr.ErrValidation)
		}
		if err := s.rbacService.ValidateRoles(req.Type, req.Roles); err != nil {
			return nil, err
		}
		if err := s.rbacService.CanGrantRoles(types.GetRoles(ctx), req.Roles); err != nil {
			return nil, err
		}
		newUser = &user.User{
			ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
			Name:  req.Name,
			Email: "",
			Type:  types.UserTypeServiceAccount,
			Roles: req.Roles,
		}
		newUser.BaseModel = types.GetDefaultBaseModel(ctx)
		newUser.BaseModel.CreatedBy = currentUserID
		newUser.BaseModel.UpdatedBy = currentUserID

		if err := newUser.Validate(); err != nil {
			return nil, err
		}
		if err := s.userRepo.Create(ctx, newUser); err != nil {
			return nil, err
		}
	default:
		return nil, ierr.NewError("invalid user type").WithHint("Type must be 'user' or 'service_account'").Mark(ierr.ErrValidation)
	}

	passwordValue := ""
	if password != nil {
		passwordValue = *password
	}
	return &dto.CreateUserResponse{
		UserResponse: dto.NewUserResponse(newUser, tenant),
		Password:     passwordValue,
	}, nil
}

func (s *userService) ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error) {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant_id not found in context").
			WithHint("Authentication context is missing tenant information").
			Mark(ierr.ErrValidation)
	}

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	wantType := (*types.UserType)(nil)
	if filter != nil {
		wantType = filter.Type
	}

	var users []*user.User

	includeHumans := wantType == nil || *wantType == types.UserTypeUser
	includeSA := wantType == nil || *wantType == types.UserTypeServiceAccount

	if includeHumans && s.membershipRepo != nil {
		memberships, err := s.membershipRepo.ListByTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, m := range memberships {
			u, err := s.userRepo.GetByIDGlobal(ctx, m.UserID)
			if err != nil {
				if ierr.IsNotFound(err) {
					continue
				}
				return nil, err
			}
			if u.Status != types.StatusPublished || u.Type != types.UserTypeUser {
				continue
			}
			users = append(users, s.overlayMembershipRoles(u, m))
		}
	}

	if includeSA {
		saFilter := &types.UserFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			Type:        lo.ToPtr(types.UserTypeServiceAccount),
		}
		serviceAccounts, _, err := s.userRepo.ListByFilter(ctx, saFilter)
		if err != nil {
			return nil, err
		}
		users = append(users, serviceAccounts...)
	}

	total := int64(len(users))
	limit := filter.GetLimit()
	offset := filter.GetOffset()
	if offset > len(users) {
		users = []*user.User{}
	} else {
		end := offset + limit
		if limit <= 0 || end > len(users) {
			end = len(users)
		}
		users = users[offset:end]
	}

	userResponses := make([]*dto.UserResponse, len(users))
	for i, u := range users {
		userResponses[i] = dto.NewUserResponse(u, tenant)
	}

	return &dto.ListUsersResponse{
		Items:      userResponses,
		Pagination: types.NewPaginationResponse(int(total), filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *userService) UpdateUser(ctx context.Context, req *dto.UpdateUserRequest) (*dto.UpdateUserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("User ID is required").
			Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	existingUser, err := s.loadUserForTenant(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	mergedMetadata := make(map[string]string, len(existingUser.Metadata)+len(req.Metadata))
	for key, value := range existingUser.Metadata {
		mergedMetadata[key] = value
	}
	for key, value := range req.Metadata {
		mergedMetadata[key] = value
	}
	existingUser.Metadata = mergedMetadata

	if req.Name != "" {
		existingUser.Name = req.Name
	}

	existingUser.UpdatedBy = types.GetUserID(ctx)
	existingUser.UpdatedAt = types.GetDefaultBaseModel(ctx).UpdatedAt

	if err := s.userRepo.Update(ctx, existingUser); err != nil {
		return nil, err
	}

	return &dto.UpdateUserResponse{UserResponse: dto.NewUserResponse(existingUser, tenant)}, nil
}

func (s *userService) UpdateServiceAccount(ctx context.Context, id string, req *dto.UpdateServiceAccountRequest) (*dto.UpdateServiceAccountResponse, error) {
	if id == "" {
		return nil, ierr.NewError("service account ID is required").
			WithHint("Provide a valid service account ID").
			Mark(ierr.ErrValidation)
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	existingUser, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existingUser.Type != types.UserTypeServiceAccount {
		return nil, ierr.NewError("service account not found").
			WithHint("The provided ID does not belong to a service account").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrNotFound)
	}
	if existingUser.Status == types.StatusArchived {
		return nil, ierr.NewError("service account is archived").
			WithHint("Archived service accounts cannot be updated").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrValidation)
	}

	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	if req.Name != existingUser.Name {
		existingUser.Name = req.Name
		existingUser.UpdatedBy = types.GetUserID(ctx)
		existingUser.UpdatedAt = types.GetDefaultBaseModel(ctx).UpdatedAt

		if err := s.userRepo.Update(ctx, existingUser); err != nil {
			return nil, err
		}
	}

	return &dto.UpdateServiceAccountResponse{UserResponse: dto.NewUserResponse(existingUser, tenant)}, nil
}

// UpdateUserRoles updates the roles of a user account. Only a super_admin may
// call it, and nobody may change their own roles — so a tenant can never be
// left without a super_admin. Service accounts are excluded: their roles are
// fixed at creation.
func (s *userService) UpdateUserRoles(ctx context.Context, id string, req *dto.UpdateUserRolesRequest) (*dto.UpdateUserRolesResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("Provide a valid user ID").
			Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	if !lo.Contains(types.GetRoles(ctx), types.RoleSuperAdmin.String()) {
		return nil, ierr.NewError("only super_admin can update roles").
			WithHint("Ask a tenant super_admin to update roles").
			Mark(ierr.ErrPermissionDenied)
	}

	callerID := types.GetUserID(ctx)
	if id == callerID {
		return nil, ierr.NewError("cannot update your own roles").
			WithHint("Ask another super_admin to change your roles").
			Mark(ierr.ErrPermissionDenied)
	}

	existingUser, err := s.loadUserForTenant(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if existingUser.Type != types.UserTypeUser {
		return nil, ierr.NewError("role updates are only supported for user accounts").
			WithHint("Service account roles are fixed at creation and cannot be changed").
			WithReportableDetails(map[string]interface{}{"id": id, "type": existingUser.Type}).
			Mark(ierr.ErrValidation)
	}
	if existingUser.Status == types.StatusArchived {
		return nil, ierr.NewError("user is archived").
			WithHint("Archived users cannot have their roles updated").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrValidation)
	}

	if s.rbacService == nil {
		return nil, ierr.NewError("RBAC not configured").
			WithHint("Role assignment requires RBAC for role validation; provide a non-nil RBAC service.").
			Mark(ierr.ErrValidation)
	}
	if err := s.rbacService.ValidateRoles(existingUser.Type, req.Roles); err != nil {
		return nil, err
	}

	if err := s.db.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.ensureNoActiveAPIKeys(txCtx, id); err != nil {
			return err
		}
		if s.membershipRepo != nil {
			if err := s.membershipRepo.UpdateRoles(txCtx, id, tenantID, req.Roles); err != nil {
				return err
			}
		}
		home, err := s.userRepo.GetByIDGlobal(txCtx, id)
		if err != nil {
			return err
		}
		if home.TenantID == tenantID {
			return s.userRepo.UpdateRoles(txCtx, id, req.Roles)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "user roles updated",
		"user_id", id, "tenant_id", tenantID, "actor_id", callerID,
		"old_roles", existingUser.Roles, "new_roles", req.Roles)

	updatedUser, err := s.loadUserForTenant(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return &dto.UpdateUserRolesResponse{UserResponse: dto.NewUserResponse(updatedUser, tenant)}, nil
}

// ensureNoActiveAPIKeys fails if the user still holds an active (published,
// unexpired) API key anywhere in the tenant. A key snapshots its owner's roles
// at creation and never re-reads them (see ent/schema/secret.go), so changing
// the owner's roles would leave the key running on the old ones indefinitely.
// The error names the offending keys, grouped by environment ID, so the caller
// can tell the user exactly what to expire before retrying.
//
// Secrets are environment-scoped while roles are tenant-wide, hence the search
// across every environment rather than just the caller's.
func (s *userService) ensureNoActiveAPIKeys(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	tenantCtx := types.SetEnvironmentID(ctx, "")

	secrets, err := s.secretRepo.ListAll(tenantCtx, &types.SecretFilter{
		QueryFilter:  types.NewNoLimitPublishedQueryFilter(),
		UserID:       &userID,
		NotExpiredAt: &now,
	})
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		return nil
	}

	secretsByEnv := lo.GroupBy(secrets, func(sec *domainSecret.Secret) string {
		return sec.EnvironmentID
	})

	keysByEnv := make(map[string]dto.ActiveEnvironmentAPIKeys, len(secretsByEnv))
	for envID, envSecrets := range secretsByEnv {
		env, err := s.environmentRepo.Get(ctx, envID)
		if err != nil {
			return err
		}
		keysByEnv[envID] = dto.ActiveEnvironmentAPIKeys{
			EnvName: env.Name,
			APIKeys: lo.Map(envSecrets, func(sec *domainSecret.Secret, _ int) dto.ActiveAPIKey {
				return dto.ActiveAPIKey{ID: sec.ID, KeyName: sec.Name}
			}),
		}
	}

	return ierr.NewError("user has active API keys").
		WithHint("Expire this user's existing API keys before changing their role").
		WithReportableDetails(map[string]interface{}{
			"id":                   userID,
			"active_api_keys":      keysByEnv,
			"active_api_key_count": len(secrets),
		}).
		Mark(ierr.ErrValidation)
}

// InviteUser invites a user to the tenant
func (s *userService) InviteUser(ctx context.Context, req *dto.CreateUserRequest, currentUserID string) (*user.User, *string, error) {

	var userID string

	// Admitting someone to the tenant is an administrative act, so it is
	// restricted to super_admins regardless of which role the invitee is given.
	if !lo.Contains(types.GetRoles(ctx), types.RoleSuperAdmin.String()) {
		return nil, nil, ierr.NewError("only super_admin can invite users").
			WithHint("Ask a tenant super_admin to invite this user").
			Mark(ierr.ErrPermissionDenied)
	}

	// Resolve and check the roles up front. Everything below provisions state —
	// a provider identity, an auth record — that this function does not roll
	// back, so a rejected role must fail before any of it is created rather than
	// leaving an orphaned account behind holding the invitee's email.
	// Invited users default to super_admin when no roles are specified.
	roles := req.Roles
	if len(roles) == 0 {
		roles = []string{types.RoleSuperAdmin.String()}
	} else {
		if s.rbacService == nil {
			return nil, nil, ierr.NewError("RBAC not configured").
				WithHint("Role assignment requires RBAC for role validation; provide a non-nil RBAC service.").
				Mark(ierr.ErrValidation)
		}
		if err := s.rbacService.ValidateRoles(types.UserTypeUser, roles); err != nil {
			return nil, nil, err
		}
		if err := s.rbacService.CanGrantRoles(types.GetRoles(ctx), roles); err != nil {
			return nil, nil, err
		}
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required in context").
			Mark(ierr.ErrValidation)
	}

	// Look up globally so an existing account in another tenant can be invited in.
	existingUser, err := s.userRepo.GetByEmail(types.SetTenantID(ctx, ""), req.Email)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, nil, err
	}
	if existingUser != nil {
		if existingUser.Type != types.UserTypeUser {
			return nil, nil, ierr.NewError("email already in use").
				WithHint("This email cannot be invited").
				WithReportableDetails(map[string]interface{}{"email": req.Email}).
				Mark(ierr.ErrAlreadyExists)
		}
		if s.membershipRepo == nil {
			return nil, nil, ierr.NewError("email already in use").
				WithHint("A user with this email already exists").
				WithReportableDetails(map[string]interface{}{"email": req.Email}).
				Mark(ierr.ErrAlreadyExists)
		}
		if _, memErr := s.membershipRepo.GetByUserAndTenant(ctx, existingUser.ID, tenantID); memErr == nil {
			return nil, nil, ierr.NewError("email already in use").
				WithHint("A user with this email already exists in this tenant").
				WithReportableDetails(map[string]interface{}{"email": req.Email}).
				Mark(ierr.ErrAlreadyExists)
		} else if !ierr.IsNotFound(memErr) {
			return nil, nil, memErr
		}
		// Existing identity: add a membership only (no new auth provider user).
		svc, ok := s.settingsService.(*settingsService)
		if !ok || svc == nil {
			return nil, nil, ierr.NewError("settings service not configured").
				WithHint("User creation requires settings service for add_user_config.").
				Mark(ierr.ErrValidation)
		}
		addUserConfig, err := GetSetting[types.TenantConfig](svc, ctx, types.SettingKeyTenantConfig)
		if err != nil {
			return nil, nil, err
		}
		activeCount, err := s.membershipRepo.CountByTenant(ctx, tenantID)
		if err != nil {
			return nil, nil, err
		}
		if activeCount >= int64(addUserConfig.MaxUsers) {
			return nil, nil, ierr.NewError("user limit reached: you cannot add any more users").
				WithHintf("Maximum %d user(s) allowed for this tenant. Limit reached.", addUserConfig.MaxUsers).
				WithReportableDetails(map[string]interface{}{"max_users": addUserConfig.MaxUsers, "current_active_users": activeCount}).
				Mark(ierr.ErrValidation)
		}
		if err := s.createMembership(ctx, existingUser.ID, tenantID, roles); err != nil {
			return nil, nil, err
		}
		joined := s.overlayMembershipRoles(existingUser, &domainMembership.Membership{
			UserID:    existingUser.ID,
			Roles:     roles,
			BaseModel: types.BaseModel{TenantID: tenantID, Status: types.StatusPublished},
		})
		s.logger.Info(ctx, "existing user joined tenant via invite",
			"user_id", existingUser.ID,
			"tenant_id", tenantID,
			"actor_id", currentUserID,
		)
		return joined, nil, nil
	}
	// Enforce per-tenant user limit from add_user_config (GetSetting returns default when not set)
	svc, ok := s.settingsService.(*settingsService)
	if !ok || svc == nil {
		return nil, nil, ierr.NewError("settings service not configured").
			WithHint("User creation requires settings service for add_user_config.").
			Mark(ierr.ErrValidation)
	}
	addUserConfig, err := GetSetting[types.TenantConfig](svc, ctx, types.SettingKeyTenantConfig)
	if err != nil {
		return nil, nil, err
	}
	var totalActiveUsers int64
	if s.membershipRepo != nil {
		totalActiveUsers, err = s.membershipRepo.CountByTenant(ctx, tenantID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		_, totalActiveUsers, err = s.userRepo.ListByFilter(ctx, &types.UserFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(1),
				Offset: lo.ToPtr(0),
				Status: lo.ToPtr(types.StatusPublished),
			},
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if totalActiveUsers >= int64(addUserConfig.MaxUsers) {
		return nil, nil, ierr.NewError("user limit reached: you cannot add any more users").
			WithHintf("Maximum %d user(s) allowed for this tenant. Limit reached.", addUserConfig.MaxUsers).
			WithReportableDetails(map[string]interface{}{"max_users": addUserConfig.MaxUsers, "current_active_users": totalActiveUsers}).
			Mark(ierr.ErrValidation)
	}

	if s.cfg == nil {
		return nil, nil, ierr.NewError("auth configuration missing").
			WithHint("User creation requires auth provider configuration").
			Mark(ierr.ErrValidation)
	}

	provider := authProvider.NewProvider(s.cfg)
	inviteResp, err := provider.UserInvite(ctx, authProvider.UserInviteRequest{
		Email: req.Email,
	})
	if err != nil {
		return nil, nil, err
	}
	userID = inviteResp.ID
	password := inviteResp.Password

	// Persist provider-specific auth material for later login (e.g. Flexprice bcrypt hash).
	if inviteResp.AuthRecord != nil {
		if s.authRepo == nil {
			return nil, nil, ierr.NewError("auth repository not configured").
				WithHint("Auth provider returned an auth record but auth repository is nil").
				Mark(ierr.ErrValidation)
		}
		if err := s.authRepo.CreateAuth(ctx, inviteResp.AuthRecord); err != nil {
			return nil, nil, err
		}
	}

	newUser := &user.User{
		ID:    userID,
		Email: req.Email,
		Type:  types.UserTypeUser,
		Roles: roles,
	}
	newUser.BaseModel = types.GetDefaultBaseModel(ctx)
	newUser.BaseModel.CreatedBy = currentUserID
	newUser.BaseModel.UpdatedBy = currentUserID

	if err := newUser.Validate(); err != nil {
		return nil, nil, err
	}
	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, nil, err
	}
	if s.membershipRepo != nil {
		if err := s.createMembership(ctx, newUser.ID, newUser.TenantID, roles); err != nil {
			return nil, nil, err
		}
	}
	return newUser, &password, nil
}

func (s *userService) DeleteUser(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("service account ID is required").
			WithHint("Provide a valid service account ID").
			Mark(ierr.ErrValidation)
	}
	existingUser, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existingUser.Type != types.UserTypeServiceAccount {
		return ierr.NewError("only service accounts can be deleted").
			WithHint("Deletion is supported for service accounts only").
			Mark(ierr.ErrValidation)
	}

	// Block archive if the service account has active (published, non-expired) API keys.
	// Service accounts are tenant-scoped, so check across all environments.
	now := time.Now().UTC()
	tenantCtx := types.SetEnvironmentID(ctx, "")
	activeCount, err := s.secretRepo.Count(tenantCtx, &types.SecretFilter{
		QueryFilter: &types.QueryFilter{
			Status: lo.ToPtr(types.StatusPublished),
		},
		UserID:       &id,
		NotExpiredAt: &now,
	})
	if err != nil {
		return err
	}
	if activeCount > 0 {
		return ierr.NewError("service account has active API keys").
			WithHint("Revoke all API keys before archiving this service account").
			Mark(ierr.ErrValidation)
	}

	return s.userRepo.Delete(ctx, id)
}

func (s *userService) CreateSupportChatToken(ctx context.Context) (*dto.SupportChatTokenResponse, error) {
	if s.cfg == nil || s.cfg.ChatSupport.AppID == "" || s.cfg.ChatSupport.IdentitySecret == "" {
		return nil, ierr.NewError("chat support identity verification is not configured").
			WithHint("Set chat_support.app_id and chat_support.identity_secret").
			Mark(ierr.ErrInternal)
	}

	u, err := s.GetUserInfo(ctx)
	if err != nil {
		return nil, err
	}

	if u.Email == "" {
		return nil, ierr.NewError("user has no email").
			WithHint("Support chat identity verification requires a user with an email").
			Mark(ierr.ErrValidation)
	}

	secretBytes, err := hex.DecodeString(s.cfg.ChatSupport.IdentitySecret)
	if err != nil {

		return nil, ierr.WithError(err).
			WithHint("chat_support.identity_secret must be a hex string").
			Mark(ierr.ErrInternal)
	}

	mac := hmac.New(sha256.New, secretBytes)
	if _, err := mac.Write([]byte(u.Email)); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to hash the support chat email").
			Mark(ierr.ErrInternal)
	}

	return &dto.SupportChatTokenResponse{
		Token: hex.EncodeToString(mac.Sum(nil)),
	}, nil
}

// RemoveUser removes a human user (type=user) from their tenant. Service accounts
// are not supported here; use DeleteUser for those.
// RemoveUser removes a human user (type=user) from the current tenant. Service accounts
// are not supported here; use DeleteUser for those. The auth identity is deleted only
// when the user has no remaining tenant memberships.
func (s *userService) RemoveUser(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("user ID is required").
			WithHint("Provide a valid user ID").
			Mark(ierr.ErrValidation)
	}

	if !lo.Contains(types.GetRoles(ctx), types.RoleSuperAdmin.String()) {
		return ierr.NewError("only super_admin can remove users").
			WithHint("Ask a tenant super_admin to remove this user").
			Mark(ierr.ErrPermissionDenied)
	}

	actorUserID := types.GetUserID(ctx)
	if id == actorUserID {
		return ierr.NewError("cannot remove yourself").
			WithHint("Use leave tenant to leave this organisation, or ask another super_admin to remove you").
			Mark(ierr.ErrPermissionDenied)
	}

	authTenantID := types.GetTenantID(ctx)
	if authTenantID == "" {
		return ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required in context").
			Mark(ierr.ErrValidation)
	}

	existingUser, err := s.userRepo.GetByIDGlobal(ctx, id)
	if err != nil {
		return err
	}
	if existingUser.Type != types.UserTypeUser {
		return ierr.NewError("only human users can be removed").
			WithHint("Use the service account delete API to remove a service account").
			Mark(ierr.ErrValidation)
	}
	if s.membershipRepo != nil {
		if _, err := s.membershipRepo.GetByUserAndTenant(ctx, id, authTenantID); err != nil {
			if !(ierr.IsNotFound(err) && existingUser.TenantID == authTenantID) {
				if ierr.IsNotFound(err) {
					return ierr.NewError("user not found").
						WithHint("User does not belong to your tenant").
						Mark(ierr.ErrNotFound)
				}
				return err
			}
		}
	} else if existingUser.TenantID != authTenantID {
		return ierr.NewError("user not found").
			WithHint("User does not belong to your tenant").
			Mark(ierr.ErrNotFound)
	}

	if err := s.removeMembershipFromTenant(ctx, existingUser, authTenantID); err != nil {
		return err
	}

	s.logger.Info(ctx, "user removed from tenant",
		"actor_user_id", actorUserID,
		"target_user_id", id,
		"tenant_id", authTenantID,
	)

	return nil
}
