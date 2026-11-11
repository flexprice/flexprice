package export

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	result    *dto.AnalyticsQueryResult
	viewAt    time.Time
	err       error
	gotRange  string
	gotKey    string
	gotViewID string
	gotParams *types.ReportParams
	calls     int
}

func (f *fakeRunner) RunReportForExport(_ context.Context, key string, params *types.ReportParams, rng string) (*dto.AnalyticsQueryResult, error) {
	f.gotKey, f.gotRange, f.gotParams = key, rng, params
	f.calls++
	return f.result, f.err
}

func (f *fakeRunner) RunViewForExport(_ context.Context, viewID, rng string) (*dto.AnalyticsQueryResult, time.Time, error) {
	f.gotViewID, f.gotRange = viewID, rng
	return f.result, f.viewAt, f.err
}

func reportRequest(entity types.ScheduledTaskEntityType) *dto.ExportRequest {
	req := managedRequest(true, platformCfg("exports/t1/e1/st_1"))
	req.EntityType = entity
	req.SourceConfig = &types.ExportSourceConfig{ReportKey: "rk", ViewID: "view_1"}
	return req
}

func TestReportExporter_PrepareDataCSV(t *testing.T) {
	runner := &fakeRunner{result: &dto.AnalyticsQueryResult{
		Columns: []*dto.AnalyticsColumn{{Name: "a"}, {Name: "b"}},
		Rows:    [][]string{{"1", "x,y"}, {"2", "z"}},
	}}
	exp := NewReportExporter(runner, types.ScheduledTaskEntityTypeReport, nil)

	data, n, err := exp.PrepareData(context.Background(), reportRequest(types.ScheduledTaskEntityTypeReport))

	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "a,b\n1,\"x,y\"\n2,z\n", string(data))
	require.Equal(t, "2026-01-01..2026-01-02", runner.gotRange)
	require.Equal(t, "rk", runner.gotKey)
	require.Equal(t, "report", exp.GetFilenamePrefix())
}

func TestReportExporter_EmptyResultHeaderOnly(t *testing.T) {
	runner := &fakeRunner{result: &dto.AnalyticsQueryResult{Columns: []*dto.AnalyticsColumn{{Name: "a"}}}}
	exp := NewReportExporter(runner, types.ScheduledTaskEntityTypeReport, nil)

	data, n, err := exp.PrepareData(context.Background(), reportRequest(types.ScheduledTaskEntityTypeReport))

	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, "a\n", string(data))
}

func TestReportExporter_ViewMetadata(t *testing.T) {
	runner := &fakeRunner{
		result: &dto.AnalyticsQueryResult{Columns: []*dto.AnalyticsColumn{{Name: "a"}}},
		viewAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
	}
	exp := NewReportExporter(runner, types.ScheduledTaskEntityTypeAnalyticsView, nil)

	_, _, err := exp.PrepareData(context.Background(), reportRequest(types.ScheduledTaskEntityTypeAnalyticsView))

	require.NoError(t, err)
	require.Equal(t, "view_1", runner.gotViewID)
	require.Equal(t, "2026-11-01T10:00:00Z", exp.RunMetadata()["view_updated_at"])
	require.Equal(t, "analytics_view", exp.GetFilenamePrefix())
}

func TestReportExporter_ReportHasNoMetadata(t *testing.T) {
	runner := &fakeRunner{result: &dto.AnalyticsQueryResult{}}
	exp := NewReportExporter(runner, types.ScheduledTaskEntityTypeReport, nil)
	_, _, err := exp.PrepareData(context.Background(), reportRequest(types.ScheduledTaskEntityTypeReport))
	require.NoError(t, err)
	require.Empty(t, exp.RunMetadata())
}

func TestReportExporter_RunnerErrorFailsWithoutUpload(t *testing.T) {
	store := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
	svc := managedService(store, nil, &panicConnRepo{})
	svc.reportRunner = &fakeRunner{err: errors.New("boom")}

	_, err := svc.Export(context.Background(), reportRequest(types.ScheduledTaskEntityTypeReport))

	require.Error(t, err)
	require.Empty(t, store.keys)
}

func TestExport_ReportMetadataInResponse(t *testing.T) {
	store := &recordingStore{provider: storage.ProviderS3, bucket: "platform-bucket"}
	svc := managedService(store, nil, &panicConnRepo{})
	svc.reportRunner = &fakeRunner{
		result: &dto.AnalyticsQueryResult{Columns: []*dto.AnalyticsColumn{{Name: "a"}}, Rows: [][]string{{"1"}}},
		viewAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
	}

	resp, err := svc.Export(context.Background(), reportRequest(types.ScheduledTaskEntityTypeAnalyticsView))

	require.NoError(t, err)
	require.Equal(t, map[string]string{"view_updated_at": "2026-11-01T10:00:00Z"}, resp.Metadata)
	require.Len(t, store.keys, 1)
}

func TestExport_ReportWithoutRunnerUnsupported(t *testing.T) {
	svc := managedService(&recordingStore{}, nil, &panicConnRepo{})
	_, err := svc.Export(context.Background(), reportRequest(types.ScheduledTaskEntityTypeReport))
	require.Error(t, err)
}

func TestReportExporter_NilResultIsError(t *testing.T) {
	for _, entity := range []types.ScheduledTaskEntityType{types.ScheduledTaskEntityTypeReport, types.ScheduledTaskEntityTypeAnalyticsView} {
		exp := NewReportExporter(&fakeRunner{}, entity, nil)

		_, _, err := exp.PrepareData(context.Background(), reportRequest(entity))

		require.Error(t, err)
	}
}
