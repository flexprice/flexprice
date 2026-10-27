package dto

import (
	"strings"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestSignUpRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "long enough password", password: "long-enough-1"},
		{name: "no password, as when signing up with a token", password: ""},
		{name: "password of exactly 72 bytes", password: strings.Repeat("a", 72)},
		{name: "password over 72 bytes", password: strings.Repeat("a", 73), wantErr: true},
		{name: "19 emoji are 19 characters but 76 bytes", password: strings.Repeat("😀", 19), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := (&SignUpRequest{Email: "member@example.com", Password: tc.password}).Validate()

			if tc.wantErr {
				require.True(t, ierr.IsValidation(err), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSetPasswordRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     SetPasswordRequest
		wantErr bool
	}{
		{name: "token and a long enough password", req: SetPasswordRequest{Token: "link-token", Password: "long-enough-1"}},
		{name: "no token", req: SetPasswordRequest{Password: "long-enough-1"}, wantErr: true},
		{name: "password shorter than 8 characters", req: SetPasswordRequest{Token: "link-token", Password: "short12"}, wantErr: true},
		{name: "password of exactly 72 bytes", req: SetPasswordRequest{Token: "link-token", Password: strings.Repeat("a", 72)}},
		{name: "password over 72 bytes", req: SetPasswordRequest{Token: "link-token", Password: strings.Repeat("a", 73)}, wantErr: true},
		{name: "19 emoji are 19 characters but 76 bytes", req: SetPasswordRequest{Token: "link-token", Password: strings.Repeat("😀", 19)}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()

			if tc.wantErr {
				require.True(t, ierr.IsValidation(err), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestChangePasswordRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "long enough password", password: "long-enough-1"},
		{name: "no password", password: "", wantErr: true},
		{name: "password shorter than 8 characters", password: "short12", wantErr: true},
		{name: "password of exactly 72 bytes", password: strings.Repeat("a", 72)},
		{name: "password over 72 bytes", password: strings.Repeat("a", 73), wantErr: true},
		{name: "19 emoji are 19 characters but 76 bytes", password: strings.Repeat("😀", 19), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := (&ChangePasswordRequest{Password: tc.password}).Validate()

			if tc.wantErr {
				require.True(t, ierr.IsValidation(err), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}
