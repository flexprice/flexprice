//go:build e2eprobe_integration

package integration

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestE2EProbe_LocalAuth(t *testing.T) {
	host := os.Getenv("E2EPROBE_API_HOST")
	email, password := os.Getenv("E2EPROBE_LOCAL_EMAIL"), os.Getenv("E2EPROBE_LOCAL_PASSWORD")
	signupUserID := os.Getenv("E2EPROBE_LOCAL_SIGNUP_USER_ID")
	key := os.Getenv("E2EPROBE_API_KEY")
	if host == "" || key == "" || email == "" || password == "" || signupUserID == "" {
		t.Skip("local auth requires an API host and key plus the credentials and user ID from signup; use make local-verify LOCAL_SUITE=auth")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	api := newProbeHTTP(host)

	var login struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
	}
	credentials := map[string]string{"email": email, "password": password}
	api.require(t, ctx, http.MethodPost, "/auth/login", credentials, http.StatusOK, &login)
	if login.Token == "" || login.UserID != signupUserID {
		t.Fatal("login must return a token for the user created by signup")
	}
	var user struct {
		ID string `json:"id"`
	}
	bearer := api.withAuth("Authorization", "Bearer "+login.Token)
	bearer.require(t, ctx, http.MethodGet, "/users/me", nil, http.StatusOK, &user)
	if user.ID != signupUserID {
		t.Fatal("authenticated user lookup returned a different user from signup")
	}
	credentials["password"] = password + "-incorrect"
	api.require(t, ctx, http.MethodPost, "/auth/login", credentials, http.StatusForbidden, nil)
	api.require(t, ctx, http.MethodGet, "/users/me", nil, http.StatusUnauthorized, nil)

	keyed := api.withAuth("X-API-Key", key)
	user.ID = ""
	keyed.require(t, ctx, http.MethodGet, "/users/me", nil, http.StatusOK, &user)
	if user.ID != signupUserID {
		t.Fatal("API-key user lookup returned a different user from signup")
	}
}
