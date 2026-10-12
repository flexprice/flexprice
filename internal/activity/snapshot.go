package activity

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	entgo "entgo.io/ent"
	"github.com/samber/lo"
)

// Domain struct tags that shape a snapshot: `activity:"drop"` leaves the field out,
// `activity:"redact"` replaces its value with "[redacted]".
const (
	tagDrop   = "drop"
	tagRedact = "redact"
	redacted  = "[redacted]"
)

// Snapshot converts a written ent row into its public-API shape as a JSON-ready map.
type Snapshot struct {
	convert func(entgo.Value) reflect.Value
	fields  []snapshotField
}

type snapshotField struct {
	key       string
	index     []int
	omitEmpty bool
	mode      string
}

// snapshotOf builds a Snapshot from an ent-to-domain converter; the field layout is read once from D's tags.
func snapshotOf[E, D any](convert func(*E) *D) Snapshot {
	var fields []snapshotField
	collectFields(reflect.TypeFor[D](), nil, &fields)
	if dup := lo.FindDuplicatesBy(fields, func(f snapshotField) string { return f.key }); len(dup) > 0 {
		panic(fmt.Sprintf("activity: %s has duplicate json key %q", reflect.TypeFor[D](), dup[0].key))
	}
	return Snapshot{
		convert: func(v entgo.Value) reflect.Value {
			row, ok := v.(*E)
			if !ok {
				panic(fmt.Sprintf("activity: unexpected row type %T", v))
			}
			return reflect.ValueOf(convert(row))
		},
		fields: fields,
	}
}

// state walks the converted row once; values point into it so json.Marshal encodes them as it would the struct.
func (s Snapshot) state(row entgo.Value) (map[string]any, bool) {
	v := s.convert(row)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil, false
	}
	v = v.Elem()
	state := make(map[string]any, len(s.fields))
	for _, f := range s.fields {
		if f.mode == tagDrop {
			continue
		}
		fv, err := v.FieldByIndexErr(f.index)
		if err != nil || (f.omitEmpty && isEmpty(fv)) {
			continue
		}
		if f.mode == tagRedact {
			state[f.key] = redacted
			continue
		}
		state[f.key] = fv.Addr().Interface()
	}
	return state, true
}

// collectFields lists t's JSON fields as encoding/json sees them, flattening untagged embedded structs.
func collectFields(t reflect.Type, index []int, out *[]snapshotField) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" && opts == "" {
			continue
		}
		fieldIndex := append(slices.Clone(index), i)
		embedded := f.Type
		if embedded.Kind() == reflect.Pointer {
			embedded = embedded.Elem()
		}
		if f.Anonymous && name == "" && embedded.Kind() == reflect.Struct {
			collectFields(embedded, fieldIndex, out)
			continue
		}
		if !f.IsExported() {
			continue
		}
		*out = append(*out, snapshotField{
			key: lo.Ternary(name == "", f.Name, name), index: fieldIndex,
			omitEmpty: slices.Contains(strings.Split(opts, ","), "omitempty"),
			mode:      f.Tag.Get("activity"),
		})
	}
}

// isEmpty is encoding/json's omitempty rule; reflect.Value.IsZero differs on structs and -0.
func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Struct:
		return false
	}
	return v.IsZero()
}

// stringField reads a string value from a snapshot map, following pointers.
func stringField(fields map[string]any, key string) string {
	v := reflect.ValueOf(fields[key])
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.String {
		return ""
	}
	return v.String()
}
