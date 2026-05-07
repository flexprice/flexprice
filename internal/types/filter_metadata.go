package types

// MetadataFilter is a shared embeddable filter for JSONB metadata fields.
// Semantics: AND equality — all key-value pairs must be present in the entity metadata.
// This mirrors PostgreSQL's @> containment operator.
type MetadataFilter struct {
	Metadata map[string]string `json:"metadata,omitempty" form:"metadata"`
}

// Match returns true if entityMetadata contains all key-value pairs in the filter.
// A nil or empty filter matches everything.
func (f *MetadataFilter) Match(entityMetadata map[string]string) bool {
	if f == nil || len(f.Metadata) == 0 {
		return true
	}
	for k, v := range f.Metadata {
		if entityMetadata[k] != v {
			return false
		}
	}
	return true
}

// Validate validates the metadata filter (currently a no-op for symmetry with other embeddable filters)
func (f *MetadataFilter) Validate() error {
	return nil
}
