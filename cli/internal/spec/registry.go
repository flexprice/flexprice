package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/goccy/go-yaml"

	specdata "github.com/flexprice/cli/spec"
)

// commandsFile is the on-disk shape of commands.yaml. A resource maps action
// names to operationIds; "columns" is handled separately because it is not an action.
type commandsFile struct {
	Resources map[string]map[string]any `yaml:"resources"`
	Exclude   []string                  `yaml:"exclude"`
}

type Command struct {
	Resource  string
	Action    string
	Operation Operation
	Derived   bool // true when auto-derived rather than curated
}

type Registry struct {
	commands map[string]map[string]Command // resource -> action -> command
	columns  map[string][]string
	excluded []string
	warnings []string
}

func NewRegistry(doc *openapi3.T) (*Registry, error) {
	return newRegistry(doc, specdata.Commands)
}

func newRegistry(doc *openapi3.T, raw []byte) (*Registry, error) {
	var file commandsFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse commands.yaml: %w", err)
	}

	ops := map[string]Operation{}
	for _, op := range Operations(doc) {
		ops[op.ID] = op
	}

	reg := &Registry{
		commands: map[string]map[string]Command{},
		columns:  map[string][]string{},
		excluded: file.Exclude,
	}

	excluded := map[string]bool{}
	for _, id := range file.Exclude {
		if _, ok := ops[id]; !ok {
			return nil, fmt.Errorf("commands.yaml excludes %q, which is not an operation in the spec", id)
		}
		excluded[id] = true
	}

	mapped := map[string]bool{}
	for rawResource, actions := range file.Resources {
		// Normalize casing so "customers" and "Customers" collide instead of
		// silently coexisting as two resources.
		resource := strings.ToLower(rawResource)
		for action, target := range actions {
			if action == "columns" {
				reg.columns[resource] = toStrings(target)
				continue
			}
			id, ok := target.(string)
			if !ok {
				return nil, fmt.Errorf("commands.yaml: %s.%s must be an operationId string", resource, action)
			}
			op, ok := ops[id]
			if !ok {
				return nil, fmt.Errorf("commands.yaml maps %s %s to %q, which is not an operation in the spec", resource, action, id)
			}
			if err := reg.add(Command{Resource: resource, Action: action, Operation: op}); err != nil {
				return nil, err
			}
			mapped[id] = true
		}
	}

	// Default-allow: anything unmapped gets a derived name and a warning. A strict
	// gate would tax every backend PR that adds an endpoint. Design doc §5.
	for _, op := range Operations(doc) {
		if mapped[op.ID] || excluded[op.ID] {
			continue
		}
		resource, action := DeriveName(op.Tag, op.ID)
		if err := reg.add(Command{Resource: resource, Action: action, Operation: op, Derived: true}); err != nil {
			return nil, err
		}
		reg.warnings = append(reg.warnings,
			fmt.Sprintf("operation %q is unmapped; using derived name `flexprice %s %s`", op.ID, resource, action))
	}

	sort.Strings(reg.warnings)
	return reg, nil
}

func (r *Registry) add(c Command) error {
	if _, ok := r.commands[c.Resource]; !ok {
		r.commands[c.Resource] = map[string]Command{}
	}
	if existing, ok := r.commands[c.Resource][c.Action]; ok {
		return fmt.Errorf("command collision: %s %s maps to both %q and %q",
			c.Resource, c.Action, existing.Operation.ID, c.Operation.ID)
	}
	r.commands[c.Resource][c.Action] = c
	return nil
}

func (r *Registry) Lookup(resource, action string) (Command, bool) {
	c, ok := r.commands[resource][action]
	return c, ok
}

func (r *Registry) Resources() []string {
	out := make([]string, 0, len(r.commands))
	for name := range r.commands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Actions(resource string) []string {
	out := make([]string, 0, len(r.commands[resource]))
	for a := range r.commands[resource] {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Commands() []Command {
	var out []Command
	for _, actions := range r.commands {
		for _, c := range actions {
			out = append(out, c)
		}
	}
	return out
}

func (r *Registry) Columns(resource string) []string { return r.columns[resource] }
func (r *Registry) Excluded() []string               { return r.excluded }
func (r *Registry) Warnings() []string               { return r.warnings }

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// Pure function of tag and operationId, whose stability is already an SDK
// contract, so derived names are just as stable.
func DeriveName(tag, operationID string) (resource, action string) {
	resource = kebab(tag)
	action = kebab(operationID)
	return resource, action
}

func kebab(s string) string {
	s = camelBoundary.ReplaceAllString(s, "${1}-${2}")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return strings.ToLower(s)
}

func toStrings(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, i := range items {
		if s, ok := i.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
