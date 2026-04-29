package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/sethvargo/go-password/password"
	"golang.org/x/crypto/bcrypt"
)

type flexpriceAuth struct {
	AuthConfig config.AuthConfig
}

func NewFlexpriceAuth(cfg *config.Configuration) *flexpriceAuth {
	return &flexpriceAuth{
		AuthConfig: cfg.Auth,
	}
}

func (f *flexpriceAuth) GetProvider() types.AuthProvider {
	return types.AuthProviderFlexprice
}

func (f *flexpriceAuth) SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	if req.Password == "" {
		return nil, ierr.NewError("password is required").
			WithHint("Password is required").
			Mark(ierr.ErrValidation)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to hash password").
			Mark(ierr.ErrSystem)
	}

	userID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER)

	authToken, err := f.generateToken(userID, req.TenantID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to generate token").
			Mark(ierr.ErrSystem)
	}

	response := &AuthResponse{
		ProviderToken: string(hashedPassword),
		AuthToken:     authToken,
		ID:            userID,
	}

	return response, nil
}

func (f *flexpriceAuth) Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error) {
	// Validate the user provided hashed password with the saved hashed password
	err := bcrypt.CompareHashAndPassword([]byte(userAuthInfo.Token), []byte(req.Password))
	if err != nil {
		return nil, ierr.NewError("invalid password").
			WithHint("Invalid password").
			Mark(ierr.ErrValidation)
	}

	// Validated then generate a JWT token
	authToken, err := f.generateToken(userAuthInfo.UserID, req.TenantID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to generate token").
			Mark(ierr.ErrValidation)
	}

	response := &AuthResponse{
		ProviderToken: userAuthInfo.Token,
		AuthToken:     authToken,
		ID:            userAuthInfo.UserID,
	}

	return response, nil
}

func (f *flexpriceAuth) ValidateToken(ctx context.Context, token string) (*auth.Claims, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ierr.NewError("unexpected signing method").
				WithHint(fmt.Sprintf("unexpected signing method: %v", token.Header["alg"])).
				Mark(ierr.ErrPermissionDenied)
		}
		secret := f.AuthConfig.Secret
		return []byte(secret), nil
	})

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Token parse error").
			Mark(ierr.ErrPermissionDenied)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, ierr.NewError("invalid token claims").
			WithHint("Invalid token claims").
			Mark(ierr.ErrPermissionDenied)
	}

	userID, userOk := claims["user_id"].(string)
	if !userOk {
		return nil, ierr.NewError("token missing user ID").
			WithHint("Token missing user ID").
			Mark(ierr.ErrPermissionDenied)
	}

	tenantID, tenantOk := claims["tenant_id"].(string)
	if !tenantOk {
		tenantID = types.DefaultTenantID
	}

	return &auth.Claims{UserID: userID, TenantID: tenantID}, nil
}

func (f *flexpriceAuth) generateToken(userID, tenantID string) (string, error) {
	// generate a JWT token with the user ID and tenant ID with 30 days expiration
	expiration := time.Now().Add(30 * 24 * time.Hour)

	claims := jwt.MapClaims{
		"user_id":   userID,
		"tenant_id": tenantID,
		"exp":       expiration.Unix(),
		"iat":       time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(f.AuthConfig.Secret))
}

func (f *flexpriceAuth) AssignUserToTenant(ctx context.Context, userID string, tenantID string) error {
	// No action required for Flexprice as we do not support
	// reassigning users to a different tenant for now
	// and in case of flexprice auth it is mandatory to have a tenant ID
	// when creating a new user hence this case needs no implementation
	return nil
}

