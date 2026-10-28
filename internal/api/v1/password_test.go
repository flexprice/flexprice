package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// stubPasswordAuthService answers the public password calls and records what the handler passed on.
type stubPasswordAuthService struct {
	service.AuthService
	details  *dto.PasswordLinkDetailsResponse
	err      error
	called   bool
	token    string
	password string
}

func (s *stubPasswordAuthService) CheckPasswordLink(_ context.Context, req *dto.CheckPasswordLinkRequest) (*dto.PasswordLinkDetailsResponse, error) {
	s.called, s.token = true, req.Token
	return s.details, s.err
}

func (s *stubPasswordAuthService) SetPassword(_ context.Context, req *dto.SetPasswordRequest) error {
	s.called, s.token, s.password = true, req.Token, req.Password
	return s.err
}

// stubPasswordUserService answers the signed-in password calls and records what the handler passed on.
type stubPasswordUserService struct {
	service.UserService
	link     *dto.PasswordLinkResponse
	err      error
	called   bool
	userID   string
	password string
}

func (s *stubPasswordUserService) ChangePassword(_ context.Context, req *dto.ChangePasswordRequest) error {
	s.called, s.password = true, req.Password
	return s.err
}

func (s *stubPasswordUserService) CreatePasswordLink(_ context.Context, id string) (*dto.PasswordLinkResponse, error) {
	s.called, s.userID = true, id
	return s.link, s.err
}

func setupPasswordRouter(t *testing.T, authSvc service.AuthService, userSvc service.UserService) *gin.Engine {
	t.Helper()

	cfg := &config.Configuration{}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	authHandler := NewAuthHandler(cfg, authSvc, log)
	userHandler := NewUserHandler(userSvc, log)

	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/v1/auth/password/check", authHandler.CheckPasswordLink)
	router.POST("/v1/auth/password", authHandler.SetPassword)
	router.PUT("/v1/users/me/password", userHandler.ChangePassword)
	router.POST("/v1/users/:id/password-link", userHandler.CreatePasswordLink)
	return router
}

func doPasswordRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCheckPasswordLinkHandler(t *testing.T) {
	t.Run("returns what the link is for", func(t *testing.T) {
		authSvc := &stubPasswordAuthService{details: &dto.PasswordLinkDetailsResponse{Purpose: "invite", Email: "member@example.com"}}

		rec := doPasswordRequest(setupPasswordRouter(t, authSvc, nil), http.MethodPost, "/v1/auth/password/check", `{"token":"link-token"}`)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "link-token", authSvc.token)
		var got dto.PasswordLinkDetailsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, *authSvc.details, got)
	})

	t.Run("refuses a body without a token", func(t *testing.T) {
		authSvc := &stubPasswordAuthService{}

		rec := doPasswordRequest(setupPasswordRouter(t, authSvc, nil), http.MethodPost, "/v1/auth/password/check", `{}`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.False(t, authSvc.called)
	})

	t.Run("answers 404 for a link that cannot be used", func(t *testing.T) {
		authSvc := &stubPasswordAuthService{err: ierr.NewError("password link is invalid").Mark(ierr.ErrNotFound)}

		rec := doPasswordRequest(setupPasswordRouter(t, authSvc, nil), http.MethodPost, "/v1/auth/password/check", `{"token":"used-token"}`)

		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestSetPasswordHandler(t *testing.T) {
	t.Run("passes the token and password on and answers 204", func(t *testing.T) {
		authSvc := &stubPasswordAuthService{}

		rec := doPasswordRequest(setupPasswordRouter(t, authSvc, nil), http.MethodPost, "/v1/auth/password", `{"token":"link-token","password":"long-enough-1"}`)

		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, "link-token", authSvc.token)
		require.Equal(t, "long-enough-1", authSvc.password)
	})

	t.Run("refuses a password shorter than 8 characters", func(t *testing.T) {
		authSvc := &stubPasswordAuthService{}

		rec := doPasswordRequest(setupPasswordRouter(t, authSvc, nil), http.MethodPost, "/v1/auth/password", `{"token":"link-token","password":"short12"}`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.False(t, authSvc.called)
	})

	t.Run("answers 404 for a link that cannot be used", func(t *testing.T) {
		authSvc := &stubPasswordAuthService{err: ierr.NewError("password link is invalid").Mark(ierr.ErrNotFound)}

		rec := doPasswordRequest(setupPasswordRouter(t, authSvc, nil), http.MethodPost, "/v1/auth/password", `{"token":"used-token","password":"long-enough-1"}`)

		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestChangePasswordHandler(t *testing.T) {
	t.Run("passes the new password on and answers 204", func(t *testing.T) {
		userSvc := &stubPasswordUserService{}

		rec := doPasswordRequest(setupPasswordRouter(t, nil, userSvc), http.MethodPut, "/v1/users/me/password", `{"password":"new-password-2"}`)

		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, "new-password-2", userSvc.password)
	})

	t.Run("refuses a body without a password", func(t *testing.T) {
		userSvc := &stubPasswordUserService{}

		rec := doPasswordRequest(setupPasswordRouter(t, nil, userSvc), http.MethodPut, "/v1/users/me/password", `{}`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.False(t, userSvc.called)
	})

	t.Run("answers 403 when the service refuses the caller", func(t *testing.T) {
		userSvc := &stubPasswordUserService{err: ierr.NewError("password change outside a dashboard session").Mark(ierr.ErrPermissionDenied)}

		rec := doPasswordRequest(setupPasswordRouter(t, nil, userSvc), http.MethodPut, "/v1/users/me/password", `{"password":"new-password-2"}`)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestCreatePasswordLinkHandler(t *testing.T) {
	t.Run("returns the link for the member named in the path", func(t *testing.T) {
		expiresAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
		userSvc := &stubPasswordUserService{link: &dto.PasswordLinkResponse{Token: "reset-token", Purpose: "reset", ExpiresAt: expiresAt}}

		rec := doPasswordRequest(setupPasswordRouter(t, nil, userSvc), http.MethodPost, "/v1/users/user_member/password-link", "")

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "user_member", userSvc.userID)
		var got dto.PasswordLinkResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, "reset-token", got.Token)
		require.Equal(t, "reset", got.Purpose)
		require.True(t, expiresAt.Equal(got.ExpiresAt))
	})

	t.Run("answers 403 when the caller is not a super admin", func(t *testing.T) {
		userSvc := &stubPasswordUserService{err: ierr.NewError("only super_admin can create password links").Mark(ierr.ErrPermissionDenied)}

		rec := doPasswordRequest(setupPasswordRouter(t, nil, userSvc), http.MethodPost, "/v1/users/user_member/password-link", "")

		require.Equal(t, http.StatusForbidden, rec.Code)
	})
}
