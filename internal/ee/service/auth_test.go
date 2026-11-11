package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

// recordingHook records each tenant handed to self-billing as "tenant|name|email".
type recordingHook struct {
	calls   []string
	updated []string
}

func (h *recordingHook) OnTenantCreated(_ context.Context, tenantID, name, email string) {
	h.calls = append(h.calls, tenantID+"|"+name+"|"+email)
}

func (h *recordingHook) OnTenantBillingDetailsUpdated(_ context.Context, tenantID string) {
	h.updated = append(h.updated, tenantID)
}

type AuthServiceSuite struct {
	testutil.BaseServiceTestSuite
	authService AuthService
	userRepo    *testutil.InMemoryUserStore
	tenantRepo  *testutil.InMemoryTenantStore
}

func TestAuthService(t *testing.T) {
	suite.Run(t, new(AuthServiceSuite))
}

func (s *AuthServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *AuthServiceSuite) setupService() {
	s.userRepo = s.GetStores().UserRepo.(*testutil.InMemoryUserStore)
	s.tenantRepo = s.GetStores().TenantRepo.(*testutil.InMemoryTenantStore)
	pubSub := testutil.NewInMemoryPubSub()

	s.authService = NewAuthService(ServiceParams{
		SelfBilling:                  &recordingHook{},
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		PriceUnitRepo:                s.GetStores().PriceUnitRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		UserRepo:                     s.GetStores().UserRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		TaskRepo:                     s.GetStores().TaskRepo,
		SecretRepo:                   s.GetStores().SecretRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		EventPublisher:               s.GetPublisher(),
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		WebhookPublisher:             s.GetWebhookPublisher(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	}, pubSub)
}

func (s *AuthServiceSuite) setupTestData() {
	// Clear any existing data
	s.BaseServiceTestSuite.ClearStores()
}

func (s *AuthServiceSuite) TestSignUp() {
	testCases := []struct {
		name          string
		req           *dto.SignUpRequest
		setupFunc     func()
		expectedError bool
	}{
		{
			name: "successful_signup",
			req: &dto.SignUpRequest{
				Email:    "test@example.com",
				Password: "securepassword",
			},
			setupFunc:     nil,
			expectedError: false,
		},
		{
			name: "successful_signup_with_metadata",
			req: &dto.SignUpRequest{
				Email:    "metadata@example.com",
				Password: "securepassword",
				Metadata: map[string]string{
					"signup_source": "landing_page",
					"utm_campaign":  "q2_launch",
				},
			},
			setupFunc:     nil,
			expectedError: false,
		},
		{
			name: "duplicate_email",
			req: &dto.SignUpRequest{
				Email:    "existing@example.com",
				Password: "securepassword",
			},
			setupFunc: func() {
				// Create an existing user to trigger a duplicate scenario
				_ = s.userRepo.Create(s.GetContext(), &user.User{
					ID:    "user-1",
					Email: "existing@example.com",
				})
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.setupTestData()
			if tc.setupFunc != nil {
				tc.setupFunc()
			}

			resp, err := s.authService.SignUp(s.GetContext(), tc.req)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				// We used a real provider, so check that token exists (not necessarily 'auth-token' as before)
				s.NotEmpty(resp.Token)

				// The user who creates a tenant owns it, so onboarding must grant
				// super_admin outright — no roles would leave them unable to
				// administer the tenant they just signed up for.
				owner, err := s.userRepo.GetByEmail(s.GetContext(), tc.req.Email)
				s.NoError(err)
				s.Equal([]string{types.RoleSuperAdmin.String()}, owner.Roles)
				s.Equal(types.UserTypeUser, owner.Type)

				if tc.req.Metadata != nil {
					createdUser, err := s.userRepo.GetByEmail(s.GetContext(), tc.req.Email)
					s.NoError(err)
					s.Equal(tc.req.Metadata, createdUser.Metadata)

					createdTenant, err := s.tenantRepo.GetByID(s.GetContext(), resp.TenantID)
					s.NoError(err)
					s.Equal(tc.req.Metadata, map[string]string(createdTenant.Metadata))
				}
			}
		})
	}
}

