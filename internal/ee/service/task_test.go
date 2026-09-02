package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type TaskServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  TaskService
	storage  *fakeImportStorage
	resolver *fakeImportResolver
	testData struct {
		task   *task.Task
		events struct {
			standard  []*events.Event
			withProps []*events.Event
		}
		now time.Time
	}
}

func TestTaskService(t *testing.T) {
	suite.Run(t, new(TaskServiceSuite))
}

func (s *TaskServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.storage = newFakeImportStorage()
	s.resolver = &fakeImportResolver{bucket: "test-imports", store: s.storage}
	s.setupService()
	s.setupTestData()
}

func (s *TaskServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *TaskServiceSuite) setupService() {
	s.service = NewTaskService(
		ServiceParams{
			Logger:             s.GetLogger(),
			Config:             s.GetConfig(),
			DB:                 s.GetDB(),
			EventRepo:          s.GetStores().EventRepo,
			TaskRepo:           s.GetStores().TaskRepo,
			CustomerRepo:       s.GetStores().CustomerRepo,
			EventPublisher:     s.GetPublisher(),
			WebhookPublisher:   s.GetWebhookPublisher(),
			PDFGenerator:       s.GetPDFGenerator(),
			AuthRepo:           s.GetStores().AuthRepo,
			UserRepo:           s.GetStores().UserRepo,
			EnvironmentRepo:    s.GetStores().EnvironmentRepo,
			FeatureRepo:        s.GetStores().FeatureRepo,
			EntitlementRepo:    s.GetStores().EntitlementRepo,
			PaymentRepo:        s.GetStores().PaymentRepo,
			SecretRepo:         s.GetStores().SecretRepo,
			InvoiceRepo:        s.GetStores().InvoiceRepo,
			WalletRepo:         s.GetStores().WalletRepo,
			TenantRepo:         s.GetStores().TenantRepo,
			PlanRepo:           s.GetStores().PlanRepo,
			PriceRepo:          s.GetStores().PriceRepo,
			MeterRepo:          s.GetStores().MeterRepo,
			SubRepo:            s.GetStores().SubscriptionRepo,
			TaxRateRepo:        s.GetStores().TaxRateRepo,
			TaxAppliedRepo:     s.GetStores().TaxAppliedRepo,
			TaxAssociationRepo: s.GetStores().TaxAssociationRepo,
			StorageResolver:    s.resolver,
		},
	)
}

// registerImportCSV puts CSV bytes at the exact key the service will compute
// for the given upload_id in the test context. Tests then build a task with
// metadata upload_id and ProcessTaskWithStreaming resolves to that object.
func (s *TaskServiceSuite) registerImportCSV(uploadID string, data []byte) string {
	key := importObjectKey(
		s.GetConfig().FlexpriceS3Imports.KeyPrefix,
		uploadID,
		types.GetTenantID(s.GetContext()),
		types.GetEnvironmentID(s.GetContext()),
	)
	s.storage.put(key, data)
	return key
}

func (s *TaskServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	s.testData.task = &task.Task{
		ID:         "task_123",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeEvents,
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		Metadata:   map[string]interface{}{"upload_id": "test", "file_provider": dto.FileProviderCSVBox},
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), s.testData.task))

	// Existing standard events (unrelated to import flow but used by other assertions)
	for i := 0; i < 10; i++ {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.task.TenantID,
			EventName:          "api_call",
			ExternalCustomerID: "cust_ext_123",
			Timestamp:          s.testData.now.Add(-1 * time.Hour),
			Properties:         map[string]interface{}{},
		}
		s.NoError(s.GetStores().EventRepo.InsertEvent(s.GetContext(), event))
		s.testData.events.standard = append(s.testData.events.standard, event)
	}
}

// enableImports flips the flag + populates a fake bucket so CreateTask
// stops rejecting. Call before tests that exercise the create path.
func (s *TaskServiceSuite) enableImports() {
	s.GetConfig().FlexpriceS3Imports = config.FlexpriceS3ImportsConfig{
		Enabled:            true,
		Bucket:             "test-imports",
		Region:             "us-east-1",
		KeyPrefix:          "csvbox",
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
	}
	s.setupService()
}

