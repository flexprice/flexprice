package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/testutil"
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
	viewDays           int
	reportDays         int
	viewGrain          types.Grain
}

func (f *sourceValidatorFake) ValidateReportSource(_ context.Context, key string, _ *types.ReportParams, days int) error {
	f.calls++
	f.reportKey = key
	f.reportDays = days
	return f.reportErr
}

func (f *sourceValidatorFake) ValidateViewSource(_ context.Context, id string, days int) (types.Grain, error) {
	f.calls++
	f.viewID = id
	f.viewDays = days
	return f.viewGrain, f.viewErr
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

func TestValidateViewSource_GrainCoarserThanIntervalRejected(t *testing.T) {
	tests := []struct {
		grain    types.Grain
		interval types.ScheduledTaskInterval
		wantErr  bool
	}{
		{types.GrainWeek, types.ScheduledTaskIntervalDaily, true},
		{types.GrainMonth, types.ScheduledTaskIntervalWeekly, true},
		{types.GrainWeek, types.ScheduledTaskIntervalWeekly, false},
		{types.GrainMonth, types.ScheduledTaskIntervalMonthly, false},
		{types.GrainMonth, types.ScheduledTaskIntervalManual, false},
		{"", types.ScheduledTaskIntervalDaily, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.grain)+"/"+string(tt.interval), func(t *testing.T) {
			s := newExportService(&exportRepoFake{}, &exportTemporalFake{})
			s.analytics = &sourceValidatorFake{viewGrain: tt.grain}
			req := viewCreateReq()
			req.Interval = tt.interval

			err := s.validateSource(exportCtx(), req)
			if tt.wantErr {
				require.True(t, ierr.IsValidation(err), "got %v", err)
				require.Contains(t, err.Error(), "is coarser than the")
				return
			}
			require.NoError(t, err)
		})
	}
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

	_, err := s.TriggerForceRun(ctx, "st_1", dto.TriggerForceRunRequest{})
	require.NoError(t, err)

	_, err = s.UpdateScheduledTask(ctx, "st_1", dto.UpdateScheduledTaskRequest{Enabled: &disable})
	require.NoError(t, err)
}

func TestUpdateAndForceRun_EventsExportUnaffected(t *testing.T) {
	disable := false
	repo := &exportRepoFake{task: storedExportTask(types.ScheduledTaskEntityTypeEvents)}
	s := newExportService(repo, &exportTemporalFake{handle: &exportHandleFake{}})
	s.rbac = testRBAC(t)
	ctx := withRoles(exportCtx(), roleWithoutAnalytics)

	_, err := s.TriggerForceRun(ctx, "st_1", dto.TriggerForceRunRequest{})
	require.NoError(t, err)

	_, err = s.UpdateScheduledTask(ctx, "st_1", dto.UpdateScheduledTaskRequest{Enabled: &disable})
	require.NoError(t, err)
}

type listRepoFake struct {
	scheduledtask.Repository
	tasks []*scheduledtask.ScheduledTask
}

func (r *listRepoFake) List(context.Context, *scheduledtask.ListFilters) ([]*scheduledtask.ScheduledTask, error) {
	return r.tasks, nil
}

func TestGetScheduledTask_ReportNeedsAnalyticsRead(t *testing.T) {
	for _, entity := range []types.ScheduledTaskEntityType{types.ScheduledTaskEntityTypeReport, types.ScheduledTaskEntityTypeAnalyticsView} {
		t.Run(string(entity), func(t *testing.T) {
			s := newExportService(&exportRepoFake{task: storedExportTask(entity)}, &exportTemporalFake{})
			s.rbac = testRBAC(t)

			_, err := s.GetScheduledTask(withRoles(exportCtx(), roleWithoutAnalytics), "st_1")
			require.True(t, ierr.IsPermissionDenied(err), "got %v", err)

			resp, err := s.GetScheduledTask(withRoles(exportCtx(), roleWithAnalytics), "st_1")
			require.NoError(t, err)
			require.NotNil(t, resp.SourceConfig)
		})
	}

	s := newExportService(&exportRepoFake{task: storedExportTask(types.ScheduledTaskEntityTypeEvents)}, &exportTemporalFake{})
	s.rbac = testRBAC(t)
	_, err := s.GetScheduledTask(withRoles(exportCtx(), roleWithoutAnalytics), "st_1")
	require.NoError(t, err)
}