// fakeSupabaseProvider stands in for the Supabase provider so the signup guard
// can be driven without a live Supabase project. The interface is embedded
// rather than fully implemented so that adding a provider method does not break
// this test; only the methods the guard actually calls are defined, and any
// other call panics loudly rather than silently returning a zero value.
type fakeSupabaseProvider struct {
	authProvider.Provider
	claims     *auth.Claims
	err        error
	confirmed  bool
	confirmErr error
	assignErr  error
}

// EmailConfirmed stands in for the Admin API lookup. The guard reads
// confirmation from here, not from the token, so a token that asserts
// verification while the provider has not confirmed the address is refused.
func (f *fakeSupabaseProvider) EmailConfirmed(_ context.Context, _ string) (bool, error) {
	return f.confirmed, f.confirmErr
}

func (f *fakeSupabaseProvider) GetProvider() types.AuthProvider {
	return types.AuthProviderSupabase
}

func (f *fakeSupabaseProvider) ValidateToken(_ context.Context, _ string) (*auth.Claims, error) {
	return f.claims, f.err
}

func (f *fakeSupabaseProvider) SignUp(_ context.Context, req authProvider.AuthRequest) (*authProvider.AuthResponse, error) {
	return &authProvider.AuthResponse{
		ID:            "user_supabase_1",
		AuthToken:     "auth-token",
		ProviderToken: "provider-token",
	}, nil
}

func (f *fakeSupabaseProvider) AssignUserToTenant(_ context.Context, _ string, _ string) error {
	return f.assignErr
}

// TestSignUpRefusesUnverifiedEmail covers the signup guard: an account must not
// be created for an address the provider has not confirmed, regardless of how
// the Supabase project's "Confirm email" setting is configured.
func (s *AuthServiceSuite) TestSignUpRefusesUnverifiedEmail() {
	testCases := []struct {
		name        string
		claims      *auth.Claims
		validateErr error
		confirmed   bool
		confirmErr  error
		token       string
		email       string
		wantErr     bool
	}{
		{
			name:      "admits an email the provider has confirmed",
			claims:    &auth.Claims{UserID: "user_1", Email: "victim@example.com"},
			confirmed: true,
			token:     "token",
			email:     "victim@example.com",
			wantErr:   false,
		},
		{
			// The provider is authoritative: a token asserting verification in
			// user_metadata must not admit an address GoTrue has not confirmed.
			name:      "refuses an email the provider has not confirmed",
			claims:    &auth.Claims{UserID: "user_1", Email: "victim@example.com"},
			confirmed: false,
			token:     "token",
			email:     "victim@example.com",
			wantErr:   true,
		},
		{
			name:      "refuses a confirmed token registering a different email",
			claims:    &auth.Claims{UserID: "user_1", Email: "attacker@example.com"},
			confirmed: true,
			token:     "token",
			email:     "victim@example.com",
			wantErr:   true,
		},
		{
			name:      "refuses a missing token",
			claims:    &auth.Claims{UserID: "user_1", Email: "victim@example.com"},
			confirmed: true,
			token:     "",
			email:     "victim@example.com",
			wantErr:   true,
		},
		{
			name:        "refuses when the token fails validation",
			validateErr: errors.New("bad signature"),
			token:       "token",
			email:       "victim@example.com",
			wantErr:     true,
		},
		{
			// A failed lookup must not admit the signup: the guard cannot tell a
			// confirmed address from an unconfirmed one, so it refuses.
			name:       "refuses when the confirmation lookup fails",
			claims:     &auth.Claims{UserID: "user_1", Email: "victim@example.com"},
			confirmErr: errors.New("admin api unavailable"),
			token:      "token",
			email:      "victim@example.com",
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.BaseServiceTestSuite.ClearStores()
			svc := s.authService.(*authService)
			original := svc.authProvider
			svc.authProvider = &fakeSupabaseProvider{
				claims: tc.claims, err: tc.validateErr,
				confirmed: tc.confirmed, confirmErr: tc.confirmErr,
			}
			defer func() { svc.authProvider = original }()

			_, err := svc.SignUp(s.GetContext(), &dto.SignUpRequest{
				Email:      tc.email,
				Password:   "password123",
				Token:      tc.token,
				TenantName: "Test Tenant",
			})

			if tc.wantErr {
				s.Error(err)
				// The account must not exist after a refusal.
				u, _ := s.userRepo.GetByEmail(s.GetContext(), tc.email)
				s.Nil(u, "no user should be created for a refused signup")
			} else {
				s.NoError(err)
			}
		})
	}
}

