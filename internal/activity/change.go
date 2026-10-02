package activity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type Op int

const (
	opUnknown Op = iota
	OpCreate
	OpUpdate
	OpDelete
)

type Change struct {
	From     any  `json:"from,omitempty"`
	To       any  `json:"to,omitempty"`
	Redacted bool `json:"redacted,omitempty"`
}

// Record is one raw observation from the hook. Fields holds the final
// column values the hook saw, used for customer id and label derivation.
type Record struct {
	EntityType types.SystemEntityType
	EntityID   string
	Label      string
	Op         Op
	Changes    map[string]Change
	Snapshot   map[string]any
	Fields     map[string]any
	Degraded   string
}

var bookkeeping = map[string]bool{
	"updated_at": true, "updated_by": true, "created_at": true, "created_by": true,
	"tenant_id": true, "environment_id": true,
}

// Normalize maps driver and ent values onto a canonical string so values that
// differ only in representation compare equal.
func Normalize(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return normalizeBytes(x)
	case string:
		return x
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if x == nil {
			return ""
		}
		return x.UTC().Format(time.RFC3339Nano)
	case decimal.Decimal:
		return x.String()
	case *decimal.Decimal:
		if x == nil {
			return ""
		}
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	}

	// Dereference pointers and unwrap named string/numeric/bool kinds so a
	// type like types.BillingCadence or *string compares like its plain form.
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 64)
	case reflect.Bool:
		if rv.Bool() {
			return "true"
		}
		return "false"
	}
	if s, ok := v.(fmt.Stringer); ok {
		return s.String()
	}

	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var j any
	_ = json.Unmarshal(b, &j)
	return canonicalJSON(j)
}

// normalizeBytes handles the driver's raw column bytes: a NUMERIC column
// parses as a decimal to keep arbitrary precision, otherwise it is JSON
// (preserving number literals) or an opaque string.
func normalizeBytes(x []byte) string {
	s := string(x)
	if d, err := decimal.NewFromString(s); err == nil {
		return d.String()
	}
	dec := json.NewDecoder(bytes.NewReader(x))
	dec.UseNumber()
	var j any
	if dec.Decode(&j) == nil {
		return canonicalJSON(j)
	}
	return s
}

// canonicalJSON collapses JSON null and empty containers to the empty
// string so a NULL/{}/[] flip on a column is not a change, then marshals
// with map keys sorted so key order never affects equality.
func canonicalJSON(j any) string {
	switch t := j.(type) {
	case nil:
		return ""
	case map[string]any:
		if len(t) == 0 {
			return ""
		}
	case []any:
		if len(t) == 0 {
			return ""
		}
	}
	b, _ := json.Marshal(j)
	return string(b)
}

// Diff returns the fields whose normalized value changed, minus bookkeeping
// and ignored fields, with redacted fields marked instead of valued.
func Diff(def Definition, old, new map[string]any) (map[string]Change, bool) {
	ignore := map[string]bool{}
	for _, f := range def.IgnoreFields {
		ignore[f] = true
	}
	redact := map[string]bool{}
	for _, f := range def.RedactFields {
		redact[f] = true
	}
	out := map[string]Change{}
	for field, nv := range new {
		if bookkeeping[field] || ignore[field] {
			continue
		}
		ov, had := old[field]
		if had && Normalize(ov) == Normalize(nv) {
			continue
		}
		if !had && Normalize(nv) == "" {
			continue
		}
		if redact[field] {
			out[field] = Change{Redacted: true}
			continue
		}
		out[field] = Change{From: display(ov), To: display(nv)}
	}
	return out, len(out) > 0
}

// display keeps JSON-friendly values and stringifies the rest.
func display(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		var j any
		if json.Unmarshal(x, &j) == nil {
			return j
		}
		return string(x)
	case string, bool, float64, int, int64:
		return x
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case *time.Time:
		if x == nil {
			return nil
		}
		return x.UTC().Format(time.RFC3339)
	case decimal.Decimal:
		return x.String()
	case *decimal.Decimal:
		if x == nil {
			return nil
		}
		return x.String()
	}
	return Normalize(v)
}
