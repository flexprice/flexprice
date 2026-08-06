# API Key Management System

## Overview

FlexPrice needs a comprehensive solution to manage various types of credentials securely. Currently, API keys are managed through configuration files, but there's a need to transition to a database-backed solution with frontend management capabilities. This document outlines the technical requirements and design for a unified secrets management system.

## Types of Credentials

1. **Private API Keys (S2S)**: Used for server-to-server communication with FlexPrice
2. **Publishable API Keys**: Used from frontend applications to access FlexPrice
3. **Integration Credentials**: Third-party service credentials (e.g., Stripe, Razorpay) including API keys, client IDs, client secrets, and webhook URLs

## Goals

- Move credential storage from configuration files to a database
- Provide a secure way to generate, manage, and revoke API keys
- Allow users to manage integration credentials via the frontend
- Implement efficient validation mechanisms to minimize performance impact
- Support one publishable API key and multiple private keys per tenant
- Support one account per third-party integration
- Ensure proper security practices for credential storage
- Implement a unified approach to credential management
- Support both hashing (for API keys) and encryption (for integration credentials)

## Non-Goals

- Implementing a full-featured key rotation system (future enhancement)
- Supporting multiple accounts for the same integration type (current limitation)
- Custom permission schemas beyond read/write permissions (future enhancement)

## Design Considerations

### Unified Data Model

We'll implement a unified `Secret` entity to store all types of credentials:

```go
type Secret struct {
    ID             string     // UUID with prefix
    Name           string     // Human-readable name
    Type           string     // "private_key", "publishable_key", "integration"
    Provider       string     // "flexprice", "stripe", "razorpay", etc.
    Value          string     // Hashed or encrypted value based on type
    Prefix         string     // First 8 characters (for display purposes)
    Permissions    []string   // Array of permissions (read, write)
    ExpiresAt      *time.Time // Optional expiration
    LastUsedAt     *time.Time // Track usage
    Metadata       map[string]string // Additional configuration
    // BaseMixin fields (tenant_id, status, created_at, updated_at, etc.)
}
```

### Security Implementation

#### 1. Credential Storage Strategy

We'll implement a dual approach for credential storage:

1. **For API Keys (FlexPrice)**: 
   - Use one-way hashing (SHA-256) for storage
   - Only store the key prefix in plaintext for display
   - Validate by hashing the incoming key and comparing

2. **For Integration Credentials**:
   - Use reversible encryption (AES-GCM via Google Tink)
   - Store encrypted values in the database
   - Decrypt when needed for API calls

#### 2. Encryption/Decryption Module

We'll create a dedicated encryption service using Google Tink:

```go
// in internal/security/encryption.go
type EncryptionService interface {
    Encrypt(plaintext string) (string, error)
    Decrypt(ciphertext string) (string, error)
    Hash(value string) string
}

type tinkEncryptionService struct {
    keysetHandle *keyset.Handle
    logger       *logger.Logger
}

func NewEncryptionService(cfg *config.Configuration, logger *logger.Logger) (EncryptionService, error) {
    // Initialize with master key from config
    // Support future KMS integration
}
```

### Security Considerations

1. **Master Key Management**:
   - Initially store the master encryption key in the configuration
   - Support future integration with AWS KMS or HashiCorp Vault
   - Implement key rotation capabilities

2. **Access Control**:
   - Implement proper authorization to manage secrets
   - Only administrators should be able to create, list, or revoke credentials
   - Audit all credential operations

3. **Validation Performance**:
   - Implement an in-memory cache for API key validation
   - Periodically refresh the cache to reflect changes
   - Use Redis for distributed environments

## Implementation Details

### Database Schema

Following the entity development guide, we'll create the Secret schema:

```go
// in ent/schema/secret.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
    "github.com/flexprice/flexprice/ent/schema/mixin"
)

type Secret struct {
    ent.Schema
}

func (Secret) Mixin() []ent.Mixin {
    return []ent.Mixin{
        mixin.BaseMixin{},
    }
}

func (Secret) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            Unique().
            Immutable(),
        field.String("name").NotEmpty(),
        field.String("type").NotEmpty(), // "private_key", "publishable_key", "integration"
        field.String("provider").NotEmpty(), // "flexprice", "stripe", etc.
        field.String("value").NotEmpty(), // Hashed or encrypted value
        field.String("prefix").NotEmpty(),
        field.Strings("permissions"),
        field.Time("expires_at").Optional().Nillable(),
        field.Time("last_used_at").Optional().Nillable(),
        field.JSON("metadata", map[string]string{}).Optional(),
    }
}

func (Secret) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("tenant", Tenant.Type).Ref("secrets").Unique().Required(),
    }
}

func (Secret) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("tenant_id", "status"),
        index.Fields("type", "provider"),
        index.Fields("value"),
    }
}
```

