package svix

import "testing"

func TestDashboardAccessRequestSetsExpiry(t *testing.T) {
	req := dashboardAccessRequest()
	if req.Expiry == nil || *req.Expiry != dashboardAccessExpirySeconds {
		t.Fatalf("expected expiry %d, got %v", dashboardAccessExpirySeconds, req.Expiry)
	}
}
