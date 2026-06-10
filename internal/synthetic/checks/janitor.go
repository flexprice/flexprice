package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
)

type Janitor struct {
	client synthetic.Client
	reg    synthetic.Registry
	maxAge time.Duration
	runID  string
}

func NewJanitor(c synthetic.Client, r synthetic.Registry, maxAge time.Duration, runID string) *Janitor {
	if maxAge == 0 {
		maxAge = 4 * time.Hour
	}
	return &Janitor{client: c, reg: r, maxAge: maxAge, runID: runID}
}

func (j *Janitor) Name() string         { return "janitor" }
func (j *Janitor) Kind() synthetic.Kind { return synthetic.KindMaintenance }

func (j *Janitor) Run(ctx context.Context) error {
	cutoff := time.Now().Add(-j.maxAge)
	for _, kind := range []string{"customer", "subscription"} {
		for _, e := range j.reg.Ephemerals(kind) {
			if e.CreatedAt.After(cutoff) {
				continue
			}
			if err := j.archive(ctx, e); err != nil {
				return fmt.Errorf("archive %s/%s: %w", kind, e.ID, err)
			}
			j.reg.ArchiveEphemeral(kind, e.ID)
		}
	}
	return nil
}

func (j *Janitor) archive(ctx context.Context, e synthetic.EphemeralEntity) error {
	switch e.Kind {
	case "customer":
		if _, err := j.client.Customers().GetByExternalID(ctx, e.ID); err != nil {
			return nil
		}
		if _, err := j.client.Customers().Delete(ctx, e.ID); err != nil {
			return err
		}
	case "subscription":
	}
	return nil
}
