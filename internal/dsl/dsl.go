package dsl

import (
	"reflect"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type Predicate = func(*sql.Selector) // Ent aliases these under predicate.<Entity>
type OrderFunc = func(*sql.Selector) // Ent aliases these under ent.<Entity>.Asc/Desc

type FieldResolver func(logical string) (string, error)

// QueryBuilder is a generic interface for Ent query builders
type QueryBuilder interface {
	Where(...interface{}) interface{}
	Order(...interface{}) interface{}
}

// ApplyFilters applies filter conditions to a query
// T is the query builder type (e.g., *ent.FeatureQuery)
// P is the predicate type (e.g., predicate.Feature)
func ApplyFilters[T any, P any](
	query T,
	filters []*types.FilterCondition,
	resolve FieldResolver,
	predicateConverter func(Predicate) P,
) (T, error) {
	if len(filters) == 0 {
		return query, nil
	}

	// Build predicates using DSL
	predicates, err := BuildPredicates(filters, resolve)
	if err != nil {
		return query, err
	}

	if len(predicates) > 0 {
		// Convert DSL predicates to entity-specific predicates
		entPredicates := make([]P, len(predicates))
		for i, p := range predicates {
			entPredicates[i] = predicateConverter(p)
		}
		// Use reflection to call Where method with individual predicates
		args := make([]reflect.Value, len(entPredicates))
		for i, p := range entPredicates {
			args[i] = reflect.ValueOf(p)
		}
		result := reflect.ValueOf(query).MethodByName("Where").Call(args)
		if len(result) > 0 {
			query = result[0].Interface().(T)
		}
	}

	return query, nil
}

// ApplySorts applies sort conditions to a query
// T is the query builder type (e.g., *ent.FeatureQuery)
// O is the order option type (e.g., feature.OrderOption)
func ApplySorts[T any, O any](
	query T,
	sort []*types.SortCondition,
	resolve FieldResolver,
	orderConverter func(OrderFunc) O,
) (T, error) {
	if len(sort) == 0 {
		return query, nil
	}

	// Build order functions using DSL
	orders, err := BuildOrders(sort, resolve)
	if err != nil {
		return query, err
	}

	if len(orders) > 0 {
		// Convert DSL order functions to entity-specific order options
		entOrders := make([]O, len(orders))
		for i, o := range orders {
			entOrders[i] = orderConverter(o)
		}
		// Use reflection to call Order method with individual order options
		args := make([]reflect.Value, len(entOrders))
		for i, o := range entOrders {
			args[i] = reflect.ValueOf(o)
		}
		result := reflect.ValueOf(query).MethodByName("Order").Call(args)
		if len(result) > 0 {
			query = result[0].Interface().(T)
		}
	}

	return query, nil
}

func BuildPredicates(filters []*types.FilterCondition, resolve FieldResolver) ([]Predicate, error) {
	out := make([]Predicate, 0, len(filters))

	for _, f := range filters {
		if f == nil {
			continue
		}
		fi, err := resolve(lo.FromPtr(f.Field))
		if err != nil {
			return nil, err
		}
		pred := predicateFromFilter(f, fi)
		if pred != nil {
			out = append(out, pred)
		}
	}
	return out, nil
}

func BuildOrders(sort []*types.SortCondition, resolve FieldResolver) ([]OrderFunc, error) {
	out := make([]OrderFunc, 0, len(sort))

	for _, s := range sort {
		if s == nil {
			continue
		}
		fi, err := resolve(s.Field)
		if err != nil {
			return nil, err
		}
		var of OrderFunc
		switch s.Direction {
		case types.SortDirectionAsc:
			of = func(sel *sql.Selector) { sel.OrderBy(sql.Asc(fi)) }
		case types.SortDirectionDesc:
			of = func(sel *sql.Selector) { sel.OrderBy(sql.Desc(fi)) }
		}
		if of != nil {
			out = append(out, of)
		}
	}
	return out, nil
}

func predicateFromFilter(f *types.FilterCondition, fi string) Predicate {
	if f.Operator == nil {
		return nil
	}

	switch lo.FromPtr(f.Operator) {
	case types.EQUAL:
		return func(sel *sql.Selector) { sel.Where(sql.EQ(fi, valueAny(f))) }

	// string operators
	case types.CONTAINS:
		if v := valueString(f); v != nil {
			return func(sel *sql.Selector) { sel.Where(sql.ContainsFold(fi, *v)) }
		}
	case types.GREATER_THAN:
		if num := valueNumber(f); num != nil {
			return func(sel *sql.Selector) { sel.Where(sql.GT(fi, *num)) }
		}

	case types.LESS_THAN:
		if num := valueNumber(f); num != nil {
			return func(sel *sql.Selector) { sel.Where(sql.LT(fi, *num)) }
		}

	case types.IN:
		if arr := valueArray(f); len(arr) > 0 {
			return func(sel *sql.Selector) { sel.Where(sql.In(fi, toAny(arr)...)) }
		}

	case types.NOT_IN:
		if arr := valueArray(f); len(arr) > 0 {
			return func(sel *sql.Selector) { sel.Where(sql.NotIn(fi, toAny(arr)...)) }
		}

	case types.BEFORE:
		if t := valueDate(f); t != nil {
			return func(sel *sql.Selector) { sel.Where(sql.LT(fi, *t)) }
		}

	case types.AFTER:
		if t := valueDate(f); t != nil {
			return func(sel *sql.Selector) { sel.Where(sql.GT(fi, *t)) }
		}
	}
	return nil // fell through → invalid combi
}

func valueAny(f *types.FilterCondition) interface{} {
	// fallback when type known from column
	if s := valueString(f); s != nil {
		return *s
	}
	if n := valueNumber(f); n != nil {
		return *n
	}
	if b := valueBool(f); b != nil {
		return *b
	}
	if d := valueDate(f); d != nil {
		return *d
	}
	if a := valueArray(f); len(a) > 0 {
		return toAny(a)
	}
	return nil
}

func valueString(f *types.FilterCondition) *string  { return f.Value.String }
func valueNumber(f *types.FilterCondition) *float64 { return f.Value.Number }
func valueBool(f *types.FilterCondition) *bool      { return f.Value.Boolean }
func valueDate(f *types.FilterCondition) *time.Time { return f.Value.Date }
func valueArray(f *types.FilterCondition) []string  { return f.Value.Array }
func toAny(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
