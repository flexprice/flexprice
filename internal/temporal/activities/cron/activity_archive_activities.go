package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/activity/archive"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
)

const (
	activityArchiveDestinationLocal = "local"
	activityArchiveDestinationS3    = "s3"

	activityPartitionsAhead = 3 // months created past the current one

	defaultActivityArchiveRowsPerFile = 50000
)

// ActivityArchiveActivities creates activity_logs partitions ahead and archives old ones to Parquet.
type ActivityArchiveActivities struct {
	db      postgres.IClient
	cfg     config.ActivityConfig
	storage storagetypes.Storage // nil unless activity.archive.destination is s3
	logger  *logger.Logger
}

func NewActivityArchiveActivities(db postgres.IClient, cfg *config.Configuration, storage storagetypes.Storage, log *logger.Logger) *ActivityArchiveActivities {
	return &ActivityArchiveActivities{db: db, cfg: cfg.Activity, storage: storage, logger: log}
}

// MaintainPartitionsActivity creates the current and next three monthly partitions.
// Not gated on archive.enabled: without the current partition every insert fails.
func (a *ActivityArchiveActivities) MaintainPartitionsActivity(ctx context.Context) error {
	for _, name := range archive.NeededAhead(time.Now().UTC(), activityPartitionsAhead) {
		if _, err := a.db.Writer(ctx).ExecContext(ctx, archive.CreatePartitionSQL(name)); err != nil {
			return fmt.Errorf("create partition %s: %w", name, err)
		}
	}
	return nil
}

