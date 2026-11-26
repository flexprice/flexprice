package inbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestServer_ClosesStalledBodyReadsAndServesCompleteOnes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := NewServer(0, handler).WithTimeouts(Timeouts{ReadHeader: time.Second, Read: 200 * time.Millisecond, Write: time.Second, Idle: time.Second})
	port, err := srv.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("POST /webhook HTTP/1.1\r\nHost: probe\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Read(make([]byte, 512)); errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("a client that stalls mid-body must be cut off by the read timeout")
	}

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/webhook", port), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("a complete request must still be served, got %d", resp.StatusCode)
	}
}
