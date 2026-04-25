package dsl

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// And creates a group FilterNode with AND logic.
func And(conditions ...*types.FilterNode) *types.FilterNode {
	return &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpAnd),
		Conditions: conditions,
	}
}

// Or creates a group FilterNode with OR logic.
func Or(conditions ...*types.FilterNode) *types.FilterNode {
	return &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpOr),
		Conditions: conditions,
	}
}

// Not creates a group FilterNode that negates a single child.
func Not(condition *types.FilterNode) *types.FilterNode {
	return &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpNot),
		Conditions: []*types.FilterNode{condition},
	}
}

// Leaf creates a leaf FilterNode wrapping a raw FilterCondition.
func Leaf(condition *types.FilterCondition) *types.FilterNode {
	return &types.FilterNode{Condition: condition}
}

// Eq creates a string equality leaf node.
func Eq(field, value string) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.EQUAL),
		DataType: lo.ToPtr(types.DataTypeString),
		Value:    &types.Value{String: &value},
	})
}

// Gt creates a "greater than" numeric leaf node.
func Gt(field string, value float64) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.GREATER_THAN),
		DataType: lo.ToPtr(types.DataTypeNumber),
		Value:    &types.Value{Number: &value},
	})
}

// Gte creates a "greater than or equal" numeric leaf node.
func Gte(field string, value float64) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.GREATER_THAN_OR_EQUAL),
		DataType: lo.ToPtr(types.DataTypeNumber),
		Value:    &types.Value{Number: &value},
	})
}

// Lt creates a "less than" numeric leaf node.
func Lt(field string, value float64) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.LESS_THAN),
		DataType: lo.ToPtr(types.DataTypeNumber),
		Value:    &types.Value{Number: &value},
	})
}

// Lte creates a "less than or equal" numeric leaf node.
func Lte(field string, value float64) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.LESS_THAN_OR_EQUAL),
		DataType: lo.ToPtr(types.DataTypeNumber),
		Value:    &types.Value{Number: &value},
	})
}

// In creates an "in array" leaf node.
func In(field string, values []string) *types.FilterNode {
	if len(values) == 0 {
		panic("dsl.In: values must be non-empty")
	}
	cp := make([]string, len(values))
	copy(cp, values)
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.IN),
		DataType: lo.ToPtr(types.DataTypeArray),
		Value:    &types.Value{Array: cp},
	})
}

// NotIn creates a "not in array" leaf node.
func NotIn(field string, values []string) *types.FilterNode {
	if len(values) == 0 {
		panic("dsl.NotIn: values must be non-empty")
	}
	cp := make([]string, len(values))
	copy(cp, values)
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.NOT_IN),
		DataType: lo.ToPtr(types.DataTypeArray),
		Value:    &types.Value{Array: cp},
	})
}

// Contains creates a case-insensitive string contains leaf node.
func Contains(field, value string) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.CONTAINS),
		DataType: lo.ToPtr(types.DataTypeString),
		Value:    &types.Value{String: &value},
	})
}

// NotContains creates a case-insensitive string not-contains leaf node.
func NotContains(field, value string) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.NOT_CONTAINS),
		DataType: lo.ToPtr(types.DataTypeString),
		Value:    &types.Value{String: &value},
	})
}

// Before creates a "date before" leaf node.
func Before(field string, t time.Time) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.BEFORE),
		DataType: lo.ToPtr(types.DataTypeDate),
		Value:    &types.Value{Date: &t},
	})
}

// After creates a "date after" leaf node.
func After(field string, t time.Time) *types.FilterNode {
	return Leaf(&types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.AFTER),
		DataType: lo.ToPtr(types.DataTypeDate),
		Value:    &types.Value{Date: &t},
	})
}
