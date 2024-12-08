package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type UserServiceSuite struct {
	suite.Suite
	ctx         context.Context
	userService *userService
	userRepo    *testutil.InMemoryUserRepository
}

func TestUserService(t *testing.T) {
	suite.Run(t, new(UserServiceSuite))
}

func (s *UserServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.userRepo = testutil.NewInMemoryUserRepository()

	s.userService = &userService{
		userRepo: s.userRepo,
	}
}

func (s *UserServiceSuite) TestCreateUser() {
	testCases := []struct {
		name          string
		email         string
		setup         func()
		expectedError bool
	}{
		{
			name:          "successful_creation",
			email:         "newuser@example.com",
			expectedError: false,
		},
		{
			name:  "duplicate_email",
			email: "existing@example.com",
			setup: func() {
				// Pre-insert a user to simulate a duplicate
				_ = s.userRepo.Create(s.ctx, &user.User{
					ID:    "user-1",
					Email: "existing@example.com",
				})
			},
			expectedError: true,
		},
		{
			name:          "empty_email",
			email:         "",
			expectedError: true, // Assumes userService should not allow empty emails
		},
		{
			name:          "invalid_email_format",
			email:         "not-an-email",
			expectedError: true, // Assumes userService should validate email format
		},
		{
			name:          "very_long_email",
			email:         "this_is_a_very_long_email_address_that_might_break_some_limitations@somewhere.com",
			expectedError: false, // If you decide to allow any length, keep false; else true.
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			err := s.userService.CreateUser(s.ctx, &user.User{
				Email: tc.email,
			})

			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)
			}
		})
	}
}

func (s *UserServiceSuite) TestGetUserByEmail() {
	// Insert a known user
	_ = s.userRepo.Create(s.ctx, &user.User{
		ID:    "user-1",
		Email: "test@example.com",
	})

	testCases := []struct {
		name          string
		email         string
		expectedError bool
		expectedID    string
	}{
		{
			name:          "user_found",
			email:         "test@example.com",
			expectedError: false,
			expectedID:    "user-1",
		},
		{
			name:          "user_not_found",
			email:         "unknown@example.com",
			expectedError: true,
		},
		{
			name:          "empty_email",
			email:         "",
			expectedError: true, // Assuming service should handle empty input as invalid.
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			u, err := s.userService.GetUserByEmail(s.ctx, tc.email)

			if tc.expectedError {
				s.Error(err)
				s.Nil(u)
			} else {
				s.NoError(err)
				s.NotNil(u)
				s.Equal(tc.expectedID, u.ID)
			}
		})
	}
}