const testLinkAuthSecret = "test-link-auth-secret"

// useLinkSecret rebuilds the service so its provider and link checks share one secret.
func (s *AuthServiceSuite) useLinkSecret() {
	s.GetConfig().Auth.Secret = testLinkAuthSecret
	s.setupService()
}

// seedMember stores a human user and, when password is set, their flexprice password row.
func (s *AuthServiceSuite) seedMember(id string, status types.Status, password string) *user.User {
	ctx := testutil.SetupContext()
	member := &user.User{ID: id, Email: id + "@example.com", Type: types.UserTypeUser, BaseModel: types.GetDefaultBaseModel(ctx)}
	member.Status = status
	s.Require().NoError(s.userRepo.Create(ctx, member))

	if password != "" {
		hash, err := authProvider.HashPassword(password)
		s.Require().NoError(err)
		s.Require().NoError(s.GetStores().AuthRepo.CreateAuth(ctx, auth.NewAuth(id, types.AuthProviderFlexprice, hash)))
	}
	return member
}

// signLink signs a link for target against their current password row, the way the user service does.
func (s *AuthServiceSuite) signLink(target *user.User, tenantID, secret string, expiresAt time.Time) string {
	signer, err := authProvider.NewPasswordLinkSigner(secret)
	s.Require().NoError(err)

	currentHash := ""
	purpose := authProvider.PasswordLinkPurposeInvite
	if existing, err := s.GetStores().AuthRepo.GetAuthByUserID(testutil.SetupContext(), target.ID); err == nil {
		currentHash = existing.Token
		purpose = authProvider.PasswordLinkPurposeReset
	}

	token, err := signer.Sign(authProvider.PasswordLinkClaims{
		UserID:    target.ID,
		TenantID:  tenantID,
		Purpose:   purpose,
		ExpiresAt: expiresAt.Unix(),
	}, currentHash)
	s.Require().NoError(err)
	return token
}

func (s *AuthServiceSuite) login(email, password string) error {
	_, err := s.authService.Login(context.Background(), &dto.LoginRequest{Email: email, Password: password})
	return err
}

func (s *AuthServiceSuite) TestCheckPasswordLink() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")
	removed := s.seedMember("user_removed", types.StatusArchived, "")
	inAnHour := time.Now().Add(time.Hour)

	s.Run("a valid invite link names the purpose and the email", func() {
		token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, inAnHour)

		details, err := s.authService.CheckPasswordLink(context.Background(), &dto.CheckPasswordLinkRequest{Token: token})

		s.Require().NoError(err)
		s.Equal("invite", details.Purpose)
		s.Equal("user_invitee@example.com", details.Email)
	})

	s.Run("a link pasted with surrounding whitespace still works", func() {
		token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, inAnHour)

		details, err := s.authService.CheckPasswordLink(context.Background(), &dto.CheckPasswordLinkRequest{Token: "  " + token + "\n"})

		s.Require().NoError(err)
		s.Equal("user_invitee@example.com", details.Email)
	})

	refused := []struct {
		name  string
		token string
	}{
		{name: "signed with another secret", token: s.signLink(invitee, types.DefaultTenantID, "another-secret", inAnHour)},
		{name: "expired", token: s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(-time.Minute))},
		{name: "member was removed", token: s.signLink(removed, types.DefaultTenantID, testLinkAuthSecret, inAnHour)},
		{name: "names another tenant", token: s.signLink(invitee, "tenant_other", testLinkAuthSecret, inAnHour)},
		{name: "not a link", token: "not-a-link"},
	}
	for _, tc := range refused {
		s.Run(tc.name, func() {
			details, err := s.authService.CheckPasswordLink(context.Background(), &dto.CheckPasswordLinkRequest{Token: tc.token})

			s.Nil(details)
			s.True(ierr.IsNotFound(err), "expected the bad-link error, got %v", err)
		})
	}
}