### Domain Model

```go
// in internal/domain/secret/model.go
package secret

import (
    "time"

    "github.com/flexprice/flexprice/ent"
    "github.com/flexprice/flexprice/internal/types"
)

// Secret types
const (
    SecretTypePrivateKey     = "private_key"
    SecretTypePublishableKey = "publishable_key"
    SecretTypeIntegration    = "integration"
)

// Provider types
const (
    SecretProviderFlexPrice = "flexprice"
    SecretProviderStripe    = "stripe"
    SecretProviderRazorpay  = "razorpay"
    // Add more as needed
)

// Secret represents a credential in the system
type Secret struct {
    ID          string
    Name        string
    Type        string
    Provider    string
    Value       string
    Prefix      string
    Permissions []string
    ExpiresAt   *time.Time
    LastUsedAt  *time.Time
    Metadata    map[string]string
    types.BaseModel
}

// FromEnt converts an ent.Secret to a domain Secret
func FromEnt(e *ent.Secret) *Secret {
    if e == nil {
        return nil
    }
    
    return &Secret{
        ID:          e.ID,
        Name:        e.Name,
        Type:        e.Type,
        Provider:    e.Provider,
        Value:       e.Value,
        Prefix:      e.Prefix,
        Permissions: e.Permissions,
        ExpiresAt:   e.ExpiresAt,
        LastUsedAt:  e.LastUsedAt,
        Metadata:    e.Metadata,
        BaseModel: types.BaseModel{
            TenantID:  e.TenantID,
            Status:    types.Status(e.Status),
            CreatedBy: e.CreatedBy,
            UpdatedBy: e.UpdatedBy,
            CreatedAt: e.CreatedAt,
            UpdatedAt: e.UpdatedAt,
        },
    }
}

// FromEntList converts a list of ent.Secret to domain Secrets
func FromEntList(list []*ent.Secret) []*Secret {
    if list == nil {
        return nil
    }
    
    secrets := make([]*Secret, len(list))
    for i, e := range list {
        secrets[i] = FromEnt(e)
    }
    
    return secrets
}
```

### Repository Interface

```go
// in internal/domain/secret/repository.go
package secret

import (
    "context"

    "github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for secret storage
type Repository interface {
    // Core CRUD operations
    Create(ctx context.Context, secret *Secret) error
    Get(ctx context.Context, id string) (*Secret, error)
    List(ctx context.Context, filter *types.EntityFilter) ([]*Secret, error)
    Count(ctx context.Context, filter *types.EntityFilter) (int, error)
    Update(ctx context.Context, secret *Secret) error
    Delete(ctx context.Context, id string) error
    
    // Specialized operations
    GetByValue(ctx context.Context, hashedValue string) (*Secret, error)
    GetByTypeAndProvider(ctx context.Context, tenantID, secretType, provider string) ([]*Secret, error)
    GetActiveIntegration(ctx context.Context, tenantID, provider string) (*Secret, error)
}
```

### Service Layer

