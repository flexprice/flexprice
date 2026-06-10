package e2eprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type ListenerEvent struct {
	Source     string
	ReceivedAt time.Time
	Payload    map[string]any
}

type Listener interface {
	Name() string
	Subscribe(ctx context.Context) (<-chan ListenerEvent, error)
}

// ---------- Context plumbing ----------

type listenerEventCtxKey struct{}

var ListenerEventKey = listenerEventCtxKey{}

func ContextWithEvent(parent context.Context, ev ListenerEvent) context.Context {
	return context.WithValue(parent, ListenerEventKey, &ev)
}

func EventFromContext(ctx context.Context) *ListenerEvent {
	v := ctx.Value(ListenerEventKey)
	if v == nil {
		return nil
	}
	return v.(*ListenerEvent)
}

// ---------- HTTP webhook listener ----------

func NewHTTPWebhookListener(port int) *HTTPWebhookListener {
	return &HTTPWebhookListener{requestedPort: port}
}

type HTTPWebhookListener struct {
	requestedPort int

	mu        sync.Mutex
	boundPort int
	srv       *http.Server
	ch        chan ListenerEvent
}

func (h *HTTPWebhookListener) Name() string { return "http-webhook" }

func (h *HTTPWebhookListener) BoundPort() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.boundPort
}

func (h *HTTPWebhookListener) Subscribe(ctx context.Context) (<-chan ListenerEvent, error) {
	h.mu.Lock()
	if h.ch != nil {
		h.mu.Unlock()
		return h.ch, nil
	}
	ch := make(chan ListenerEvent, 32)
	h.ch = ch
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		select {
		case ch <- ListenerEvent{Source: "http-webhook", ReceivedAt: time.Now(), Payload: payload}:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})
	addr := fmt.Sprintf(":%d", h.requestedPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	h.boundPort = ln.Addr().(*net.TCPAddr).Port
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	h.mu.Unlock()

	go func() { _ = h.srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.srv.Shutdown(shutCtx)
	}()
	return ch, nil
}

// ---------- ListenerScheduler ----------

func NewListenerScheduler(check Check, source Listener) Scheduler {
	return &listenerScheduler{check: check, source: source}
}

type listenerScheduler struct {
	check  Check
	source Listener
}

func (s *listenerScheduler) Schedule() string { return "listener:" + s.source.Name() }

func (s *listenerScheduler) Start(ctx context.Context, run RunFunc) {
	ch, err := s.source.Subscribe(ctx)
	if err != nil {
		<-ctx.Done()
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			run(ContextWithEvent(ctx, ev), s.check)
		}
	}
}