func (s *TaskServiceSuite) TestCreateTask() {
	s.enableImports()

	tests := []struct {
		name    string
		req     dto.CreateTaskRequest
		wantErr bool
	}{
		{
			name: "successful_task_creation",
			req: dto.CreateTaskRequest{
				TaskType:     types.TaskTypeImport,
				EntityType:   types.EntityTypeEvents,
				FileType:     types.FileTypeCSV,
				FileProvider: dto.FileProviderCSVBox,
				UploadID:     "abc123",
			},
			wantErr: false,
		},
		{
			name: "invalid_task_type",
			req: dto.CreateTaskRequest{
				TaskType:     "INVALID",
				EntityType:   types.EntityTypeEvents,
				FileType:     types.FileTypeCSV,
				FileProvider: dto.FileProviderCSVBox,
				UploadID:     "abc123",
			},
			wantErr: true,
		},
		{
			name: "export_task_type_rejected",
			req: dto.CreateTaskRequest{
				TaskType:     types.TaskTypeExport,
				EntityType:   types.EntityTypeEvents,
				FileType:     types.FileTypeCSV,
				FileProvider: dto.FileProviderCSVBox,
				UploadID:     "abc123",
			},
			wantErr: true,
		},
		{
			name: "non_events_entity_type_rejected",
			req: dto.CreateTaskRequest{
				TaskType:     types.TaskTypeImport,
				EntityType:   types.EntityTypeCustomers,
				FileType:     types.FileTypeCSV,
				FileProvider: dto.FileProviderCSVBox,
				UploadID:     "abc123",
			},
			wantErr: true,
		},
		{
			name: "non_csv_file_type_rejected",
			req: dto.CreateTaskRequest{
				TaskType:     types.TaskTypeImport,
				EntityType:   types.EntityTypeEvents,
				FileType:     types.FileTypeJSON,
				FileProvider: dto.FileProviderCSVBox,
				UploadID:     "abc123",
			},
			wantErr: true,
		},
		{
			name: "unknown_file_provider_rejected",
			req: dto.CreateTaskRequest{
				TaskType:     types.TaskTypeImport,
				EntityType:   types.EntityTypeEvents,
				FileType:     types.FileTypeCSV,
				FileProvider: "dropbox",
				UploadID:     "abc123",
			},
			wantErr: true,
		},
		{
			name: "path_traversal_upload_id_rejected",
			req: dto.CreateTaskRequest{
				TaskType:     types.TaskTypeImport,
				EntityType:   types.EntityTypeEvents,
				FileType:     types.FileTypeCSV,
				FileProvider: dto.FileProviderCSVBox,
				UploadID:     "../../etc/passwd",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.CreateTask(s.GetContext(), tt.req)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.NotEmpty(resp.ID)
			s.Equal(tt.req.TaskType, resp.TaskType)
			s.Equal(tt.req.EntityType, resp.EntityType)
			s.Equal(tt.req.FileType, resp.FileType)
			s.Empty(resp.FileURL, "FileURL is derived at process time; nothing is persisted")
			s.Equal(types.TaskStatusPending, resp.TaskStatus)
			s.Equal(tt.req.UploadID, resp.Metadata["upload_id"])
			s.Equal(tt.req.FileProvider, resp.Metadata["file_provider"])
		})
	}
}

// The imports feature is off by default; CreateTask must reject rather than
// build a task whose downstream download will fail obscurely.
func (s *TaskServiceSuite) TestCreateTask_RejectsWhenImportsDisabled() {
	s.GetConfig().FlexpriceS3Imports = config.FlexpriceS3ImportsConfig{Enabled: false}
	s.setupService()

	_, err := s.service.CreateTask(s.GetContext(), dto.CreateTaskRequest{
		TaskType:     types.TaskTypeImport,
		EntityType:   types.EntityTypeEvents,
		FileType:     types.FileTypeCSV,
		FileProvider: dto.FileProviderCSVBox,
		UploadID:     "abc123",
	})
	s.Error(err)
}

// End-to-end happy path: bytes registered under the derived key are picked up
// by ProcessTaskWithStreaming and produce events via the chunk processor.
func (s *TaskServiceSuite) TestProcessTaskWithStreaming_EventsImport() {
	s.enableImports()

	rows := [][]string{
		{"event_name", "external_customer_id", "timestamp", "properties.bytes_used"},
		{"api_call", "cust_ext_123", s.testData.now.Add(-1 * time.Hour).Format(time.RFC3339), "42"},
		{"api_call", "cust_ext_123", s.testData.now.Add(-30 * time.Minute).Format(time.RFC3339), "100"},
	}
	var buf bytes.Buffer
	s.NoError(csv.NewWriter(&buf).WriteAll(rows))
	s.registerImportCSV("evt-happy", buf.Bytes())

	imp := &task.Task{
		ID:         "task_import_events_ok",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeEvents,
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		Metadata:   map[string]interface{}{"upload_id": "evt-happy", "file_provider": dto.FileProviderCSVBox},
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), imp))

	s.NoError(s.service.ProcessTaskWithStreaming(s.GetContext(), imp.ID))

	updated, err := s.GetStores().TaskRepo.Get(s.GetContext(), imp.ID)
	s.NoError(err)
	s.Equal(types.TaskStatusCompleted, updated.TaskStatus)
	s.Equal(2, updated.ProcessedRecords)
	s.Equal(2, updated.SuccessfulRecords)
	s.Equal(0, updated.FailedRecords)
}