func (s *AuthServiceSuite) TestSetPassword_Invite() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")
	token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))

	err := s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "first-password-1"})

	s.Require().NoError(err)
	s.NoError(s.login(invitee.Email, "first-password-1"))

	err = s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "second-password-2"})
	s.True(ierr.IsNotFound(err), "a used invite link must stop working, got %v", err)
	s.NoError(s.login(invitee.Email, "first-password-1"), "the second attempt must not change the password")
}

func (s *AuthServiceSuite) TestSetPassword_Reset() {
	s.useLinkSecret()
	member := s.seedMember("user_member", types.StatusPublished, "old-password-1")
	first := s.signLink(member, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))
	second := s.signLink(member, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))

	err := s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: first, Password: "new-password-2"})

	s.Require().NoError(err)
	s.True(ierr.IsPermissionDenied(s.login(member.Email, "old-password-1")), "the old password must stop working")
	s.NoError(s.login(member.Email, "new-password-2"))

	err = s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: second, Password: "third-password-3"})
	s.True(ierr.IsNotFound(err), "every older link must stop working once the password changes, got %v", err)
}

func (s *AuthServiceSuite) TestSetPassword_ShortPassword() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")
	token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))

	err := s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "short12"})

	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
	s.NoError(s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "long-enough-1"}),
		"a refused password must not use up the link")
}

func (s *AuthServiceSuite) TestSetPassword_RemovedUser() {
	s.useLinkSecret()
	removed := s.seedMember("user_removed", types.StatusArchived, "")
	token := s.signLink(removed, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))

	err := s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "long-enough-1"})

	s.True(ierr.IsNotFound(err), "expected the bad-link error, got %v", err)
}

func (s *AuthServiceSuite) TestLogin_InvitedUserWithoutPassword() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")

	err := s.login(invitee.Email, "any-password-1")

	s.True(ierr.IsPermissionDenied(err), "expected the wrong-password answer, got %v", err)
	s.False(ierr.IsNotFound(err), "a 404 would tell a stranger the email belongs to a pending invitee")
}

// Every refused login must resolve to one status. A refusal that still carries the
// cause's error kind would answer 400 or 404 on some requests, telling a caller
// which emails exist.
func (s *AuthServiceSuite) TestLogin_RefusalsAreOnlyPermissionDenied() {
	s.useLinkSecret()
	member := s.seedMember("user_member", types.StatusPublished, "right-password-1")

	s.Run("wrong password", func() {
		err := s.login(member.Email, "wrong-password-1")

		s.True(ierr.IsPermissionDenied(err), "got %v", err)
		s.False(ierr.IsValidation(err), "a wrong password must not also be a validation error")
	})

	s.Run("unknown email", func() {
		err := s.login("nobody@example.com", "any-password-1")

		s.True(ierr.IsPermissionDenied(err), "got %v", err)
		s.False(ierr.IsNotFound(err), "an unknown email must not also be a not-found error")
	})
}

func (s *AuthServiceSuite) TestSetPassword_PasswordOver72Bytes() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")
	token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))

	err := s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: strings.Repeat("a", 73)})

	s.True(ierr.IsValidation(err), "a too-long password is the user's to fix, got %v", err)
	s.False(ierr.IsSystem(err), "it must not answer 500")
	s.NoError(s.authService.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "long-enough-1"}),
		"a refused password must not use up the link")
}

