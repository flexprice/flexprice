package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/nedpals/supabase-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// brokenLookupStore fails every email lookup, like a database error during the duplicate check.
type brokenLookupStore struct {
	*testutil.InMemoryUserStore
}

func (brokenLookupStore) GetByEmail(context.Context, string) (*user.User, error) {
	return nil, ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

func TestRequireUnusedEmail(t *testing.T) {
	ctx := context.Background()
	users := testutil.NewInMemoryUserStore()
	require.NoError(t, users.Create(ctx, user.NewUser("taken@acme.com", "tenant_1")))

	assert.NoError(t, requireUnusedEmail(ctx, users, "free@acme.com"))
	assert.True(t, ierr.IsAlreadyExists(requireUnusedEmail(ctx, users, "taken@acme.com")))
	assert.True(t, ierr.IsDatabase(requireUnusedEmail(ctx, brokenLookupStore{users}, "free@acme.com")),
		"a failed lookup must be reported, not treated as an unused email")
}

func TestSupabaseError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want func(error) bool
	}{
		{name: "supabase unreachable", err: errors.New("dial tcp: connection refused"), want: ierr.IsSystem},
		{name: "conflict", err: &supabase.ErrorResponse{Code: http.StatusConflict, Message: "conflict"}, want: ierr.IsAlreadyExists},
		{
			name: "email already registered",
			err:  &supabase.ErrorResponse{Code: http.StatusUnprocessableEntity, Message: "A user with this email address has already been registered"},
			want: ierr.IsAlreadyExists,
		},
		{
			name: "weak password",
			err:  &supabase.ErrorResponse{Code: http.StatusUnprocessableEntity, Message: "Password should be at least 10 characters"},
			want: ierr.IsValidation,
		},
		{
			name: "invalid email",
			err:  &supabase.ErrorResponse{Code: http.StatusBadRequest, Message: "Unable to validate email address: invalid format"},
			want: ierr.IsValidation,
		},
		{name: "supabase failure", err: &supabase.ErrorResponse{Code: http.StatusInternalServerError, Message: "boom"}, want: ierr.IsSystem},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, tc.want(supabaseError(tc.err, "owner@acme.com")))
		})
	}
}
