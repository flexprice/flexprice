package scenario

import (
	"fmt"
	"sort"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

// Builder makes a fresh scenario instance for one binding.
type Builder func(d Deps) Scenario

type entry struct {
	info  suite.ScenarioInfo
	build Builder
}

// Catalog is every scenario the binary knows, by name.
type Catalog struct {
	entries map[string]entry
}

func NewCatalog() *Catalog {
	return &Catalog{entries: map[string]entry{}}
}

// Register adds a scenario; a duplicate name is a programming error.
func (c *Catalog) Register(info suite.ScenarioInfo, b Builder) {
	if _, dup := c.entries[info.Name]; dup {
		panic(fmt.Sprintf("scenario: %q registered twice", info.Name))
	}
	c.entries[info.Name] = entry{info: info, build: b}
}

func (c *Catalog) Scenario(name string) (suite.ScenarioInfo, bool) {
	e, ok := c.entries[name]
	return e.info, ok
}

func (c *Catalog) Profile(name string) (int, bool) {
	p, ok := ledger.Lookup(name)
	return p.Size(), ok
}

func (c *Catalog) Build(name string, d Deps) (Scenario, error) {
	e, ok := c.entries[name]
	if !ok {
		return nil, fmt.Errorf("scenario: unknown scenario %q", name)
	}
	return e.build(d), nil
}

func (c *Catalog) Names() []string {
	out := make([]string, 0, len(c.entries))
	for name := range c.entries {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
