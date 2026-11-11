package service

import (
	"context"
	"errors"
	"strings"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type downloadStore struct {
	fakeImportStorage
	bucket    string
	missing   bool
	existsErr error
}

func (d *downloadStore) Exists(context.Context, string) (bool, error) {
	return !d.missing && d.existsErr == nil, d.existsErr
}

func (d *downloadStore) FileURL(key string) string { return "s3://" + d.bucket + "/" + key }

type downloadResolver struct {
	platform     storagetypes.Storage
	conn         storagetypes.Storage
	platformCall int
	connCall     int
}

func (r *downloadResolver) ForPlatform(context.Context, storage.Purpose) (storagetypes.Storage, error) {
	r.platformCall++
	return r.platform, nil
}
func (r *downloadResolver) ForConnection(context.Context, string) (storagetypes.Storage, error) {
	return nil, nil
}
func (r *downloadResolver) ForConnectionExport(context.Context, string, *types.S3JobConfig) (storagetypes.Storage, error) {
	r.connCall++
	return r.conn, nil
}
func (r *downloadResolver) Provider() storage.Provider { return storage.ProviderS3 }
func (r *downloadResolver) BucketConfigFor(storage.Purpose) (config.BucketConfig, error) {
	return config.BucketConfig{}, nil
}

type stubScheduledTaskRepo struct {
	scheduledtask.Repository
	st *scheduledtask.ScheduledTask
}

func (r *stubScheduledTaskRepo) Get(context.Context, string) (*scheduledtask.ScheduledTask, error) {
	return r.st, nil
}

type stubConnectionRepo struct {
	connection.Repository
}

func (r *stubConnectionRepo) Get(_ context.Context, id string) (*connection.Connection, error) {
	return &connection.Connection{ID: id, ProviderType: types.SecretProviderS3}, nil
}

type downloadFixture struct {
	svc      TaskService
	resolver *downloadResolver
	ctx      context.Context
	prefix   string
}

func (s *TaskServiceSuite) newDownloadFixture(st *scheduledtask.ScheduledTask, tk *task.Task) *downloadFixture {
	t := s.T()
	ctx := types.SetEnvironmentID(s.GetContext(), "env_test")

	r := &downloadResolver{
		platform: &downloadStore{fakeImportStorage: *newFakeImportStorage(), bucket: "platform-bkt"},
		conn:     &downloadStore{fakeImportStorage: *newFakeImportStorage(), bucket: "cust-bkt"},
	}
	tk.BaseModel = types.GetDefaultBaseModel(ctx)
	require.NoError(t, s.GetStores().TaskRepo.Create(ctx, tk))

	svc := NewTaskService(ServiceParams{
		Logger:            s.GetLogger(),
		Config:            s.GetConfig(),
		TaskRepo:          s.GetStores().TaskRepo,
		ScheduledTaskRepo: &stubScheduledTaskRepo{st: st},
		ConnectionRepo:    &stubConnectionRepo{},
		StorageResolver:   r,
	})
	return &downloadFixture{
		svc:      svc,
		resolver: r,
		ctx:      ctx,
		prefix:   "exports/" + types.GetTenantID(ctx) + "/" + types.GetEnvironmentID(ctx) + "/",
	}
}

func managedScheduledTask(bucket string) *scheduledtask.ScheduledTask {
	return &scheduledtask.ScheduledTask{
		ID:          "sched_1",
		ManagedCopy: true,
		JobConfig:   &types.S3JobConfig{Bucket: bucket, Region: "us-east-1"},
	}
}

func exportTask(id, fileURL string) *task.Task {
	return &task.Task{
		ID:              id,
		TaskType:        types.TaskTypeExport,
		EntityType:      types.EntityTypeEvents,
		FileType:        types.FileTypeCSV,
		TaskStatus:      types.TaskStatusCompleted,
		ScheduledTaskID: "sched_1",
		FileURL:         fileURL,
	}
}

func (s *TaskServiceSuite) TestDownload_ManagedCopyPresigns() {
	t := s.T()
	st := managedScheduledTask("platform-bkt")
	f := s.newDownloadFixture(st, exportTask("t1", ""))
	key := f.prefix + "sched_1/run.csv"
	tk, err := f.svc.(*taskService).TaskRepo.Get(f.ctx, "t1")
	require.NoError(t, err)
	tk.FileURL = "s3://platform-bkt/" + key
	require.NoError(t, f.svc.(*taskService).TaskRepo.Update(f.ctx, tk))

	url, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	require.NoError(t, err)
	require.Equal(t, "https://fake/"+key, url)
	require.Equal(t, 1, f.resolver.platformCall)
	require.Equal(t, 0, f.resolver.connCall)
}

func (s *TaskServiceSuite) requireRejected(f *downloadFixture, id string) {
	_, err := f.svc.GenerateDownloadURL(f.ctx, id)
	s.Require().Error(err)
	s.Require().True(ierr.IsValidation(err), "want validation error, got %v", err)
	s.Require().Equal(0, f.resolver.platformCall)
	s.Require().Equal(0, f.resolver.connCall)
}

func (s *TaskServiceSuite) setFileURL(f *downloadFixture, id, fileURL string) {
	repo := f.svc.(*taskService).TaskRepo
	tk, err := repo.Get(f.ctx, id)
	s.Require().NoError(err)
	tk.FileURL = fileURL
	s.Require().NoError(repo.Update(f.ctx, tk))
}

func (s *TaskServiceSuite) TestDownload_ManagedCopyRejectsOtherTenantKey() {
	cases := map[string]func(prefix string) string{
		"other tenant":       func(string) string { return "s3://platform-bkt/exports/other/env/sched_1/run.csv" },
		"no trailing slash":  func(p string) string { return "s3://platform-bkt/" + p[:len(p)-1] + "x/run.csv" },
		"traversal":          func(p string) string { return "s3://platform-bkt/" + p + "../other/run.csv" },
		"non-exports prefix": func(string) string { return "s3://platform-bkt/invoices/run.csv" },
	}
	for name, fileURL := range cases {
		s.Run(name, func() {
			id := "t_" + strings.ReplaceAll(name, " ", "_")
			f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask(id, ""))
			s.setFileURL(f, id, fileURL(f.prefix))
			s.requireRejected(f, id)
		})
	}
}

