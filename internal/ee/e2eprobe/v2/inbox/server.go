package inbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server binds the probe's HTTP port for the inbox and status handlers.
type Server struct {
	port     int
	handler  http.Handler
	timeouts Timeouts
}

// Timeouts bound every connection so a stalled client cannot hold a handler.
type Timeouts struct {
	ReadHeader time.Duration
	Read       time.Duration
	Write      time.Duration
	Idle       time.Duration
}

// DefaultTimeouts fits the one-MiB webhook payload limit with room to spare.
func DefaultTimeouts() Timeouts {
	return Timeouts{ReadHeader: 5 * time.Second, Read: 15 * time.Second, Write: 15 * time.Second, Idle: 60 * time.Second}
}

func NewServer(port int, h http.Handler) *Server {
	return &Server{port: port, handler: h, timeouts: DefaultTimeouts()}
}

// WithTimeouts replaces the default connection timeouts.
func (s *Server) WithTimeouts(t Timeouts) *Server {
	s.timeouts = t
	return s
}

// Start listens, serves in the background and shuts down when ctx ends. It
// returns the bound port so a zero port works in tests.
func (s *Server) Start(ctx context.Context) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return 0, err
	}
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.timeouts.ReadHeader,
		ReadTimeout:       s.timeouts.Read,
		WriteTimeout:      s.timeouts.Write,
		IdleTimeout:       s.timeouts.Idle,
	}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // #nosec G118 -- parent ctx is done; shutdown needs its own deadline
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
