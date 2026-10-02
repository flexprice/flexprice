package activity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/types"
)

type SummaryInput struct {
	ActorLabel  string
	EntityLabel string
	Action      string
	Changes     map[string]any
}

func Summary(def Definition, in SummaryInput) string {
	if tpl, ok := def.Actions[in.Action]; ok {
		return strings.NewReplacer("{actor}", in.ActorLabel, "{entity}", in.EntityLabel).Replace(tpl)
	}
	verb := in.Action
	if i := strings.LastIndex(in.Action, "."); i >= 0 {
		verb = in.Action[i+1:]
	}
	verb = strings.ReplaceAll(verb, "_", " ")
	noun := strings.ReplaceAll(string(def.EntityType), "_", " ")
	entity := strings.TrimSpace(noun + " " + in.EntityLabel)
	switch len(in.Changes) {
	case 0:
		return fmt.Sprintf("%s %s %s", in.ActorLabel, verb, entity)
	case 1:
		for field, raw := range in.Changes {
			ch, _ := raw.(map[string]any)
			name := strings.ReplaceAll(field, "_", " ")
			if ch["redacted"] == true {
				return fmt.Sprintf("%s %s %s: %s changed", in.ActorLabel, verb, entity, name)
			}
			return fmt.Sprintf("%s %s %s: %s from %v to %v", in.ActorLabel, verb, entity, name, ch["from"], ch["to"])
		}
	}
	return fmt.Sprintf("%s %s %d fields on %s", in.ActorLabel, verb, len(in.Changes), entity)
}

// Humanize turns billing_anchor into "Billing anchor".
func Humanize(field string) string {
	s := strings.ReplaceAll(field, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// AnnotateChanges adds label and format hints to each change for the API.
func AnnotateChanges(def Definition, changes map[string]any) map[string]any {
	if changes == nil {
		return nil
	}
	out := make(map[string]any, len(changes))
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, field := range keys {
		ch, _ := changes[field].(map[string]any)
		if ch == nil {
			ch = map[string]any{}
		}
		label, format := Humanize(field), "text"
		if fd, ok := def.FieldLabels[field]; ok {
			label, format = fd.Label, fd.Format
		}
		if ref, ok := refTypeFor(ch["to"]); ok {
			format = "ref:" + string(ref)
		} else if ref, ok := refTypeFor(ch["from"]); ok {
			format = "ref:" + string(ref)
		}
		ch["label"], ch["format"] = label, format
		out[field] = ch
	}
	return out
}

// AnnotateMetadata wraps each metadata value as {value, label, format} so the
// frontend renders it with the same formatter table as changes.
func AnnotateMetadata(def Definition, meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	out := make(map[string]any, len(meta))
	for key, v := range meta {
		label, format := Humanize(key), "text"
		if fd, ok := def.FieldLabels[key]; ok {
			label, format = fd.Label, fd.Format
		}
		if ref, ok := refTypeFor(v); ok {
			format = "ref:" + string(ref)
		}
		out[key] = map[string]any{"value": v, "label": label, "format": format}
	}
	return out
}

var refPrefixes = map[string]types.SystemEntityType{
	types.UUID_PREFIX_CUSTOMER + "_":     types.SystemEntityTypeCustomer,
	types.UUID_PREFIX_SUBSCRIPTION + "_": types.SystemEntityTypeSubscription,
	types.UUID_PREFIX_PLAN + "_":         types.SystemEntityTypePlan,
	types.UUID_PREFIX_PRICE + "_":        types.SystemEntityTypePrice,
	types.UUID_PREFIX_INVOICE + "_":      types.SystemEntityTypeInvoice,
	types.UUID_PREFIX_WALLET + "_":       types.SystemEntityTypeWallet,
	types.UUID_PREFIX_PAYMENT + "_":      types.SystemEntityTypePayment,
}

// refTypeFor detects a reference by id prefix, e.g. "plan_01HX…" -> plan.
func refTypeFor(v any) (types.SystemEntityType, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	for prefix, t := range refPrefixes {
		if strings.HasPrefix(s, prefix) {
			return t, true
		}
	}
	return "", false
}