// failingAuthRepo fails every password lookup, as a database outage would.
type failingAuthRepo struct{ auth.Repository }

func (failingAuthRepo) GetAuthByUserID(context.Context, string) (*auth.Auth, error) {
	return nil, ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

// failingUserRepo fails every user lookup, as a database outage would.
type failingUserRepo struct{ user.Repository }

func (failingUserRepo) GetByID(context.Context, string) (*user.User, error) {
	return nil, ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

// failingLockDB refuses every advisory lock, as a lock timeout would.
type failingLockDB struct{ postgres.IClient }

func (failingLockDB) LockWithWait(context.Context, postgres.LockRequest) error {
	return errors.New("lock wait timed out")
}

// racingAuthRepo reads the store once, then returns the password row another
// request saved after using the same link.
type racingAuthRepo struct {
	auth.Repository
	lookups int
}

func (r *racingAuthRepo) GetAuthByUserID(ctx context.Context, userID string) (*auth.Auth, error) {
	r.lookups++
	if r.lookups == 1 {
		return r.Repository.GetAuthByUserID(ctx, userID)
	}
	return auth.NewAuth(userID, types.AuthProviderFlexprice, "hash-saved-by-another-request"), nil
}

// A failed lookup or a missing secret must come back as itself, not as an expired link
// that would send the member to ask for a new one. Supabase deployments have no links.
func (s *AuthServiceSuite) TestCheckPasswordLink_Failures() {
	s.useLinkSecret()
	member := s.seedMember("user_member", types.StatusPublished, "")
	token := s.signLink(member, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))

	tests := []struct {
		name    string
		inject  func(svc *authService)
		wantErr func(error) bool
	}{
		{name: "user lookup fails", inject: func(svc *authService) { svc.UserRepo = failingUserRepo{svc.UserRepo} }, wantErr: ierr.IsDatabase},
		{name: "password lookup fails", inject: func(svc *authService) { svc.AuthRepo = failingAuthRepo{svc.AuthRepo} }, wantErr: ierr.IsDatabase},
		{name: "auth secret is not set", inject: func(svc *authService) { svc.Config.Auth.Secret = "" }, wantErr: ierr.IsSystem},
		{name: "Supabase deployment", inject: func(svc *authService) { svc.authProvider = &fakeSupabaseProvider{} }, wantErr: ierr.IsNotFound},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.useLinkSecret()
			svc := s.authService.(*authService)
			tc.inject(svc)

			details, err := svc.CheckPasswordLink(context.Background(), &dto.CheckPasswordLinkRequest{Token: token})

			s.Nil(details)
			s.True(tc.wantErr(err), "unexpected error: %v", err)
		})
	}
}

// TestSignUpProvisionsSelfBillingAfterCommit checks signup hands the new tenant to self-billing only once
// its transaction has committed, so a rollback never leaves a billing customer for a missing tenant.
func (s *AuthServiceSuite) TestSignUpProvisionsSelfBillingAfterCommit() {
	testCases := []struct {
		name       string
		tenantName string
		assignErr  error
		wantCall   bool
		wantName   string
	}{
		{name: "provisions the committed tenant", tenantName: "Acme", wantCall: true, wantName: "Acme"},
		{name: "provisions under the default tenant name", wantCall: true, wantName: s.GetConfig().Onboarding.DefaultTenantName},
		{name: "skips provisioning when the transaction fails", tenantName: "Acme", assignErr: errors.New("assign failed")},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.BaseServiceTestSuite.ClearStores()
			svc := s.authService.(*authService)
			original, originalHook := svc.authProvider, svc.SelfBilling
			svc.authProvider = &fakeSupabaseProvider{
				claims:    &auth.Claims{UserID: "user_1", Email: "owner@acme.io"},
				confirmed: true,
				assignErr: tc.assignErr,
			}
			hook := &recordingHook{}
			svc.SelfBilling = hook
			defer func() {
				svc.authProvider = original
				svc.SelfBilling = originalHook
			}()

			resp, err := svc.SignUp(s.GetContext(), &dto.SignUpRequest{
				Email:      "owner@acme.io",
				Password:   "password123",
				Token:      "token",
				TenantName: tc.tenantName,
			})

			if !tc.wantCall {
				s.Error(err)
				s.Empty(hook.calls)
				return
			}
			s.Require().NoError(err)
			s.Equal([]string{resp.TenantID + "|" + tc.wantName + "|owner@acme.io"}, hook.calls)
		})
	}
}

