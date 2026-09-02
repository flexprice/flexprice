package storage_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/storage"
	"github.com/stretchr/testify/assert"
)

func TestObjectKey(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		entityType string
		filename   string
		format     string
		compressed bool
		want       string
	}{
		{
			name:       "with prefix, uncompressed",
			prefix:     "tenant-1/env-1",
			entityType: "events",
			filename:   "export-20260713",
			format:     "csv",
			want:       "tenant-1/env-1/events/export-20260713.csv",
		},
		{
			name:       "no prefix",
			prefix:     "",
			entityType: "invoice",
			filename:   "inv_123",
			format:     "pdf",
			want:       "invoice/inv_123.pdf",
		},
		{
			name:       "compressed appends .gz",
			prefix:     "p",
			entityType: "events",
			filename:   "f",
			format:     "csv",
			compressed: true,
			want:       "p/events/f.csv.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storage.ObjectKey(tt.prefix, tt.entityType, tt.filename, tt.format, tt.compressed)
			assert.Equal(t, tt.want, got)
		})
	}
}
