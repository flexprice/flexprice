package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainConnection "github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/secret"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock repository
type MockConnectionRepository struct {
	mock.Mock
}

func (m *MockConnectionRepository) Create(ctx context.Context, connection *domainConnection.Connection) error {
	args := m.Called(ctx, connection)
	return args.Error(0)
}

func (m *MockConnectionRepository) Get(ctx context.Context, id string) (*domainConnection.Connection, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domainConnection.Connection), args.Error(1)
}

func (m *MockConnectionRepository) GetByProviderType(ctx context.Context, providerType types.SecretProvider) (*domainConnection.Connection, error) {
	args := m.Called(ctx, providerType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domainConnection.Connection), args.Error(1)
}

func (m *MockConnectionRepository) GetByConnectionCode(ctx context.Context, connectionCode string) (*domainConnection.Connection, error) {
	args := m.Called(ctx, connectionCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domainConnection.Connection), args.Error(1)
}

func (m *MockConnectionRepository) List(ctx context.Context, filter *types.ConnectionFilter) ([]*domainConnection.Connection, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domainConnection.Connection), args.Error(1)
}

func (m *MockConnectionRepository) Count(ctx context.Context, filter *types.ConnectionFilter) (int, error) {
	args := m.Called(ctx, filter)
	return args.Int(0), args.Error(1)
}

func (m *MockConnectionRepository) Update(ctx context.Context, connection *domainConnection.Connection) error {
	args := m.Called(ctx, connection)
	return args.Error(0)
}

func (m *MockConnectionRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// Mock secret service
type MockSecretService struct {
	mock.Mock
}

func (m *MockSecretService) CreateAPIKey(ctx context.Context, req *dto.CreateAPIKeyRequest) (*secret.Secret, string, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, "", args.Error(2)
	}
	return args.Get(0).(*secret.Secret), args.String(1), args.Error(2)
}

func (m *MockSecretService) ListAPIKeys(ctx context.Context, filter *types.SecretFilter) (*dto.ListSecretsResponse, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.ListSecretsResponse), args.Error(1)
}

func (m *MockSecretService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockSecretService) CreateIntegration(ctx context.Context, req *dto.CreateIntegrationRequest) (*secret.Secret, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*secret.Secret), args.Error(1)
}

func (m *MockSecretService) ListIntegrations(ctx context.Context, filter *types.SecretFilter) (*dto.ListSecretsResponse, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dto.ListSecretsResponse), args.Error(1)
}

func (m *MockSecretService) GetIntegrationCredentials(ctx context.Context, provider types.SecretProvider) (map[string]string, error) {
	args := m.Called(ctx, provider)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockSecretService) getIntegrationCredentials(ctx context.Context, provider types.SecretProvider) ([]map[string]string, error) {
	args := m.Called(ctx, provider)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]map[string]string), args.Error(1)
}

func (m *MockSecretService) VerifyAPIKey(ctx context.Context, apiKey string) (*secret.Secret, error) {
	args := m.Called(ctx, apiKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*secret.Secret), args.Error(1)
}

func (m *MockSecretService) ListLinkedIntegrations(ctx context.Context) ([]types.SecretProvider, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.SecretProvider), args.Error(1)
}

// Test cases
func TestConnectionService_Create(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSecretSvc := new(MockSecretService)
	log := &logger.Logger{}

	svc := NewConnectionService(mockRepo, mockSecretSvc, log)

	ctx := context.Background()

	// Test successful creation
	req := &dto.CreateConnectionRequest{
		Name:           "Test Connection",
		ProviderType:   types.SecretProviderStripe,
		ConnectionCode: "stripe_test",
		Credentials: map[string]string{
			"api_key": "sk_test_123",
		},
	}

	secret := &secret.Secret{
		ID:       "sec_123",
		Name:     "Test Connection",
		Provider: types.SecretProviderStripe,
	}

	mockSecretSvc.On("CreateIntegration", ctx, mock.AnythingOfType("*dto.CreateIntegrationRequest")).Return(secret, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*connection.Connection")).Return(nil)

	conn, err := svc.Create(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Equal(t, "Test Connection", conn.Name)
	assert.Equal(t, types.SecretProviderStripe, conn.ProviderType)
	assert.Equal(t, "stripe_test", conn.ConnectionCode)
	assert.Equal(t, "sec_123", conn.SecretID)

	mockSecretSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)

	// Test failure in creating integration
	mockSecretSvc.On("CreateIntegration", ctx, mock.AnythingOfType("*dto.CreateIntegrationRequest")).Return(nil, errors.New("failed to create integration"))

	conn, err = svc.Create(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "Failed to create integration secret for connection")

	// Test failure in creating connection
	mockSecretSvc.On("CreateIntegration", ctx, mock.AnythingOfType("*dto.CreateIntegrationRequest")).Return(secret, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*connection.Connection")).Return(errors.New("failed to create connection"))
	mockSecretSvc.On("Delete", ctx, "sec_123").Return(nil)

	conn, err = svc.Create(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "failed to create connection")

	mockSecretSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestConnectionService_Get(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSecretSvc := new(MockSecretService)
	log := &logger.Logger{}

	svc := NewConnectionService(mockRepo, mockSecretSvc, log)

	ctx := context.Background()

	// Test successful get
	conn := &domainConnection.Connection{
		ID:             "conn_123",
		Name:           "Test Connection",
		ProviderType:   types.SecretProviderStripe,
		ConnectionCode: "stripe_test",
		SecretID:       "sec_123",
		BaseModel: types.BaseModel{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Status:    types.StatusPublished,
		},
	}

	mockRepo.On("Get", ctx, "conn_123").Return(conn, nil)

	result, err := svc.Get(ctx, "conn_123")

	assert.NoError(t, err)
	assert.Equal(t, conn, result)

	mockRepo.AssertExpectations(t)

	// Test not found
	mockRepo.On("Get", ctx, "conn_456").Return(nil, ierr.ErrNotFound)

	result, err = svc.Get(ctx, "conn_456")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, ierr.IsNotFound(err))

	mockRepo.AssertExpectations(t)
}

func TestConnectionService_Delete(t *testing.T) {
	mockRepo := new(MockConnectionRepository)
	mockSecretSvc := new(MockSecretService)
	log := &logger.Logger{}

	svc := NewConnectionService(mockRepo, mockSecretSvc, log)

	ctx := context.Background()

	// Test successful delete
	conn := &domainConnection.Connection{
		ID:       "conn_123",
		SecretID: "sec_123",
	}

	mockRepo.On("Get", ctx, "conn_123").Return(conn, nil)
	mockRepo.On("Delete", ctx, "conn_123").Return(nil)
	mockSecretSvc.On("Delete", ctx, "sec_123").Return(nil)

	err := svc.Delete(ctx, "conn_123")

	assert.NoError(t, err)

	mockRepo.AssertExpectations(t)
	mockSecretSvc.AssertExpectations(t)

	// Test connection not found
	mockRepo.On("Get", ctx, "conn_456").Return(nil, ierr.ErrNotFound)

	err = svc.Delete(ctx, "conn_456")

	assert.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))

	// Test delete connection error
	mockRepo.On("Get", ctx, "conn_789").Return(conn, nil)
	mockRepo.On("Delete", ctx, "conn_789").Return(errors.New("failed to delete"))

	err = svc.Delete(ctx, "conn_789")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete")

	mockRepo.AssertExpectations(t)
}
