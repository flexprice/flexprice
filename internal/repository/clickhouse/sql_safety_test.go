package clickhouse

import "testing"

func TestParseGroupByPropertyPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "valid simple", input: "properties.org_id", want: "org_id", wantOK: true},
		{name: "valid nested", input: "properties.account.plan_tier", want: "account.plan_tier", wantOK: true},
		{name: "missing prefix", input: "org_id", want: "", wantOK: false},
		{name: "empty after prefix", input: "properties.", want: "", wantOK: false},
		{name: "invalid quote", input: "properties.org'id", want: "", wantOK: false},
		{name: "invalid sql payload", input: "properties.org_id);DROP", want: "", wantOK: false},
		{name: "invalid double dot", input: "properties.a..b", want: "", wantOK: false},
		{name: "invalid space", input: "properties.has space", want: "", wantOK: false},
		{name: "invalid leading dot", input: "properties..foo", want: "", wantOK: false},
		{name: "invalid trailing dot", input: "properties.foo.", want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseGroupByPropertyPath(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("parseGroupByPropertyPath() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("parseGroupByPropertyPath() got = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPropertyAlias(t *testing.T) {
	if got := propertyAlias("account.plan_tier"); got != "prop_account_plan_tier" {
		t.Fatalf("propertyAlias() got = %q, want %q", got, "prop_account_plan_tier")
	}

	if got := propertyAlias("bad path"); got != "prop_invalid" {
		t.Fatalf("propertyAlias() got = %q, want %q", got, "prop_invalid")
	}
}
