package email

import "testing"

func TestMaskAddress(t *testing.T) {
	tests := map[string]string{
		"alice@acme.com": "a***@acme.com",
		"x":              "***",
		"":               "***",
		"@acme.com":      "***",
		"a@":             "***",
	}
	for in, want := range tests {
		if got := MaskAddress(in); got != want {
			t.Errorf("MaskAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
