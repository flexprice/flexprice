package dto

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestSetPasswordRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     SetPasswordRequest
		wantErr bool
	}{
		{name: "token and a long enough password", req: SetPasswordRequest{Token: "link-token", Password: "long-enough-1"}},
		{name: "no token", req: SetPasswordRequest{Password: "long-enough-1"}, wantErr: true},
		{name: "password shorter than 8 characters", req: SetPasswordRequest{Token: "link-token", Password: "short12"}, wantErr: true},
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
