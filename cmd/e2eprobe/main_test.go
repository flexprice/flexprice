package main

import "testing"

func TestParseFirstOTLPHeader(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantName    string
		wantValue   string
		wantOK      bool
	}{
		{name: "empty", raw: "", wantOK: false},
		{name: "whitespace", raw: "   ", wantOK: false},
		{name: "no equals", raw: "signoz-access-token", wantOK: false},
		{name: "empty name", raw: "=abc", wantOK: false},
		{name: "empty value", raw: "signoz-access-token=", wantOK: false},
		{name: "single pair", raw: "signoz-access-token=xyz", wantName: "signoz-access-token", wantValue: "xyz", wantOK: true},
		{name: "single pair with spaces", raw: " signoz-access-token = xyz ", wantName: "signoz-access-token", wantValue: "xyz", wantOK: true},
		{name: "multiple pairs takes first", raw: "signoz-access-token=xyz,other=abc", wantName: "signoz-access-token", wantValue: "xyz", wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotValue, gotOK := parseFirstOTLPHeader(tc.raw)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotName != tc.wantName {
				t.Errorf("name = %q, want %q", gotName, tc.wantName)
			}
			if gotValue != tc.wantValue {
				t.Errorf("value = %q, want %q", gotValue, tc.wantValue)
			}
		})
	}
}
