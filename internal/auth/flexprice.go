package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt"
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
	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	authToken, err := f.generateToken(req.UserID, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	response := &AuthResponse{
		ProviderToken: string(hashedPassword),
		AuthToken:     authToken,
	}

	return response, nil
}

func (f *flexpriceAuth) Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error) {
	// Validate the user password with the saved hashed password
	err := bcrypt.CompareHashAndPassword([]byte(userAuthInfo.Token), []byte(req.Password))
	if err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	// Validated then generate a JWT token
	authToken, err := f.generateToken(userAuthInfo.UserID, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	response := &AuthResponse{
		ProviderToken: userAuthInfo.Token,
		AuthToken:     authToken,
	}

	return response, nil
}

func (f *flexpriceAuth) ValidateToken(ctx context.Context, token string) (*auth.Claims, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		secret := f.AuthConfig.Secret
		return []byte(secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("token parse error: %w", err)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	userID, userOk := claims["user_id"].(string)
	if !userOk {
		return nil, fmt.Errorf("token missing user ID")
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
	return fmt.Errorf("not implemented")
}
