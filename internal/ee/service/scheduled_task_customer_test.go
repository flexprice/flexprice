package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type originRepoFake struct {
	scheduledtask.Repository
	created *scheduledtask.ScheduledTask
	listed  *scheduledtask.ListFilters
}

func (r *originRepoFake) Create(_ context.Context, t *scheduledtask.ScheduledTask) error {
	r.created = t
	return nil
}

func (r *originRepoFake) Get(context.Context, string) (*scheduledtask.ScheduledTask, error) {
	return r.created, nil
}

func (r *originRepoFake) List(_ context.Context, f *scheduledtask.ListFilters) ([]*scheduledtask.ScheduledTask, error) {
	r.listed = f
	return nil, nil
}

type customerCountFake struct {
	customer.Repository
	err error
	got *types.CustomerFilter
}

func (c *customerCountFake) Count(_ context.Context, f *types.CustomerFilter) (int, error) {
	c.got = f
	return 0, c.err
}

func customerExportService(repo *originRepoFake, cust *customerCountFake) *scheduledTaskService {
	s := newExportService(&exportRepoFake{}, &exportTemporalFake{})
	s.repo = repo
	s.customerRepo = cust
	return s
}

func customersReq() dto.CreateScheduledTaskRequest {
	return dto.CreateScheduledTaskRequest{
		EntityType:   types.ScheduledTaskEntityTypeCustomers,
		Interval:     types.ScheduledTaskIntervalManual,
		Enabled:      true,
		Origin:       types.ExportOriginCustomersPage,
		SourceConfig: &types.ExportSourceConfig{CustomerFilter: &types.CustomerFilter{}},
	}
}

func TestCreate_DefaultOriginIsConsole(t *testing.T) {
	repo := &originRepoFake{}
	s := customerExportService(repo, &customerCountFake{})

	resp, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		EntityType: types.ScheduledTaskEntityTypeInvoice,
		Interval:   types.ScheduledTaskIntervalManual,
		Enabled:    true,
	})
	require.NoError(t, err)
	require.Equal(t, types.ExportOriginConsole, repo.created.Origin)
	require.Equal(t, types.ExportOriginConsole, resp.Origin)
}

func TestCreate_CustomersWithoutFilterObjectIsRejected(t *testing.T) {
	s := customerExportService(&originRepoFake{}, &customerCountFake{})
	req := customersReq()
	req.SourceConfig = nil
	_, err := s.CreateScheduledTask(exportCtx(), req)
	require.True(t, ierr.IsValidation(err), "got %v", err)

	req.SourceConfig = &types.ExportSourceConfig{CustomerFilter: &types.CustomerFilter{}, ViewID: "v"}
	_, err = s.CreateScheduledTask(exportCtx(), req)
	require.True(t, ierr.IsValidation(err), "got %v", err)

	req.SourceConfig = &types.ExportSourceConfig{ReportKey: "k", CustomerFilter: &types.CustomerFilter{}}
	_, err = s.CreateScheduledTask(exportCtx(), req)
	require.True(t, ierr.IsValidation(err), "got %v", err)
}

func TestCreate_CustomersEmptyFilterMeansAll(t *testing.T) {
	repo := &originRepoFake{}
	s := customerExportService(repo, &customerCountFake{})
	resp, err := s.CreateScheduledTask(exportCtx(), customersReq())
	require.NoError(t, err)
	require.NotNil(t, repo.created.SourceConfig.CustomerFilter)
	require.Equal(t, types.ExportOriginCustomersPage, resp.Origin)
	require.Nil(t, resp.Run)
}

func TestCreate_CustomerFilterSanitizedAndDryRun(t *testing.T) {
	repo, cust := &originRepoFake{}, &customerCountFake{}
	s := customerExportService(repo, cust)
	req := customersReq()
	req.SourceConfig.CustomerFilter = types.NewCustomerFilter()
	req.SourceConfig.CustomerFilter.CustomerIDs = []string{"c1"}
	req.SourceConfig.CustomerFilter.Email = "a@b.co"

	_, err := s.CreateScheduledTask(exportCtx(), req)
	require.NoError(t, err)
	stored := repo.created.SourceConfig.CustomerFilter
	require.Empty(t, stored.CustomerIDs)
	require.Equal(t, "a@b.co", stored.Email)
	require.Nil(t, stored.QueryFilter)
	require.Same(t, stored, cust.got)
}

func TestCreate_CustomerFilterBadFieldIs400(t *testing.T) {
	bad := ierr.NewError("unknown field").Mark(ierr.ErrValidation)
	s := customerExportService(&originRepoFake{}, &customerCountFake{err: bad})
	_, err := s.CreateScheduledTask(exportCtx(), customersReq())
	require.True(t, ierr.IsValidation(err), "got %v", err)
}

