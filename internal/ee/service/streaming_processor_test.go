package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
)

func TestStreamingProcessor_DownloadFileStream_BlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.GetDefaultConfig()
	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("failed to build logger: %v", err)
	}

	sp := NewStreamingProcessor(log)
	sp.RetryClient.RetryMax = 0 // keep the test fast; the guard applies per attempt regardless of retries

	tsk := &task.Task{FileURL: srv.URL + "/file.csv"}
	if _, err := sp.downloadFileStream(context.Background(), tsk); err == nil {
		t.Fatal("expected downloadFileStream to a loopback address to be blocked, got nil error")
	}
}
