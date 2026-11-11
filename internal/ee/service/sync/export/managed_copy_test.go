package export

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type recordingStore struct {
	storage.Storage
	provider storage.Provider
	bucket   string
	keys     []string
	bodies   [][]byte
	gzipped  []bool
}

func (s *recordingStore) Provider() storage.Provider { return s.provider }

func (s *recordingStore) FileURL(key string) string {
	scheme := "s3"
	if s.provider == storage.ProviderGCS {
		scheme = "gs"
	}
	return scheme + "://" + s.bucket + "/" + key
}

func (s *recordingStore) UploadStream(_ context.Context, req *storage.StreamUploadRequest) (*storage.UploadResponse, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	s.keys = append(s.keys, req.Key)
	s.bodies = append(s.bodies, body)
	s.gzipped = append(s.gzipped, req.Gzipped)
	return &storage.UploadResponse{FileURL: s.FileURL(req.Key), FileSizeBytes: req.UncompressedSize, UploadedAt: time.Now()}, nil
}

type managedResolver struct {
	storage.Resolver
	platform   storage.Storage
	connection storage.Storage
}

func (r managedResolver) ForPlatform(context.Context, storage.Purpose) (storage.Storage, error) {
	return r.platform, nil
}

func (r managedResolver) ForConnectionExport(context.Context, string, *types.S3JobConfig) (storage.Storage, error) {
	return r.connection, nil
}

type panicConnRepo struct {
	connection.Repository
	calls int
}

func (r *panicConnRepo) Get(context.Context, string) (*connection.Connection, error) {
	r.calls++
	return &connection.Connection{ProviderType: types.SecretProviderS3}, nil
}

type csvExporter struct{}

func (csvExporter) PrepareData(context.Context, *dto.ExportRequest) ([]byte, int, error) {
	return []byte("a,b\n1,2\n"), 1, nil
}

func (csvExporter) GetFilenamePrefix() string { return "events" }

func managedRequest(managed bool, cfg *types.S3JobConfig) *dto.ExportRequest {
	return &dto.ExportRequest{
		EntityType:   types.ScheduledTaskEntityTypeEvents,
		ConnectionID: "conn_1",
		TenantID:     "t1",
		EnvID:        "e1",
		StartTime:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		JobConfig:    cfg,
		ManagedCopy:  managed,
	}
}

func managedService(platform, conn storage.Storage, repo *panicConnRepo) *ExportService {
	return &ExportService{
		connectionRepo:  repo,
		storageResolver: managedResolver{platform: platform, connection: conn},
		logger:          logger.NewNoopLogger(),
	}
}

func platformCfg(prefix string) *types.S3JobConfig {
	return &types.S3JobConfig{Bucket: "platform-bucket", Region: "ap-south-1", Provider: types.SecretProviderS3, KeyPrefix: prefix}
}

func TestExport_ManagedCopyWritesPlatformBucket(t *testing.T) {
	platform := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
	repo := &panicConnRepo{}
	svc := managedService(platform, nil, repo)

	resp, err := svc.executeExport(context.Background(), managedRequest(true, platformCfg("exports/t1/e1/st_1")), csvExporter{})

	require.NoError(t, err)
	require.Zero(t, repo.calls)
	require.Len(t, platform.keys, 1)
	require.True(t, strings.HasPrefix(platform.keys[0], "exports/t1/e1/st_1/"), platform.keys[0])
	require.True(t, strings.HasPrefix(resp.FileURL, "s3://platform-bucket/exports/t1/e1/st_1/"), resp.FileURL)
}