func TestCreate_CustomerFilterEntityAllowlist(t *testing.T) {
	filter := &types.ExportSourceConfig{CustomerFilter: &types.CustomerFilter{}}
	allowed := []types.ScheduledTaskEntityType{
		types.ScheduledTaskEntityTypeWalletLedger,
		types.ScheduledTaskEntityTypeInvoice,
		types.ScheduledTaskEntityTypeCreditTopups,
		types.ScheduledTaskEntityTypeCreditUsage,
		types.ScheduledTaskEntityTypeUsageAnalytics,
	}
	denied := []types.ScheduledTaskEntityType{
		types.ScheduledTaskEntityTypeEvents,
		types.ScheduledTaskEntityTypeWalletDebits,
		types.ScheduledTaskEntityTypeRevenueFacts,
		types.ScheduledTaskEntityTypeAnalyticsView,
	}
	for _, e := range allowed {
		t.Run("allow "+string(e), func(t *testing.T) {
			s := customerExportService(&originRepoFake{}, &customerCountFake{})
			_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
				EntityType: e, Interval: types.ScheduledTaskIntervalManual, Enabled: true,
				Origin: types.ExportOriginCustomersPage, SourceConfig: filter,
			})
			require.NoError(t, err)
		})
	}
	for _, e := range denied {
		t.Run("deny "+string(e), func(t *testing.T) {
			s := customerExportService(&originRepoFake{}, &customerCountFake{})
			sc := filter
			if e == types.ScheduledTaskEntityTypeAnalyticsView {
				sc = &types.ExportSourceConfig{ViewID: "v", CustomerFilter: &types.CustomerFilter{}}
			}
			_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
				EntityType: e, Interval: types.ScheduledTaskIntervalManual, Enabled: true,
				Origin: types.ExportOriginCustomersPage, SourceConfig: sc,
			})
			require.True(t, ierr.IsValidation(err), "got %v", err)
		})
	}
}

func TestCreate_CustomerFilterNeedsCustomersPageOrigin(t *testing.T) {
	s := customerExportService(&originRepoFake{}, &customerCountFake{})
	req := customersReq()
	req.Origin = ""
	_, err := s.CreateScheduledTask(exportCtx(), req)
	require.True(t, ierr.IsValidation(err), "got %v", err)
}

func TestCreate_ReportCustomerFilterNeedsCustomerDimension(t *testing.T) {
	tests := []struct {
		key string
		ok  bool
	}{
		{"invoice_total", true},
		{"receivables_aging", true},
		{"credit_topups", true},
		{"net_billing", false},
		{"active_customers", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			s := customerExportService(&originRepoFake{}, &customerCountFake{})
			s.rbac = testRBAC(t)
			s.analytics = &sourceValidatorFake{}
			_, err := s.CreateScheduledTask(withRoles(exportCtx(), roleWithAnalytics), dto.CreateScheduledTaskRequest{
				EntityType: types.ScheduledTaskEntityTypeReport,
				Interval:   types.ScheduledTaskIntervalManual,
				Enabled:    true,
				Origin:     types.ExportOriginCustomersPage,
				SourceConfig: &types.ExportSourceConfig{
					ReportKey:      tt.key,
					ReportParams:   &types.ReportParams{Shape: "breakdown"},
					CustomerFilter: &types.CustomerFilter{},
				},
			})
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.True(t, ierr.IsValidation(err), "got %v", err)
		})
	}
}

func TestCreate_RunNow(t *testing.T) {
	t.Run("rejected on scheduled interval", func(t *testing.T) {
		s := customerExportService(&originRepoFake{}, &customerCountFake{})
		req := customersReq()
		req.Interval = types.ScheduledTaskIntervalDaily
		req.RunNow = true
		_, err := s.CreateScheduledTask(exportCtx(), req)
		require.True(t, ierr.IsValidation(err), "got %v", err)
	})
	t.Run("manual returns the run", func(t *testing.T) {
		repo := &originRepoFake{}
		s := customerExportService(repo, &customerCountFake{})
		req := customersReq()
		req.RunNow = true
		resp, err := s.CreateScheduledTask(exportCtx(), req)
		require.NoError(t, err)
		require.NotNil(t, resp.Run)
		require.Equal(t, "wf_1", resp.Run.WorkflowID)
	})
	t.Run("trigger failure still returns the export", func(t *testing.T) {
		repo := &originRepoFake{}
		s := customerExportService(repo, &customerCountFake{})
		s.temporalClient = nil
		req := customersReq()
		req.RunNow = true
		resp, err := s.CreateScheduledTask(exportCtx(), req)
		require.NoError(t, err)
		require.NotEmpty(t, resp.ID)
		require.Nil(t, resp.Run)
	})
}

func TestList_OriginFilters(t *testing.T) {
	repo := &originRepoFake{}
	s := customerExportService(repo, &customerCountFake{})

	_, err := s.ListScheduledTasks(exportCtx(), nil, "", "", "", "", types.ExportOriginCustomersPage, "")
	require.NoError(t, err)
	require.Equal(t, types.ExportOriginCustomersPage, repo.listed.Origin)
	require.Empty(t, repo.listed.ExcludeOrigin)

	_, err = s.ListScheduledTasks(exportCtx(), nil, "", "", "", "", "", types.ExportOriginCustomersPage)
	require.NoError(t, err)
	require.Empty(t, repo.listed.Origin)
	require.Equal(t, types.ExportOriginCustomersPage, repo.listed.ExcludeOrigin)
}
