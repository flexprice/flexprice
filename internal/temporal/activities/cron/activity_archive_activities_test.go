package cron

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

// newLocalArchiveActivities has no DB: any path reaching Postgres panics, which
// proves the guards under test refuse before touching a partition.
func newLocalArchiveActivities(t *testing.T, destination string) *ActivityArchiveActivities {
	cfg := &config.Configuration{Activity: config.ActivityConfig{
		HotWindowDays: 90,
		Archive: config.ArchiveConfig{
			Enabled:     true,
			Destination: destination,
			LocalDir:    t.TempDir(),
			KeyPrefix:   "activity_logs",
			RowsPerFile: 10,
		},
	}}
	return NewActivityArchiveActivities(nil, cfg, nil, logger.NewNoopLogger())
}

func requireNonRetryable(t *testing.T, err error, errType string) {
	t.Helper()
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
	require.Equal(t, errType, appErr.Type())
}

func TestActivityArchive_LocalManifestRoundTrip(t *testing.T) {
	a := newLocalArchiveActivities(t, activityArchiveDestinationLocal)
	ctx := context.Background()

	_, found, err := a.loadManifest(ctx, "activity_logs_2026_05")
	require.NoError(t, err)
	require.False(t, found)

	want := cronModels.ActivityArchiveExportResult{Partition: "activity_logs_2026_05", Counts: map[string]int{"t": 3}, Objects: []string{"k"}}
	data, err := json.Marshal(want)
	require.NoError(t, err)
	require.NoError(t, a.put(ctx, a.manifestKey(want.Partition), data, "json", "application/json"))
	require.Equal(t, "activity_logs/_manifests/activity_logs_2026_05.json", a.manifestKey(want.Partition))

	got, found, err := a.loadManifest(ctx, want.Partition)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, *got)
}

func TestActivityArchive_DropRefusesWithoutManifest(t *testing.T) {
	a := newLocalArchiveActivities(t, activityArchiveDestinationLocal)
	err := a.DropPartitionActivity(context.Background(), cronModels.ActivityArchiveExportResult{Partition: "activity_logs_2026_05"})
	requireNonRetryable(t, err, "ArchiveManifestMissing")
}

func TestActivityArchive_DropRefusesWhenArchivedObjectMissing(t *testing.T) {
	a := newLocalArchiveActivities(t, activityArchiveDestinationLocal)
	ctx := context.Background()
	m := cronModels.ActivityArchiveExportResult{
		Partition: "activity_logs_2026_05",
		Counts:    map[string]int{"t": 1},
		Objects:   []string{"activity_logs/tenant_id=t/year=2026/month=05/part-0.parquet"},
	}
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, a.put(ctx, a.manifestKey(m.Partition), data, "json", "application/json"))

	err = a.DropPartitionActivity(ctx, m)
	requireNonRetryable(t, err, "ArchiveObjectMissing")
}

func TestActivityArchive_RejectsNonPartitionNames(t *testing.T) {
	a := newLocalArchiveActivities(t, activityArchiveDestinationLocal)
	ctx := context.Background()

	_, err := a.ExportPartitionActivity(ctx, cronModels.ActivityArchiveExportInput{Partition: "activity_logs; DROP TABLE tenants"})
	requireNonRetryable(t, err, "InvalidPartition")
	err = a.DropPartitionActivity(ctx, cronModels.ActivityArchiveExportResult{Partition: "activity_logs"})
	requireNonRetryable(t, err, "InvalidPartition")
}

func TestActivityArchive_S3WithoutStorageNeverFallsBackToLocal(t *testing.T) {
	a := newLocalArchiveActivities(t, activityArchiveDestinationS3)
	err := a.put(context.Background(), "activity_logs/x.parquet", []byte("x"), "parquet", "")
	requireNonRetryable(t, err, "ArchiveStorageMissing")
}

func TestActivityArchive_ListSkipsWhenArchiveDisabled(t *testing.T) {
	a := newLocalArchiveActivities(t, activityArchiveDestinationLocal)
	a.cfg.Archive.Enabled = false
	got, err := a.ListArchivablePartitionsActivity(context.Background())
	require.NoError(t, err)
	require.Empty(t, got)
}