func TestExport_GzipConfigUploadsGzippedBody(t *testing.T) {
	tests := []struct {
		name    string
		managed bool
		cfg     *types.S3JobConfig
	}{
		{"platform bucket", true, &types.S3JobConfig{Bucket: "platform-bucket", Provider: types.SecretProviderS3, KeyPrefix: "exports/t1/e1/st_1", Compression: types.S3CompressionTypeGzip}},
		{"customer bucket", false, &types.S3JobConfig{Bucket: "customer-bucket", KeyPrefix: "mine", Compression: types.S3CompressionTypeGzip}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingStore{provider: storage.ProviderS3, bucket: tt.cfg.Bucket}
			svc := managedService(store, store, &panicConnRepo{})

			resp, err := svc.executeExport(context.Background(), managedRequest(tt.managed, tt.cfg), csvExporter{})

			require.NoError(t, err)
			require.Len(t, store.keys, 1)
			require.True(t, strings.HasSuffix(store.keys[0], ".csv.gz"), store.keys[0])
			require.True(t, store.gzipped[0])
			gz, err := gzip.NewReader(bytes.NewReader(store.bodies[0]))
			require.NoError(t, err)
			csv, err := io.ReadAll(gz)
			require.NoError(t, err)
			require.Equal(t, "a,b\n1,2\n", string(csv))
			require.Equal(t, int64(len(csv)), resp.FileSizeBytes)
		})
	}
}

func TestExport_NoCompressionUploadsPlainBody(t *testing.T) {
	store := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
	svc := managedService(store, nil, &panicConnRepo{})

	_, err := svc.executeExport(context.Background(), managedRequest(true, platformCfg("exports/t1/e1/st_1")), csvExporter{})

	require.NoError(t, err)
	require.True(t, strings.HasSuffix(store.keys[0], ".csv"), store.keys[0])
	require.False(t, store.gzipped[0])
	require.Equal(t, "a,b\n1,2\n", string(store.bodies[0]))
}

func TestExport_ManagedCopyRejectsForeignPrefix(t *testing.T) {
	tests := []struct {
		name string
		cfg  *types.S3JobConfig
	}{
		{"other tenant", platformCfg("exports/t2/e1/st_1")},
		{"other env", platformCfg("exports/t1/e2/st_1")},
		{"no exports root", platformCfg("t1/e1/st_1")},
		{"prefix without trailing scope", platformCfg("exports/t1/e1")},
		{"dot-dot traversal", platformCfg("exports/t1/e1/../e2/st_1")},
		{"foreign bucket", &types.S3JobConfig{Bucket: "customer-bucket", Provider: types.SecretProviderS3, KeyPrefix: "exports/t1/e1/st_1"}},
		{"provider mismatch", &types.S3JobConfig{Bucket: "platform-bucket", Provider: types.SecretProviderGCS, KeyPrefix: "exports/t1/e1/st_1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
			svc := managedService(platform, nil, &panicConnRepo{})

			_, err := svc.executeExport(context.Background(), managedRequest(true, tt.cfg), csvExporter{})

			require.Error(t, err)
			require.True(t, ierr.IsValidation(err))
			require.Empty(t, platform.keys)
		})
	}
}

func TestExport_ConnectionPathUnchanged(t *testing.T) {
	platform := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
	customer := &recordingStore{provider: storage.ProviderS3, bucket: "customer-bucket"}
	repo := &panicConnRepo{}
	svc := managedService(platform, customer, repo)

	resp, err := svc.executeExport(context.Background(), managedRequest(false, &types.S3JobConfig{Bucket: "customer-bucket", KeyPrefix: "mine"}), csvExporter{})

	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Empty(t, platform.keys)
	require.Len(t, customer.keys, 1)
	require.True(t, strings.HasPrefix(resp.FileURL, "s3://customer-bucket/mine/"), resp.FileURL)
}

func TestExportObjectKey_TaskIDMakesKeysDistinct(t *testing.T) {
	cfg := platformCfg("exports/t1/e1/st_1")
	first, second := managedRequest(true, cfg), managedRequest(true, cfg)
	first.TaskID, second.TaskID = "export-wf/run:aaaa1111", "export-wf/run:bbbb2222"

	k1, k2 := exportObjectKey(first, csvExporter{}), exportObjectKey(second, csvExporter{})

	require.NotEqual(t, k1, k2)
	require.NotContains(t, k1, ":")
	require.Equal(t, exportObjectKey(managedRequest(true, cfg), csvExporter{}), exportObjectKey(managedRequest(true, cfg), csvExporter{}))
	require.NotContains(t, exportObjectKey(managedRequest(true, cfg), csvExporter{}), "aaaa")
}