```go
// in internal/ee/service/secret.go
package service

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "time"

    "github.com/flexprice/flexprice/internal/domain/secret"
    "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/logger"
    "github.com/flexprice/flexprice/internal/security"
    "github.com/flexprice/flexprice/internal/types"
)

// SecretService handles business logic for secrets
type SecretService struct {
    secretRepo       secret.Repository
    encryptionService security.EncryptionService
    cacheService     CacheService
    logger           *logger.Logger
}

// NewSecretService creates a new secret service
func NewSecretService(
    secretRepo secret.Repository,
    encryptionService security.EncryptionService,
    cacheService CacheService,
    logger *logger.Logger,
) *SecretService {
    return &SecretService{
        secretRepo:       secretRepo,
        encryptionService: encryptionService,
        cacheService:     cacheService,
        logger:           logger,
    }
}

// CreateAPIKey creates a new FlexPrice API key
func (s *SecretService) CreateAPIKey(ctx context.Context, tenantID, name, keyType string, permissions []string) (*secret.Secret, string, error) {
    // Validate input
    if name == "" {
        return nil, "", errors.New(errors.ErrCodeValidation, "name is required")
    }
    
    if keyType != secret.SecretTypePrivateKey && keyType != secret.SecretTypePublishableKey {
        return nil, "", errors.New(errors.ErrCodeValidation, "invalid key type")
    }
    
    // Check if publishable key already exists for this tenant
    if keyType == secret.SecretTypePublishableKey {
        existingKeys, err := s.secretRepo.GetByTypeAndProvider(ctx, tenantID, secret.SecretTypePublishableKey, secret.SecretProviderFlexPrice)
        if err != nil {
            return nil, "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to check existing keys")
        }
        
        if len(existingKeys) > 0 {
            return nil, "", errors.New(errors.ErrCodeInvalidOperation, "tenant already has a publishable key")
        }
    }
    
    // Generate a new API key
    rawKey, err := s.generateAPIKey()
    if err != nil {
        return nil, "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to generate API key")
    }
    
    // Hash the key for storage
    hashedKey := s.encryptionService.Hash(rawKey)
    prefix := rawKey[:8] // Store first 8 chars as prefix
    
    // Create the secret entity
    newSecret := &secret.Secret{
        ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SECRET),
        Name:        name,
        Type:        keyType,
        Provider:    secret.SecretProviderFlexPrice,
        Value:       hashedKey,
        Prefix:      prefix,
        Permissions: permissions,
        Metadata:    map[string]string{},
        BaseModel: types.BaseModel{
            TenantID:  tenantID,
            Status:    types.StatusPublished,
            CreatedBy: ctx.Value(types.CtxUserID).(string),
            UpdatedBy: ctx.Value(types.CtxUserID).(string),
            CreatedAt: time.Now(),
            UpdatedAt: time.Now(),
        },
    }
    
    // Save to repository
    if err := s.secretRepo.Create(ctx, newSecret); err != nil {
        return nil, "", errors.Wrap(err, errors.ErrCodeSystemError, "failed to create API key")
    }
    
    // Return the created secret and the raw key (which won't be stored)
    return newSecret, rawKey, nil
}

// CreateIntegration creates or updates integration credentials
func (s *SecretService) CreateIntegration(ctx context.Context, tenantID, name, provider string, credentials map[string]string) (*secret.Secret, error) {
    // Validate input
    if name == "" || provider == "" {
        return nil, errors.New(errors.ErrCodeValidation, "name and provider are required")
    }
    
    // Check if integration already exists
    existingIntegration, err := s.secretRepo.GetActiveIntegration(ctx, tenantID, provider)
    if err != nil && !errors.IsNotFound(err) {
        return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to check existing integration")
    }
    
    // Encrypt each credential value
    encryptedCreds := make(map[string]string)
    for key, value := range credentials {
        encrypted, err := s.encryptionService.Encrypt(value)
        if err != nil {
            return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to encrypt credentials")
        }
        encryptedCreds[key] = encrypted
    }
    
    // Convert to JSON string for storage
    credentialsJSON, err := json.Marshal(encryptedCreds)
    if err != nil {
        return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to marshal credentials")
    }
    
    // Create or update the secret
    var newSecret *secret.Secret
    
    if existingIntegration != nil {
        // Update existing integration
        existingIntegration.Name = name
        existingIntegration.Value = string(credentialsJSON)
        existingIntegration.UpdatedBy = ctx.Value(types.CtxUserID).(string)
        existingIntegration.UpdatedAt = time.Now()
        
        if err := s.secretRepo.Update(ctx, existingIntegration); err != nil {
            return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to update integration")
        }
        
        newSecret = existingIntegration
    } else {
        // Create new integration
        newSecret = &secret.Secret{
            ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SECRET),
            Name:        name,
            Type:        secret.SecretTypeIntegration,
            Provider:    provider,
            Value:       string(credentialsJSON),
            Prefix:      "", // No prefix for integrations
            Permissions: []string{}, // No permissions for integrations
            Metadata:    map[string]string{},
            BaseModel: types.BaseModel{
                TenantID:  tenantID,
                Status:    types.StatusPublished,
                CreatedBy: ctx.Value(types.CtxUserID).(string),
                UpdatedBy: ctx.Value(types.CtxUserID).(string),
                CreatedAt: time.Now(),
                UpdatedAt: time.Now(),
            },
        }
        
        if err := s.secretRepo.Create(ctx, newSecret); err != nil {
            return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to create integration")
        }
    }
    
    return newSecret, nil
}

// GetIntegrationCredentials retrieves and decrypts integration credentials
func (s *SecretService) GetIntegrationCredentials(ctx context.Context, tenantID, provider string) (map[string]string, error) {
    // Get the integration secret
    integration, err := s.secretRepo.GetActiveIntegration(ctx, tenantID, provider)
    if err != nil {
        if errors.IsNotFound(err) {
            return nil, errors.New(errors.ErrCodeNotFound, fmt.Sprintf("%s integration not found", provider))
        }
        return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to get integration")
    }
    
    // Parse the encrypted credentials
    var encryptedCreds map[string]string
    if err := json.Unmarshal([]byte(integration.Value), &encryptedCreds); err != nil {
        return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to parse credentials")
    }
    
    // Decrypt each credential
    decryptedCreds := make(map[string]string)
    for key, encryptedValue := range encryptedCreds {
        decrypted, err := s.encryptionService.Decrypt(encryptedValue)
        if err != nil {
            return nil, errors.Wrap(err, errors.ErrCodeSystemError, "failed to decrypt credentials")
        }
        decryptedCreds[key] = decrypted
    }
    
    return decryptedCreds, nil
}

// ValidateAPIKey validates an API key and returns tenant ID if valid
func (s *SecretService) ValidateAPIKey(ctx context.Context, apiKey string) (string, []string, bool) {
    if apiKey == "" {
        return "", nil, false
    }
    
    // Hash the API key
    hashedKey := s.encryptionService.Hash(apiKey)
    
    // Try to get from cache first
    cacheKey := fmt.Sprintf("api_key:%s", hashedKey)
    cachedData, found := s.cacheService.Get(cacheKey)
    if found {
        data := cachedData.(map[string]interface{})
        return data["tenant_id"].(string), data["permissions"].([]string), true
    }
    
    // If not in cache, query database
    secretEntity, err := s.secretRepo.GetByValue(ctx, hashedKey)
    if err != nil || secretEntity == nil || secretEntity.Status != types.StatusPublished {
        return "", nil, false
    }
    
    // Update last used timestamp asynchronously
    go func() {
        secretEntity.LastUsedAt = lo.ToPtr(time.Now())
        s.secretRepo.Update(context.Background(), secretEntity)
    }()
    
    // Cache the result
    s.cacheService.Set(cacheKey, map[string]interface{}{
        "tenant_id":   secretEntity.TenantID,
        "permissions": secretEntity.Permissions,
    }, 10*time.Minute)
    
    return secretEntity.TenantID, secretEntity.Permissions, true
}

// generateAPIKey generates a new random API key
func (s *SecretService) generateAPIKey() (string, error) {
    // Generate 32 random bytes
    bytes := make([]byte, 32)
    if _, err := rand.Read(bytes); err != nil {
        return "", err
    }
    
    // Convert to hex string
    return hex.EncodeToString(bytes), nil
}

// Additional methods for listing, disabling, etc.
```

