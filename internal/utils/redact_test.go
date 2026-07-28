package utils

import "testing"

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		secrets []string
		want    string
	}{
		{
			name: "no secrets leaves the message untouched",
			msg:  "AccessDenied: not authorized to perform sts:AssumeRole",
			want: "AccessDenied: not authorized to perform sts:AssumeRole",
		},
		{
			name:    "empty secrets are skipped, not redacted as empty matches",
			msg:     "status 401: invalid_client",
			secrets: []string{"", "", ""},
			want:    "status 401: invalid_client",
		},
		{
			name:    "the surrounding message is kept verbatim",
			msg:     `AADSTS7000215: Invalid client secret provided. Trace ID: abc-123`,
			secrets: []string{"nothing-matches-here"},
			want:    `AADSTS7000215: Invalid client secret provided. Trace ID: abc-123`,
		},
		{
			name:    "every occurrence of a secret is removed",
			msg:     "role arn:aws:iam::1:role/r denied for arn:aws:iam::1:role/r",
			secrets: []string{"arn:aws:iam::1:role/r"},
			want:    "role [redacted] denied for [redacted]",
		},
		{
			name:    "multiple distinct secrets are all removed",
			msg:     "audience //iam.googleapis.com/p/1 sa svc@p.iam.gserviceaccount.com",
			secrets: []string{"//iam.googleapis.com/p/1", "svc@p.iam.gserviceaccount.com"},
			want:    "audience [redacted] sa [redacted]",
		},
		{
			// A shorter secret that is a prefix of a longer one must not match first and leave the
			// longer secret's remainder ("def") exposed.
			name:    "overlapping secrets, shorter listed first",
			msg:     "abcdef",
			secrets: []string{"abc", "abcdef"},
			want:    "[redacted]",
		},
		{
			name:    "overlapping secrets, longer listed first",
			msg:     "abcdef",
			secrets: []string{"abcdef", "abc"},
			want:    "[redacted]",
		},
		{
			name:    "overlapping secrets where the shorter is a suffix",
			msg:     "tenant-secret-value",
			secrets: []string{"value", "secret-value", "tenant-secret-value"},
			want:    "[redacted]",
		},
		{
			name:    "the shorter secret is still redacted where it stands alone",
			msg:     "abcdef and abc",
			secrets: []string{"abc", "abcdef"},
			want:    "[redacted] and [redacted]",
		},
		{
			name:    "duplicate secrets are harmless",
			msg:     "token t-1",
			secrets: []string{"t-1", "t-1"},
			want:    "token [redacted]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactSecrets(tt.msg, tt.secrets...); got != tt.want {
				t.Errorf("RedactSecrets(%q, %q)\n got: %q\nwant: %q", tt.msg, tt.secrets, got, tt.want)
			}
		})
	}
}
