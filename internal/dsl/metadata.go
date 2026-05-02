package dsl

import (
	"encoding/json"
	"reflect"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/internal/types"
)

// JSONBContains returns a Predicate for PostgreSQL @> JSONB containment.
// The generated SQL is: <column> @> $1 — fully parameterized and GIN-index eligible.
func JSONBContains(column string, kv map[string]string) Predicate {
	return func(s *sql.Selector) {
		jsonBytes, _ := json.Marshal(kv)
		s.Where(sql.ExprP(s.C(column)+" @> ?", string(jsonBytes)))
	}
}

// ApplyMetadataFilter applies a JSONB containment predicate on the "metadata" column.
// T is the Ent query builder type (e.g. *ent.CustomerQuery).
// P is the entity predicate type (e.g. predicate.Customer).
// No-ops when filter is nil or empty.
func ApplyMetadataFilter[T any, P any](
	query T,
	filter *types.MetadataFilter,
	predicateConverter func(Predicate) P,
) (T, error) {
	if filter == nil || len(filter.Metadata) == 0 {
		return query, nil
	}
	pred := predicateConverter(JSONBContains("metadata", filter.Metadata))
	args := []reflect.Value{reflect.ValueOf(pred)}
	result := reflect.ValueOf(query).MethodByName("Where").Call(args)
	if len(result) > 0 {
		query = result[0].Interface().(T)
	}
	return query, nil
}