### Authentication Middleware Updates

```go
// in internal/rest/middleware/auth.go
func APIKeyAuthMiddleware(secretService *service.SecretService, logger *logger.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        apiKey := c.GetHeader("x-api-key")
        tenantID, permissions, valid := secretService.ValidateAPIKey(c.Request.Context(), apiKey)
        
        if !valid {
            logger.Debugw("invalid api key")
            c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
            c.Abort()
            return
        }
        
        // Set context values including permissions
        ctx := c.Request.Context()
        ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
        ctx = context.WithValue(ctx, types.CtxPermissions, permissions)
        
        // Set additional headers for downstream handlers
        environmentID := c.GetHeader(types.HeaderEnvironment)
        if environmentID != "" {
            ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
        }
        
        c.Request = c.Request.WithContext(ctx)
        c.Next()
    }
}
```

## API Endpoints

### Secret Management

1. **List API Keys**
   - `GET /api/v1/secrets/api-keys`
   - Returns a list of API keys with their type, prefix, creation date, and status
   - Does not return the actual keys

2. **Create API Key**
   - `POST /api/v1/secrets/api-keys`
   - Generates a new API key
   - Parameters: `name`, `type` (private/publishable), `permissions`
   - Returns the full API key only once upon creation

3. **Disable/Enable API Key**
   - `PATCH /api/v1/secrets/api-keys/{key_id}`
   - Parameters: `status` (published/archived)

