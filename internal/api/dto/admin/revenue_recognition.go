package admin

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// StartRevenueRecognitionBackfillRequest names the environment to rebuild from history.
type StartRevenueRecognitionBackfillRequest struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

func (r *StartRevenueRecognitionBackfillRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}
	return validateEnvironmentTarget(&r.TenantID, &r.EnvironmentID)
}

// StartRevenueRecognitionRunRequest starts a recognition run: every enabled environment, one tenant's, or one environment.
// Since overrides how far back changed billing is posted.
type StartRevenueRecognitionRunRequest struct {
	TenantID      string     `json:"tenant_id"`
	EnvironmentID string     `json:"environment_id"`
	Since         *time.Time `json:"since"`
}

func (r *StartRevenueRecognitionRunRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}
	r.TenantID, r.EnvironmentID = strings.TrimSpace(r.TenantID), strings.TrimSpace(r.EnvironmentID)
	if r.EnvironmentID != "" && r.TenantID == "" {
		return ierr.NewError("tenant_id is required with environment_id").
			WithHint("Provide the tenant the environment belongs to").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RevenueRecognitionRun reports a background backfill or recognition run.
type RevenueRecognitionRun struct {
	WorkflowID string       `json:"workflow_id"`
	RunID      string       `json:"run_id"`
	Status     ReplayStatus `json:"status"`
	Attempt    int32        `json:"attempt,omitempty"`
	LastError  string       `json:"last_error,omitempty"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	ClosedAt   *time.Time   `json:"closed_at,omitempty"`
}

// validateEnvironmentTarget trims and requires the tenant and environment IDs.
func validateEnvironmentTarget(tenantID, environmentID *string) error {
	*tenantID, *environmentID = strings.TrimSpace(*tenantID), strings.TrimSpace(*environmentID)
	if *tenantID == "" || *environmentID == "" {
		return ierr.NewError("tenant_id and environment_id are required").Mark(ierr.ErrValidation)
	}
	return nil
}