func TestListScheduledTasks_HidesReportSourceWithoutAnalyticsRead(t *testing.T) {
	repo := &listRepoFake{tasks: []*scheduledtask.ScheduledTask{
		storedExportTask(types.ScheduledTaskEntityTypeReport),
		storedExportTask(types.ScheduledTaskEntityTypeEvents),
	}}
	s := newExportService(&exportRepoFake{}, &exportTemporalFake{})
	s.repo = repo
	s.rbac = testRBAC(t)

	resp, err := s.ListScheduledTasks(withRoles(exportCtx(), roleWithoutAnalytics), nil, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, resp.Items, 2)
	require.Nil(t, resp.Items[0].SourceConfig)
	require.Equal(t, types.ScheduledTaskEntityTypeReport, resp.Items[0].EntityType)

	resp, err = s.ListScheduledTasks(withRoles(exportCtx(), roleWithAnalytics), nil, "", "", "", "", "", "")
	require.NoError(t, err)
	require.NotNil(t, resp.Items[0].SourceConfig)
	require.NotNil(t, repo.tasks[0].SourceConfig)
}

func TestCheckReportGrain(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		shape    string
		grain    string
		interval types.ScheduledTaskInterval
		wantErr  bool
	}{
		{"week grain daily", "invoice_total", "timeseries", "week", types.ScheduledTaskIntervalDaily, true},
		{"month grain daily", "invoice_total", "timeseries", "month", types.ScheduledTaskIntervalDaily, true},
		{"month grain weekly", "invoice_total", "timeseries", "month", types.ScheduledTaskIntervalWeekly, true},
		{"default month grain weekly", "arpu", "timeseries", "", types.ScheduledTaskIntervalWeekly, true},
		{"week grain weekly", "invoice_total", "timeseries", "week", types.ScheduledTaskIntervalWeekly, false},
		{"day grain daily", "invoice_total", "timeseries", "day", types.ScheduledTaskIntervalDaily, false},
		{"default day grain daily", "invoice_total", "timeseries", "", types.ScheduledTaskIntervalDaily, false},
		{"month grain monthly", "invoice_total", "timeseries", "month", types.ScheduledTaskIntervalMonthly, false},
		{"month grain manual", "invoice_total", "timeseries", "month", types.ScheduledTaskIntervalManual, false},
		{"breakdown ignores grain", "invoice_total", "breakdown", "month", types.ScheduledTaskIntervalDaily, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkReportGrain(tt.key, &types.ReportParams{Shape: tt.shape, Grain: tt.grain}, tt.interval)
			if tt.wantErr {
				require.True(t, ierr.IsValidation(err), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCreateReportExport_GrainCoarserThanIntervalRejected(t *testing.T) {
	repo := &exportRepoFake{}
	s := newExportService(repo, &exportTemporalFake{})
	s.rbac = testRBAC(t)
	s.analytics = &sourceValidatorFake{}
	req := reportCreateReq()
	req.Interval = types.ScheduledTaskIntervalDaily
	req.Schedule = &types.ExportSchedule{Hour: 1, Timezone: "UTC"}
	req.SourceConfig.ReportKey = "invoice_total"
	req.SourceConfig.ReportParams.Grain = "week"

	_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithAnalytics), req)
	require.True(t, ierr.IsValidation(err), "got %v", err)
	require.Nil(t, repo.created)
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
			s.analytics, _ = exportSvc(&testutil.InMemoryReportsRepo{On: true})
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
