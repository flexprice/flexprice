package api

import (
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/gin-gonic/gin/binding"
)

var installBindingOnce sync.Once

// installMicrosecondBinding wraps gin's JSON binding so every bound request
// body has its time fields truncated to microseconds, the precision Postgres
// stores. This is the one ingress point for client-supplied timestamps (the
// Go SDK serializes nanoseconds); together with types.Now() for generated
// instants and the ent write hook as a backstop, no other layer needs to
// handle precision.
func installMicrosecondBinding() {
	installBindingOnce.Do(func() {
		binding.JSON = microsecondBinding{inner: binding.JSON}
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
	truncateTimes(reflect.ValueOf(obj))
	return nil
}

func (b microsecondBinding) BindBody(body []byte, obj any) error {
	if err := b.inner.BindBody(body, obj); err != nil {
		return err
	}
	truncateTimes(reflect.ValueOf(obj))
	return nil
}

var timeType = reflect.TypeOf(time.Time{})

func truncateTimes(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			truncateTimes(v.Elem())
		}
	case reflect.Struct:
		if v.Type() == timeType {
			if v.CanSet() {
				t := v.Interface().(time.Time)
				v.Set(reflect.ValueOf(t.Truncate(time.Microsecond)))
			}
			return
		}
		for i := 0; i < v.NumField(); i++ {
			truncateTimes(v.Field(i))
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			truncateTimes(v.Index(i))
		}
	case reflect.Map:
		if !v.CanInterface() {
			return
		}
		for iter := v.MapRange(); iter.Next(); {
			mv := iter.Value()
			if mv.Kind() == reflect.Pointer {
				truncateTimes(mv)
			} else if mv.Type() == timeType {
				t := mv.Interface().(time.Time)
				v.SetMapIndex(iter.Key(), reflect.ValueOf(t.Truncate(time.Microsecond)))
			}
		}
	case reflect.Interface:
		// Interface elements are only addressable through a pointer.
		if !v.IsNil() && v.Elem().Kind() == reflect.Pointer {
			truncateTimes(v.Elem())
		}
	}
}
