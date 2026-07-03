package main

import (
	"reflect"
	"testing"
)

func TestSplitSQL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitSQL(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("splitSQL()\n got=%#v\nwant=%#v", got, c.want)
			}
		})
	}
}
