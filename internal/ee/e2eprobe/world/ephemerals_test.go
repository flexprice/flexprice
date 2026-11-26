package world

import (
	"testing"
	"time"
)

func TestEphemerals_RegisterListArchiveCount(t *testing.T) {
	e := NewEphemerals()
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	e.Register("customer", "e2eprobe-eph-a", at)
	e.Register("customer", "e2eprobe-eph-b", at.Add(time.Minute))
	e.Register("subscription", "sub_1", at)
	if e.Count("customer") != 2 || e.Count("subscription") != 1 || e.Count("wallet") != 0 {
		t.Fatalf("counts wrong: %d %d %d", e.Count("customer"), e.Count("subscription"), e.Count("wallet"))
	}
	list := e.List("customer")
	if len(list) != 2 || list[0].ID != "e2eprobe-eph-a" || !list[0].CreatedAt.Equal(at) || list[1].ID != "e2eprobe-eph-b" {
		t.Errorf("list = %+v", list)
	}
	e.Archive("customer", "e2eprobe-eph-a")
	if e.Count("customer") != 1 || e.List("customer")[0].ID != "e2eprobe-eph-b" {
		t.Errorf("archive failed: %+v", e.List("customer"))
	}
	e.Archive("customer", "missing")
	if e.Count("customer") != 1 {
		t.Error("archiving an unknown id must be a no-op")
	}
}

func TestEphemerals_RegisteredIsMonotonic(t *testing.T) {
	e := NewEphemerals()
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	e.Register("customer", "a", at)
	e.Register("customer", "b", at)
	e.Archive("customer", "a")
	if e.Registered("customer") != 2 || e.Count("customer") != 1 {
		t.Errorf("registered=%d count=%d", e.Registered("customer"), e.Count("customer"))
	}
}
