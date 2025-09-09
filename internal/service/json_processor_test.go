package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONProcessor_PrepareJSONReader(t *testing.T) {
	tests := []struct {
		name        string
		fileContent []byte
		wantErr     bool
		errMsg      string
	}{
		{
			name:        "Valid JSON array",
			fileContent: []byte(`[{"name": "test", "value": 123}]`),
			wantErr:     false,
		},
		{
			name:        "Empty array",
			fileContent: []byte(`[]`),
			wantErr:     false,
		},
		{
			name:        "Invalid JSON",
			fileContent: []byte(`not json`),
			wantErr:     true,
			errMsg:      "invalid JSON content",
		},
		{
			name:        "Not an array",
			fileContent: []byte(`{"name": "test"}`),
			wantErr:     true,
			errMsg:      "JSON content must start with an array",
		},
		{
			name:        "With BOM",
			fileContent: []byte{0xEF, 0xBB, 0xBF, '[', '{', '"', 'a', '"', ':', '1', '}', ']'},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jp := NewJSONProcessor(newTestLogger())
			decoder, err := jp.PrepareJSONReader(tt.fileContent)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, decoder)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, decoder)
			}
		})
	}
}

func TestJSONProcessor_ExtractHeaders(t *testing.T) {
	tests := []struct {
		name        string
		fileContent []byte
		want        []string
		wantErr     bool
	}{
		{
			name:        "Simple object",
			fileContent: []byte(`[{"name": "test", "value": 123}]`),
			want:        []string{"name", "value"},
			wantErr:     false,
		},
		{
			name:        "Empty object",
			fileContent: []byte(`[{}]`),
			want:        []string{},
			wantErr:     false,
		},
		{
			name:        "Multiple fields",
			fileContent: []byte(`[{"id": 1, "name": "test", "email": "test@example.com", "age": 30}]`),
			want:        []string{"id", "name", "email", "age"},
			wantErr:     false,
		},
		{
			name:        "Invalid JSON",
			fileContent: []byte(`not json`),
			want:        nil,
			wantErr:     true,
		},
		{
			name:        "Not an array",
			fileContent: []byte(`{"name": "test"}`),
			want:        nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jp := NewJSONProcessor(newTestLogger())
			decoder, err := jp.PrepareJSONReader(tt.fileContent)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("PrepareJSONReader() error = %v", err)
				}
				return
			}

			headers, err := jp.ExtractHeaders(decoder)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, headers)
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.want, headers)
			}
		})
	}
}

func TestJSONProcessor_ValidateJSONStructure(t *testing.T) {
	tests := []struct {
		name        string
		fileContent []byte
		wantErr     bool
		errMsg      string
	}{
		{
			name:        "Valid array of objects",
			fileContent: []byte(`[{"name": "test"}]`),
			wantErr:     false,
		},
		{
			name:        "Empty array",
			fileContent: []byte(`[]`),
			wantErr:     true,
			errMsg:      "JSON array is empty",
		},
		{
			name:        "Array of non-objects",
			fileContent: []byte(`[1, 2, 3]`),
			wantErr:     true,
			errMsg:      "JSON array must contain objects",
		},
		{
			name:        "Not an array",
			fileContent: []byte(`{"name": "test"}`),
			wantErr:     true,
			errMsg:      "JSON content must start with an array",
		},
		{
			name:        "Invalid JSON",
			fileContent: []byte(`not json`),
			wantErr:     true,
			errMsg:      "invalid JSON content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jp := NewJSONProcessor(newTestLogger())
			decoder, err := jp.PrepareJSONReader(tt.fileContent)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("PrepareJSONReader() error = %v", err)
				}
				return
			}

			err = jp.ValidateJSONStructure(decoder)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
