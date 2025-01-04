package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type AuthServiceSuite struct {
	suite.Suite
	ctx         context.Context
	authService *authService
	userRepo    *testutil.InMemoryUserStore
	authRepo    *testutil.InMemoryAuthRepository
}

func TestAuthService(t *testing.T) {
	suite.Run(t, new(AuthServiceSuite))
}

func (s *AuthServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.userRepo = testutil.NewInMemoryUserStore()
	s.authRepo = testutil.NewInMemoryAuthRepository()

	// Create a real provider (e.g., flexpriceAuth) with test config
	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   "test-secret", // Use a test secret
		},
	}

	realProvider := authProvider.NewFlexpriceAuth(cfg)

	s.authService = &authService{
		userRepo:     s.userRepo,
		authProvider: realProvider,
		authRepo:     s.authRepo,
	}
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
			name: "duplicate_email",
			req: &dto.SignUpRequest{
				Email:    "existing@example.com",
				Password: "securepassword",
			},
			setupFunc: func() {
				// Create an existing user to trigger a duplicate scenario
				_ = s.userRepo.Create(s.ctx, &user.User{
					ID:    "user-1",
					Email: "existing@example.com",
				})
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			if tc.setupFunc != nil {
				tc.setupFunc()
			}

			resp, err := s.authService.SignUp(s.ctx, tc.req)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				// We used a real provider, so check that token exists (not necessarily 'auth-token' as before)
				s.NotEmpty(resp.Token)
			}
		})
	}
}
