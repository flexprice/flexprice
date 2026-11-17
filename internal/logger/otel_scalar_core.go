package logger

import (
	"reflect"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// scalarCore rewrites zap Reflect fields that hold named scalar types (e.g.
// types.SecretProvider) into typed fields before they reach the otelzap core.
// otelzap only converts builtin types and renders anything else as
// "unhandled: (T) v", which is what SigNoz showed for such fields. Values that
// implement fmt.Stringer never get here: zap already encodes them as StringerType.
type scalarCore struct {
	zapcore.Core
}

func newScalarCore(core zapcore.Core) zapcore.Core {
	return scalarCore{Core: core}
}

func (c scalarCore) With(fields []zapcore.Field) zapcore.Core {
	return scalarCore{Core: c.Core.With(normalizeScalarFields(fields))}
}

func (c scalarCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c scalarCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(entry, normalizeScalarFields(fields))
}

// normalizeScalarFields copies the slice on first rewrite so the caller's fields
// (shared with the stdout core by the tee) are never mutated.
func normalizeScalarFields(fields []zapcore.Field) []zapcore.Field {
	var out []zapcore.Field
	for i, f := range fields {
		nf, ok := scalarField(f)
		if !ok {
			continue
		}
		if out == nil {
			out = append([]zapcore.Field(nil), fields...)
		}
		out[i] = nf
	}
	if out == nil {
		return fields
	}
	return out
}

func scalarField(f zapcore.Field) (zapcore.Field, bool) {
	if f.Type != zapcore.ReflectType || f.Interface == nil {
		return f, false
	}
	rv := reflect.ValueOf(f.Interface)
	switch rv.Kind() {
	case reflect.String:
		return zap.String(f.Key, rv.String()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return zap.Int64(f.Key, rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return zap.Uint64(f.Key, rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return zap.Float64(f.Key, rv.Float()), true
	case reflect.Bool:
		return zap.Bool(f.Key, rv.Bool()), true
	default:
		return f, false
	}
}
