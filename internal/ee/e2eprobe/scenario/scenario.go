// Package scenario is the framework scenarios run in: the interface they
// implement, the context they get, the catalog that names them and the runner
// that schedules every binding and feeds results to the incident tracker.
package scenario

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/logger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"go.opentelemetry.io/otel/trace"
)

// Scenario is one check. Requires lists the traits the bound persona must
// carry; an empty list means the scenario is tenant-level.
type Scenario interface {
	Name() string
	Kind() e2eprobe.Kind
	Requires() []e2eprobe.Trait
	Run(ctx context.Context, sc *Context) e2eprobe.Result
}

// Deps is everything a scenario may reach; the runner hands it to every Context.
type Deps struct {
	Client    e2eprobe.Client
	World     *world.World
	Ledger    *ledger.Ledger
	Inbox     *inbox.Inbox
	Suite     *suite.Suite
	Logger    *logger.Logger
	Tracer    trace.Tracer
	RunID     string
	Tick      time.Duration
	Now       func() time.Time
	StartedAt time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
