package types

import "strings"

// ExpandableField represents a field that can be expanded in API responses
type ExpandableField string

// Common expandable fields
const (
	ExpandPrices      ExpandableField = "prices"
	ExpandPlan        ExpandableField = "plan"
	ExpandFeatures    ExpandableField = "features"
	ExpandUsageMeters ExpandableField = "usage_meters"
)

// Expand represents the expand parameter in API requests
type Expand struct {
	Fields map[ExpandableField]bool
}

// NewExpand creates a new Expand from a comma-separated string of fields
func NewExpand(expand string) Expand {
	if expand == "" {
		return Expand{Fields: make(map[ExpandableField]bool)}
	}

	fields := make(map[ExpandableField]bool)
	for _, field := range strings.Split(expand, ",") {
		fields[ExpandableField(strings.TrimSpace(field))] = true
	}
	return Expand{Fields: fields}
}

// Has checks if a field should be expanded
func (e Expand) Has(field ExpandableField) bool {
	return e.Fields[field]
}

// IsEmpty checks if no fields are to be expanded
func (e Expand) IsEmpty() bool {
	return len(e.Fields) == 0
}
