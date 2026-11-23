//go:build e2eprobe_integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

func TestE2EProbe_LocalAuth(t *testing.T) {
	host := strings.TrimRight(os.Getenv("E2EPROBE_API_HOST"), "/")
	email, password := os.Getenv("E2EPROBE_LOCAL_EMAIL"), os.Getenv("E2EPROBE_LOCAL_PASSWORD")
	signupUserID := os.Getenv("E2EPROBE_LOCAL_SIGNUP_USER_ID")
	key := os.Getenv("E2EPROBE_API_KEY")
	if host == "" || key == "" || email == "" || password == "" || signupUserID == "" {
		t.Skip("local auth requires an API host and key plus the credentials and user ID from signup; use make local-verify LOCAL_SUITE=auth")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request := func(method, path string, payload []byte, token string, want int, out any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, host+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: got HTTP %d, want %d (request ID %s)",
				method, path, resp.StatusCode, want, resp.Header.Get("X-Request-ID"))
		}
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("%s %s: decode response: %v", method, path, err)
			}
		}
	}
	loginPayload := func(password string) []byte {
		t.Helper()
		body, err := json.Marshal(map[string]string{"email": email, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}

	var login struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
	}
	request(http.MethodPost, "/auth/login", loginPayload(password), "", http.StatusOK, &login)
	if login.Token == "" || login.UserID != signupUserID {
		t.Fatal("login must return a token for the user created by signup")
	}
	var user struct {
		ID string `json:"id"`
	}
	request(http.MethodGet, "/users/me", nil, login.Token, http.StatusOK, &user)
	if user.ID != signupUserID {
		t.Fatal("authenticated user lookup returned a different user from signup")
	}
	request(http.MethodPost, "/auth/login", loginPayload(password+"-incorrect"), "", http.StatusForbidden, nil)
	request(http.MethodGet, "/users/me", nil, "", http.StatusUnauthorized, nil)
	if err := e2eprobe.NewSDKClient(host, key).Raw().Do(ctx, http.MethodGet, "/users/me", nil, nil); err != nil {
		t.Fatal("user lookup with the provisioned API key failed")
	}
}
