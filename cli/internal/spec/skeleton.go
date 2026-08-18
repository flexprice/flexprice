package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// maxSkeletonDepth is 16: the deepest natural nesting in this spec is 14, and a
// cap of 12 was measured to truncate real nodes.
const maxSkeletonDepth = 16

// Skeleton renders an editable JSON document for an operation's request body.
//
// Only required fields are emitted as live JSON; optional ones are commented
// out for the user to uncomment. Not stylistic: an untouched optional numeric
// field sent as "" fails the server's request binding with no details. The
// commented block carries most of the value here — every nested structure
// --edit exists for is optional in the spec.
//
// Termination is guaranteed by the depth cap; the cycle guard bounds breadth
// (without it the SubscriptionResponse walk grows from 1,693 to 17,789 nodes).
func Skeleton(cmd Command) (string, error) {
	op := cmd.Operation.Op
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return "", fmt.Errorf("%s %s takes no request body", cmd.Resource, cmd.Action)
	}
	media := op.RequestBody.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		return "", fmt.Errorf("%s %s has no JSON request schema", cmd.Resource, cmd.Action)
	}

	value := build(media.Schema, map[*openapi3.Schema]bool{}, 0)
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render skeleton: %w", err)
	}

	var header strings.Builder
	fmt.Fprintf(&header, "// flexprice %s %s\n", cmd.Resource, cmd.Action)
	fmt.Fprintf(&header, "// Required fields are pre-filled below. Lines starting with // are ignored.\n")

	var optional []string
	for _, f := range BodyFields(cmd) {
		if !f.Required {
			optional = append(optional, fmt.Sprintf("%s (%s)", f.Name, f.Type))
		}
	}
	sort.Strings(optional)
	if len(optional) > 0 {
		fmt.Fprintf(&header, "//\n// Optional fields you may add:\n")
		for _, o := range optional {
			fmt.Fprintf(&header, "//   %s\n", o)
		}
	}
	header.WriteString("\n")

	return header.String() + string(body) + "\n", nil
}

func build(ref *openapi3.SchemaRef, onPath map[*openapi3.Schema]bool, depth int) any {
	if ref == nil || ref.Value == nil || depth > maxSkeletonDepth {
		return nil
	}
	s := ref.Value
	if onPath[s] {
		return nil // cycle: stop descending
	}
	onPath[s] = true
	defer delete(onPath, s)

	switch schemaType(s) {
	case "object":
		out := map[string]any{}
		required := map[string]bool{}
		for _, r := range s.Required {
			required[r] = true
		}
		for name, prop := range s.Properties {
			if !required[name] {
				continue
			}
			out[name] = build(prop, onPath, depth+1)
		}
		return out
	case "array":
		inner := build(s.Items, onPath, depth+1)
		if inner == nil {
			return []any{}
		}
		return []any{inner}
	case "integer":
		return 0
	case "number":
		return 0
	case "boolean":
		return false
	default:
		if len(s.Enum) > 0 {
			return s.Enum[0]
		}
		return ""
	}
}

// StripComments removes // lines so an edited skeleton can be parsed as JSON.
func StripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
