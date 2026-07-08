package main

import (
	"reflect"
	"testing"
)

func TestSplitSQL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{
			name: "line comments stripped, split on semicolon",
			in:   "-- a comment\nCREATE TABLE IF NOT EXISTS x (a Int8);\n-- another\nALTER TABLE x ADD INDEX IF NOT EXISTS i a TYPE minmax GRANULARITY 1;",
			want: []string{"CREATE TABLE IF NOT EXISTS x (a Int8)", "ALTER TABLE x ADD INDEX IF NOT EXISTS i a TYPE minmax GRANULARITY 1"},
		},
		{
			name: "block comment stripped",
			in:   "/* header\nmulti line */ CREATE TABLE y (a Int8);",
			want: []string{"CREATE TABLE y (a Int8)"},
		},
		{
			name: "trailing empty statement dropped",
			in:   "SELECT 1;   \n  ;",
			want: []string{"SELECT 1"},
		},
		{
			name: "no trailing semicolon still yields statement",
			in:   "CREATE TABLE z (a Int8)",
			want: []string{"CREATE TABLE z (a Int8)"},
		},
		{
			name: "plain string literal without markers is fine",
			in:   "SELECT 'hello world';",
			want: []string{"SELECT 'hello world'"},
		},
		{
			name: "escaped and doubled quotes stay in literal",
			in:   "SELECT 'a''b', 'c\\'d';",
			want: []string{"SELECT 'a''b', 'c\\'d'"},
		},
		{
			name:    "semicolon inside literal fails loudly",
			in:      "SELECT 'a;b';",
			wantErr: true,
		},
		{
			name:    "line comment marker inside literal fails loudly",
			in:      "SELECT 'a -- b';",
			wantErr: true,
		},
		{
			name:    "block comment marker inside literal fails loudly",
			in:      "SELECT 'a /* b';",
			wantErr: true,
		},
		{
			name:    "marker inside backtick identifier fails loudly",
			in:      "SELECT `a;b`;",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := splitSQL(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("splitSQL() expected error, got nil (out=%#v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitSQL() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("splitSQL()\n got=%#v\nwant=%#v", got, c.want)
			}
		})
	}
}
