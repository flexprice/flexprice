package types

import (
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
	CONTAINS FilterOperatorType = "contains"
	// TODO: add these operators
	// NOT_CONTAINS FilterOperatorType = "NOT_CONTAINS"
	// STARTS_WITH  FilterOperatorType = "STARTS_WITH"
	// ENDS_WITH    FilterOperatorType = "ENDS_WITH"

	// number
	GREATER_THAN FilterOperatorType = "gt"
	LESS_THAN    FilterOperatorType = "lt"

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
