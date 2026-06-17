package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/secret"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
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
