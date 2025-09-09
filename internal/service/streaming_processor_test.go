package service

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockChunkProcessor is a mock implementation of ChunkProcessor
type MockChunkProcessor struct {
	mock.Mock
}

func (m *MockChunkProcessor) ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error) {
	args := m.Called(ctx, chunk, headers, chunkIndex)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ChunkResult), args.Error(1)
}

// MockHTTPClient is a mock implementation of httpclient.Client
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Send(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*httpclient.Response), args.Error(1)
}

func TestStreamingProcessor_DetectFileType(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantType FileType
	}{
		{
			name:     "CSV content",
			content:  []byte("header1,header2\nvalue1,value2"),
			wantType: FileTypeCSV,
		},
		{
			name:     "JSON array",
			content:  []byte(`[{"name": "test"}]`),
			wantType: FileTypeJSON,
		},
		{
			name:     "Empty content",
			content:  []byte{},
			wantType: FileTypeCSV,
		},
		{
			name:     "Whitespace before JSON",
			content:  []byte("  \n\t[{\"name\": \"test\"}]"),
			wantType: FileTypeJSON,
		},
		{
			name:     "With BOM - CSV",
			content:  []byte{0xEF, 0xBB, 0xBF, 'a', ',', 'b'},
			wantType: FileTypeCSV,
		},
		{
			name:     "With BOM - JSON",
			content:  []byte{0xEF, 0xBB, 0xBF, '[', '{', '}', ']'},
			wantType: FileTypeJSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := NewStreamingProcessor(nil, newTestLogger())
			got := sp.detectFileType(tt.content)
			assert.Equal(t, tt.wantType, got)
		})
	}
}

func TestStreamingProcessor_ProcessJSONStream(t *testing.T) {
	tests := []struct {
		name          string
		jsonContent   []byte
		chunkSize     int
		expectedCalls int
		wantErr       bool
	}{
		{
			name: "Single chunk",
			jsonContent: mustMarshalJSON([]map[string]interface{}{
				{"name": "test1", "value": 1},
				{"name": "test2", "value": 2},
			}),
			chunkSize:     5,
			expectedCalls: 1,
			wantErr:       false,
		},
		{
			name: "Multiple chunks",
			jsonContent: mustMarshalJSON([]map[string]interface{}{
				{"name": "test1", "value": 1},
				{"name": "test2", "value": 2},
				{"name": "test3", "value": 3},
				{"name": "test4", "value": 4},
			}),
			chunkSize:     2,
			expectedCalls: 2,
			wantErr:       false,
		},
		{
			name:          "Empty array",
			jsonContent:   []byte(`[]`),
			chunkSize:     5,
			expectedCalls: 0,
			wantErr:       true,
		},
		{
			name:          "Invalid JSON",
			jsonContent:   []byte(`not json`),
			chunkSize:     5,
			expectedCalls: 0,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock processor
			mockProcessor := &MockChunkProcessor{}
			for i := 0; i < tt.expectedCalls; i++ {
				mockProcessor.On("ProcessChunk",
					mock.Anything,
					mock.Anything,
					mock.Anything,
					i,
				).Return(&ChunkResult{
					ProcessedRecords:  2,
					SuccessfulRecords: 2,
					FailedRecords:     0,
				}, nil)
			}

			// Create streaming processor
			sp := NewStreamingProcessor(nil, newTestLogger())

			// Create test task
			testTask := &task.Task{
				ID: "test-task",
			}

			// Create config
			config := &StreamingConfig{
				ChunkSize:      tt.chunkSize,
				BufferSize:     1024,
				UpdateInterval: time.Second,
				MaxRetries:     1,
				RetryDelay:     time.Millisecond,
			}

			// Process the stream
			err := sp.processJSONStream(
				context.Background(),
				testTask,
				mockProcessor,
				config,
				bytes.NewReader(tt.jsonContent),
			)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				mockProcessor.AssertNumberOfCalls(t, "ProcessChunk", tt.expectedCalls)
			}
		})
	}
}

func TestStreamingProcessor_ProcessFileStream(t *testing.T) {
	tests := []struct {
		name        string
		fileContent []byte
		fileType    FileType
		wantErr     bool
	}{
		{
			name: "JSON file",
			fileContent: mustMarshalJSON([]map[string]interface{}{
				{"name": "test1", "value": 1},
				{"name": "test2", "value": 2},
			}),
			fileType: FileTypeJSON,
			wantErr:  false,
		},
		{
			name:        "CSV file",
			fileContent: []byte("name,value\ntest1,1\ntest2,2"),
			fileType:    FileTypeCSV,
			wantErr:     false,
		},
		{
			name:        "Invalid content",
			fileContent: []byte("invalid content"),
			fileType:    FileTypeCSV,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := &MockHTTPClient{}
			mockClient.On("Send", mock.Anything, mock.Anything).Return(&httpclient.Response{
				StatusCode: 200,
				Body:       tt.fileContent,
			}, nil)

			// Create mock processor
			mockProcessor := &MockChunkProcessor{}
			mockProcessor.On("ProcessChunk",
				mock.Anything,
				mock.Anything,
				mock.Anything,
				mock.Anything,
			).Return(&ChunkResult{
				ProcessedRecords:  2,
				SuccessfulRecords: 2,
				FailedRecords:     0,
			}, nil)

			// Create streaming processor
			sp := NewStreamingProcessor(mockClient, newTestLogger())

			// Create test task
			testTask := &task.Task{
				ID:      "test-task",
				FileURL: "http://example.com/test.json",
			}

			// Process the file
			err := sp.ProcessFileStream(
				context.Background(),
				testTask,
				mockProcessor,
				DefaultStreamingConfig(),
			)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				mockProcessor.AssertCalled(t, "ProcessChunk",
					mock.Anything,
					mock.Anything,
					mock.Anything,
					mock.Anything,
				)
			}
		})
	}
}

// Helper function to marshal JSON and panic on error
func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
