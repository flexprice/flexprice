package dsl

import (
	"encoding/json"
	"fmt"
	"reflect"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/internal/types"
)

// JSONBContains returns a Predicate for PostgreSQL @> JSONB containment.
// The generated SQL is: <column> @> $1 — fully parameterized and GIN-index eligible.
func JSONBContains(column string, kv map[string]string) Predicate {
	return func(s *sql.Selector) {
		jsonBytes, _ := json.Marshal(kv)
		col := s.C(column)
		jsonStr := string(jsonBytes)
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString(col + " @> ").Arg(jsonStr)
		}))
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

	method := reflect.ValueOf(query).MethodByName("Where")
	if !method.IsValid() {
		return query, fmt.Errorf("ApplyMetadataFilter: query type %T does not have a Where method", query)
	}

	result := method.Call(args)
	if len(result) == 0 || !result[0].IsValid() {
		return query, fmt.Errorf("ApplyMetadataFilter: MethodByName(\"Where\") returned no valid value for query type %T", query)
	}

	q, ok := result[0].Interface().(T)
	if !ok {
		return query, fmt.Errorf("ApplyMetadataFilter: Where method return type %T is not assignable to expected query type", result[0].Interface())
	}
	return q, nil
}
