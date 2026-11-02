package middleware

import (
	"net/http"
	"sync"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/gin-gonic/gin/binding"
)

var installBindingOnce sync.Once

// InstallMicrosecondBinding wraps gin's JSON, query and form bindings so every
// bound request has its time fields truncated to microseconds, the precision
// Postgres stores. This is the one ingress point for client-supplied
// timestamps (the Go SDK serializes nanoseconds); together with clock.Now()
// for generated instants, the Temporal activity-input interceptor and the ent
// write hook as a backstop, no other layer needs to handle precision.
//
// It replaces gin's package-level bindings, so it must run while routers are
// built, before the server starts serving requests.
func InstallMicrosecondBinding() {
	installBindingOnce.Do(func() {
		binding.JSON = microsecondBinding{inner: binding.JSON}
		binding.Query = microsecondValueBinding{inner: binding.Query}
		binding.Form = microsecondValueBinding{inner: binding.Form}
	})
}

type microsecondBinding struct {
	inner binding.BindingBody
}

func (b microsecondBinding) Name() string {
	return b.inner.Name()
}

func (b microsecondBinding) Bind(req *http.Request, obj any) error {
	if err := b.inner.Bind(req, obj); err != nil {
		return err
	}
	clock.TruncateTimes(obj)
	return nil
}

func (b microsecondBinding) BindBody(body []byte, obj any) error {
	if err := b.inner.BindBody(body, obj); err != nil {
		return err
	}
	clock.TruncateTimes(obj)
	return nil
}

// microsecondValueBinding wraps bindings without a body (query strings, forms).
type microsecondValueBinding struct {
	inner binding.Binding
}

func (b microsecondValueBinding) Name() string {
	return b.inner.Name()
}

func (b microsecondValueBinding) Bind(req *http.Request, obj any) error {
	if err := b.inner.Bind(req, obj); err != nil {
		return err
	}
	clock.TruncateTimes(obj)
	return nil
}
