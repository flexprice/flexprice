package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/secret"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type SecretServiceSuite struct {
	testutil.BaseServiceTestSuite
	service       SecretService
	secretRepo    secret.Repository
	encryptionSvc security.EncryptionService
	testData      struct {
		secrets struct {
			apiKey      *secret.Secret
			integration *secret.Secret
		}
	}
}

func TestSecretService(t *testing.T) {
	suite.Run(t, new(SecretServiceSuite))
}

func (s *SecretServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *SecretServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SecretServiceSuite) setupService() {
	// Create encryption service with test config
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}

	var err error
	s.encryptionSvc, err = security.NewEncryptionService(cfg, s.GetLogger())
	s.Require().NoError(err, "Failed to create encryption service")

	s.secretRepo = s.GetStores().SecretRepo
	userRepo := s.GetStores().UserRepo
	s.service = NewSecretService(s.secretRepo, userRepo, cfg, s.GetLogger())
}

func (s *SecretServiceSuite) setupTestData() {
	// Clean up any existing test data
	if s.testData.secrets.apiKey != nil {
		_ = s.secretRepo.Delete(s.GetContext(), s.testData.secrets.apiKey.ID)
		s.testData.secrets.apiKey = nil
	}
	if s.testData.secrets.integration != nil {
		_ = s.secretRepo.Delete(s.GetContext(), s.testData.secrets.integration.ID)
		s.testData.secrets.integration = nil
	}

	// Create default test user (needed for API key creation)
	userRepo := s.GetStores().UserRepo
	testUser := &domainUser.User{
		ID:    types.DefaultUserID,
		Email: "test@example.com",
		Type:  types.UserTypeUser,
		Roles: []string{}, // Empty roles = full access
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	_ = userRepo.Create(s.GetContext(), testUser)

	// Create test API key
	apiKey := &secret.Secret{
		ID:        "secret_test_api_key",
		Name:      "Test API Key",
		Type:      types.SecretTypePrivateKey,
		Provider:  types.SecretProviderFlexPrice,
		Value:     s.encryptionSvc.Hash("test_api_key"),
		DisplayID: "test1",
		Roles:     []string{}, // Empty roles = full access
		UserType:  "user",     // Default user type
		UserID:    types.DefaultUserID,
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	err := s.secretRepo.Create(s.GetContext(), apiKey)
	s.Require().NoError(err)
	s.testData.secrets.apiKey = apiKey

	// Create test integration with encrypted credentials
	encryptedCreds, err := s.encryptionSvc.Encrypt("test_stripe_key")
	s.Require().NoError(err)

	integration := &secret.Secret{
		ID:           "secret_test_integration",
		Name:         "Test Integration",
		Type:         types.SecretTypeIntegration,
		Provider:     types.SecretProviderStripe,
		ProviderData: map[string]string{"api_key": encryptedCreds},
		DisplayID:    "test2",
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	err = s.secretRepo.Create(s.GetContext(), integration)
	s.Require().NoError(err)
	s.testData.secrets.integration = integration
}

func (s *SecretServiceSuite) TestCreateAPIKey() {
	tests := []struct {
		name      string
		req       dto.CreateAPIKeyRequest
		wantErr   bool
		errString string
	}{
		{
			name: "successful creation of private API key",
			req: dto.CreateAPIKeyRequest{
				Name: "Test Key",
				Type: types.SecretTypePrivateKey,
			},
			wantErr: false,
		},
		{
			name: "successful creation of publishable API key",
			req: dto.CreateAPIKeyRequest{
				Name: "Test Key",
				Type: types.SecretTypePublishableKey,
			},
			wantErr: false,
		},
		{
			name: "error - missing name",
			req: dto.CreateAPIKeyRequest{
				Type: types.SecretTypePrivateKey,
			},
			wantErr:   true,
			errString: "Error:Field validation",
		},
		{
			name: "error - invalid type",
			req: dto.CreateAPIKeyRequest{
				Name: "Test Key",
				Type: "invalid",
			},
			wantErr:   true,
			errString: "invalid secret type",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, apiKey, err := s.service.CreateAPIKey(s.GetContext(), &tt.req)
			if tt.wantErr {
				s.Error(err)
				s.Contains(err.Error(), tt.errString)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.NotEmpty(apiKey)
			s.Equal(tt.req.Name, resp.Name)
			s.Equal(tt.req.Type, resp.Type)
			s.Equal(types.SecretProviderFlexPrice, resp.Provider)
			s.NotEmpty(resp.DisplayID)
			s.Len(resp.DisplayID, 10)
		})
	}
}

func (s *SecretServiceSuite) TestCreateIntegration() {
	tests := []struct {
		name      string
		req       dto.CreateIntegrationRequest
		wantErr   bool
		errString string
	}{
		{
			name: "successful creation of integration",
			req: dto.CreateIntegrationRequest{
				Name:     "Test Integration",
				Provider: types.SecretProviderStripe,
				Credentials: map[string]string{
					"api_key": "test_key",
				},
			},
			wantErr: false,
		},
		{
			name: "error - missing name",
			req: dto.CreateIntegrationRequest{
				Provider: types.SecretProviderStripe,
				Credentials: map[string]string{
					"api_key": "test_key",
				},
			},
			wantErr:   true,
			errString: "validation failed",
		},
		{
			name: "error - missing credentials",
			req: dto.CreateIntegrationRequest{
				Name:     "Test Integration",
				Provider: types.SecretProviderStripe,
			},
			wantErr:   true,
			errString: "validation failed",
		},
		{
			name: "error - invalid provider",
			req: dto.CreateIntegrationRequest{
				Name:     "Test Integration",
				Provider: types.SecretProviderFlexPrice,
				Credentials: map[string]string{
					"api_key": "test_key",
				},
			},
			wantErr:   true,
			errString: "validation failed",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.CreateIntegration(s.GetContext(), &tt.req)
			if tt.wantErr {
				s.Error(err)
				s.Contains(err.Error(), tt.errString)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.req.Name, resp.Name)
			s.Equal(types.SecretTypeIntegration, resp.Type)
			s.Equal(tt.req.Provider, resp.Provider)
			s.NotEmpty(resp.DisplayID)
		})
	}
}

func (s *SecretServiceSuite) TestVerifyAPIKey() {
	s.setupTestData() // Setup test data once for all test cases

	tests := []struct {
		name      string
		apiKey    string
		wantErr   bool
		errString string
	}{
		{
			name:    "successful verification",
			apiKey:  "test_api_key",
			wantErr: false,
		},
		{
			name:      "error - empty API key",
			apiKey:    "",
			wantErr:   true,
			errString: "validation failed",
		},
		{
			name:      "error - invalid API key",
			apiKey:    "invalid_key",
			wantErr:   true,
			errString: "invalid API key",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			secret, err := s.service.VerifyAPIKey(s.GetContext(), tt.apiKey)
			if tt.wantErr {
				s.Error(err)
				s.Contains(err.Error(), tt.errString)
				return
			}

			s.NoError(err)
			s.NotNil(secret)
			s.Equal(types.SecretTypePrivateKey, secret.Type)
			s.Equal(types.SecretProviderFlexPrice, secret.Provider)
		})
	}
}

func (s *SecretServiceSuite) TestGetIntegrationCredentials() {
	s.setupTestData() // Setup test data once for all test cases

	tests := []struct {
		name           string
		provider       string
		wantErr        bool
		errString      string
		wantCredential string
	}{
		{
			name:           "successful retrieval",
			provider:       string(types.SecretProviderStripe),
			wantCredential: "test_stripe_key",
		},
		{
			name:      "error - provider not found",
			provider:  "non_existent_provider",
			wantErr:   true,
			errString: "non_existent_provider integration not configured",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			creds, err := s.service.getIntegrationCredentials(s.GetContext(), tt.provider)
			if tt.wantErr {
				s.Error(err)
				s.Contains(err.Error(), tt.errString)
				return
			}

			s.NoError(err)
			s.NotNil(creds)
			s.Equal(tt.wantCredential, creds[0]["api_key"])
		})
	}
}