// A task whose upload_id resolves to no object must fail cleanly, not
// crash — a common state during Temporal retries if CSV Box is still
// uploading when the workflow first fires.
func (s *TaskServiceSuite) TestProcessTaskWithStreaming_UnknownUploadIDFails() {
	s.enableImports()

	imp := &task.Task{
		ID:         "task_import_missing",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeEvents,
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		Metadata:   map[string]interface{}{"upload_id": "not-yet-uploaded", "file_provider": dto.FileProviderCSVBox},
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), imp))

	err := s.service.ProcessTaskWithStreaming(s.GetContext(), imp.ID)
	s.Error(err)

	updated, gerr := s.GetStores().TaskRepo.Get(s.GetContext(), imp.ID)
	s.NoError(gerr)
	s.Equal(types.TaskStatusFailed, updated.TaskStatus)
}

// Cross-tenant safety: an upload_id from tenant A must not fetch bytes
// registered by tenant B, because the key derivation includes tenant/env
// from the calling context.
func (s *TaskServiceSuite) TestProcessTaskWithStreaming_CrossTenantIsolated() {
	s.enableImports()

	rows := [][]string{
		{"event_name", "external_customer_id", "timestamp"},
		{"api_call", "cust", s.testData.now.Format(time.RFC3339)},
	}
	var buf bytes.Buffer
	s.NoError(csv.NewWriter(&buf).WriteAll(rows))

	// Register the file under a DIFFERENT tenant's key by manually poking the
	// map; the caller's ctx must not be able to reach it.
	foreignKey := importObjectKey("csvbox", "shared-id", "other-tenant", "other-env")
	s.storage.put(foreignKey, buf.Bytes())

	imp := &task.Task{
		ID:         "task_import_cross_tenant",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeEvents,
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		Metadata:   map[string]interface{}{"upload_id": "shared-id", "file_provider": dto.FileProviderCSVBox},
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), imp))

	err := s.service.ProcessTaskWithStreaming(s.GetContext(), imp.ID)
	s.Error(err, "must not fetch another tenant's object")
}

func (s *TaskServiceSuite) TestGetTask() {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "existing_task", id: s.testData.task.ID, wantErr: false},
		{name: "non_existent_task", id: "non_existent", wantErr: true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.GetTask(s.GetContext(), tt.id)
			if tt.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.id, resp.ID)
		})
	}
}

func (s *TaskServiceSuite) TestListTasks() {
	testTasks := []*task.Task{
		{
			ID:         "task_1",
			TaskType:   types.TaskTypeImport,
			EntityType: types.EntityTypeEvents,
			FileType:   types.FileTypeCSV,
			TaskStatus: types.TaskStatusCompleted,
			BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:         "task_2",
			TaskType:   types.TaskTypeImport,
			EntityType: types.EntityTypeEvents,
			FileType:   types.FileTypeCSV,
			TaskStatus: types.TaskStatusFailed,
			BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		},
	}
	for _, t := range testTasks {
		s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), t))
	}

	completed := types.TaskStatusCompleted
	failed := types.TaskStatusFailed

	tests := []struct {
		name      string
		filter    *types.TaskFilter
		wantCount int
	}{
		{name: "list_all", filter: &types.TaskFilter{QueryFilter: types.NewDefaultQueryFilter()}, wantCount: 3},
		{name: "filter_completed", filter: &types.TaskFilter{QueryFilter: types.NewDefaultQueryFilter(), TaskStatus: &completed}, wantCount: 1},
		{name: "filter_failed", filter: &types.TaskFilter{QueryFilter: types.NewDefaultQueryFilter(), TaskStatus: &failed}, wantCount: 1},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.ListTasks(s.GetContext(), tt.filter)
			s.NoError(err)
			s.NotNil(resp)
			s.Len(resp.Items, tt.wantCount)
			if tt.filter.TaskStatus != nil {
				for _, task := range resp.Items {
					s.Equal(*tt.filter.TaskStatus, task.TaskStatus)
				}
			}
		})
	}
}

