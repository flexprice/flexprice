package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/nedpals/supabase-go"
)

type SigningAlgorithm string

const (
	SigningAlgorithmHS256 SigningAlgorithm = "HS256" // Legacy: uses shared secret
	SigningAlgorithmES256 SigningAlgorithm = "ES256" // New: uses JWKS public keys
)

type supabaseAuth struct {
	AuthConfig config.AuthConfig
	client     *supabase.Client
	logger     *logger.Logger
	httpClient *http.Client
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

func NewSupabaseAuth(cfg *config.Configuration) Provider {
	supabaseUrl := cfg.Auth.Supabase.BaseURL
	adminApiKey := cfg.Auth.Supabase.ServiceKey

	client := supabase.CreateClient(supabaseUrl, adminApiKey)
	if client == nil {
		log.Fatalf("failed to create Supabase client")
	}

	logger, _ := logger.NewLogger(cfg)

	return &supabaseAuth{
		AuthConfig: cfg.Auth,
		client:     client,
		logger:     logger,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (s *supabaseAuth) GetProvider() types.AuthProvider {
	return types.AuthProviderSupabase
}

// SignUp is not used directly for Supabase as users sign up through the Supabase UI
// This method is kept for compatibility with the Provider interface
func (s *supabaseAuth) SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	// For Supabase, we don't directly sign up users through this method
	// Instead, we validate the token and get user info
	// For Supabase, we validate the token and extract user info
	if req.Token == "" {
		return nil, ierr.NewError("token is required").
			Mark(ierr.ErrPermissionDenied)
	}

	// Validate the token and extract user ID
	claims, err := s.ValidateToken(ctx, req.Token)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to validate Supabase token").
			Mark(ierr.ErrPermissionDenied)
	}

	if claims.Email != req.Email {
		return nil, ierr.NewError("email mismatch").
			Mark(ierr.ErrPermissionDenied)
	}

	// Create auth response with the token
	authResponse := &AuthResponse{
		ProviderToken: claims.UserID,
		AuthToken:     req.Token,
		ID:            claims.UserID,
	}

	return authResponse, nil
}

// Login validates the token and returns user info
func (s *supabaseAuth) Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error) {
	user, err := s.client.Auth.SignIn(ctx, supabase.UserCredentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get user").
			Mark(ierr.ErrPermissionDenied)
	}
	return &AuthResponse{
		ProviderToken: user.User.ID,
		AuthToken:     user.AccessToken,
		ID:            user.User.ID,
	}, nil
}

func (s *supabaseAuth) ValidateToken(ctx context.Context, token string) (*auth.Claims, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		alg := SigningAlgorithm(token.Method.Alg())

		switch alg {
		case SigningAlgorithmHS256:
			return []byte(s.AuthConfig.Secret), nil

		case SigningAlgorithmES256:
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, ierr.NewError("token missing key ID").
					WithHint("ES256 token must include 'kid' in header").
					Mark(ierr.ErrPermissionDenied)
			}
			return s.getPublicKeyFromJWKS(ctx, kid)

		default:
			return nil, ierr.NewError("unsupported signing method").
				WithHint("Supported: HS256 (legacy), ES256 (new)").
				WithReportableDetails(map[string]interface{}{
					"signing_method": alg,
				}).
				Mark(ierr.ErrPermissionDenied)
		}
	})

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse JWT token").
			Mark(ierr.ErrPermissionDenied)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, ierr.NewError("invalid token claims").
			Mark(ierr.ErrPermissionDenied)
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		return nil, ierr.NewError("token missing user ID").
			Mark(ierr.ErrPermissionDenied)
	}

	email, ok := claims["email"].(string)
	if !ok || email == "" {
		return nil, ierr.NewError("token missing email").
			Mark(ierr.ErrPermissionDenied)
	}

	var tenantID string
	if appMetadata, ok := claims["app_metadata"].(map[string]interface{}); ok {
		if tid, ok := appMetadata["tenant_id"].(string); ok {
			tenantID = tid
		}
	}

	return &auth.Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
	}, nil
}

func (s *supabaseAuth) getPublicKeyFromJWKS(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	jwksURL := s.AuthConfig.Supabase.BaseURL + "/auth/v1/.well-known/jwks.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrSystem)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrSystem)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ierr.NewError("failed to fetch JWKS").
			WithReportableDetails(map[string]interface{}{"status_code": resp.StatusCode}).
			Mark(ierr.ErrSystem)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrSystem)
	}

	for _, key := range jwks.Keys {
		if key.Kid != kid || key.Kty != "EC" || key.Crv != "P-256" || key.Alg != "ES256" {
			continue
		}

		xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
		if err != nil {
			continue
		}

		yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
		if err != nil {
			continue
		}

		return &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}, nil
	}

	return nil, ierr.NewError("key not found in JWKS").
		WithReportableDetails(map[string]interface{}{"kid": kid}).
		Mark(ierr.ErrPermissionDenied)
}

func (s *supabaseAuth) AssignUserToTenant(ctx context.Context, userID string, tenantID string) error {
	params := supabase.AdminUserParams{
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	}

	_, err := s.client.Admin.UpdateUser(ctx, userID, params)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to assign tenant to user").
			Mark(ierr.ErrSystem)
	}

	return nil
}

func (s *supabaseAuth) GenerateSessionToken(customerID, externalCustomerID, tenantID, environmentID string, timeoutHours int) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (s *supabaseAuth) ValidateSessionToken(ctx context.Context, token string) (*auth.SessionClaims, error) {
	return nil, nil
}