func (s *SecretServiceSuite) TestListAPIKeys() {
	s.setupTestData() // Ensure test data exists

	tests := []struct {
		name          string
		filter        *types.SecretFilter
		expectedTotal int
		wantErr       bool
		errString     string
	}{
		{
			name: "list all API keys",
			filter: &types.SecretFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				Type:        lo.ToPtr(types.SecretTypePrivateKey),
			},
			expectedTotal: 1,
		},
		{
			name: "list with pagination",
			filter: &types.SecretFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(1),
					Offset: lo.ToPtr(0),
				},
				Type: lo.ToPtr(types.SecretTypePrivateKey),
			},
			expectedTotal: 1,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.ListAPIKeys(s.GetContext(), tt.filter)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.expectedTotal, resp.Pagination.Total)
			s.Len(resp.Items, tt.expectedTotal)
		})
	}
}

func (s *SecretServiceSuite) TestListIntegrations() {
	s.setupTestData() // Ensure test data exists

	tests := []struct {
		name          string
		filter        *types.SecretFilter
		expectedTotal int
		wantErr       bool
		errString     string
	}{
		{
			name: "list all integrations",
			filter: &types.SecretFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				Type:        lo.ToPtr(types.SecretTypeIntegration),
			},
			expectedTotal: 1,
		},
		{
			name: "list with provider filter",
			filter: &types.SecretFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				Type:        lo.ToPtr(types.SecretTypeIntegration),
				Provider:    lo.ToPtr(types.SecretProviderStripe),
			},
			expectedTotal: 1,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.ListIntegrations(s.GetContext(), tt.filter)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.expectedTotal, resp.Pagination.Total)
			s.Len(resp.Items, tt.expectedTotal)
		})
	}
}

