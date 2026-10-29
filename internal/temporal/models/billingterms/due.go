package billingterms

import (
	"strings"
	"time"

	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// DueInput retains a scoped cutoff and continuation across bounded workflow runs.
type DueInput struct {
	TenantID      string    `json:"tenant_id"`
	EnvironmentID string    `json:"environment_id"`
	UserID        string    `json:"user_id,omitempty"`
	AsOf          time.Time `json:"as_of,omitempty"`
	Cursor        string    `json:"cursor,omitempty"`
	BatchSize     int       `json:"batch_size,omitempty"`
	Progress      DueResult `json:"progress"`
}

// DueResult counts preparation and durable handoffs, independently of payment completion.
type DueResult struct {
	Scanned  int `json:"scanned"`
	Started  int `json:"started"`
	Deferred int `json:"deferred"`
	Failed   int `json:"failed"`
}

// Validate rejects unscoped work and oversized workflow payloads.
func (i DueInput) Validate() error {
	if strings.TrimSpace(i.TenantID) == "" || strings.TrimSpace(i.EnvironmentID) == "" || len(i.TenantID) > 50 || len(i.EnvironmentID) > 50 || len(i.UserID) > 50 || len(i.Cursor) > 2048 || i.BatchSize < 0 || i.BatchSize > 100 {
		return ierr.Validation("billing schedule workflow requires tenant/environment scope and a batch size up to 100")
	}
	if !i.AsOf.IsZero() && (i.AsOf.Year() < 1 || i.AsOf.Year() > 9999) {
		return ierr.Validation("billing schedule cutoff must be finite")
	}
	if i.Cursor != "" && i.AsOf.IsZero() {
		return ierr.Validation("a resumed billing schedule run requires its original cutoff")
	}
	if i.Progress.Scanned < 0 || i.Progress.Started < 0 || i.Progress.Deferred < 0 || i.Progress.Failed < 0 {
		return ierr.Validation("billing schedule progress cannot be negative")
	}
	return nil
}

// PrepareInput restores scope for one bounded preparation activity.
type PrepareInput struct {
	TenantID      string               `json:"tenant_id"`
	EnvironmentID string               `json:"environment_id"`
	UserID        string               `json:"user_id,omitempty"`
	Request       core.DueBatchRequest `json:"request"`
}

// CreateInput restores scope for one idempotent invoice activity.
type CreateInput struct {
	TenantID      string                 `json:"tenant_id"`
	EnvironmentID string                 `json:"environment_id"`
	UserID        string                 `json:"user_id,omitempty"`
	Request       core.DueInvoiceRequest `json:"request"`
}
