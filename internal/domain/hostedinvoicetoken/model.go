package hostedinvoicetoken

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	validityAfterDue = 30 * 24 * time.Hour
	maxLifetime      = 120 * 24 * time.Hour
)

// HostedInvoiceToken records a public hosted invoice token for one invoice. The token
// string is signed from the id at read time; neither the token nor the URL is stored.
type HostedInvoiceToken struct {
	ID            string     `json:"id,omitempty"`
	EnvironmentID string     `json:"environment_id,omitempty"`
	InvoiceID     string     `json:"invoice_id,omitempty"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	types.BaseModel
}

// IsActive reports whether the token can still open its invoice at now.
func (l *HostedInvoiceToken) IsActive(now time.Time) bool {
	return l.RevokedAt == nil && now.Before(l.ExpiresAt)
}

// FromEnt converts an Ent HostedInvoiceToken to a domain HostedInvoiceToken.
func FromEnt(e *ent.HostedInvoiceToken) *HostedInvoiceToken {
	if e == nil {
		return nil
	}
	return &HostedInvoiceToken{
		ID:            e.ID,
		EnvironmentID: e.EnvironmentID,
		InvoiceID:     e.InvoiceID,
		ExpiresAt:     e.ExpiresAt,
		RevokedAt:     e.RevokedAt,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of Ent HostedInvoiceTokens to domain HostedInvoiceTokens.
func FromEntList(list []*ent.HostedInvoiceToken) []*HostedInvoiceToken {
	if list == nil {
		return nil
	}
	return lo.Map(list, func(item *ent.HostedInvoiceToken, _ int) *HostedInvoiceToken {
		return FromEnt(item)
	})
}

// CalculateExpiry returns min((due date, else finalization) + 30 days, createdAt + 120 days).
func CalculateExpiry(dueDate *time.Time, finalizedAt, createdAt time.Time) time.Time {
	expiresAt := lo.FromPtrOr(dueDate, finalizedAt).Add(validityAfterDue)
	if limit := createdAt.Add(maxLifetime); expiresAt.After(limit) {
		return limit
	}
	return expiresAt
}