// writerCtx returns a context for a non-super_admin human user holding the
// wildcard all_writer role, the weakest principal that passes write(secret).
func (s *SecretServiceSuite) writerCtx(userID string) context.Context {
	ctx := context.WithValue(s.GetContext(), types.CtxUserID, userID)
	ctx = context.WithValue(ctx, types.CtxUserType, string(types.UserTypeUser))
	return context.WithValue(ctx, types.CtxRoles, []string{types.RoleAllWriter.String()})
}

func (s *SecretServiceSuite) superAdminCtx(userID string) context.Context {
	ctx := context.WithValue(s.GetContext(), types.CtxUserID, userID)
	ctx = context.WithValue(ctx, types.CtxUserType, string(types.UserTypeUser))
	return context.WithValue(ctx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})
}

// createServiceAccount persists a privileged service account owned by nobody in
// particular, standing in for the target a writer must not be able to hijack.
func (s *SecretServiceSuite) createServiceAccount(id string) *domainUser.User {
	sa := &domainUser.User{
		ID:    id,
		Email: id + "@example.com",
		Type:  types.UserTypeServiceAccount,
		Roles: []string{types.RoleSuperAdmin.String()},
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	s.Require().NoError(s.GetStores().UserRepo.Create(s.GetContext(), sa))
	return sa
}

// A writer must not be able to mint a key that inherits another principal's
// roles by naming it in service_account_id.
func (s *SecretServiceSuite) TestCreateAPIKeyRejectsCrossPrincipalMinting() {
	sa := s.createServiceAccount("user_privileged_sa")

	writer := s.writerCtx("user_attacker")
	_, _, err := s.service.CreateAPIKey(writer, &dto.CreateAPIKeyRequest{
		Name:             "stolen",
		Type:             types.SecretTypePrivateKey,
		ServiceAccountID: sa.ID,
	})
	s.Require().Error(err, "writer must not mint a key for another service account")
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got: %v", err)

	// A service account may not escalate via its own key either, even when its
	// stored roles include super_admin.
	saCtx := context.WithValue(s.GetContext(), types.CtxUserID, sa.ID)
	saCtx = context.WithValue(saCtx, types.CtxUserType, string(types.UserTypeServiceAccount))
	saCtx = context.WithValue(saCtx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})
	_, _, err = s.service.CreateAPIKey(saCtx, &dto.CreateAPIKeyRequest{
		Name:             "self-mint",
		Type:             types.SecretTypePrivateKey,
		ServiceAccountID: sa.ID,
	})
	s.Require().Error(err, "service account must not mint service-account keys")
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got: %v", err)
}

// A super_admin retains the ability to mint service-account keys, and the key
// still inherits the target's roles.
func (s *SecretServiceSuite) TestCreateAPIKeyAllowsSuperAdminCrossPrincipalMinting() {
	sa := s.createServiceAccount("user_sa_for_admin")

	resp, apiKey, err := s.service.CreateAPIKey(s.superAdminCtx("user_admin"), &dto.CreateAPIKeyRequest{
		Name:             "legit",
		Type:             types.SecretTypePrivateKey,
		ServiceAccountID: sa.ID,
	})
	s.Require().NoError(err)
	s.NotEmpty(apiKey)
	s.Equal(sa.ID, resp.UserID)
	s.Equal(string(types.UserTypeServiceAccount), resp.UserType)
	s.Equal(sa.Roles, resp.Roles)
}