// ListArchivablePartitionsActivity returns attached partitions whose range ended
// before the hot window, oldest first. Empty when archiving is disabled.
func (a *ActivityArchiveActivities) ListArchivablePartitionsActivity(ctx context.Context) ([]string, error) {
	if !a.cfg.Archive.Enabled {
		a.logger.Info(ctx, "activity archive disabled by config, skipping")
		return nil, nil
	}
	if a.cfg.HotWindowDays <= 0 {
		a.logger.Info(ctx, "activity archive skipped: hot_window_days must be positive", "hot_window_days", a.cfg.HotWindowDays)
		return nil, nil
	}
	rows, err := a.db.Reader(ctx).QueryContext(ctx,
		`SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid JOIN pg_class p ON p.oid = i.inhparent WHERE p.relname = 'activity_logs' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return archive.Archivable(names, time.Now().UTC(), a.cfg.HotWindowDays), nil
}

// ExportPartitionActivity writes one partition to per-tenant Parquet files, then a
// manifest. A rerun whose manifest still matches the partition skips the export.
func (a *ActivityArchiveActivities) ExportPartitionActivity(ctx context.Context, in cronModels.ActivityArchiveExportInput) (*cronModels.ActivityArchiveExportResult, error) {
	start, _, ok := archive.PartitionRange(in.Partition)
	if !ok {
		return nil, temporal.NewNonRetryableApplicationError("not an activity_logs partition: "+in.Partition, "InvalidPartition", nil)
	}

	if m, found, err := a.loadManifest(ctx, in.Partition); err != nil {
		return nil, err
	} else if found {
		counts, exists, err := a.partitionCounts(ctx, a.db.Reader(ctx), in.Partition)
		if err != nil {
			return nil, err
		}
		if !exists || maps.Equal(counts, m.Counts) {
			a.logger.Info(ctx, "activity partition already exported, skipping export", "partition", in.Partition, "attached", exists)
			return m, nil
		}
		a.logger.Info(ctx, "activity partition manifest does not match partition, re-exporting", "partition", in.Partition)
	}

	rowsPerFile := a.cfg.Archive.RowsPerFile
	if rowsPerFile <= 0 {
		rowsPerFile = defaultActivityArchiveRowsPerFile
	}

	rows, err := a.db.Reader(ctx).QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM %s ORDER BY tenant_id, occurred_at, id`, archive.SelectList(), in.Partition))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &cronModels.ActivityArchiveExportResult{Partition: in.Partition, Counts: map[string]int{}, Objects: []string{}}
	var batch []archive.Row
	var curTenant string
	part := 0
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		if err := archive.WriteParquet(&buf, batch); err != nil {
			return err
		}
		key := path.Join(a.cfg.Archive.KeyPrefix,
			"tenant_id="+curTenant,
			fmt.Sprintf("year=%04d", start.Year()),
			fmt.Sprintf("month=%02d", int(start.Month())),
			fmt.Sprintf("part-%d.parquet", part))
		if err := a.put(ctx, key, buf.Bytes(), storagetypes.UploadFormatParquet, "application/vnd.apache.parquet"); err != nil {
			return err
		}
		res.Counts[curTenant] += len(batch)
		res.Objects = append(res.Objects, key)
		batch = batch[:0]
		part++
		return nil
	}
	for rows.Next() {
		var r archive.Row
		if err := rows.Scan(r.ScanDest()...); err != nil {
			return nil, err
		}
		if r.TenantID != curTenant {
			if err := flushBatch(); err != nil {
				return nil, err
			}
			curTenant, part = r.TenantID, 0
		}
		batch = append(batch, r)
		if len(batch) >= rowsPerFile {
			if err := flushBatch(); err != nil {
				return nil, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := flushBatch(); err != nil {
		return nil, err
	}

	// Written last: a manifest's presence means every object above was written.
	data, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	if err := a.put(ctx, a.manifestKey(in.Partition), data, storagetypes.UploadFormatJSON, "application/json"); err != nil {
		return nil, err
	}
	a.logger.Info(ctx, "activity partition exported", "partition", in.Partition, "tenants", len(res.Counts), "objects", len(res.Objects))
	return res, nil
}

// DropPartitionActivity detaches and drops a partition only after the stored
// manifest's objects exist and its per-tenant counts match the primary.
func (a *ActivityArchiveActivities) DropPartitionActivity(ctx context.Context, in cronModels.ActivityArchiveExportResult) error {
	if _, _, ok := archive.PartitionRange(in.Partition); !ok {
		return temporal.NewNonRetryableApplicationError("not an activity_logs partition: "+in.Partition, "InvalidPartition", nil)
	}

	m, found, err := a.loadManifest(ctx, in.Partition)
	if err != nil {
		return err
	}
	if !found {
		return temporal.NewNonRetryableApplicationError("no archive manifest for "+in.Partition+"; refusing to drop", "ArchiveManifestMissing", nil)
	}
	if m.Partition != in.Partition {
		return temporal.NewNonRetryableApplicationError(fmt.Sprintf("manifest for %s names partition %s", in.Partition, m.Partition), "ArchiveManifestMismatch", nil)
	}
	for _, key := range m.Objects {
		ok, err := a.exists(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			return temporal.NewNonRetryableApplicationError(fmt.Sprintf("archived object %s for %s is missing; refusing to drop", key, in.Partition), "ArchiveObjectMissing", nil)
		}
	}

	w := a.db.Writer(ctx)
	counts, exists, err := a.partitionCounts(ctx, w, in.Partition)
	if err != nil {
		return err
	}
	if !exists {
		a.logger.Info(ctx, "activity partition already dropped", "partition", in.Partition)
		return nil
	}
	if !maps.Equal(counts, m.Counts) {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("count mismatch for %s: db=%v manifest=%v; refusing to drop", in.Partition, counts, m.Counts),
			"ArchiveCountMismatch", nil)
	}

	attached, err := a.partitionAttached(ctx, w, in.Partition)
	if err != nil {
		return err
	}
	if attached {
		if _, err := w.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE activity_logs DETACH PARTITION %s`, in.Partition)); err != nil {
			return err
		}
	}
	if _, err := w.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, in.Partition)); err != nil {
		return err
	}
	a.logger.Info(ctx, "activity partition dropped", "partition", in.Partition, "rows", sumCounts(m.Counts))
	return nil
}

// partitionCounts returns rows per tenant (attached or detached) and whether the table exists.
func (a *ActivityArchiveActivities) partitionCounts(ctx context.Context, client *ent.Client, partition string) (map[string]int, bool, error) {
	var exists bool
	rows, err := client.QueryContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, partition)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		if err := rows.Scan(&exists); err != nil {
			rows.Close()
			return nil, false, err
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}

	rows, err = client.QueryContext(ctx, fmt.Sprintf(`SELECT tenant_id, count(*) FROM %s GROUP BY 1`, partition))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var tenant string
		var n int
		if err := rows.Scan(&tenant, &n); err != nil {
			return nil, false, err
		}
		counts[tenant] = n
	}
	return counts, true, rows.Err()
}

func (a *ActivityArchiveActivities) partitionAttached(ctx context.Context, client *ent.Client, partition string) (bool, error) {
	rows, err := client.QueryContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid JOIN pg_class p ON p.oid = i.inhparent WHERE p.relname = 'activity_logs' AND c.relname = $1)`,
		partition)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var attached bool
	for rows.Next() {
		if err := rows.Scan(&attached); err != nil {
			return false, err
		}
	}
	return attached, rows.Err()
}

// manifestKey is under an underscore-prefixed dir, which Hive-style readers skip.
func (a *ActivityArchiveActivities) manifestKey(partition string) string {
	return path.Join(a.cfg.Archive.KeyPrefix, "_manifests", partition+".json")
}

func (a *ActivityArchiveActivities) loadManifest(ctx context.Context, partition string) (*cronModels.ActivityArchiveExportResult, bool, error) {
	key := a.manifestKey(partition)
	ok, err := a.exists(ctx, key)
	if err != nil || !ok {
		return nil, false, err
	}
	data, err := a.get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	var m cronModels.ActivityArchiveExportResult
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, fmt.Errorf("decode archive manifest %s: %w", key, err)
	}
	if m.Counts == nil {
		m.Counts = map[string]int{}
	}
	return &m, true, nil
}

