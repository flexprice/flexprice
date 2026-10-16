package revenue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

type timezoneRepository struct {
	subscription.Repository
	timezone string
	err      error
	calls    int
}

func (r *timezoneRepository) GetTimezone(context.Context, string) (string, error) {
	r.calls++
	return r.timezone, r.err
}

func TestSubscriptionLocation(t *testing.T) {
	tests := []struct {
		name     string
		timezone string
		err      error
		want     string
	}{
		{name: "subscription timezone", timezone: "Asia/Kolkata", want: "Asia/Kolkata"},
		{name: "empty timezone", want: "UTC"},
		{name: "invalid timezone", timezone: "invalid/timezone", want: "UTC"},
		{name: "lookup failure", err: errors.New("subscription unavailable"), want: "UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &timezoneRepository{timezone: tt.timezone, err: tt.err}
			svc := New(service.ServiceParams{SubRepo: repo, Logger: logger.NewNoopLogger()}).(*revenueService)
			locations := map[string]*time.Location{}
			for i := 0; i < 2; i++ {
				loc := svc.subscriptionLocation(context.Background(), locations, "subs_test")
				require.Equal(t, tt.want, loc.String())
			}
			require.Equal(t, 1, repo.calls, "timezone reads should be memoized within an invoice")
		})
	}
}