// A writer creating a key without service_account_id is bound to itself.
func (s *SecretServiceSuite) TestCreateAPIKeyBindsToCaller() {
	caller := &domainUser.User{
		ID:    "user_self_writer",
		Email: "self@example.com",
		Type:  types.UserTypeUser,
		Roles: []string{types.RoleAllWriter.String()},
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	s.Require().NoError(s.GetStores().UserRepo.Create(s.GetContext(), caller))

	resp, _, err := s.service.CreateAPIKey(s.writerCtx(caller.ID), &dto.CreateAPIKeyRequest{
		Name: "own key",
		Type: types.SecretTypePrivateKey,
	})
	s.Require().NoError(err)
	s.Equal(caller.ID, resp.UserID)
}

// Listing must not expose keys belonging to other principals, and a supplied
// user_id filter must not let a writer read around that scoping.
func (s *SecretServiceSuite) TestListAPIKeysScopedToCaller() {
	foreign := &secret.Secret{
		ID:        "secret_foreign_key",
		Name:      "Foreign Key",
		Type:      types.SecretTypePrivateKey,
		Provider:  types.SecretProviderFlexPrice,
		Value:     s.encryptionSvc.Hash("foreign_api_key"),
		DisplayID: "frgn1",
		UserID:    "user_victim",
		UserType:  string(types.UserTypeUser),
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	s.Require().NoError(s.secretRepo.Create(s.GetContext(), foreign))

	own := &secret.Secret{
		ID:        "secret_own_key",
		Name:      "Own Key",
		Type:      types.SecretTypePrivateKey,
		Provider:  types.SecretProviderFlexPrice,
		Value:     s.encryptionSvc.Hash("own_api_key"),
		DisplayID: "own01",
		UserID:    "user_attacker",
		UserType:  string(types.UserTypeUser),
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	s.Require().NoError(s.secretRepo.Create(s.GetContext(), own))

	writer := s.writerCtx("user_attacker")

	resp, err := s.service.ListAPIKeys(writer, &types.SecretFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Require().NoError(err)
	s.Require().NotEmpty(resp.Items, "writer must still see its own key")
	ids := lo.Map(resp.Items, func(item *dto.SecretResponse, _ int) string { return item.ID })
	s.Contains(ids, own.ID)
	s.NotContains(ids, foreign.ID)
	for _, item := range resp.Items {
		s.Equal("user_attacker", item.UserID, "writer listed a key owned by another principal")
	}

	// An attacker-supplied user_id must not widen the scope back out.
	resp, err = s.service.ListAPIKeys(writer, &types.SecretFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
		UserID:      lo.ToPtr("user_victim"),
	})
	s.Require().NoError(err)
	s.Require().NotEmpty(resp.Items, "the user_id filter must be replaced, not intersected away")
	ids = lo.Map(resp.Items, func(item *dto.SecretResponse, _ int) string { return item.ID })
	s.Contains(ids, own.ID)
	s.NotContains(ids, foreign.ID)
	for _, item := range resp.Items {
		s.Equal("user_attacker", item.UserID, "writer read another principal's keys via a user_id filter")
	}

	// A super_admin still sees the whole environment.
	resp, err = s.service.ListAPIKeys(s.superAdminCtx("user_admin"), &types.SecretFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	s.Require().NoError(err)
	s.GreaterOrEqual(len(resp.Items), 2, "super_admin must still see all keys")
}

// A writer must not be able to revoke a credential it does not own.
func (s *SecretServiceSuite) TestDeleteRejectsForeignSecret() {
	foreign := &secret.Secret{
		ID:        "secret_foreign_delete",
		Name:      "Foreign Key",
		Type:      types.SecretTypePrivateKey,
		Provider:  types.SecretProviderFlexPrice,
		Value:     s.encryptionSvc.Hash("foreign_delete_key"),
		DisplayID: "frgn2",
		UserID:    "user_victim",
		UserType:  string(types.UserTypeUser),
		BaseModel: types.BaseModel{
			TenantID:  types.DefaultTenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}
	s.Require().NoError(s.secretRepo.Create(s.GetContext(), foreign))

	err := s.service.Delete(s.writerCtx("user_attacker"), foreign.ID)
	s.Require().Error(err, "writer must not revoke another principal's key")
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got: %v", err)

	// The key survives, and a super_admin can still revoke it.
	still, err := s.secretRepo.Get(s.GetContext(), foreign.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusPublished, still.Status)

	s.Require().NoError(s.service.Delete(s.superAdminCtx("user_admin"), foreign.ID))
}

func (s *SecretServiceSuite) TestDelete() {
	tests := []struct {
		name      string
		setupID   string
		wantErr   bool
		errString string
	}{
		{
			name:    "successful deletion of API key",
			setupID: "secret_test_api_key",
			wantErr: false,
		},
		{
			name:    "successful deletion of integration",
			setupID: "secret_test_integration",
			wantErr: false,
		},
		{
			name:      "error - secret not found",
			setupID:   "non_existent_id",
			wantErr:   true,
			errString: "not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.Delete(s.GetContext(), tt.setupID)
			if tt.wantErr {
				s.Error(err)
				s.Contains(err.Error(), tt.errString)
				return
			}

			s.NoError(err)

			// Verify secret is deleted
			secret, err := s.secretRepo.Get(s.GetContext(), tt.setupID)
			s.Error(err)
			s.Nil(secret)
		})
	}
}