4. **Delete API Key**
   - `DELETE /api/v1/secrets/api-keys/{key_id}`
   - Permanently removes the API key

### Integration Management

1. **List Integrations**
   - `GET /api/v1/secrets/integrations`
   - Returns a list of configured integrations with their type and status

2. **Get Integration Status**
   - `GET /api/v1/secrets/integrations/{provider}`
   - Returns status of a specific integration (configured/not configured)

3. **Create/Update Integration**
   - `POST /api/v1/secrets/integrations/{provider}`
   - Parameters: `name` and provider-specific credentials
   - Validates credentials with the third-party service before saving

4. **Delete Integration**
   - `DELETE /api/v1/secrets/integrations/{provider}`
   - Removes the integration configuration

## Caching and Performance

To ensure efficient API key validation without database hits on every request:

1. Implement a caching layer using Redis or an in-memory cache
2. Cache structure:
   ```
   {
     "api_key:{hash}": {
       "tenant_id": "...",
       "permissions": ["read", "write"]
     }
   }
   ```
3. Cache invalidation strategies:
   - Invalidate on API key creation, update, or deletion
   - Periodic refresh (every 10 minutes)
   - TTL-based expiration as a fallback

## Frontend Requirements

### API Key Management UI

1. **API Keys Page**
   - Display list of existing API keys with:
     - Name
     - Type (private/publishable)
     - Creation date
     - Key prefix (first few characters)
     - Status (enabled/disabled)
   - Actions: create, disable/enable, delete

2. **Create API Key Modal**
   - Fields:
     - Name
     - Type (private/publishable)
     - Permissions
   - Display the full API key only once upon creation with copy button
   - Warning message that the key won't be shown again

### Integration Management UI

1. **Integrations Page**
   - List of supported integrations
   - Status for each (configured/not configured)
   - Configuration date if applicable

2. **Integration Setup Modal**
   - Integration-specific form fields for credentials
   - Test connection button
   - Save button

## Implementation Phases

### Phase 1: Core Infrastructure (Week 1)
1. Create Secret schema and generate Ent code
2. Implement encryption/decryption service using Google Tink
3. Implement Secret repository and domain model
4. Create caching service for API key validation

### Phase 2: Service Layer (Week 2)
1. Implement SecretService with API key and integration management
2. Update authentication middleware to use the new service
3. Migrate existing API keys from configuration
4. Add logging and audit trail

### Phase 3: API Endpoints (Week 3)
1. Implement API endpoints for API key management
2. Implement API endpoints for integration management
3. Add validation and testing functions for each integration type
4. Create integration tests

### Phase 4: Frontend Implementation (Week 4)
1. Design and implement API key management UI
2. Design and implement integration management UI
3. Add appropriate error handling and user feedback
4. Perform end-to-end testing

## Future Enhancements

1. **Role-Based Access Control**
   - Implement more granular permissions for API keys using Casbin
   - Define role templates for common access patterns

2. **Key Rotation**
   - Automated key rotation policies
   - Grace periods for transitioning to new keys

3. **External Key Management**
   - Integration with AWS KMS or HashiCorp Vault for master key storage
   - Hardware security module (HSM) support

4. **Usage Analytics**
   - Track and display API key usage statistics
   - Anomaly detection for potential security issues

## Additional Considerations

### Audit Logging
- Log all secret operations (creation, deletion, etc.)
- Track which user performed each action
- Store timestamps for all operations

### Rate Limiting
- Implement rate limiting per API key
- Allow configuration of rate limits based on key type

### Testing Strategy
- Unit tests for encryption/decryption service
- Integration tests for the caching mechanism
- End-to-end tests for the API endpoints
- Security testing for credential storage 