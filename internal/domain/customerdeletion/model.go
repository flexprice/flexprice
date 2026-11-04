package customerdeletion

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Request states; later phases need no migration
const (
	StateSuppressed = "suppressed"
	StateRedacting  = "redacting"
	StateCompleted  = "completed"
	StateFailed     = "failed"
)

// Channels a deletion request can arrive through.
const (
	RequestedViaAPI     = "api"
	RequestedViaWritten = "written"
)

// Per-category outcomes.
const (
	DispositionPending  = "pending"
	DispositionPurged   = "purged"
	DispositionRedacted = "redacted"
	DispositionRetained = "retained"
)

// Touchpoint categories.
const (
	CategoryCustomerPG = "customer_pg"
)

// CustomerDeletionRequest is the durable record of a customer deletion request.
type CustomerDeletionRequest struct {
	ID                    string                 `db:"id" json:"id"`
	CustomerID            string                 `db:"customer_id" json:"customer_id"`
	ExternalID            string                 `db:"external_id" json:"external_id"`
	State                 string                 `db:"state" json:"state"`
	RequestedAt           time.Time              `db:"requested_at" json:"requested_at"`
	SuppressedAt          *time.Time             `db:"suppressed_at" json:"suppressed_at,omitempty"`
	RedactedAt            *time.Time             `db:"redacted_at" json:"redacted_at,omitempty"`
	CooldownUntil         *time.Time             `db:"cooldown_until" json:"cooldown_until,omitempty"`
	IrreversibleStartedAt *time.Time             `db:"irreversible_started_at" json:"irreversible_started_at,omitempty"`
	RequestedVia          string                 `db:"requested_via" json:"requested_via"`
	RetentionPolicy       map[string]interface{} `db:"retention_policy" json:"retention_policy,omitempty"`
	FailureReason         string                 `db:"failure_reason" json:"failure_reason,omitempty"`
	EnvironmentID         string                 `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// CustomerDeletionCategory is the outcome of one touchpoint of one request.
type CustomerDeletionCategory struct {
	ID                 string                 `db:"id" json:"id"`
	RequestID          string                 `db:"request_id" json:"request_id"`
	Category           string                 `db:"category" json:"category"`
	Disposition        string                 `db:"disposition" json:"disposition"`
	LegalBasis         string                 `db:"legal_basis" json:"legal_basis,omitempty"`
	VerifiedAt         *time.Time             `db:"verified_at" json:"verified_at,omitempty"`
	VerificationResult map[string]interface{} `db:"verification_result" json:"verification_result,omitempty"`
	EnvironmentID      string                 `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// FromEnt converts an ent CustomerDeletionRequest to the domain model.
func FromEnt(r *ent.CustomerDeletionRequest) *CustomerDeletionRequest {
	if r == nil {
		return nil
	}
	return &CustomerDeletionRequest{
		ID:                    r.ID,
		CustomerID:            r.CustomerID,
		ExternalID:            r.ExternalID,
		State:                 r.State,
		RequestedAt:           r.RequestedAt,
		SuppressedAt:          r.SuppressedAt,
		RedactedAt:            r.RedactedAt,
		CooldownUntil:         r.CooldownUntil,
		IrreversibleStartedAt: r.IrreversibleStartedAt,
		RequestedVia:          r.RequestedVia,
		RetentionPolicy:       r.RetentionPolicy,
		FailureReason:         r.FailureReason,
		EnvironmentID:         r.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  r.TenantID,
			Status:    types.Status(r.Status),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			CreatedBy: r.CreatedBy,
			UpdatedBy: r.UpdatedBy,
		},
	}
}

// CategoryFromEnt converts an ent CustomerDeletionCategory to the domain model.
func CategoryFromEnt(c *ent.CustomerDeletionCategory) *CustomerDeletionCategory {
	if c == nil {
		return nil
	}
	return &CustomerDeletionCategory{
		ID:                 c.ID,
		RequestID:          c.RequestID,
		Category:           c.Category,
		Disposition:        c.Disposition,
		LegalBasis:         c.LegalBasis,
		VerifiedAt:         c.VerifiedAt,
		VerificationResult: c.VerificationResult,
		EnvironmentID:      c.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  c.TenantID,
			Status:    types.Status(c.Status),
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			CreatedBy: c.CreatedBy,
			UpdatedBy: c.UpdatedBy,
		},
	}
}

// CategoryListFromEnt converts a slice of ent categories to domain models.
func CategoryListFromEnt(cs []*ent.CustomerDeletionCategory) []*CustomerDeletionCategory {
	if cs == nil {
		return nil
	}
	out := make([]*CustomerDeletionCategory, len(cs))
	for i, c := range cs {
		out[i] = CategoryFromEnt(c)
	}
	return out
}
