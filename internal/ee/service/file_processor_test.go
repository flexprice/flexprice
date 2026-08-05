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

func newTestFileProcessor(t *testing.T) *FileProcessor {
	t.Helper()
	cfg := config.GetDefaultConfig()
	log, err := logger.NewLogger(cfg)
	if err != nil {
		t.Fatalf("failed to build logger: %v", err)
	}
	return NewFileProcessor(log)
}

func newLoopbackServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("a,b\n1,2\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFileProcessor_DownloadFile_BlocksLoopback(t *testing.T) {
	srv := newLoopbackServer(t)
	fp := newTestFileProcessor(t)
	tsk := &task.Task{FileURL: srv.URL + "/file.csv"}

	if _, err := fp.DownloadFile(context.Background(), tsk); err == nil {
		t.Fatal("expected DownloadFile to a loopback address to be blocked, got nil error")
	}
}

func TestFileProcessor_DownloadFileStream_BlocksLoopback(t *testing.T) {
	srv := newLoopbackServer(t)
	fp := newTestFileProcessor(t)
	tsk := &task.Task{FileURL: srv.URL + "/file.csv"}

	if _, err := fp.DownloadFileStream(context.Background(), tsk); err == nil {
		t.Fatal("expected DownloadFileStream to a loopback address to be blocked, got nil error")
	}
}

func TestFileProcessor_GetFileSize_BlocksLoopback(t *testing.T) {
	srv := newLoopbackServer(t)
	fp := newTestFileProcessor(t)
	tsk := &task.Task{FileURL: srv.URL + "/file.csv"}

	if _, err := fp.GetFileSize(context.Background(), tsk); err == nil {
		t.Fatal("expected GetFileSize to a loopback address to be blocked, got nil error")
	}
}