func (s *TaskServiceSuite) TestDownload_ManagedCopyRejectsEmptyEnvironment() {
	f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t1", ""))
	s.setFileURL(f, "t1", "s3://platform-bkt/exports/"+types.GetTenantID(f.ctx)+"//sched_1/run.csv")
	f.ctx = types.SetEnvironmentID(f.ctx, "")
	s.requireRejected(f, "t1")
}

func (s *TaskServiceSuite) TestDownload_ManagedCopyRejectsBucketMismatch() {
	f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t1", ""))
	s.setFileURL(f, "t1", "s3://other-bkt/"+f.prefix+"sched_1/run.csv")
	s.requireRejected(f, "t1")
}

func (s *TaskServiceSuite) TestDownload_ConnectionPathUnchanged() {
	t := s.T()
	st := &scheduledtask.ScheduledTask{
		ID:           "sched_1",
		ConnectionID: "conn_1",
		JobConfig:    &types.S3JobConfig{Bucket: "cust-bkt", Region: "us-east-1"},
	}
	f := s.newDownloadFixture(st, exportTask("t1", "s3://cust-bkt/some/run.csv"))

	url, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	require.NoError(t, err)
	require.Equal(t, "https://fake/some/run.csv", url)
	require.Equal(t, 1, f.resolver.connCall)
	require.Equal(t, 0, f.resolver.platformCall)
}

func (s *TaskServiceSuite) downloadAs(entity types.ScheduledTaskEntityType, roles ...string) (string, error, *downloadFixture) {
	st := managedScheduledTask("platform-bkt")
	st.EntityType = entity
	f := s.newDownloadFixture(st, exportTask("t1", ""))
	s.setFileURL(f, "t1", "s3://platform-bkt/"+f.prefix+"sched_1/run.csv")
	f.svc.(*taskService).RBAC = testRBAC(s.T())
	url, err := f.svc.GenerateDownloadURL(withRoles(f.ctx, roles...), "t1")
	return url, err, f
}

func (s *TaskServiceSuite) TestDownload_ReportNeedsAnalyticsRead() {
	_, err, f := s.downloadAs(types.ScheduledTaskEntityTypeReport, roleWithoutAnalytics)
	s.Require().Error(err)
	s.Require().True(ierr.IsPermissionDenied(err), "got %v", err)
	s.Require().Equal(0, f.resolver.platformCall)
}

func (s *TaskServiceSuite) TestDownload_ReportAllowedWithAnalyticsRead() {
	_, err, _ := s.downloadAs(types.ScheduledTaskEntityTypeReport, roleWithAnalytics)
	s.Require().NoError(err)
}

func (s *TaskServiceSuite) TestDownload_EventsExportUnaffectedByAnalyticsRead() {
	_, err, _ := s.downloadAs(types.ScheduledTaskEntityTypeEvents, roleWithoutAnalytics)
	s.Require().NoError(err)
}

func (s *TaskServiceSuite) requireExpired(f *downloadFixture) {
	_, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	s.Require().Error(err)
	s.Require().True(ierr.IsNotFound(err), "got %v", err)
	s.Require().Contains(err.Error(), "expired or was removed")
}

func (s *TaskServiceSuite) TestDownload_ManagedCopyMissingObjectExpired() {
	f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t1", ""))
	s.setFileURL(f, "t1", "s3://platform-bkt/"+f.prefix+"sched_1/run.csv")
	f.resolver.platform.(*downloadStore).missing = true
	s.requireExpired(f)
}

func (s *TaskServiceSuite) TestDownload_ConnectionMissingObjectExpired() {
	st := &scheduledtask.ScheduledTask{
		ID:           "sched_1",
		ConnectionID: "conn_1",
		JobConfig:    &types.S3JobConfig{Bucket: "cust-bkt", Region: "us-east-1"},
	}
	f := s.newDownloadFixture(st, exportTask("t1", "s3://cust-bkt/some/run.csv"))
	f.resolver.conn.(*downloadStore).missing = true
	s.requireExpired(f)
}

func (s *TaskServiceSuite) TestDownload_ConnectionExistsErrorPresigns() {
	st := &scheduledtask.ScheduledTask{
		ID:           "sched_1",
		ConnectionID: "conn_1",
		JobConfig:    &types.S3JobConfig{Bucket: "cust-bkt", Region: "us-east-1"},
	}
	f := s.newDownloadFixture(st, exportTask("t1", "s3://cust-bkt/some/run.csv"))
	f.resolver.conn.(*downloadStore).existsErr = errors.New("403")
	url, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	s.Require().NoError(err)
	s.Require().Equal("https://fake/some/run.csv", url)
}

func (s *TaskServiceSuite) TestDownload_ExistsErrorPropagates() {
	f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t1", ""))
	s.setFileURL(f, "t1", "s3://platform-bkt/"+f.prefix+"sched_1/run.csv")
	f.resolver.platform.(*downloadStore).existsErr = errors.New("boom")
	_, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	s.Require().Error(err)
	s.Require().False(ierr.IsNotFound(err))
}
