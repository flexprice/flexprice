package scenarios

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestCancel_SkipsWhenNothingOldEnough(t *testing.T) {
	d := newTestDeps(t)
	s := newCancellation(d.Deps)
	s.poll = fastPoll
	if r := run(t, d, s, "baseline-a"); r.Status != e2eprobe.StatusSkip || r.Step != "nothing_old_enough" {
		t.Fatalf("empty registry: %+v", r)
	}
	cloneOf(t, d, "baseline-a", "lifecycle")
	d.advance(10 * time.Minute)
	if r := run(t, d, s, "baseline-a"); r.Status != e2eprobe.StatusSkip || r.Step != "nothing_old_enough" {
		t.Fatalf("ten-minute-old clone: %+v", r)
	}
	if len(d.fc.Subscription.Cancelled) != 0 || len(d.fc.Customer.Deleted) != 0 {
		t.Error("a skipped run must not touch the tenant")
	}
}

func TestCancel_DeletesOldestAndArchives(t *testing.T) {
	d := newTestDeps(t)
	oldest := cloneOf(t, d, "baseline-a", "lifecycle")
	d.advance(5 * time.Minute)
	newer := cloneOf(t, d, "baseline-a", "tax-preview")
	d.advance(30 * time.Minute)
	s := newCancellation(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if !slices.Contains(d.fc.Subscription.Cancelled, oldest.SubscriptionID()) || slices.Contains(d.fc.Subscription.Cancelled, newer.SubscriptionID()) {
		t.Errorf("cancelled = %v, want only %s", d.fc.Subscription.Cancelled, oldest.SubscriptionID())
	}
	if !slices.Contains(d.fc.Customer.Deleted, oldest.CustomerID()) || len(d.fc.Customer.Deleted) != 1 {
		t.Errorf("deleted = %v, want only %s", d.fc.Customer.Deleted, oldest.CustomerID())
	}
	reg := d.World.Ephemerals()
	if reg.Count("customer") != 1 || reg.List("customer")[0].ID != newer.ExternalID() {
		t.Errorf("customers left = %v", reg.List("customer"))
	}
	if reg.Count("subscription") != 1 || reg.List("subscription")[0].ID != newer.SubscriptionID() {
		t.Errorf("subscriptions left = %v", reg.List("subscription"))
	}
	if r.Attrs["ephemeral_external_id"] != oldest.ExternalID() || r.Attrs["ephemeral_customer_id"] != oldest.CustomerID() || r.Attrs["subscriptions"] != "1" {
		t.Errorf("attrs = %v", r.Attrs)
	}
}

func TestCancel_ArchivesVanishedCustomer(t *testing.T) {
	d := newTestDeps(t)
	eph := cloneOf(t, d, "baseline-a", "lifecycle")
	delete(d.fc.Customer.Items, eph.ExternalID())
	d.advance(30 * time.Minute)
	s := newCancellation(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusSkip || r.Step != "already_gone" {
		t.Fatalf("result = %+v", r)
	}
	if d.World.Ephemerals().Count("customer") != 0 {
		t.Error("a vanished customer must leave the registry")
	}
}

func TestCancel_MismatchWhenStatusNeverFlips(t *testing.T) {
	d := newTestDeps(t)
	eph := cloneOf(t, d, "baseline-a", "lifecycle")
	d.fc.Subscription.CancelStatus = types.SubscriptionStatusActive
	d.advance(30 * time.Minute)
	s := newCancellation(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "cancel" || r.Infra || len(r.Mismatches) != 1 || !strings.Contains(r.Mismatches[0], eph.SubscriptionID()) {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Customer.Deleted) != 0 || d.World.Ephemerals().Count("customer") != 1 {
		t.Error("a customer whose subscription is still open must be left for the janitor")
	}
}

func TestCancel_StatusReadErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	cloneOf(t, d, "baseline-a", "lifecycle")
	d.fc.Subscription.GetErr = probetest.ServerError(500)
	d.advance(30 * time.Minute)
	s := newCancellation(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "await_cancelled" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Customer.Deleted) != 0 || d.World.Ephemerals().Count("customer") != 1 {
		t.Error("an unconfirmed cancellation must leave the customer for the janitor")
	}
}

func TestCancel_CancelErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	cloneOf(t, d, "baseline-a", "lifecycle")
	d.fc.Subscription.CancelErr = probetest.ServerError(500)
	d.advance(30 * time.Minute)
	s := newCancellation(d.Deps)
	s.poll = fastPoll
	r := run(t, d, s, "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "cancel_subscription" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(d.fc.Customer.Deleted) != 0 {
		t.Error("the customer must not be deleted while its subscription is open")
	}
}
