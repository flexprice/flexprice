package scheduledtask

import (
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Window returns UTC run bounds.
func Window(interval types.ScheduledTaskInterval, schedule *types.ExportSchedule, scheduledStart time.Time) (time.Time, time.Time, error) {
	loc := time.UTC
	if interval != types.ScheduledTaskIntervalManual {
		if schedule == nil {
			return time.Time{}, time.Time{}, ierr.NewError("schedule is required").
				WithHint("Schedule is required to compute the export window").
				Mark(ierr.ErrValidation)
		}
		var err error
		if loc, err = schedule.Location(); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}

	b := scheduledStart.In(loc)
	y, m, d := b.Date()
	var start, end time.Time
	switch interval {
	case types.ScheduledTaskIntervalHourly:
		end = b.Add(-time.Duration(b.Minute())*time.Minute - time.Duration(b.Second())*time.Second - time.Duration(b.Nanosecond()))
		start = end.Add(-time.Hour)
	case types.ScheduledTaskIntervalDaily, types.ScheduledTaskIntervalManual:
		end = time.Date(y, m, d, 0, 0, 0, 0, loc)
		start = end.AddDate(0, 0, -1)
	case types.ScheduledTaskIntervalWeekly:
		daysSinceMonday := (int(b.Weekday()) + 6) % 7
		end = time.Date(y, m, d-daysSinceMonday, 0, 0, 0, 0, loc)
		start = end.AddDate(0, 0, -7)
	case types.ScheduledTaskIntervalMonthly:
		end = time.Date(y, m, 1, 0, 0, 0, 0, loc)
		start = end.AddDate(0, -1, 0)
	default:
		return time.Time{}, time.Time{}, ierr.NewError("unsupported interval").
			WithHint("Interval does not support export windows").
			Mark(ierr.ErrValidation)
	}
	return start.UTC(), end.UTC(), nil
}

// ReportWindow returns whole UTC day bounds for report exports.
func ReportWindow(interval types.ScheduledTaskInterval, scheduledStart time.Time) (time.Time, time.Time, error) {
	if interval == types.ScheduledTaskIntervalHourly {
		return time.Time{}, time.Time{}, ierr.NewError("hourly report exports are not supported").
			WithHint("Report exports run daily, weekly or monthly").
			Mark(ierr.ErrValidation)
	}
	return Window(interval, &types.ExportSchedule{Timezone: "UTC"}, scheduledStart)
}

// ReportRange formats a window as date..date.
func ReportRange(start, end time.Time) string {
	return start.UTC().Format("2006-01-02") + ".." + end.UTC().Format("2006-01-02")
}

// Cron returns the schedule's cron expression.
func Cron(interval types.ScheduledTaskInterval, schedule *types.ExportSchedule) (string, error) {
	if interval == types.ScheduledTaskIntervalManual {
		return "", ierr.NewError("manual exports have no cron").
			WithHint("Manual exports are triggered on demand").
			Mark(ierr.ErrValidation)
	}
	if schedule == nil {
		return "", ierr.NewError("schedule is required").
			WithHint("Schedule is required to build a cron expression").
			Mark(ierr.ErrValidation)
	}
	switch interval {
	case types.ScheduledTaskIntervalHourly:
		return fmt.Sprintf("%d * * * *", schedule.Minute), nil
	case types.ScheduledTaskIntervalDaily:
		return fmt.Sprintf("%d %d * * *", schedule.Minute, schedule.Hour), nil
	case types.ScheduledTaskIntervalWeekly:
		if schedule.DayOfWeek == nil {
			break
		}
		return fmt.Sprintf("%d %d * * %d", schedule.Minute, schedule.Hour, *schedule.DayOfWeek), nil
	case types.ScheduledTaskIntervalMonthly:
		if schedule.DayOfMonth == nil {
			break
		}
		return fmt.Sprintf("%d %d %d * *", schedule.Minute, schedule.Hour, *schedule.DayOfMonth), nil
	}
	return "", ierr.NewError("unsupported interval or incomplete schedule").
		WithHint("Interval must be hourly, daily, weekly or monthly with its schedule fields").
		Mark(ierr.ErrValidation)
}
