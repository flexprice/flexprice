package cron

import "testing"

func TestMapRazorpayPaymentStatusToClaimStatus(t *testing.T) {
	tests := []struct {
		name           string
		razorpayStatus string
		want           string
	}{
		{name: "captured maps to succeeded", razorpayStatus: "captured", want: "succeeded"},
		{name: "failed maps to failed", razorpayStatus: "failed", want: "failed"},
		{name: "created is still ambiguous", razorpayStatus: "created", want: ""},
		{name: "authorized is still ambiguous", razorpayStatus: "authorized", want: ""},
		{name: "refunded is unexpected but not actionable here", razorpayStatus: "refunded", want: ""},
		{name: "empty status is ambiguous", razorpayStatus: "", want: ""},
		{name: "unknown status is ambiguous", razorpayStatus: "some_future_status", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapRazorpayPaymentStatusToClaimStatus(tt.razorpayStatus)
			if got != tt.want {
				t.Errorf("mapRazorpayPaymentStatusToClaimStatus(%q) = %q, want %q", tt.razorpayStatus, got, tt.want)
			}
		})
	}
}
