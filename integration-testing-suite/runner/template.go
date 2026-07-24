package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"text/template"
	"time"
)

// RenderCtx is the data available to {{ }} templates in journey YAML:
//
//	.vars.<name>        journey-level constants
//	.steps.<id>.<key>   values captured by earlier steps
//	.env.<VAR>          process environment
//	.run.id / .run.ts   unique run id (10 hex chars) / epoch millis
//	.target.name        current target label
//	.iter               0-based iteration index inside repeat steps
type RenderCtx struct {
	data map[string]any
}

// NewRenderCtx builds the template context for one journey run.
func NewRenderCtx(vars map[string]any, targetName string) *RenderCtx {
	env := map[string]any{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	if vars == nil {
		vars = map[string]any{}
	}
	return &RenderCtx{data: map[string]any{
		"vars":  vars,
		"steps": map[string]any{},
		"env":   env,
		"run": map[string]any{
			"id": randomHex(5),
			"ts": time.Now().UnixMilli(),
		},
		"target": map[string]any{"name": targetName},
		"iter":   0,
	}}
}

// SetIter sets the repeat-iteration index.
func (c *RenderCtx) SetIter(i int) { c.data["iter"] = i }

// Captures returns the mutable step-captures map.
func (c *RenderCtx) Captures() map[string]any {
	return c.data["steps"].(map[string]any)
}

// SetCaptures stores a step's captured values under .steps.<id>.
func (c *RenderCtx) SetCaptures(stepID string, values map[string]any) {
	if stepID == "" {
		return
	}
	c.Captures()[stepID] = values
}

// RunID returns the unique id for this journey run.
func (c *RenderCtx) RunID() string {
	return c.data["run"].(map[string]any)["id"].(string)
}

var tmplFuncs = template.FuncMap{
	// now returns the current time in RFC3339 (UTC).
	"now": func() string { return time.Now().UTC().Format(time.RFC3339) },
	// nowAdd shifts now by a Go duration, e.g. (nowAdd "-1h").
	"nowAdd": func(d string) (string, error) {
		dur, err := time.ParseDuration(d)
		if err != nil {
			return "", fmt.Errorf("nowAdd: %w", err)
		}
		return time.Now().UTC().Add(dur).Format(time.RFC3339), nil
	},
	// uuid returns a random RFC-4122-shaped identifier.
	"uuid": func() string {
		b := randomBytes(16)
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	},
	// randInt returns a random integer in [min, max].
	"randInt": func(min, max int) (int, error) {
		if max < min {
			return 0, fmt.Errorf("randInt: max < min")
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
		if err != nil {
			return 0, err
		}
		return min + int(n.Int64()), nil
	},
}

// parseTemplate compiles a template string (used by validation too).
func parseTemplate(s string) (*template.Template, error) {
	return template.New("t").Funcs(tmplFuncs).Option("missingkey=error").Parse(s)
}

// ErrMissingDependency marks a template that referenced a capture which was
// never produced (its step failed or was skipped). The engine converts this
// into a SKIP rather than a failure.
type ErrMissingDependency struct{ inner error }

func (e *ErrMissingDependency) Error() string { return e.inner.Error() }

// RenderString renders one template string. The marker form "{{= expr }}"
// JSON-parses the rendered output back into a native type (number/bool/etc.).
func (c *RenderCtx) RenderString(s string) (any, error) {
	coerce := false
	if strings.HasPrefix(s, "{{=") {
		coerce = true
		s = "{{" + s[3:]
	}
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	t, err := parseTemplate(s)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, c.data); err != nil {
		if strings.Contains(err.Error(), "map has no entry for key") {
			return nil, &ErrMissingDependency{inner: err}
		}
		return nil, err
	}
	out := sb.String()
	if coerce {
		var v any
		if jsonErr := json.Unmarshal([]byte(out), &v); jsonErr == nil {
			return v, nil
		}
		// Not valid JSON — fall back to the rendered string.
	}
	return out, nil
}

// Render recursively renders every string in a nested document.
func (c *RenderCtx) Render(doc any) (any, error) {
	switch v := doc.(type) {
	case string:
		return c.RenderString(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			r, err := c.Render(val)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			r, err := c.Render(val)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return doc, nil
	}
}

// RenderStringMap renders a map[string]string (http query/headers).
func (c *RenderCtx) RenderStringMap(m map[string]string) (map[string]string, error) {
	if m == nil {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		r, err := c.RenderString(v)
		if err != nil {
			return nil, err
		}
		out[k] = fmt.Sprintf("%v", r)
	}
	return out, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable; fall back to time-derived bytes.
		ts := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(ts >> (8 * (i % 8)))
		}
	}
	return b
}

func randomHex(n int) string {
	return hex.EncodeToString(randomBytes(n))
}
