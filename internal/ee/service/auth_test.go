package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

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
	return nil
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
