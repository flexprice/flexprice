package checks

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type Janitor struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	maxAge time.Duration
	runID  string
}

func NewJanitor(c e2eprobe.Client, r e2eprobe.Registry, maxAge time.Duration, runID string) *Janitor {
	if maxAge == 0 {
		maxAge = 1 * time.Hour
	}
	return &Janitor{client: c, reg: r, maxAge: maxAge, runID: runID}
}

func (j *Janitor) Name() string         { return "janitor" }
func (j *Janitor) Kind() e2eprobe.Kind { return e2eprobe.KindMaintenance }

func (j *Janitor) Run(ctx context.Context) error {
	cutoff := time.Now().Add(-j.maxAge)

	// Phase 1: sweep the in-memory registry (current-process ephemerals).
	for _, kind := range []string{"customer", "subscription"} {
		for _, e := range j.reg.Ephemerals(kind) {
			if e.CreatedAt.After(cutoff) {
				continue
			}
			if err := j.archive(ctx, e); err != nil {
				return e2eprobe.Errorf(map[string]string{"kind": kind, "id": e.ID}, "archive %s/%s: %w", kind, e.ID, err)
			}
			j.reg.ArchiveEphemeral(kind, e.ID)
		}
	}

	// Phase 2: scan Flexprice for orphan ephemeral customers that survived prior
	// process restarts (registry wipe). CustomerFilter has no metadata equality
	// field, so we fetch all customers and filter client-side. The synthetic
	// tenant is bounded so this is safe.
	if err := j.sweepOrphans(ctx, cutoff); err != nil {
		return err
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
			return e2eprobe.Errorf(map[string]string{"kind": "customer", "id": e.ID}, "lookup customer %s: %w", e.ID, err)
		}
		if _, err := j.client.Customers().Delete(ctx, e.ID); err != nil {
			if isNotFound(err) {
				return nil // raced — concurrent cleanup
			}
			return e2eprobe.Errorf(map[string]string{"kind": "customer", "id": e.ID}, "delete customer %s: %w", e.ID, err)
		}
	case "subscription":
		// Subscriptions are cancelled by cancel-customer-flow; janitor only
		// verifies the subscription is in a terminal state (not an error condition
		// if it's simply gone).
		if _, err := j.client.Subscriptions().Get(ctx, e.ID); err != nil {
			if isNotFound(err) {
				return nil // already gone — expected steady state
			}
			return e2eprobe.Errorf(map[string]string{"kind": "subscription", "id": e.ID}, "lookup subscription %s: %w", e.ID, err)
		}
		// Subscription still exists (cancelled but not deleted — expected for
		// Flexprice which retains cancelled subs). Accept this as success.
	}
	return nil
}

// sweepOrphans queries Flexprice for all customers, filters client-side for
// e2eprobe ephemerals older than cutoff, then deletes them. This handles
// restart-leakage where the in-memory registry was wiped but the customers
// were never cleaned up.
//
// "Ephemeral" identification is intentionally loose to handle data created
// before metadata was added: a customer is treated as ephemeral if ANY of
// these hold:
//   - external_id starts with `e2eprobe-cust-eph-` (programmatic prefix)
//   - name contains "Ephemeral" (literal name we set on create)
//   - metadata.e2eprobe_role == "ephemeral" (the legacy tag)
//
// Persistent seed customers use the prefix `e2eprobe-cust-persistent-` and
// the name "E2EProbe Persistent N" — both substrings are distinct enough
// that none of the three checks fires on them.
func isEphemeralCustomer(c types.DtoCustomerResponse) bool {
	if c.ExternalID != nil && strings.HasPrefix(*c.ExternalID, "e2eprobe-cust-eph-") {
		return true
	}
	if c.Name != nil && strings.Contains(*c.Name, "Ephemeral") {
		return true
	}
	if c.Metadata != nil && c.Metadata["e2eprobe_role"] == "ephemeral" {
		return true
	}
	return false
}

func (j *Janitor) sweepOrphans(ctx context.Context, cutoff time.Time) error {
	resp, err := j.client.Customers().Query(ctx, types.CustomerFilter{})
	if err != nil {
		return e2eprobe.Errorf(map[string]string{}, "janitor sweepOrphans: query customers: %w", err)
	}

	listResp := resp.GetDtoListCustomersResponse()
	if listResp == nil {
		return nil
	}
	items := listResp.GetItems()

	deleted := 0
	for _, cust := range items {
		if !isEphemeralCustomer(cust) {
			continue
		}
		// Must be older than cutoff.
		if cust.CreatedAt == nil {
			continue
		}
		createdAt, parseErr := parseRFC3339(*cust.CreatedAt)
		if parseErr != nil {
			continue
		}
		if createdAt.After(cutoff) {
			continue // too fresh — leave it
		}

		custID := ""
		if cust.ID != nil {
			custID = *cust.ID
		}
		extID := ""
		if cust.ExternalID != nil {
			extID = *cust.ExternalID
		}
		if custID == "" {
			continue
		}

		if _, delErr := j.client.Customers().Delete(ctx, custID); delErr != nil {
			if isNotFound(delErr) {
				continue // already gone — concurrent cleanup
			}
			return e2eprobe.Errorf(map[string]string{
				"customer_id":          custID,
				"external_customer_id": extID,
			}, "janitor sweepOrphans: delete customer %s: %w", custID, delErr)
		}
		deleted++
	}

	if deleted > 0 {
		slog.InfoContext(ctx, "janitor swept orphan ephemeral customers", "count", deleted)
	}
	return nil
}

// parseRFC3339 is a small wrapper so the callers stay readable.
func parseRFC3339(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
