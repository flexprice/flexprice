package ent

import (
	"encoding/json"

	"entgo.io/ent/dialect/sql"
)

// JSONBContains returns an Ent SQL selector predicate that filters rows where
// the given JSONB column contains all key-value pairs in kv (PostgreSQL @> operator).
// The generated SQL is: <column> @> $1 — fully parameterized and GIN-index eligible.
//
// Usage:
//
//	query.Where(predicate.Customer(JSONBContains("metadata", f.MetadataFilter)))
func JSONBContains(column string, kv map[string]string) func(*sql.Selector) {
	return func(s *sql.Selector) {
		jsonBytes, _ := json.Marshal(kv)
		s.Where(sql.ExprP(s.C(column)+" @> ?", string(jsonBytes)))
	}
}
