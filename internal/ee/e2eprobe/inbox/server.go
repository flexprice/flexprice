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
	port    int
	handler http.Handler
}

func NewServer(port int, h http.Handler) *Server {
	return &Server{port: port, handler: h}
}

// Start listens, serves in the background and shuts down when ctx ends. It
// returns the bound port so a zero port works in tests.
func (s *Server) Start(ctx context.Context) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return 0, err
	}
	srv := &http.Server{Handler: s.handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // #nosec G118 -- parent ctx is done; shutdown needs its own deadline
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
