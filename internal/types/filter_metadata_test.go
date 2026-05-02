package types

import (
	"testing"
)

func TestMetadataFilter_Match(t *testing.T) {
	tests := []struct {
		name           string
		filter         *MetadataFilter
		entityMetadata map[string]string
		want           bool
	}{
		{
			name:           "nil filter matches everything",
			filter:         nil,
			entityMetadata: map[string]string{"plan": "enterprise"},
			want:           true,
		},
		{
			name:           "empty filter matches everything",
			filter:         &MetadataFilter{},
			entityMetadata: map[string]string{"plan": "enterprise"},
			want:           true,
		},
		{
			name:           "single key match",
			filter:         &MetadataFilter{Metadata: map[string]string{"plan": "enterprise"}},
			entityMetadata: map[string]string{"plan": "enterprise", "region": "us-east"},
			want:           true,
		},
		{
			name:           "multiple keys AND match",
			filter:         &MetadataFilter{Metadata: map[string]string{"plan": "enterprise", "region": "us-east"}},
			entityMetadata: map[string]string{"plan": "enterprise", "region": "us-east"},
			want:           true,
		},
		{
			name:           "value mismatch",
			filter:         &MetadataFilter{Metadata: map[string]string{"plan": "enterprise"}},
			entityMetadata: map[string]string{"plan": "starter"},
			want:           false,
		},
		{
			name:           "key missing from entity",
			filter:         &MetadataFilter{Metadata: map[string]string{"plan": "enterprise"}},
			entityMetadata: map[string]string{"region": "us-east"},
			want:           false,
		},
		{
			name:           "empty entity metadata",
			filter:         &MetadataFilter{Metadata: map[string]string{"plan": "enterprise"}},
			entityMetadata: map[string]string{},
			want:           false,
		},
		{
			name:           "nil entity metadata",
			filter:         &MetadataFilter{Metadata: map[string]string{"plan": "enterprise"}},
			entityMetadata: nil,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Match(tt.entityMetadata)
			if got != tt.want {
				t.Errorf("MetadataFilter.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}