// GenerateSessionToken generates a JWT token for session access
// This token is specifically for customers (not users) and has a shorter expiration
func (f *flexpriceAuth) GenerateSessionToken(customerID, externalCustomerID, tenantID, environmentID string, timeoutHours int) (string, time.Time, error) {
	// Validate required parameters
	customerID = strings.TrimSpace(customerID)
	externalCustomerID = strings.TrimSpace(externalCustomerID)
	tenantID = strings.TrimSpace(tenantID)
	environmentID = strings.TrimSpace(environmentID)

	if customerID == "" {
		return "", time.Time{}, ierr.NewError("missing required parameter: customerID").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation)
	}
	if externalCustomerID == "" {
		return "", time.Time{}, ierr.NewError("missing required parameter: externalCustomerID").
			WithHint("External Customer ID is required").
			Mark(ierr.ErrValidation)
	}
	if tenantID == "" {
		return "", time.Time{}, ierr.NewError("missing required parameter: tenantID").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if environmentID == "" {
		return "", time.Time{}, ierr.NewError("missing required parameter: environmentID").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}

	// Dashboard tokens expire based on the provided timeout
	expiresAt := time.Now().Add(time.Duration(timeoutHours) * time.Hour)

	claims := jwt.MapClaims{
		"customer_id":          customerID,
		"external_customer_id": externalCustomerID,
		"tenant_id":            tenantID,
		"environment_id":       environmentID,
		"exp":                  expiresAt.Unix(),
		"iat":                  time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(f.AuthConfig.Secret))
	if err != nil {
		return "", time.Time{}, ierr.WithError(err).
			WithHint("Failed to sign session token").
			Mark(ierr.ErrSystem)
	}

	return signedToken, expiresAt, nil
}

// ValidateSessionToken validates a session token and returns the claims
func (f *flexpriceAuth) ValidateSessionToken(ctx context.Context, token string) (*auth.SessionClaims, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ierr.NewError("unexpected signing method").
				WithHint(fmt.Sprintf("unexpected signing method: %v", token.Header["alg"])).
				Mark(ierr.ErrPermissionDenied)
		}
		return []byte(f.AuthConfig.Secret), nil
	})

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid session token").
			Mark(ierr.ErrPermissionDenied)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, ierr.NewError("invalid token claims").
			WithHint("Invalid token claims").
			Mark(ierr.ErrPermissionDenied)
	}

	customerID, ok := claims["customer_id"].(string)
	if !ok {
		return nil, ierr.NewError("invalid customer_id claim").
			WithHint("customer_id claim is missing or has wrong type").
			Mark(ierr.ErrPermissionDenied)
	}

	externalCustomerID, ok := claims["external_customer_id"].(string)
	if !ok {
		return nil, ierr.NewError("invalid external_customer_id claim").
			WithHint("external_customer_id claim is missing or has wrong type").
			Mark(ierr.ErrPermissionDenied)
	}

	tenantID, ok := claims["tenant_id"].(string)
	if !ok {
		return nil, ierr.NewError("invalid tenant_id claim").
			WithHint("tenant_id claim is missing or has wrong type").
			Mark(ierr.ErrPermissionDenied)
	}

	environmentID, ok := claims["environment_id"].(string)
	if !ok {
		return nil, ierr.NewError("invalid environment_id claim").
			WithHint("environment_id claim is missing or has wrong type").
			Mark(ierr.ErrPermissionDenied)
	}

	if customerID == "" || externalCustomerID == "" || tenantID == "" || environmentID == "" {
		return nil, ierr.NewError("missing required claims").
			WithHint("Session token is missing required claims").
			Mark(ierr.ErrPermissionDenied)
	}

	return &auth.SessionClaims{
		CustomerID:         customerID,
		ExternalCustomerID: externalCustomerID,
		TenantID:           tenantID,
		EnvironmentID:      environmentID,
	}, nil
}

// UserInvite provisions a user in the configured auth provider and returns the newly created user ID and password.
func (f *flexpriceAuth) UserInvite(ctx context.Context, req UserInviteRequest) (*UserInviteResponse, error) {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant id is required").
			WithHint("Provide tenant id in request context when creating a user").
			Mark(ierr.ErrValidation)
	}

	// Generate an initial password (no auth token issuance here).
	password, err := password.Generate(16, 4, 2, false, false)
	if err != nil {
		return nil, err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Flexprice auth provisioning here is about allocating a user ID and returning
	// the hashed password as an auth record so the caller can persist it (no auth token issuance).
	userID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER)
	return &UserInviteResponse{
		ID:         userID,
		Password:   password,
		AuthRecord: auth.NewAuth(userID, types.AuthProviderFlexprice, string(hashedPassword)),
	}, nil
}