// Two requests racing with one link: the one that waited for the lock finds the
// password already saved and is refused instead of overwriting it.
func (s *AuthServiceSuite) TestSetPassword_LinkUsedWhileWaitingForLock() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")
	token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))
	svc := s.authService.(*authService)
	svc.AuthRepo = &racingAuthRepo{Repository: svc.AuthRepo}

	err := svc.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "long-enough-1"})

	s.True(ierr.IsNotFound(err), "expected the bad-link error, got %v", err)
	_, err = s.GetStores().AuthRepo.GetAuthByUserID(testutil.SetupContext(), invitee.ID)
	s.True(ierr.IsNotFound(err), "the refused request must not save a password")
}

func (s *AuthServiceSuite) TestSetPassword_LockFails() {
	s.useLinkSecret()
	invitee := s.seedMember("user_invitee", types.StatusPublished, "")
	token := s.signLink(invitee, types.DefaultTenantID, testLinkAuthSecret, time.Now().Add(time.Hour))
	svc := s.authService.(*authService)
	svc.DB = failingLockDB{svc.DB}

	err := svc.SetPassword(context.Background(), &dto.SetPasswordRequest{Token: token, Password: "long-enough-1"})

	s.True(ierr.IsInternal(err), "got %v", err)
	_, err = s.GetStores().AuthRepo.GetAuthByUserID(testutil.SetupContext(), invitee.ID)
	s.True(ierr.IsNotFound(err), "no password may be saved without the lock")
}

func (s *AuthServiceSuite) TestEmailCaseInsensitive() {
	ctx := s.GetContext()
	resp, err := s.authService.SignUp(ctx, &dto.SignUpRequest{Email: "  Alice@Example.com ", Password: "securepassword"})
	s.Require().NoError(err)

	for _, email := range []string{"alice@example.com", "ALICE@EXAMPLE.COM", "Alice@Example.com"} {
		found, err := s.userRepo.GetByEmail(ctx, email)
		s.Require().NoError(err, email)
		s.Equal(resp.UserID, found.ID, email)
		s.Equal("alice@example.com", found.Email, email)

		login, err := s.authService.Login(ctx, &dto.LoginRequest{Email: email, Password: "securepassword"})
		s.Require().NoError(err, email)
		s.Equal(resp.UserID, login.UserID, email)
	}

	for _, email := range []string{"alice@example.com", "ALICE@example.com"} {
		dup, err := s.authService.SignUp(ctx, &dto.SignUpRequest{Email: email, Password: "securepassword"})
		s.Error(err, email)
		s.Nil(dup, email)
	}
}

func (s *AuthServiceSuite) TestEmailLookupIgnoresArchivedUsers() {
	ctx := s.GetContext()
	archived := &user.User{ID: "user-old", Email: "Alice@Example.com"}
	archived.Status = types.StatusArchived
	s.Require().NoError(s.userRepo.Create(ctx, archived))
	active := &user.User{ID: "user-new", Email: "alice@example.com"}
	active.Status = types.StatusPublished
	s.Require().NoError(s.userRepo.Create(ctx, active))

	for _, email := range []string{"Alice@Example.com", "alice@example.com"} {
		found, err := s.userRepo.GetByEmail(ctx, email)
		s.Require().NoError(err, email)
		s.Equal("user-new", found.ID, email)
	}
}
