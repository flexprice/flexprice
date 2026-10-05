package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type actorCapturingAuthService struct {
	ctx context.Context
}

func (s *actorCapturingAuthService) SignUp(ctx context.Context, _ *dto.SignUpRequest) (*dto.AuthResponse, error) {
	s.ctx = ctx
	return &dto.AuthResponse{}, nil
}

func (s *actorCapturingAuthService) Login(ctx context.Context, _ *dto.LoginRequest) (*dto.AuthResponse, error) {
	s.ctx = ctx
	return &dto.AuthResponse{}, nil
}

func TestAuthHandlerSetsSystemActor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name      string
		route     func(h *AuthHandler) gin.HandlerFunc
		wantActor types.Actor
	}{
		{
			name:      "signup",
			route:     func(h *AuthHandler) gin.HandlerFunc { return h.SignUp },
			wantActor: types.SystemActor("signup", "Signup"),
		},
		{
			name:      "login",
			route:     func(h *AuthHandler) gin.HandlerFunc { return h.Login },
			wantActor: types.SystemActor("login", "Login"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &actorCapturingAuthService{}
			log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
			require.NoError(t, err)
			h := NewAuthHandler(nil, svc, log)
			r := gin.New()
			r.POST("/auth", tc.route(h))

			w := httptest.NewRecorder()
			body := `{"email":"dev@example.com","password":"password12345"}`
			req, _ := http.NewRequest(http.MethodPost, "/auth", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NotNil(t, svc.ctx)
			assert.Equal(t, types.ActorTypeSystem, types.GetActor(svc.ctx).Type)
			assert.Equal(t, tc.wantActor, types.GetActor(svc.ctx))
			assert.Equal(t, types.SourceDashboard, types.GetSource(svc.ctx))
		})
	}
}