// objectStorage returns the object storage, or nil for local. s3 without storage
// errors rather than falling back to the worker's ephemeral disk.
func (a *ActivityArchiveActivities) objectStorage() (storagetypes.Storage, error) {
	switch a.cfg.Archive.Destination {
	case activityArchiveDestinationS3:
		if a.storage == nil {
			return nil, temporal.NewNonRetryableApplicationError("activity.archive.destination is s3 but no archive storage is configured", "ArchiveStorageMissing", nil)
		}
		return a.storage, nil
	case activityArchiveDestinationLocal, "":
		return nil, nil
	default:
		return nil, temporal.NewNonRetryableApplicationError("unsupported activity.archive.destination: "+a.cfg.Archive.Destination, "ArchiveDestinationInvalid", nil)
	}
}

func (a *ActivityArchiveActivities) localPath(key string) string {
	return filepath.Join(a.cfg.Archive.LocalDir, filepath.FromSlash(key))
}

func (a *ActivityArchiveActivities) put(ctx context.Context, key string, data []byte, format storagetypes.UploadFormat, contentType string) error {
	s, err := a.objectStorage()
	if err != nil {
		return err
	}
	if s != nil {
		_, err := s.Upload(ctx, &storagetypes.UploadRequest{Key: key, Data: data, Format: format, ContentType: contentType})
		return err
	}
	p := a.localPath(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (a *ActivityArchiveActivities) get(ctx context.Context, key string) ([]byte, error) {
	s, err := a.objectStorage()
	if err != nil {
		return nil, err
	}
	if s != nil {
		return s.Download(ctx, key)
	}
	return os.ReadFile(a.localPath(key))
}

func (a *ActivityArchiveActivities) exists(ctx context.Context, key string) (bool, error) {
	s, err := a.objectStorage()
	if err != nil {
		return false, err
	}
	if s != nil {
		return s.Exists(ctx, key)
	}
	_, err = os.Stat(a.localPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func sumCounts(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}
