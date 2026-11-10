package scheduledtask

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

func ip(v int) *int { return &v }

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWindow(t *testing.T) {
	tests := []struct {
		name      string
		interval  types.ScheduledTaskInterval
		schedule  *types.ExportSchedule
		start     string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{"daily kolkata", types.ScheduledTaskIntervalDaily, &types.ExportSchedule{Minute: 30, Timezone: "Asia/Kolkata"}, "2026-11-09T19:00:00Z", "2026-11-08T18:30:00Z", "2026-11-09T18:30:00Z", false},
		{"hourly minute 15", types.ScheduledTaskIntervalHourly, &types.ExportSchedule{Minute: 15, Timezone: "UTC"}, "2026-11-10T10:15:00Z", "2026-11-10T09:00:00Z", "2026-11-10T10:00:00Z", false},
		{"hourly minute 0", types.ScheduledTaskIntervalHourly, &types.ExportSchedule{Timezone: "UTC"}, "2026-11-10T10:00:00Z", "2026-11-10T08:00:00Z", "2026-11-10T09:00:00Z", false},
		{"weekly wednesday", types.ScheduledTaskIntervalWeekly, &types.ExportSchedule{Hour: 9, DayOfWeek: ip(3), Timezone: "UTC"}, "2026-11-11T09:00:00Z", "2026-11-02T00:00:00Z", "2026-11-09T00:00:00Z", false},
		{"monthly kolkata", types.ScheduledTaskIntervalMonthly, &types.ExportSchedule{Hour: 2, DayOfMonth: ip(1), Timezone: "Asia/Kolkata"}, "2026-11-30T20:30:00Z", "2026-10-31T18:30:00Z", "2026-11-30T18:30:00Z", false},
		{"daily LA fall back", types.ScheduledTaskIntervalDaily, &types.ExportSchedule{Hour: 1, Timezone: "America/Los_Angeles"}, "2026-11-02T09:00:00Z", "2026-11-01T07:00:00Z", "2026-11-02T08:00:00Z", false},
		{"manual", types.ScheduledTaskIntervalManual, nil, "2026-11-10T00:10:00Z", "2026-11-08T00:00:00Z", "2026-11-09T00:00:00Z", false},
		{"catch-up", types.ScheduledTaskIntervalDaily, &types.ExportSchedule{Hour: 9, Timezone: "UTC"}, "2026-11-01T09:00:00Z", "2026-10-31T00:00:00Z", "2026-11-01T00:00:00Z", false},
		{"daily nil schedule", types.ScheduledTaskIntervalDaily, nil, "2026-11-10T00:00:00Z", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := Window(tt.interval, tt.schedule, mustTime(t, tt.start))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !start.Equal(mustTime(t, tt.wantStart)) || !end.Equal(mustTime(t, tt.wantEnd)) {
				t.Fatalf("got %s..%s want %s..%s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), tt.wantStart, tt.wantEnd)
			}
			if start.Location() != time.UTC || end.Location() != time.UTC {
				t.Fatal("expected UTC instants")
			}
		})
	}
}

func TestCron(t *testing.T) {
	tests := []struct {
		name     string
		interval types.ScheduledTaskInterval
		schedule *types.ExportSchedule
		want     string
		wantErr  bool
	}{
		{"daily", types.ScheduledTaskIntervalDaily, &types.ExportSchedule{Hour: 9, Minute: 30, Timezone: "UTC"}, "30 9 * * *", false},
		{"weekly", types.ScheduledTaskIntervalWeekly, &types.ExportSchedule{Hour: 9, DayOfWeek: ip(1), Timezone: "UTC"}, "0 9 * * 1", false},
		{"monthly", types.ScheduledTaskIntervalMonthly, &types.ExportSchedule{Hour: 2, DayOfMonth: ip(1), Timezone: "UTC"}, "0 2 1 * *", false},
		{"hourly", types.ScheduledTaskIntervalHourly, &types.ExportSchedule{Minute: 15, Timezone: "UTC"}, "15 * * * *", false},
		{"manual", types.ScheduledTaskIntervalManual, nil, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Cron(tt.interval, tt.schedule)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
