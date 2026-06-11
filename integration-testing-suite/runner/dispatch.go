package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	flexprice "github.com/flexprice/go-sdk/v2"
)

// Dispatcher exposes the entire generated Go SDK surface to journeys by
// reflection. Speakeasy generates a uniform method shape:
//
//	func (s *Service) Op(ctx context.Context, args..., opts ...dtos.Option) (*dtos.OpResponse, error)
//
// so any "Service.Op" string can be resolved, its YAML inputs JSON-decoded
// into the typed parameters, and the typed response re-serialized into a JSON
// document for captures and assertions. Upgrading the SDK dependency makes
// new operations callable from YAML with no engine changes.
type Dispatcher struct {
	ops      map[string]*Operation
	services map[string][]string // service name → sorted method names
}

// Operation is one resolvable SDK method.
type Operation struct {
	Name     string // "Customers.CreateCustomer"
	Service  string
	Method   string
	fn       reflect.Value
	argTypes []reflect.Type // params between ctx and variadic opts
}

var ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
var errType = reflect.TypeOf((*error)(nil)).Elem()

// NewDispatcher discovers every operation on the SDK client.
func NewDispatcher(client *flexprice.Flexprice) *Dispatcher {
	d := &Dispatcher{ops: map[string]*Operation{}, services: map[string][]string{}}

	cv := reflect.ValueOf(client).Elem()
	ct := cv.Type()
	for i := 0; i < ct.NumField(); i++ {
		field := ct.Field(i)
		if !field.IsExported() || field.Type.Kind() != reflect.Ptr || field.Type.Elem().Kind() != reflect.Struct {
			continue
		}
		svc := cv.Field(i)
		if svc.IsNil() {
			continue
		}
		svcName := field.Name
		st := svc.Type()
		for m := 0; m < st.NumMethod(); m++ {
			method := st.Method(m)
			mv := svc.Method(m)
			mt := mv.Type()

			// Expected shape: (ctx, args..., opts ...) (*Resp, error)
			if mt.NumOut() != 2 || !mt.Out(1).Implements(errType) {
				continue
			}
			if mt.NumIn() < 1 || !mt.In(0).Implements(ctxType) {
				continue
			}
			end := mt.NumIn()
			if mt.IsVariadic() {
				end--
			}
			var argTypes []reflect.Type
			for a := 1; a < end; a++ {
				argTypes = append(argTypes, mt.In(a))
			}
			op := &Operation{
				Name:     svcName + "." + method.Name,
				Service:  svcName,
				Method:   method.Name,
				fn:       mv,
				argTypes: argTypes,
			}
			d.ops[op.Name] = op
			d.services[svcName] = append(d.services[svcName], method.Name)
		}
	}
	for svc := range d.services {
		sort.Strings(d.services[svc])
	}
	return d
}

// Resolve looks up an operation by "Service.Method", with helpful errors.
func (d *Dispatcher) Resolve(name string) (*Operation, error) {
	if op, ok := d.ops[name]; ok {
		return op, nil
	}
	// Case-insensitive rescue.
	for key, op := range d.ops {
		if strings.EqualFold(key, name) {
			return op, nil
		}
	}
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("call %q must be in Service.Method form (e.g. Customers.CreateCustomer); run with -list-ops to see all operations", name)
	}
	for svc, methods := range d.services {
		if strings.EqualFold(svc, parts[0]) {
			return nil, fmt.Errorf("call %q: service %s has no method %q; available: %s", name, svc, parts[1], strings.Join(methods, ", "))
		}
	}
	svcs := make([]string, 0, len(d.services))
	for svc := range d.services {
		svcs = append(svcs, svc)
	}
	sort.Strings(svcs)
	return nil, fmt.Errorf("call %q: unknown service %q; available services: %s", name, parts[0], strings.Join(svcs, ", "))
}