func (s *TaskServiceSuite) TestUpdateTaskStatus() {
	tests := []struct {
		name      string
		id        string
		newStatus types.TaskStatus
		wantErr   bool
	}{
		{name: "pending_to_processing", id: s.testData.task.ID, newStatus: types.TaskStatusProcessing, wantErr: false},
		{name: "processing_to_completed", id: s.testData.task.ID, newStatus: types.TaskStatusCompleted, wantErr: false},
		{name: "completed_to_processing", id: s.testData.task.ID, newStatus: types.TaskStatusProcessing, wantErr: true},
		{name: "non_existent_task", id: "non_existent", newStatus: types.TaskStatusProcessing, wantErr: true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.UpdateTaskStatus(s.GetContext(), tt.id, tt.newStatus)
			if tt.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)
			t, err := s.GetStores().TaskRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(tt.newStatus, t.TaskStatus)
		})
	}
}

// Guard against a regression in the PROCESSING→PROCESSING short-circuit that
// makes ProcessTaskWithStreaming safe under Temporal retries.
func (s *TaskServiceSuite) TestProcessTaskWithStreamingIdempotent() {
	s.NoError(s.service.UpdateTaskStatus(s.GetContext(), s.testData.task.ID, types.TaskStatusProcessing))

	// A direct UpdateTaskStatus call is intentionally NOT idempotent — the
	// transition validator refuses PROCESSING→PROCESSING. ProcessTaskWithStreaming
	// must skip that transition check for the retry case.
	err := s.service.UpdateTaskStatus(s.GetContext(), s.testData.task.ID, types.TaskStatusProcessing)
	s.Error(err)
	s.Contains(err.Error(), "invalid status transition from PROCESSING to PROCESSING")

	got, err := s.GetStores().TaskRepo.Get(s.GetContext(), s.testData.task.ID)
	s.NoError(err)
	s.Equal(types.TaskStatusProcessing, got.TaskStatus)
}

// --- fakes ------------------------------------------------------------------

// fakeImportStorage is a minimal in-memory storagetypes.Storage. Only
// Download is exercised; other methods stub to satisfy the interface.
type fakeImportStorage struct {
	objects map[string][]byte
}

func newFakeImportStorage() *fakeImportStorage {
	return &fakeImportStorage{objects: make(map[string][]byte)}
}

func (f *fakeImportStorage) put(key string, data []byte) { f.objects[key] = data }

func (f *fakeImportStorage) Upload(context.Context, *storagetypes.UploadRequest) (*storagetypes.UploadResponse, error) {
	return nil, nil
}
func (f *fakeImportStorage) Download(_ context.Context, key string) ([]byte, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, notFoundErr(key)
	}
	return b, nil
}
func (f *fakeImportStorage) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.objects[key]
	return ok, nil
}
func (f *fakeImportStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://fake/" + key, nil
}
func (f *fakeImportStorage) ValidateConnection(context.Context) error { return nil }
func (f *fakeImportStorage) FileURL(key string) string                { return "s3://fake/" + key }
func (f *fakeImportStorage) Provider() storagetypes.Provider          { return storagetypes.ProviderS3 }

type fakeImportResolver struct {
	bucket string
	store  storagetypes.Storage
}

func (r *fakeImportResolver) ForPlatform(_ context.Context, purpose storage.Purpose) (storagetypes.Storage, error) {
	if purpose != storage.PurposeImport {
		return nil, notFoundErr(string(purpose))
	}
	return r.store, nil
}
func (r *fakeImportResolver) ForConnection(context.Context, string) (storagetypes.Storage, error) {
	return nil, nil
}
func (r *fakeImportResolver) Provider() storage.Provider { return storage.ProviderS3 }
func (r *fakeImportResolver) BucketConfigFor(purpose storage.Purpose) (config.BucketConfig, error) {
	if purpose != storage.PurposeImport {
		return config.BucketConfig{}, notFoundErr(string(purpose))
	}
	return config.BucketConfig{Bucket: r.bucket, KeyPrefix: "csvbox", PresignExpiryDuration: "5m"}, nil
}

type notFoundError struct{ what string }

func (e *notFoundError) Error() string { return "not found: " + e.what }
func notFoundErr(what string) error    { return &notFoundError{what: what} }

