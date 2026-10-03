// Package archive holds the pure helpers behind the activity_logs archiver:
// monthly partition naming and range math, and the Parquet row shape.
package archive

import (
	"fmt"
	"time"
)

const prefix = "activity_logs_"

// DefaultPartition is the migration's DEFAULT partition, a safety net for rows
// whose month has no partition yet. It is never archived, exported or dropped.
const DefaultPartition = "activity_logs_default"

// PartitionName returns the monthly partition holding t, e.g. activity_logs_2026_10.
func PartitionName(t time.Time) string {
	return fmt.Sprintf("%s%04d_%02d", prefix, t.Year(), int(t.Month()))
}

// PartitionRange parses a monthly partition name into its [start, end) range.
// Only the canonical form PartitionName produces is accepted, so a parsed name
// is safe to interpolate into DDL as an identifier.
func PartitionRange(name string) (time.Time, time.Time, bool) {
	if name == DefaultPartition {
		return time.Time{}, time.Time{}, false
	}
	var y, m int
	if _, err := fmt.Sscanf(name, prefix+"%d_%d", &y, &m); err != nil || m < 1 || m > 12 {
		return time.Time{}, time.Time{}, false
	}
	start := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	if PartitionName(start) != name {
		return time.Time{}, time.Time{}, false
	}
	return start, start.AddDate(0, 1, 0), true
}

// Archivable returns partitions whose range ended before now - hotDays, oldest
// first. The DEFAULT partition is never archivable.
func Archivable(partitions []string, now time.Time, hotDays int) []string {
	cutoff := now.AddDate(0, 0, -hotDays)
	var out []string
	for _, p := range partitions {
		if p == DefaultPartition {
			continue
		}
		if _, end, ok := PartitionRange(p); ok && end.Before(cutoff) {
			out = append(out, p)
		}
	}
	return out
}

// NeededAhead lists the current month plus the next `months` partitions.
func NeededAhead(now time.Time, months int) []string {
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]string, 0, months+1)
	for i := 0; i <= months; i++ {
		out = append(out, PartitionName(cur.AddDate(0, i, 0)))
	}
	return out
}

// CreatePartitionSQL returns idempotent DDL attaching the named monthly partition.
func CreatePartitionSQL(name string) string {
	start, end, _ := PartitionRange(name)
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s PARTITION OF activity_logs FOR VALUES FROM ('%s') TO ('%s')`,
		name, start.Format("2006-01-02"), end.Format("2006-01-02"))
}
