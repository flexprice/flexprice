package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

const (
	roleWithAnalytics    = "all_reader"
	roleWithoutAnalytics = "event_reader"
)

func testRBAC(t *testing.T) *rbac.RBACService {
	t.Helper()
	svc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	require.NoError(t, err)
	return svc
}

func withRoles(ctx context.Context, roles ...string) context.Context {
	return context.WithValue(ctx, types.CtxRoles, roles)
}

type sourceValidatorFake struct {
	AnalyticsService
	reportErr, viewErr error
	reportKey, viewID  string
	calls              int
}

func (f *sourceValidatorFake) ValidateReportSource(_ context.Context, key string, _ *types.ReportParams) error {
	f.calls++
	f.reportKey = key
	return f.reportErr
}

func (f *sourceValidatorFake) ValidateViewSource(_ context.Context, id string) error {
	f.calls++
	f.viewID = id
	return f.viewErr
}

func reportCreateReq() dto.CreateScheduledTaskRequest {
	return dto.CreateScheduledTaskRequest{
		EntityType:   types.ScheduledTaskEntityTypeReport,
		Interval:     types.ScheduledTaskIntervalManual,
		Enabled:      true,
		SourceConfig: &types.ExportSourceConfig{ReportKey: "k", ReportParams: &types.ReportParams{Shape: "timeseries"}},
	}
}

func viewCreateReq() dto.CreateScheduledTaskRequest {
	return dto.CreateScheduledTaskRequest{
		EntityType:   types.ScheduledTaskEntityTypeAnalyticsView,
		Interval:     types.ScheduledTaskIntervalManual,
		Enabled:      true,
		SourceConfig: &types.ExportSourceConfig{ViewID: "view_1"},
	}
}

func TestRequireAnalyticsRead(t *testing.T) {
	r := testRBAC(t)
	base := exportCtx()
	tests := []struct {
		name   string
		rbac   *rbac.RBACService
		ctx    context.Context
		entity types.ScheduledTaskEntityType
		denied bool
	}{
		{"non-report ignores nil rbac", nil, base, types.ScheduledTaskEntityTypeEvents, false},
		{"report nil rbac denied", nil, withRoles(base, roleWithAnalytics), types.ScheduledTaskEntityTypeReport, true},
		{"report no roles denied", r, base, types.ScheduledTaskEntityTypeReport, true},
		{"report without analytics denied", r, withRoles(base, roleWithoutAnalytics), types.ScheduledTaskEntityTypeReport, true},
		{"view without analytics denied", r, withRoles(base, roleWithoutAnalytics), types.ScheduledTaskEntityTypeAnalyticsView, true},
		{"report with analytics allowed", r, withRoles(base, roleWithAnalytics), types.ScheduledTaskEntityTypeReport, false},
		{"non-report without analytics allowed", r, withRoles(base, roleWithoutAnalytics), types.ScheduledTaskEntityTypeEvents, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireAnalyticsRead(tt.ctx, tt.rbac, tt.entity)
			if tt.denied {
				require.Error(t, err)
				require.True(t, ierr.IsPermissionDenied(err), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCreateReportExport_PermissionDeniedPersistsNothing(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)
	s.rbac = testRBAC(t)
	v := &sourceValidatorFake{}
	s.analytics = v

	_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithoutAnalytics), reportCreateReq())
	require.Error(t, err)
	require.True(t, ierr.IsPermissionDenied(err), "got %v", err)
	require.Nil(t, repo.created)
	require.Zero(t, v.calls)
}

func TestCreateReportExport_AllowedCreates(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)
	s.rbac = testRBAC(t)
	v := &sourceValidatorFake{}
	s.analytics = v

	_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithAnalytics), reportCreateReq())
	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.Equal(t, "k", v.reportKey)
}

func TestCreateExport_SourceValidatorErrorReturned(t *testing.T) {
	boom := ierr.NewError("bad source").Mark(ierr.ErrValidation)
	tests := []struct {
		name string
		req  dto.CreateScheduledTaskRequest
		fake *sourceValidatorFake
	}{
		{"report", reportCreateReq(), &sourceValidatorFake{reportErr: boom}},
		{"view", viewCreateReq(), &sourceValidatorFake{viewErr: boom}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, tc := &exportRepoFake{}, &exportTemporalFake{}
			s := newExportService(repo, tc)
			s.rbac = testRBAC(t)
			s.analytics = tt.fake

			_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithAnalytics), tt.req)
			require.ErrorIs(t, err, boom)
			require.Nil(t, repo.created)
			require.Equal(t, 1, tt.fake.calls)
		})
	}
}

func TestCreateViewExport_ValidatesViewID(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)
	s.rbac = testRBAC(t)
	v := &sourceValidatorFake{}
	s.analytics = v

	_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithAnalytics), viewCreateReq())
	require.NoError(t, err)
	require.Equal(t, "view_1", v.viewID)
}

func TestCreateEventsExport_NeverTouchesAnalytics(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)

	_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		ConnectionID: "conn_1",
		EntityType:   types.ScheduledTaskEntityTypeInvoice,
		Interval:     types.ScheduledTaskIntervalDaily,
		Enabled:      true,
		JobConfig:    &types.S3JobConfig{},
	})
	require.NoError(t, err)
}

