package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type downloadStore struct {
	fakeImportStorage
	bucket string
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
	ctx := s.GetContext()

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

func (s *TaskServiceSuite) TestDownload_ManagedCopyRejectsOtherTenantKey() {
	t := s.T()
	f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t1", "s3://platform-bkt/exports/other/env/sched_1/run.csv"))
	_, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	require.Error(t, err)
	require.Equal(t, 0, f.resolver.platformCall)

	// prefix without trailing slash must not match
	f2 := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t2", ""))
	tk, _ := f2.svc.(*taskService).TaskRepo.Get(f2.ctx, "t2")
	tk.FileURL = "s3://platform-bkt/" + f2.prefix[:len(f2.prefix)-1] + "x/run.csv"
	require.NoError(t, f2.svc.(*taskService).TaskRepo.Update(f2.ctx, tk))
	_, err = f2.svc.GenerateDownloadURL(f2.ctx, "t2")
	require.Error(t, err)

	// traversal
	tk.FileURL = "s3://platform-bkt/" + f2.prefix + "../other/run.csv"
	require.NoError(t, f2.svc.(*taskService).TaskRepo.Update(f2.ctx, tk))
	_, err = f2.svc.GenerateDownloadURL(f2.ctx, "t2")
	require.Error(t, err)
	require.Equal(t, 0, f2.resolver.platformCall)
}

func (s *TaskServiceSuite) TestDownload_ManagedCopyRejectsBucketMismatch() {
	t := s.T()
	f := s.newDownloadFixture(managedScheduledTask("platform-bkt"), exportTask("t1", ""))
	tk, _ := f.svc.(*taskService).TaskRepo.Get(f.ctx, "t1")
	tk.FileURL = "s3://other-bkt/" + f.prefix + "sched_1/run.csv"
	require.NoError(t, f.svc.(*taskService).TaskRepo.Update(f.ctx, tk))

	_, err := f.svc.GenerateDownloadURL(f.ctx, "t1")
	require.Error(t, err)
	require.Equal(t, 0, f.resolver.platformCall)
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
