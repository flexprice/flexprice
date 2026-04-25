package types

import (
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// filtering options
type DataType string

const (
	DataTypeString DataType = "string"
	DataTypeNumber DataType = "number"
	DataTypeDate   DataType = "date"
	DataTypeArray  DataType = "array"
)

// Value is a tagged union. Only one member should be non-nil / non-zero.
type Value struct {
	String  *string    `json:"string,omitempty"`
	Number  *float64   `json:"number,omitempty"`
	Boolean *bool      `json:"boolean,omitempty"`
	Date    *time.Time `json:"date,omitempty"`
	Array   []string   `json:"array,omitempty"`
}

type FilterOperatorType string

const (
	// equal
	EQUAL FilterOperatorType = "eq"

	// string
	CONTAINS     FilterOperatorType = "contains"
	NOT_CONTAINS FilterOperatorType = "not_contains"
	// TODO: add these operators
	// STARTS_WITH  FilterOperatorType = "STARTS_WITH"
	// ENDS_WITH    FilterOperatorType = "ENDS_WITH"

	// number
	GREATER_THAN           FilterOperatorType = "gt"
	GREATER_THAN_OR_EQUAL  FilterOperatorType = "gte"
	LESS_THAN              FilterOperatorType = "lt"
	LESS_THAN_OR_EQUAL     FilterOperatorType = "lte"

	// array
	IN     FilterOperatorType = "in"
	NOT_IN FilterOperatorType = "not_in"

	// date
	BEFORE FilterOperatorType = "before"
	AFTER  FilterOperatorType = "after"
)

type FilterCondition struct {
	Field    *string             `json:"field" form:"field"`
	Operator *FilterOperatorType `json:"operator" form:"operator"`
	DataType *DataType           `json:"data_type" form:"data_type"`
	Value    *Value              `json:"value" form:"value"`
}

func (f *FilterCondition) Validate() error {

	// check for empty fields
	if f.Field == nil {
		return ierr.NewError("field is required").
			WithHint("Field is required").
			Mark(ierr.ErrValidation)
	}

	if f.Operator == nil {
		return ierr.NewError("operator is required").
			WithHint("Operator is required").
			Mark(ierr.ErrValidation)
	}

	if f.DataType == nil {
		return ierr.NewError("data_type is required").
			WithHint("Data type is required").
			Mark(ierr.ErrValidation)
	}

	if f.Value == nil {
		return ierr.NewError("value is required").
			WithHint("Value is required").
			Mark(ierr.ErrValidation)
	}

	if f.Value.String == nil && f.Value.Number == nil && f.Value.Date == nil && f.Value.Array == nil {
		return ierr.NewError("At least one of the value fields must be provided").
			WithHint("At least one of the value fields must be provided").
			Mark(ierr.ErrValidation)
	}

	if lo.FromPtr(f.DataType) == DataTypeString {
		if f.Value.String == nil {
			return ierr.NewError("value_string is required").
				WithHint("Value string is required").
				Mark(ierr.ErrValidation)
		}
	}

	if lo.FromPtr(f.DataType) == DataTypeNumber {
		if f.Value.Number == nil {
			return ierr.NewError("value_number is required").
				WithHint("Value number is required").
				Mark(ierr.ErrValidation)
		}
	}

	if lo.FromPtr(f.DataType) == DataTypeArray {
		if f.Value.Array == nil {
			return ierr.NewError("value_array is required").
				WithHint("Value array is required").
				Mark(ierr.ErrValidation)
		}
	}

	if lo.FromPtr(f.DataType) == DataTypeDate {
		if f.Value.Date == nil {
			return ierr.NewError("value_date is required").
				WithHint("Value date is required").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// sorting options
type SortDirection string

const (
	SortDirectionAsc  SortDirection = "asc"
	SortDirectionDesc SortDirection = "desc"
)

type SortCondition struct {
	Field     string        `json:"field" form:"field"`
	Direction SortDirection `json:"direction" form:"direction"`
}

func (s *SortCondition) Validate() error {
	if s.Field == "" {
		return ierr.NewError("field is required").
			WithHint("Field is required").
			Mark(ierr.ErrValidation)
	}

	if s.Direction == "" {
		return ierr.NewError("direction is required").
			WithHint("Direction is required").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// MaxFilterDepth is the maximum allowed nesting depth for a FilterNode tree.
const MaxFilterDepth = 5

// MaxFilterNodes is the maximum total nodes allowed in a FilterNode tree.
const MaxFilterNodes = 100

// GroupOp is the logical operator for a group FilterNode.
type GroupOp string

const (
	GroupOpAnd GroupOp = "AND"
	GroupOpOr  GroupOp = "OR"
	GroupOpNot GroupOp = "NOT"
)

// FilterNode is a query tree node. Exactly one role must be active:
//   - Leaf:  Condition != nil, Operator and Conditions must be nil/empty.
//   - Group: Operator != nil, len(Conditions) > 0, Condition must be nil.
//
// NOT groups must have exactly 1 child.
// Max nesting depth: MaxFilterDepth. Max total nodes: MaxFilterNodes.
type FilterNode struct {
	Operator   *GroupOp         `json:"operator,omitempty"   form:"operator"`
	Conditions []*FilterNode    `json:"conditions,omitempty" form:"conditions"`
	Condition  *FilterCondition `json:"condition,omitempty"  form:"condition"`
}

// IsLeaf returns true when this node is a leaf (wraps a FilterCondition).
func (n *FilterNode) IsLeaf() bool { return n.Condition != nil }

// IsGroup returns true when this node has a group operator set (does not validate children).
func (n *FilterNode) IsGroup() bool { return n.Operator != nil }

// Validate validates the FilterNode tree recursively.
func (n *FilterNode) Validate() error {
	if n == nil {
		return nil
	}
	return n.validateRecursive(0, &filterNodeCounter{})
}

type filterNodeCounter struct{ n int }

func (n *FilterNode) validateRecursive(depth int, counter *filterNodeCounter) error {
	counter.n++
	if depth > MaxFilterDepth {
		return ierr.NewError("filter tree exceeds maximum nesting depth").
			WithHint("Reduce nesting to 5 or fewer levels").
			WithReportableDetails(map[string]any{"max_depth": MaxFilterDepth, "current_depth": depth}).
			Mark(ierr.ErrValidation)
	}
	if counter.n > MaxFilterNodes {
		return ierr.NewError("filter tree exceeds maximum node count").
			WithHint(fmt.Sprintf("Reduce total conditions to %d or fewer", MaxFilterNodes)).
			WithReportableDetails(map[string]any{"max_nodes": MaxFilterNodes}).
			Mark(ierr.ErrValidation)
	}

	isLeaf := n.Condition != nil
	isGroup := n.Operator != nil

	if isLeaf && isGroup {
		return ierr.NewError("a filter node must be either a leaf or a group, not both").
			WithHint("Set either 'condition' (leaf) or 'operator'+'conditions' (group), not both").
			Mark(ierr.ErrValidation)
	}
	if !isLeaf && !isGroup {
		return ierr.NewError("a filter node must be either a leaf or a group").
			WithHint("Set 'condition' for a leaf node, or 'operator'+'conditions' for a group node").
			Mark(ierr.ErrValidation)
	}

	if isLeaf {
		return n.Condition.Validate()
	}

	// group validation
	if len(n.Conditions) == 0 {
		return ierr.NewError("group FilterNode must have at least one condition").
			WithHint("Add at least one child to 'conditions'").
			Mark(ierr.ErrValidation)
	}
	if lo.FromPtr(n.Operator) == GroupOpNot && len(n.Conditions) != 1 {
		return ierr.NewError("NOT group must have exactly one child").
			WithHint("A NOT group can only negate a single condition or sub-group").
			Mark(ierr.ErrValidation)
	}

	for _, child := range n.Conditions {
		if child == nil {
			return ierr.NewError("FilterNode conditions cannot contain nil entries").
				Mark(ierr.ErrValidation)
		}
		if err := child.validateRecursive(depth+1, counter); err != nil {
			return err
		}
	}
	return nil
}

// DSLFilter consolidates Filters, FilterNode, and Sort into one embeddable struct.
// Embed *DSLFilter in any entity filter struct instead of declaring these fields individually.
type DSLFilter struct {
	Filters    []*FilterCondition `json:"filters,omitempty"     form:"filters"     validate:"omitempty"`
	FilterNode *FilterNode        `json:"filter_node,omitempty" form:"filter_node" validate:"omitempty"`
	Sort       []*SortCondition   `json:"sort,omitempty"        form:"sort"        validate:"omitempty"`
}

// Validate validates all DSL fields: Filters, FilterNode, and Sort.
func (d *DSLFilter) Validate() error {
	if d == nil {
		return nil
	}
	for _, f := range d.Filters {
		if f != nil {
			if err := f.Validate(); err != nil {
				return err
			}
		}
	}
	if d.FilterNode != nil {
		if err := d.FilterNode.Validate(); err != nil {
			return err
		}
	}
	for _, s := range d.Sort {
		if s != nil {
			if err := s.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}
