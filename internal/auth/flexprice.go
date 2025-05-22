package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
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
