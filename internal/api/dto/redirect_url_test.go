package dto

import (
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestRedirectionParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https", "https://app.example.com/done?x=1", false},
		{"http localhost", "http://localhost:3000/done", false},
		{"http loopback", "http://127.0.0.1/done", false},
		{"http public host", "http://example.com/done", true},
		{"javascript", "javascript:alert(1)", true},
		{"data", "data:text/html,hi", true},
		{"protocol relative", "//evil.com/x", true},
		{"relative path", "/done", true},
		{"no host", "https:///path", true},
		{"userinfo", "https://user:pw@example.com/", true},
		{"mixed case scheme prefix bypass", "https:javascript:alert(1)", true},
		{"too long", "https://example.com/" + strings.Repeat("a", 2100), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, p := range []RedirectionParams{
				{SuccessURL: lo.ToPtr(tc.url)},
				{FailureURL: lo.ToPtr(tc.url)},
				{CancelURL: lo.ToPtr(tc.url)},
			} {
				assert.Equal(t, tc.wantErr, p.Validate() != nil)
			}
			assert.Equal(t, !tc.wantErr, isValidURL(tc.url))
		})
	}

	assert.NoError(t, RedirectionParams{}.Validate())
	assert.NoError(t, RedirectionParams{SuccessURL: lo.ToPtr("")}.Validate())
}
