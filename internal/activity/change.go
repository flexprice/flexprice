package activity

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type Op int

const (
	OpCreate Op = iota
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

var bookkeeping = map[string]bool{"updated_at": true, "updated_by": true, "created_at": true, "created_by": true}

// Normalize maps driver and ent values onto a canonical string so values that
// differ only in representation compare equal.
func Normalize(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		var j any
		if json.Unmarshal(x, &j) == nil {
			b, _ := json.Marshal(j)
			return string(b)
		}
		return string(x)
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
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", toFloat(x))
	case bool:
		if x {
			return "true"
		}
		return "false"
	case fmt.Stringer:
		return x.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var j any
	_ = json.Unmarshal(b, &j)
	b, _ = json.Marshal(j)
	return string(b)
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int8:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint8:
		return float64(x)
	case uint16:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	case float32:
		return float64(x)
	case float64:
		return x
	}
	return 0
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
