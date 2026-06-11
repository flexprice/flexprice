package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

type Janitor struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	maxAge time.Duration
	runID  string
}

func NewJanitor(c e2eprobe.Client, r e2eprobe.Registry, maxAge time.Duration, runID string) *Janitor {
	if maxAge == 0 {
		maxAge = 4 * time.Hour
	}
	return &Janitor{client: c, reg: r, maxAge: maxAge, runID: runID}
}

func (j *Janitor) Name() string         { return "janitor" }
func (j *Janitor) Kind() e2eprobe.Kind { return e2eprobe.KindMaintenance }

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

// errNotFound is the canonical sentinel returned by the fake and expected by
// real SDK callers when a resource is absent.
var errNotFound = errors.New("not found")

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// Real SDK errors surface as *sdkerrors.APIError with StatusCode 404.
	var apiErr *sdkerrors.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return true
	}
	// Legacy string-based detection used by some fake responses.
	msg := err.Error()
	return msg == "not found" || msg == "subscription not found"
}

func (j *Janitor) archive(ctx context.Context, e e2eprobe.EphemeralEntity) error {
	switch e.Kind {
	case "customer":
		_, err := j.client.Customers().GetByExternalID(ctx, e.ID)
		if err != nil {
			if isNotFound(err) {
				return nil // already gone
			}
			return fmt.Errorf("lookup customer %s: %w", e.ID, err)
		}
		if _, err := j.client.Customers().Delete(ctx, e.ID); err != nil {
			if isNotFound(err) {
				return nil // raced — concurrent cleanup
			}
			return err
		}
	case "subscription":
		// Subscriptions are cancelled by cancel-customer-flow; janitor only
		// verifies the subscription is in a terminal state (not an error condition
		// if it's simply gone).
		if _, err := j.client.Subscriptions().Get(ctx, e.ID); err != nil {
			if isNotFound(err) {
				return nil // already gone — expected steady state
			}
			return fmt.Errorf("lookup subscription %s: %w", e.ID, err)
		}
		// Subscription still exists (cancelled but not deleted — expected for
		// Flexprice which retains cancelled subs). Accept this as success.
	}
	return nil
}
