package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
)

var (
	moneyTolerance    = decimal.RequireFromString("0.01")
	quantityTolerance = decimal.RequireFromString("0.0001")
)

// Context is one run's view of the world plus the mismatches it collects, so
// a scenario reports every wrong value rather than the first.
type Context struct {
	deps       Deps
	persona    *world.Persona
	scenario   string
	attrs      map[string]string
	mismatches []string
}

func NewContext(d Deps, p *world.Persona, scenarioName string) *Context {
	c := &Context{deps: d, persona: p, scenario: scenarioName, attrs: map[string]string{"scenario": scenarioName}}
	if p != nil {
		c.attrs["persona"] = p.Name()
		c.attrs["external_customer_id"] = p.ExternalID()
	}
	return c
}

func (c *Context) Persona() *world.Persona { return c.persona }
func (c *Context) World() *world.World     { return c.deps.World }
func (c *Context) Client() e2eprobe.Client { return c.deps.Client }
func (c *Context) Ledger() *ledger.Ledger  { return c.deps.Ledger }
func (c *Context) Inbox() *inbox.Inbox     { return c.deps.Inbox }
func (c *Context) Suite() *suite.Suite     { return c.deps.Suite }
func (c *Context) Logger() *logger.Logger  { return c.deps.Logger }
func (c *Context) RunID() string           { return c.deps.RunID }
func (c *Context) Tick() time.Duration     { return c.deps.Tick }
func (c *Context) Now() time.Time          { return c.deps.now() }
func (c *Context) StartedAt() time.Time    { return c.deps.StartedAt }
func (c *Context) Scenario() string        { return c.scenario }

// Clone stamps a throwaway customer from the bound persona.
func (c *Context) Clone(ctx context.Context) (*world.Ephemeral, error) {
	if c.persona == nil {
		return nil, errors.New("scenario: a tenant-level context has no persona to clone")
	}
	return c.deps.World.Clone(ctx, c.persona, c.scenario)
}

// Money compares amounts to the cent.
func (c *Context) Money(label string, want, got decimal.Decimal) {
	if want.Sub(got).Abs().GreaterThan(moneyTolerance) {
		c.mismatches = append(c.mismatches, fmt.Sprintf("%s: want %s, got %s", label, want.StringFixed(2), got.StringFixed(2)))
	}
}

// Quantity compares usage at full precision.
func (c *Context) Quantity(label string, want, got decimal.Decimal) {
	if want.Sub(got).Abs().GreaterThan(quantityTolerance) {
		c.mismatches = append(c.mismatches, fmt.Sprintf("%s: want %s, got %s", label, want.String(), got.String()))
	}
}

func (c *Context) Count(label string, want, got int) {
	if want != got {
		c.mismatches = append(c.mismatches, fmt.Sprintf("%s: want %d, got %d", label, want, got))
	}
}

func (c *Context) Truth(label string, ok bool) {
	if !ok {
		c.mismatches = append(c.mismatches, label)
	}
}

func (c *Context) Attr(k, v string) {
	c.attrs[k] = v
}

func (c *Context) copyAttrs() map[string]string {
	out := make(map[string]string, len(c.attrs))
	for k, v := range c.attrs {
		out[k] = v
	}
	return out
}

// Result passes when nothing mismatched, else fails at step with every line.
func (c *Context) Result(step string) e2eprobe.Result {
	if len(c.mismatches) == 0 {
		r := e2eprobe.Pass()
		r.Attrs = c.copyAttrs()
		return r
	}
	return e2eprobe.Mismatch(step, c.mismatches, c.copyAttrs())
}

func (c *Context) Skip(step, reason string) e2eprobe.Result {
	r := e2eprobe.Skip(step, reason)
	r.Attrs = c.copyAttrs()
	return r
}

func (c *Context) Fail(step string, err error) e2eprobe.Result {
	return e2eprobe.Fail(step, err, c.copyAttrs())
}
