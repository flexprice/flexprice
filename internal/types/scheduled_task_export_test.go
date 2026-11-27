package types

import (
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

func intPtr(v int) *int { return &v }

func TestExportScheduleValidate(t *testing.T) {
	tests := []struct {
		name     string
		interval ScheduledTaskInterval
		s        ExportSchedule
		wantErr  bool
	}{
		{"daily ok", ScheduledTaskIntervalDaily, ExportSchedule{Hour: 9, Minute: 30, Timezone: "Asia/Kolkata"}, false},
		{"hourly ok", ScheduledTaskIntervalHourly, ExportSchedule{Hour: 0, Minute: 15, Timezone: "UTC"}, false},
		{"weekly missing dow", ScheduledTaskIntervalWeekly, ExportSchedule{Timezone: "UTC"}, true},
		{"weekly dow 7", ScheduledTaskIntervalWeekly, ExportSchedule{DayOfWeek: intPtr(7), Timezone: "UTC"}, true},
		{"weekly dow 3", ScheduledTaskIntervalWeekly, ExportSchedule{DayOfWeek: intPtr(3), Timezone: "UTC"}, false},
		{"monthly dom 29", ScheduledTaskIntervalMonthly, ExportSchedule{DayOfMonth: intPtr(29), Timezone: "UTC"}, true},
		{"monthly dom 28", ScheduledTaskIntervalMonthly, ExportSchedule{DayOfMonth: intPtr(28), Timezone: "UTC"}, false},
		{"daily with dow", ScheduledTaskIntervalDaily, ExportSchedule{DayOfWeek: intPtr(1), Timezone: "UTC"}, true},
		{"hour 24", ScheduledTaskIntervalDaily, ExportSchedule{Hour: 24, Timezone: "UTC"}, true},
		{"minute 60", ScheduledTaskIntervalDaily, ExportSchedule{Minute: 60, Timezone: "UTC"}, true},
		{"bad tz", ScheduledTaskIntervalDaily, ExportSchedule{Timezone: "Mars/Base"}, true},
		{"empty tz", ScheduledTaskIntervalDaily, ExportSchedule{}, true},
		{"local tz", ScheduledTaskIntervalDaily, ExportSchedule{Timezone: "Local"}, true},
		{"daily in dst gap", ScheduledTaskIntervalDaily, ExportSchedule{Hour: 2, Minute: 30, Timezone: "America/Los_Angeles"}, true},
		{"weekly in dst gap", ScheduledTaskIntervalWeekly, ExportSchedule{Hour: 2, DayOfWeek: intPtr(0), Timezone: "America/Los_Angeles"}, true},
		{"daily outside dst gap", ScheduledTaskIntervalDaily, ExportSchedule{Hour: 3, Timezone: "America/Los_Angeles"}, false},
		{"daily repeated hour ok", ScheduledTaskIntervalDaily, ExportSchedule{Hour: 1, Minute: 30, Timezone: "America/Los_Angeles"}, false},
		{"hourly ignores dst gap", ScheduledTaskIntervalHourly, ExportSchedule{Hour: 2, Minute: 30, Timezone: "America/Los_Angeles"}, false},
		{"weekly monday skips sunday gap", ScheduledTaskIntervalWeekly, ExportSchedule{Hour: 2, Minute: 30, DayOfWeek: intPtr(1), Timezone: "America/Los_Angeles"}, false},
		{"monthly day 1 skips gap", ScheduledTaskIntervalMonthly, ExportSchedule{Hour: 2, Minute: 30, DayOfMonth: intPtr(1), Timezone: "America/Los_Angeles"}, false},
		{"weekly missing dow in gap zone", ScheduledTaskIntervalWeekly, ExportSchedule{Hour: 2, Minute: 30, Timezone: "America/Los_Angeles"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Validate(tt.interval); (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateExportSchedule(t *testing.T) {
	daily := &ExportSchedule{Hour: 1, Timezone: "UTC"}
	tests := []struct {
		name     string
		interval ScheduledTaskInterval
		s        *ExportSchedule
		wantErr  bool
	}{
		{"weekly nil", ScheduledTaskIntervalWeekly, nil, true},
		{"monthly nil", ScheduledTaskIntervalMonthly, nil, true},
		{"manual non-nil", ScheduledTaskIntervalManual, daily, true},
		{"manual nil", ScheduledTaskIntervalManual, nil, false},
		{"daily nil", ScheduledTaskIntervalDaily, nil, false},
		{"hourly nil", ScheduledTaskIntervalHourly, nil, false},
		{"daily non-nil", ScheduledTaskIntervalDaily, daily, false},
		{"15min non-nil", ScheduledTaskIntervalEvery15Minutes, daily, true},
		{"30min non-nil", ScheduledTaskIntervalEvery30Minutes, daily, true},
		{"custom non-nil", ScheduledTaskIntervalCustom, daily, true},
		{"15min nil", ScheduledTaskIntervalEvery15Minutes, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateExportSchedule(tt.interval, tt.s); (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestSkippedByDST(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	daily := ScheduledTaskIntervalDaily
	weekly := ScheduledTaskIntervalWeekly
	monthly := ScheduledTaskIntervalMonthly
	tests := []struct {
		name     string
		loc      *time.Location
		interval ScheduledTaskInterval
		s        ExportSchedule
		want     bool
	}{
		{"la 02:00 spring forward", la, daily, ExportSchedule{Hour: 2}, true},
		{"la 02:59 spring forward", la, daily, ExportSchedule{Hour: 2, Minute: 59}, true},
		{"la 03:00", la, daily, ExportSchedule{Hour: 3}, false},
		{"la 01:59", la, daily, ExportSchedule{Hour: 1, Minute: 59}, false},
		{"kolkata no dst", kolkata, daily, ExportSchedule{Hour: 2, Minute: 30}, false},
		{"utc", time.UTC, daily, ExportSchedule{Hour: 2, Minute: 30}, false},
		{"la weekly monday 02:30", la, weekly, ExportSchedule{Hour: 2, Minute: 30, DayOfWeek: intPtr(1)}, false},
		{"la weekly sunday 02:30", la, weekly, ExportSchedule{Hour: 2, Minute: 30, DayOfWeek: intPtr(0)}, true},
		{"la monthly day 1 02:30", la, monthly, ExportSchedule{Hour: 2, Minute: 30, DayOfMonth: intPtr(1)}, false},
		{"la monthly day 8 02:30 hits 2026-03-08", la, monthly, ExportSchedule{Hour: 2, Minute: 30, DayOfMonth: intPtr(8)}, true},
		{"la monthly day 8 03:00", la, monthly, ExportSchedule{Hour: 3, DayOfMonth: intPtr(8)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.skippedByDST(tt.loc, tt.interval, from); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestLocationBadTimezoneIsValidationError(t *testing.T) {
	_, err := (&ExportSchedule{Timezone: "Mars/Base"}).Location()
	if !ierr.IsValidation(err) {
		t.Fatalf("want validation error, got %v", err)
	}
}
