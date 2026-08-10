package quickbooks

import (
	"strings"
	"testing"
)

func TestEscapeQueryLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "acme@example.com", "acme@example.com"},
		{"single quote doubled", "O'Brien", "O''Brien"},
		{
			// SHN-10: the injection payload must not break out of the literal.
			name: "soql injection payload neutralized",
			in:   "x' OR '1'='1",
			want: "x'' OR ''1''=''1",
		},
		{"control chars stripped", "ab\ncd\tef\x00gh", "abcdefgh"},
		{"del stripped", "a\x7fb", "ab"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeQueryLiteral(tt.in); got != tt.want {
				t.Fatalf("escapeQueryLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscapeQueryLiteral_LengthBound(t *testing.T) {
	long := strings.Repeat("a", maxQueryLiteralLen+50)
	got := escapeQueryLiteral(long)
	if len(got) != maxQueryLiteralLen {
		t.Fatalf("length = %d, want %d", len(got), maxQueryLiteralLen)
	}
}

func TestEscapeQueryLiteral_NoUnescapedQuoteRemains(t *testing.T) {
	// Every apostrophe in the output must be part of a doubled pair, so no lone
	// quote can terminate the surrounding string literal.
	out := escapeQueryLiteral("a'b'c")
	for i := 0; i < len(out); i++ {
		if out[i] == '\'' {
			if i+1 >= len(out) || out[i+1] != '\'' {
				t.Fatalf("lone single quote at %d in %q", i, out)
			}
			i++ // skip the pair
		}
	}
}