// Ops returns all operations sorted by name.
func (d *Dispatcher) Ops() []*Operation {
	out := make([]*Operation, 0, len(d.ops))
	for _, op := range d.ops {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Signature renders the operation's argument list for humans/agents.
func (op *Operation) Signature() string {
	parts := make([]string, len(op.argTypes))
	for i, t := range op.argTypes {
		parts[i] = typeName(t)
	}
	return fmt.Sprintf("%s(%s)", op.Name, strings.Join(parts, ", "))
}

// RequestFields lists the JSON fields of each struct argument, so agents can
// discover request shapes without reading SDK source.
func (op *Operation) RequestFields() []string {
	var out []string
	for _, t := range op.argTypes {
		base := t
		for base.Kind() == reflect.Ptr {
			base = base.Elem()
		}
		if base.Kind() != reflect.Struct || base.PkgPath() == "time" {
			continue
		}
		fields := jsonFieldNames(base)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s{%s}", typeName(t), strings.Join(fields, ", ")))
	}
	return out
}

// CheckArgs statically validates YAML-provided args against the operation's
// parameter types: arity, JSON-decodability, and unknown struct fields.
// Template placeholders are stubbed before decoding.
func (op *Operation) CheckArgs(args []any) error {
	if len(args) != len(op.argTypes) {
		return fmt.Errorf("%s takes %d argument(s) %s, got %d (use 'args:' for multi-argument calls; null is allowed for optional pointers)",
			op.Name, len(op.argTypes), op.Signature(), len(args))
	}
	for i, raw := range args {
		stubbed := stubTemplates(raw)
		if _, err := buildArg(op.argTypes[i], stubbed, true); err != nil {
			return fmt.Errorf("%s argument #%d: %v", op.Name, i+1, err)
		}
	}
	return nil
}

// Invoke calls the SDK operation with rendered args. It returns the response
// body as a generic JSON document plus the HTTP status code. API errors come
// back as callErr (matchable by expect_error); argument construction errors
// are returned as buildErr.
func (op *Operation) Invoke(ctx context.Context, args []any) (body any, status int, callErr error, buildErr error) {
	if len(args) != len(op.argTypes) {
		return nil, 0, nil, fmt.Errorf("%s takes %d argument(s), got %d", op.Name, len(op.argTypes), len(args))
	}
	in := make([]reflect.Value, 0, len(args)+1)
	in = append(in, reflect.ValueOf(ctx))
	for i, raw := range args {
		v, err := buildArg(op.argTypes[i], raw, false)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("%s argument #%d: %w", op.Name, i+1, err)
		}
		in = append(in, v)
	}

	out := op.fn.Call(in)
	if errV := out[1]; !errV.IsNil() {
		err := errV.Interface().(error)
		return nil, errorStatus(err), err, nil
	}
	b, st := unwrapResponse(out[0])
	return b, st, nil, nil
}

// buildArg converts a YAML value into the typed SDK parameter via JSON.
// strict additionally rejects unknown struct fields (validation mode).
func buildArg(t reflect.Type, raw any, strict bool) (reflect.Value, error) {
	// null → zero value (only sensible for pointers/optionals).
	if raw == nil {
		if t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Map || t.Kind() == reflect.Interface {
			return reflect.Zero(t), nil
		}
		return reflect.Value{}, fmt.Errorf("got null for required %s parameter", typeName(t))
	}

	if strict {
		if err := checkUnknownFields(t, raw, typeName(t)); err != nil {
			return reflect.Value{}, err
		}
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("encode value: %w", err)
	}
	ptr := reflect.New(t)
	if err := json.Unmarshal(data, ptr.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("value %s does not fit %s: %v", truncateStr(string(data), 120), typeName(t), err)
	}
	return ptr.Elem(), nil
}

// checkUnknownFields recursively rejects YAML keys that have no matching json
// tag on the target struct — catching typos that json.Unmarshal would
// silently drop. Types with no tagged fields (unions with custom marshalling)
// are skipped.
func checkUnknownFields(t reflect.Type, raw any, where string) error {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if t.PkgPath() == "time" {
			return nil
		}
		m, ok := raw.(map[string]any)
		if !ok {
			return nil // scalar into struct: let json.Unmarshal report it
		}
		fields := jsonFieldTypes(t)
		if len(fields) == 0 {
			return nil // union/custom-marshal type: cannot check
		}
		for k, v := range m {
			ft, ok := fields[k]
			if !ok {
				known := make([]string, 0, len(fields))
				for name := range fields {
					known = append(known, name)
				}
				sort.Strings(known)
				return fmt.Errorf("unknown field %q in %s (known fields: %s)", k, where, strings.Join(known, ", "))
			}
			if err := checkUnknownFields(ft, v, fmt.Sprintf("%s.%s", where, k)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		arr, ok := raw.([]any)
		if !ok {
			return nil
		}
		for i, el := range arr {
			if err := checkUnknownFields(t.Elem(), el, fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		m, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		for k, v := range m {
			if err := checkUnknownFields(t.Elem(), v, fmt.Sprintf("%s[%s]", where, k)); err != nil {
				return err
			}
		}
	}
	return nil
}

// jsonFieldTypes maps json tag names → field types for a struct (embedded
// fields flattened).
func jsonFieldTypes(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	var walk func(reflect.Type)
	walk = func(st reflect.Type) {
		for i := 0; i < st.NumField(); i++ {
			f := st.Field(i)
			if f.Anonymous {
				ft := f.Type
				for ft.Kind() == reflect.Ptr {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					walk(ft)
				}
				continue
			}
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			if name == "" {
				continue
			}
			out[name] = f.Type
		}
	}
	walk(t)
	return out
}

func jsonFieldNames(t reflect.Type) []string {
	fields := jsonFieldTypes(t)
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// stubTemplates replaces template expressions with placeholder strings so
// arg shapes can be validated without a live run. Strings that are a single
// "{{= ... }}" coercion expression become nil (type unknown until runtime).
func stubTemplates(raw any) any {
	switch v := raw.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(v), "{{=") {
			return nil
		}
		if strings.Contains(v, "{{") {
			// RFC3339 so the stub also satisfies time.Time fields; for plain
			// string fields any stub will do.
			return "0001-01-01T00:00:00Z"
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = stubTemplates(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = stubTemplates(val)
		}
		return out
	default:
		return raw
	}
}

// unwrapResponse converts a Speakeasy response struct into (body, status).
// The body is the first non-empty exported field other than HTTPMeta,
// round-tripped through JSON.
func unwrapResponse(rv reflect.Value) (any, int) {
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, 0
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, 0
	}

	status := 0
	var body any
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		fv := rv.Field(i)
		if f.Name == "HTTPMeta" {
			if resp := fv.FieldByName("Response"); resp.IsValid() && !resp.IsNil() {
				if hr, ok := resp.Interface().(*http.Response); ok && hr != nil {
					status = hr.StatusCode
				}
			}
			continue
		}
		if !f.IsExported() || body != nil {
			continue
		}
		if fv.Kind() == reflect.Ptr && fv.IsNil() {
			continue
		}
		if fv.IsZero() {
			continue
		}
		data, err := json.Marshal(fv.Interface())
		if err != nil {
			continue
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			continue
		}
		body = doc
	}
	return body, status
}

// errorStatus extracts an HTTP status code from any generated SDK error type
// (APIError and typed error responses) via reflection.
func errorStatus(err error) int {
	v := reflect.ValueOf(err)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0
	}
	if f := v.FieldByName("StatusCode"); f.IsValid() && f.Kind() == reflect.Int {
		return int(f.Int())
	}
	if meta := v.FieldByName("HTTPMeta"); meta.IsValid() {
		if resp := meta.FieldByName("Response"); resp.IsValid() && !resp.IsNil() {
			if hr, ok := resp.Interface().(*http.Response); ok && hr != nil {
				return hr.StatusCode
			}
		}
	}
	if raw := v.FieldByName("RawResponse"); raw.IsValid() && !raw.IsNil() {
		if hr, ok := raw.Interface().(*http.Response); ok && hr != nil {
			return hr.StatusCode
		}
	}
	return 0
}

func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + typeName(t.Elem())
	case reflect.Slice:
		return "[]" + typeName(t.Elem())
	}
	if t.PkgPath() != "" {
		parts := strings.Split(t.PkgPath(), "/")
		return parts[len(parts)-1] + "." + t.Name()
	}
	return t.String()
}