func storedExportTask(entity types.ScheduledTaskEntityType) *scheduledtask.ScheduledTask {
	task := &scheduledtask.ScheduledTask{
		ID:         "st_1",
		EntityType: entity,
		Interval:   types.ScheduledTaskIntervalDaily,
		Schedule:   &types.ExportSchedule{Timezone: "UTC"},
		Enabled:    true,
	}
	if entity.IsReportSource() {
		task.SourceConfig = &types.ExportSourceConfig{ReportKey: "k", ReportParams: &types.ReportParams{Shape: "timeseries"}}
	}
	return task
}

func TestUpdateAndForceRun_ReportNeedsAnalyticsRead(t *testing.T) {
	disable := false
	for _, entity := range []types.ScheduledTaskEntityType{types.ScheduledTaskEntityTypeReport, types.ScheduledTaskEntityTypeAnalyticsView} {
		t.Run(string(entity)+"/update", func(t *testing.T) {
			repo := &exportRepoFake{task: storedExportTask(entity)}
			s := newExportService(repo, &exportTemporalFake{})
			s.rbac = testRBAC(t)

			_, err := s.UpdateScheduledTask(withRoles(exportCtx(), roleWithoutAnalytics), "st_1", dto.UpdateScheduledTaskRequest{Enabled: &disable})
			require.Error(t, err)
			require.True(t, ierr.IsPermissionDenied(err), "got %v", err)
			require.True(t, repo.task.Enabled)
		})
		t.Run(string(entity)+"/run", func(t *testing.T) {
			tc := &exportTemporalFake{}
			s := newExportService(&exportRepoFake{task: storedExportTask(entity)}, tc)
			s.rbac = testRBAC(t)

			_, err := s.TriggerForceRun(withRoles(exportCtx(), roleWithoutAnalytics), "st_1", dto.TriggerForceRunRequest{})
			require.Error(t, err)
			require.True(t, ierr.IsPermissionDenied(err), "got %v", err)
			require.Nil(t, tc.args)
		})
	}
}

func TestUpdateAndForceRun_ReportAllowedWithAnalyticsRead(t *testing.T) {
	disable := false
	repo := &exportRepoFake{task: storedExportTask(types.ScheduledTaskEntityTypeReport)}
	s := newExportService(repo, &exportTemporalFake{handle: &exportHandleFake{}})
	s.rbac = testRBAC(t)
	ctx := withRoles(exportCtx(), roleWithAnalytics)

	_, err := s.UpdateScheduledTask(ctx, "st_1", dto.UpdateScheduledTaskRequest{Enabled: &disable})
	require.NoError(t, err)

	_, err = s.TriggerForceRun(ctx, "st_1", dto.TriggerForceRunRequest{})
	require.NoError(t, err)
}

func TestUpdateAndForceRun_EventsExportUnaffected(t *testing.T) {
	disable := false
	repo := &exportRepoFake{task: storedExportTask(types.ScheduledTaskEntityTypeEvents)}
	s := newExportService(repo, &exportTemporalFake{handle: &exportHandleFake{}})
	s.rbac = testRBAC(t)
	ctx := withRoles(exportCtx(), roleWithoutAnalytics)

	_, err := s.UpdateScheduledTask(ctx, "st_1", dto.UpdateScheduledTaskRequest{Enabled: &disable})
	require.NoError(t, err)

	_, err = s.TriggerForceRun(ctx, "st_1", dto.TriggerForceRunRequest{})
	require.NoError(t, err)
}

func TestCreateReportExport_RangeCap(t *testing.T) {
	one := 1
	sched := func(iv types.ScheduledTaskInterval) *types.ExportSchedule {
		s := &types.ExportSchedule{Hour: 1, Timezone: "UTC"}
		switch iv {
		case types.ScheduledTaskIntervalWeekly:
			s.DayOfWeek = &one
		case types.ScheduledTaskIntervalMonthly:
			s.DayOfMonth = &one
		}
		return s
	}
	tests := []struct {
		name     string
		key      string
		interval types.ScheduledTaskInterval
		wantErr  bool
	}{
		{"monthly events_volume rejected", "events_volume", types.ScheduledTaskIntervalMonthly, true},
		{"weekly events_volume accepted", "events_volume", types.ScheduledTaskIntervalWeekly, false},
		{"daily invoice_total accepted", "invoice_total", types.ScheduledTaskIntervalDaily, false},
		{"manual events_volume skipped", "events_volume", types.ScheduledTaskIntervalManual, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, tc := &exportRepoFake{}, &exportTemporalFake{}
			s := newExportService(repo, tc)
			s.rbac = testRBAC(t)
			s.analytics = &sourceValidatorFake{}
			req := reportCreateReq()
			req.Interval = tt.interval
			req.SourceConfig.ReportKey = tt.key
			if tt.interval != types.ScheduledTaskIntervalManual {
				req.Schedule = sched(tt.interval)
			}

			_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithAnalytics), req)
			if tt.wantErr {
				require.Error(t, err)
				require.True(t, ierr.IsValidation(err), "got %v", err)
				require.Nil(t, repo.created)
				return
			}
			require.NoError(t, err)
		})
	}
}
