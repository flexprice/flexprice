package dsl

import (
	"reflect"
	"time"

	"entgo.io/ent/dialect/sql"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// Apply sorts in the exact order provided by the user
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
		default:
			// Default to ASC if direction not specified
			of = func(sel *sql.Selector) { sel.OrderBy(sql.Asc(fi)) }
		}
		if of != nil {
			out = append(out, of)
		}
	}
	return out, nil
}

// leafSQLPredicate builds a *sql.Predicate for a single FilterCondition.
// Used by both BuildPredicates (flat path) and buildNodePredicate (tree path).
func leafSQLPredicate(f *types.FilterCondition, fi string) *sql.Predicate {
	if f.Operator == nil {
		return nil
	}
	switch lo.FromPtr(f.Operator) {
	case types.EQUAL:
		return sql.EQ(fi, valueAny(f))
	case types.CONTAINS:
		if v := valueString(f); v != nil {
			return sql.ContainsFold(fi, *v)
		}
	case types.NOT_CONTAINS:
		if v := valueString(f); v != nil {
			return sql.Not(sql.ContainsFold(fi, *v))
		}
	case types.GREATER_THAN:
		if num := valueNumber(f); num != nil {
			return sql.GT(fi, *num)
		}
	case types.GREATER_THAN_OR_EQUAL:
		if num := valueNumber(f); num != nil {
			return sql.GTE(fi, *num)
		}
	case types.LESS_THAN:
		if num := valueNumber(f); num != nil {
			return sql.LT(fi, *num)
		}
	case types.LESS_THAN_OR_EQUAL:
		if num := valueNumber(f); num != nil {
			return sql.LTE(fi, *num)
		}
	case types.IN:
		if arr := valueArray(f); len(arr) > 0 {
			return sql.In(fi, toAny(arr)...)
		}
	case types.NOT_IN:
		if arr := valueArray(f); len(arr) > 0 {
			return sql.NotIn(fi, toAny(arr)...)
		}
	case types.BEFORE:
		if t := valueDate(f); t != nil {
			return sql.LT(fi, *t)
		}
	case types.AFTER:
		if t := valueDate(f); t != nil {
			return sql.GT(fi, *t)
		}
	}
	return nil
}

// predicateFromFilter wraps leafSQLPredicate into the Predicate (func(*sql.Selector)) type.
// Unchanged external behaviour — existing call sites continue to work.
func predicateFromFilter(f *types.FilterCondition, fi string) Predicate {
	p := leafSQLPredicate(f, fi)
	if p == nil {
		return nil
	}
	return func(sel *sql.Selector) { sel.Where(p) }
}

// defaultBlockedFields is the set of fields blocked from filter trees by default.
// Declared as a map to prevent slice-append mutation hazards.
var defaultBlockedFields = map[string]bool{"tenant_id": true, "environment_id": true}

// buildNodePredicate recursively builds a *sql.Predicate from a FilterNode tree.
// node.Validate() must be called before this function.
func buildNodePredicate(
	node *types.FilterNode,
	resolve FieldResolver,
	blocked map[string]bool,
) (*sql.Predicate, error) {
	if node.IsLeaf() {
		c := node.Condition
		field := lo.FromPtr(c.Field)
		if blocked[field] {
			return nil, ierr.NewErrorf("field '%s' is not allowed in filter conditions", field).
				WithHint("This field cannot be used as a filter").
				Mark(ierr.ErrValidation)
		}
		fi, err := resolve(field)
		if err != nil {
			return nil, err
		}
		p := leafSQLPredicate(c, fi)
		if p == nil {
			return nil, ierr.NewErrorf("unsupported operator/value combination for field '%s'", field).
				Mark(ierr.ErrValidation)
		}
		return p, nil
	}

	// group node
	childPreds := make([]*sql.Predicate, 0, len(node.Conditions))
	for _, child := range node.Conditions {
		p, err := buildNodePredicate(child, resolve, blocked)
		if err != nil {
			return nil, err
		}
		childPreds = append(childPreds, p)
	}

	switch lo.FromPtr(node.Operator) {
	case types.GroupOpAnd:
		return sql.And(childPreds...), nil
	case types.GroupOpOr:
		return sql.Or(childPreds...), nil
	case types.GroupOpNot:
		return sql.Not(childPreds[0]), nil
	default:
		return nil, ierr.NewErrorf("unsupported group operator '%s'", lo.FromPtr(node.Operator)).
			Mark(ierr.ErrValidation)
	}
}

// ApplyFilterNode applies a grouped/nested FilterNode tree to a query.
// This is a parallel entry point — existing ApplyFilters is unchanged.
//
// blockedFields: nil uses the default ["tenant_id", "environment_id"].
// Pass a non-nil slice to replace the default per entity.
func ApplyFilterNode[T any, P any](
	query T,
	node *types.FilterNode,
	resolve FieldResolver,
	predicateConverter func(Predicate) P,
	blockedFields []string,
) (T, error) {
	if node == nil {
		return query, nil
	}
	if err := node.Validate(); err != nil {
		return query, err
	}

	var blocked map[string]bool
	if blockedFields == nil {
		blocked = defaultBlockedFields
	} else {
		blocked = make(map[string]bool, len(blockedFields))
		for _, f := range blockedFields {
			blocked[f] = true
		}
	}

	sqlPred, err := buildNodePredicate(node, resolve, blocked)
	if err != nil {
		return query, err
	}

	entPred := predicateConverter(func(sel *sql.Selector) { sel.Where(sqlPred) })
	// One composite predicate wraps the entire tree. Ent's Where is variadic AND-ed,
	// so callers using ApplyFilters + ApplyFilterNode together get implicit AND semantics.
	result := reflect.ValueOf(query).MethodByName("Where").Call(
		[]reflect.Value{reflect.ValueOf(entPred)},
	)
	if len(result) > 0 {
		query = result[0].Interface().(T)
	}
	return query, nil
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
