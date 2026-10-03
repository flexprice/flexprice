package archive

import (
	"testing"
	"time"
)

func TestPartitionNameAndRange(t *testing.T) {
	name := PartitionName(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	if name != "activity_logs_2026_10" {
		t.Fatal(name)
	}
	s, e, ok := PartitionRange(name)
	if !ok || s.Day() != 1 || s.Month() != 10 || e.Month() != 11 {
		t.Fatalf("%v %v %v", s, e, ok)
	}
	if _, _, ok := PartitionRange("activity_logs"); ok {
		t.Fatal("parent must not parse")
	}
}

func TestPartitionRangeRejectsNonCanonicalNames(t *testing.T) {
	for _, name := range []string{
		"activity_logs_2026_13",
		"activity_logs_2026_00",
		"activity_logs_2026_5",
		"activity_logs_2026_05; DROP TABLE tenants",
		"activity_logs_2026_05_old",
		"other_2026_05",
	} {
		if _, _, ok := PartitionRange(name); ok {
			t.Errorf("%q must not parse", name)
		}
	}
}

func TestArchivable(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	got := Archivable([]string{"activity_logs_2026_05", "activity_logs_2026_06", "activity_logs_2026_07", "activity_logs_2026_10"}, now, 90)
	if len(got) != 2 || got[0] != "activity_logs_2026_05" || got[1] != "activity_logs_2026_06" {
		t.Fatalf("want May and June (range end before Jul 5), got %v", got)
	}
}

func TestDefaultPartitionNeverArchivable(t *testing.T) {
	if _, _, ok := PartitionRange(DefaultPartition); ok {
		t.Fatal("the default partition must not parse as a monthly range")
	}
	for _, hotDays := range []int{0, 90, -100000} {
		got := Archivable([]string{DefaultPartition, "activity_logs_2026_05"}, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), hotDays)
		for _, p := range got {
			if p == DefaultPartition {
				t.Fatalf("hotDays=%d: the default partition must never be archivable, got %v", hotDays, got)
			}
		}
	}
}

func TestNeededAhead(t *testing.T) {
	got := NeededAhead(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), 3)
	if len(got) != 4 || got[0] != "activity_logs_2026_10" || got[3] != "activity_logs_2027_01" {
		t.Fatal(got)
	}
}

func TestCreatePartitionSQL(t *testing.T) {
	got := CreatePartitionSQL("activity_logs_2026_12")
	want := `CREATE TABLE IF NOT EXISTS activity_logs_2026_12 PARTITION OF activity_logs FOR VALUES FROM ('2026-12-01') TO ('2027-01-01')`
	if got != want {
		t.Fatalf("got %s", got)
	}
}
