package hostedinvoicetoken

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestHostedInvoiceToken_IsActive(t *testing.T) {
	expiresAt := time.Date(2026, 12, 14, 0, 0, 0, 0, time.UTC)
	revokedAt := time.Date(2026, 10, 20, 9, 12, 0, 0, time.UTC)

	tests := []struct {
		name  string
		token *HostedInvoiceToken
		now   time.Time
		want  bool
	}{
		{"live before expiry", &HostedInvoiceToken{ExpiresAt: expiresAt}, expiresAt.Add(-time.Second), true},
		{"live at expiry", &HostedInvoiceToken{ExpiresAt: expiresAt}, expiresAt, false},
		{"live after expiry", &HostedInvoiceToken{ExpiresAt: expiresAt}, expiresAt.Add(time.Hour), false},
		{"revoked before expiry", &HostedInvoiceToken{ExpiresAt: expiresAt, RevokedAt: &revokedAt}, revokedAt.Add(time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.token.IsActive(tt.now))
		})
	}
}

func TestCalculateExpiry(t *testing.T) {
	createdAt := time.Date(2026, 10, 14, 10, 5, 0, 0, time.UTC)

	tests := []struct {
		name        string
		dueDate     *time.Time
		finalizedAt time.Time
		want        time.Time
	}{
		{
			name:        "due date plus 30 days",
			dueDate:     lo.ToPtr(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)),
			finalizedAt: createdAt,
			want:        time.Date(2026, 12, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			name:        "no due date uses finalization",
			finalizedAt: createdAt,
			want:        time.Date(2026, 11, 13, 10, 5, 0, 0, time.UTC),
		},
		{
			name:        "capped at creation plus 120 days",
			dueDate:     lo.ToPtr(time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)),
			finalizedAt: createdAt,
			want:        time.Date(2027, 2, 11, 10, 5, 0, 0, time.UTC),
		},
		{
			name:        "past due date gives an already expired token",
			dueDate:     lo.ToPtr(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
			finalizedAt: createdAt,
			want:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CalculateExpiry(tt.dueDate, tt.finalizedAt, createdAt))
		})
	}
}
