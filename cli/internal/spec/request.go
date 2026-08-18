package spec

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Input is everything the user supplied on the command line.
type Input struct {
	PositionalID string
	Flags        map[string]string
	Data         map[string]any // from --data or --edit
}

type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
}

// Field describes one settable body or query field, used for --help and --edit.
type Field struct {
	Name     string
	Type     string
	Required bool
	Nested   bool // objects and arrays cannot be expressed as a scalar flag
	Doc      string
}

func BuildRequest(cmd Command, in Input) (Request, error) {
	req := Request{Method: cmd.Operation.Method, Path: cmd.Operation.Path, Query: url.Values{}}

	// Flags is a map, so passing Input by value does not stop this function's
	// deletes below from mutating the caller's original map. Callers such as the
	// --all pagination loop call BuildRequest more than once against the same
	// Input, so those deletes must apply to a private copy, never the caller's.
	flags := make(map[string]string, len(in.Flags))
	for k, v := range in.Flags {
		flags[k] = v
	}
	in.Flags = flags

	pathParams, queryParams := splitParameters(cmd.Operation.Op)

	// Path parameters. A single path parameter is filled from the positional ID so
	// that `flexprice customers retrieve cust_1` works the way Stripe's does.
	for _, p := range pathParams {
		value := in.Flags[p.Name]
		if value == "" && len(pathParams) == 1 {
			value = in.PositionalID
		}
		if value == "" {
			return req, fmt.Errorf("%s %s requires %s — pass it as an argument: flexprice %s %s <%s>",
				cmd.Resource, cmd.Action, p.Name, cmd.Resource, cmd.Action, p.Name)
		}
		req.Path = strings.ReplaceAll(req.Path, "{"+p.Name+"}", url.PathEscape(value))
		delete(in.Flags, p.Name)
	}

	known := map[string]bool{}
	for _, p := range queryParams {
		known[p.Name] = true
		if v, ok := in.Flags[p.Name]; ok {
			req.Query.Set(p.Name, v)
			delete(in.Flags, p.Name)
		}
	}

	bodyFields := BodyFields(cmd)
	byName := map[string]Field{}
	for _, f := range bodyFields {
		byName[f.Name] = f
		known[f.Name] = true
	}

	body := map[string]any{}
	for k, v := range in.Data {
		body[k] = v
	}
	for name, raw := range in.Flags {
		field, ok := byName[name]
		if !ok {
			return req, unknownFlagError(name, known)
		}
		if field.Nested {
			return req, fmt.Errorf(
				"--%s is a %s and cannot be set with a flag.\n  Use --edit, or --data @file.json",
				name, field.Type)
		}
		value, err := coerce(raw, field.Type)
		if err != nil {
			return req, fmt.Errorf("--%s expects a %s, got %q", name, field.Type, raw)
		}
		body[name] = value
	}

	if len(body) > 0 {
		req.Body = body
	} else if len(bodyFields) > 0 && requiresBody(cmd.Operation.Op) {
		req.Body = map[string]any{}
	}
	return req, nil
}

func splitParameters(op *openapi3.Operation) (path, query []*openapi3.Parameter) {
	for _, ref := range op.Parameters {
		if ref.Value == nil {
			continue
		}
		switch ref.Value.In {
		case openapi3.ParameterInPath:
			path = append(path, ref.Value)
		case openapi3.ParameterInQuery:
			query = append(query, ref.Value)
		}
	}
	return path, query
}

func requiresBody(op *openapi3.Operation) bool {
	return op.RequestBody != nil && op.RequestBody.Value != nil && op.RequestBody.Value.Required
}

// BodyFields lists the top-level properties of the request body schema.
func BodyFields(cmd Command) []Field {
	op := cmd.Operation.Op
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	media := op.RequestBody.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return nil
	}
	schema := media.Schema.Value

	required := map[string]bool{}
	for _, r := range schema.Required {
		required[r] = true
	}

	var out []Field
	for name, prop := range schema.Properties {
		if prop.Value == nil {
			continue
		}
		kind := schemaType(prop.Value)
		out = append(out, Field{
			Name:     name,
			Type:     kind,
			Required: required[name],
			Nested:   kind == "object" || kind == "array",
			Doc:      prop.Value.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func schemaType(s *openapi3.Schema) string {
	switch {
	case s.Type == nil:
		if len(s.Properties) > 0 {
			return "object"
		}
		return "string"
	case s.Type.Is("array"):
		return "array"
	case s.Type.Is("object"):
		return "object"
	case s.Type.Is("integer"):
		return "integer"
	case s.Type.Is("number"):
		return "number"
	case s.Type.Is("boolean"):
		return "boolean"
	default:
		return "string"
	}
}

// Rejected client-side rather than sent as a string: the server's decoder would
// reject a type-mismatched body with a generic "Invalid request format" and no
// field name.
func coerce(raw, kind string) (any, error) {
	switch kind {
	case "integer":
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, err
		}
		return n, nil
	case "number":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return raw, nil
}

// unknownFlagError suggests the closest known field so a typo does not become a 400.
func unknownFlagError(name string, known map[string]bool) error {
	best, bestScore := "", 1<<30
	for candidate := range known {
		if d := editDistance(name, candidate); d < bestScore {
			best, bestScore = candidate, d
		}
	}
	// Only suggest when the names are genuinely close.
	if best != "" && bestScore <= 3 {
		return fmt.Errorf("unknown flag --%s\n  Did you mean --%s?", name, best)
	}
	names := make([]string, 0, len(known))
	for k := range known {
		names = append(names, k)
	}
	sort.Strings(names)
	return fmt.Errorf("unknown flag --%s\n  Available: --%s", name, strings.Join(names, ", --"))
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		copy(prev, curr)
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
