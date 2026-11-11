package types

import (
	"testing"

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

func TestLocationBadTimezoneIsValidationError(t *testing.T) {
	_, err := (&ExportSchedule{Timezone: "Mars/Base"}).Location()
	if !ierr.IsValidation(err) {
		t.Fatalf("want validation error, got %v", err)
	}
}
