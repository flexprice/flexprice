package v1

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubOAuthService struct {
	service.OAuthService
	buildErr error
	stored   int
}

func (s *stubOAuthService) GenerateSessionID() (string, error) { return "sess", nil }
func (s *stubOAuthService) GenerateCSRFState() (string, error) { return "csrf", nil }
func (s *stubOAuthService) BuildOAuthURL(types.OAuthProvider, string, string, string, map[string]string) (string, error) {
	return "https://example.test/auth", s.buildErr
}
func (s *stubOAuthService) StoreOAuthSession(_ context.Context, _ *types.OAuthSession) error {
	s.stored++
	return nil
}

func initOAuth(t *testing.T, svc *stubOAuthService) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	h := NewOAuthHandler(svc, "https://app.test/cb", logger.NewNoopLogger())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"provider":"zoho_books","name":"z","credentials":{"client_id":"a","client_secret":"b"},"metadata":{"accounts_server":"https://accounts.zoho.com"}}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/oauth/init", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.InitiateOAuth(c)
	return w
}

func TestInitiateOAuthDoesNotStoreWhenURLBuildFails(t *testing.T) {
	svc := &stubOAuthService{buildErr: ierr.NewError("bad").Mark(ierr.ErrValidation)}
	initOAuth(t, svc)
	require.Equal(t, 0, svc.stored)
}

func TestInitiateOAuthStoresAfterURLBuilds(t *testing.T) {
	svc := &stubOAuthService{}
	w := initOAuth(t, svc)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, svc.stored)
}
