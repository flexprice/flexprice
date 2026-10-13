package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/nedpals/supabase-go"
)

// requireUnusedEmail refuses an email that already belongs to a user in any tenant.
func requireUnusedEmail(ctx context.Context, users user.Repository, email string) error {
	existing, err := users.GetByEmail(ctx, email)
	if ierr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return ierr.NewError("email already in use").
		WithHint("A user with this email already exists").
		WithReportableDetails(map[string]interface{}{"email": email, "tenant_id": existing.TenantID}).
		Mark(ierr.ErrAlreadyExists)
}

// createLogin creates a confirmed Supabase user with the given password, tagged with the tenant.
func createLogin(ctx context.Context, cfg *config.Configuration, email, password, tenantID string) (string, error) {
	client := supabase.CreateClient(cfg.Auth.Supabase.BaseURL, cfg.Auth.Supabase.ServiceKey)
	created, err := client.Admin.CreateUser(ctx, supabase.AdminUserParams{
		Email:        email,
		Password:     &password,
		EmailConfirm: true,
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	})
	if err != nil {
		return "", supabaseError(err, email)
	}
	return created.ID, nil
}

// removeLogin deletes a Supabase user whose rows were not saved, so the email can be retried.
func removeLogin(ctx context.Context, cfg *config.Configuration, log *logger.Logger, userID string) {
	if err := auth.NewSupabaseAuth(cfg).RemoveUser(context.WithoutCancel(ctx), userID); err != nil {
		log.Error(ctx, "failed to remove supabase user after its rows were not saved", "error", err, "user_id", userID)
	}
}

// supabaseError maps a Supabase failure to an API error: an email it already has is a conflict, other refused input is bad input.
func supabaseError(err error, email string) error {
	var supaErr *supabase.ErrorResponse
	if !errors.As(err, &supaErr) {
		return ierr.WithError(err).
			WithHint("Failed to create the user in Supabase").
			WithReportableDetails(map[string]interface{}{"email": email}).
			Mark(ierr.ErrSystem)
	}

	details := map[string]interface{}{
		"email":          email,
		"supabase_code":  supaErr.Code,
		"supabase_error": supaErr.Message,
	}
	if supaErr.Code == http.StatusConflict ||
		strings.Contains(strings.ToLower(supaErr.Message), "already") {
		return ierr.WithError(supaErr).
			WithHint("A user with this email already exists in Supabase").
			WithReportableDetails(details).
			Mark(ierr.ErrAlreadyExists)
	}
	// Supabase also uses 422 for input it refuses, such as a password weaker than its policy allows.
	if supaErr.Code == http.StatusBadRequest || supaErr.Code == http.StatusUnprocessableEntity {
		return ierr.WithError(supaErr).
			WithHintf("Supabase rejected the user: %s", supaErr.Message).
			WithReportableDetails(details).
			Mark(ierr.ErrValidation)
	}
	return ierr.WithError(supaErr).
		WithHintf("Supabase rejected the user (code %d)", supaErr.Code).
		WithReportableDetails(details).
		Mark(ierr.ErrSystem)
}
