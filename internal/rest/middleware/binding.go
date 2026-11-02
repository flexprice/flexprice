package middleware

import (
	"net/http"
	"sync"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/gin-gonic/gin/binding"
)

var installBindingOnce sync.Once

// InstallMicrosecondBinding truncates time fields to microseconds in gin's JSON, query and form bindings.
// It replaces gin's package-level bindings, so call it while building routers, before serving.
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
