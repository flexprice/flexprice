package admin

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// UpdateRevenueRecognitionConfigRequest turns revenue recognition on or off for one environment; Backfill also starts
// a backfill when enabling.
type UpdateRevenueRecognitionConfigRequest struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	Enabled       bool   `json:"enabled"`
	Backfill      bool   `json:"backfill"`
}

func (r *UpdateRevenueRecognitionConfigRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}
	if err := validateEnvironmentTarget(&r.TenantID, &r.EnvironmentID); err != nil {
		return err
	}
	if r.Backfill && !r.Enabled {
		return ierr.NewError("backfill requires enabled").
			WithHint("A backfill only runs while revenue recognition is enabled").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RevenueRecognitionConfigResponse is the environment's setting and, when one was started, its backfill.
type RevenueRecognitionConfigResponse struct {
	TenantID      string                         `json:"tenant_id"`
	EnvironmentID string                         `json:"environment_id"`
	Enabled       bool                           `json:"enabled"`
	Backfill      *RevenueRecognitionBackfillRun `json:"backfill,omitempty"`
}

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

// RevenueRecognitionBackfillRun reports a background backfill.
type RevenueRecognitionBackfillRun struct {
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
