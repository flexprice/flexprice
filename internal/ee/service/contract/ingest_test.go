package contract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

var pdfBytes = []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n")

type memStorage struct {
	objects map[string][]byte
	types   map[string]string
	failPut bool
}

func newMemStorage() *memStorage {
	return &memStorage{objects: map[string][]byte{}, types: map[string]string{}}
}

func (m *memStorage) Upload(_ context.Context, req *storagetypes.UploadRequest) (*storagetypes.UploadResponse, error) {
	if m.failPut {
		return nil, errors.New("put failed")
	}
	m.objects[req.Key] = req.Data
	m.types[req.Key] = req.ContentType
	return &storagetypes.UploadResponse{Key: req.Key}, nil
}

func (m *memStorage) UploadStream(context.Context, *storagetypes.StreamUploadRequest) (*storagetypes.UploadResponse, error) {
	return nil, nil
}

func (m *memStorage) Download(_ context.Context, key string) ([]byte, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return data, nil
}

func (m *memStorage) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.objects[key]
	return ok, nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	delete(m.types, key)
	return nil
}

func (m *memStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (m *memStorage) ValidateConnection(context.Context) error { return nil }

func (m *memStorage) ValidateWrite(context.Context, string) error { return nil }

func (m *memStorage) FileURL(key string) string { return "s3://test/" + key }

func (m *memStorage) Provider() storagetypes.Provider { return storagetypes.ProviderS3 }

type memResolver struct{ store *memStorage }

func (r *memResolver) ForPlatform(_ context.Context, purpose storage.Purpose) (storagetypes.Storage, error) {
	if purpose != storage.PurposeImport {
		return nil, errors.New("unexpected purpose")
	}
	return r.store, nil
}

func (r *memResolver) ForConnection(context.Context, string) (storagetypes.Storage, error) {
	return nil, nil
}

func (r *memResolver) ForConnectionExport(context.Context, string, *types.S3JobConfig) (storagetypes.Storage, error) {
	return nil, nil
}

func (r *memResolver) Provider() storage.Provider { return storage.ProviderS3 }

func (r *memResolver) BucketConfigFor(storage.Purpose) (config.BucketConfig, error) {
	return config.BucketConfig{}, nil
}

type fakeStarter struct {
	inputs []models.ContractExtractionWorkflowInput
	err    error
}

func (f *fakeStarter) ExecuteWorkflow(_ context.Context, wt types.TemporalWorkflowType, params interface{}) (models.WorkflowRun, error) {
	if wt != types.TemporalContractExtractionWorkflow {
		return nil, errors.New("unexpected workflow")
	}
	if f.err != nil {
		return nil, f.err
	}
	f.inputs = append(f.inputs, params.(models.ContractExtractionWorkflowInput))
	return nil, nil
}

type fakeExtractor struct {
	out       *extraction.Extraction
	err       error
	enrichErr error
	requests  []*extraction.ExtractRequest
}

func (f *fakeExtractor) Extract(_ context.Context, req *extraction.ExtractRequest) (*extraction.Extraction, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

type ingestFixture struct {
	svc       *contractIngestService
	contracts *testutil.InMemoryContractStore
	store     *memStorage
	starter   *fakeStarter
	extractor *fakeExtractor
	matcher   *fakeMatcher
	customers *testutil.InMemoryCustomerStore
}

func newIngestFixture() *ingestFixture {
	f := &ingestFixture{
		contracts: testutil.NewInMemoryContractStore(),
		store:     newMemStorage(),
		starter:   &fakeStarter{},
		matcher:   &fakeMatcher{configured: true},
		customers: testutil.NewInMemoryCustomerStore(),
		extractor: &fakeExtractor{out: &extraction.Extraction{
			Result: northwind(), Model: "test/model",
			Usage: &extraction.Usage{Pages: 4, Fields: 61, DurationSeconds: 8},
		}},
	}
	cfg := &config.Configuration{
		FlexpriceS3Imports: config.FlexpriceS3ImportsConfig{KeyPrefix: "/imports/"},
		CustomerMatch:      config.CustomerMatchConfig{Enabled: true},
	}
	svc := NewContractIngestService(f.contracts, &memResolver{store: f.store}, f.extractor, f.matcher, &storeCustomerService{store: f.customers}, cfg, logger.NewNoopLogger()).(*contractIngestService)
	svc.starter = f.starter
	f.svc = svc
	return f
}

func TestUpload_CreatesPendingContractAndEnqueues(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()

	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "northwind-msa.pdf", Data: pdfBytes})
	require.NoError(t, err)

	require.Equal(t, types.ContractStatusDraft, c.ContractStatus())
	require.Equal(t, types.StatusPublished, c.Status())
	require.Equal(t, types.ContractExtractionStatusPending, c.ExtractionStatus())
	require.Empty(t, c.CustomerID())
	require.Equal(t, types.DefaultTenantID, c.TenantID())
	require.Equal(t, "env_sandbox", c.EnvironmentID())

	doc := c.Document()
	require.Equal(t, "imports/contracts/"+types.DefaultTenantID+"/env_sandbox/"+c.ID()+".pdf", doc.Key)
	require.Equal(t, "northwind-msa.pdf", doc.FileName)
	require.Equal(t, contractagent.MediaTypePDF, doc.MediaType)
	require.Equal(t, int64(len(pdfBytes)), doc.SizeBytes)
	require.Len(t, doc.SHA256, 64)
	require.Equal(t, pdfBytes, f.store.objects[doc.Key])
	require.Equal(t, contractagent.MediaTypePDF, f.store.types[doc.Key])

	stored, err := f.contracts.GetByID(ctx, c.ID())
	require.NoError(t, err)
	require.Equal(t, types.ContractExtractionStatusPending, stored.ExtractionStatus())
	require.Equal(t, types.DefaultTenantID, stored.TenantID())
	require.Equal(t, "env_sandbox", stored.EnvironmentID())
	require.Equal(t, []models.ContractExtractionWorkflowInput{{ContractID: c.ID()}}, f.starter.inputs)
}

func TestUpload_Rejections(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "not a contract file", data: []byte("PK\x03\x04 this is a docx zip")},
		{name: "too large", data: append(append([]byte{}, pdfBytes...), make([]byte, maxUploadBytes)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newIngestFixture()
			_, err := f.svc.Upload(testutil.SetupContext(), &UploadRequest{FileName: "x", Data: tt.data})
			require.Error(t, err)
			require.True(t, ierr.IsValidation(err), "unexpected error kind: %v", err)
			require.Empty(t, f.store.objects)
			require.Empty(t, f.starter.inputs)
		})
	}
}

func TestUpload_StorageFailureCreatesNothing(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.store.failPut = true

	_, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.Error(t, err)
	n, err := f.contracts.Count(ctx, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, f.starter.inputs)
}

// failingCreateRepo fails every contract insert.
type failingCreateRepo struct {
	*testutil.InMemoryContractStore
}

func (r *failingCreateRepo) Create(context.Context, *domainContract.Contract) error {
	return ierr.NewError("insert failed").Mark(ierr.ErrDatabase)
}

func TestUpload_CreateFailureRemovesStoredFile(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.svc.contracts = &failingCreateRepo{InMemoryContractStore: f.contracts}

	_, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})

	require.True(t, ierr.IsDatabase(err))
	require.Empty(t, f.store.objects)
	require.Empty(t, f.starter.inputs)
}

func TestUpload_EnqueueFailureMarksContractFailed(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.starter.err = ierr.NewError("temporal down").Mark(ierr.ErrServiceUnavailable)

	_, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.Error(t, err)

	list, err := f.contracts.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, types.ContractExtractionStatusFailed, list[0].ExtractionStatus())
	require.Equal(t, "Could not start processing the contract", list[0].ExtractionError())
}

func TestProcess_StoresExtraction(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)

	require.NoError(t, f.svc.Process(ctx, c.ID()))

	require.Len(t, f.extractor.requests, 1)
	require.Equal(t, &extraction.ExtractRequest{FileName: "c.pdf", MediaType: contractagent.MediaTypePDF, Data: pdfBytes}, f.extractor.requests[0])

	stored, err := f.contracts.GetByID(ctx, c.ID())
	require.NoError(t, err)
	require.Equal(t, types.ContractExtractionStatusCompleted, stored.ExtractionStatus())
	require.Empty(t, stored.ExtractionError())

	var got extraction.Extraction
	require.NoError(t, json.Unmarshal(stored.Extraction(), &got))
	require.Equal(t, "test/model", got.Model)
	require.Equal(t, 4, got.Usage.Pages)
	require.Equal(t, "Northwind Health, Inc.", got.Result.Customer.LegalName.Value)
	require.Equal(t, &extraction.ToCheck{Total: 12, Customer: 0, Charges: 7, Billing: 5}, got.Result.ToCheck)
	require.Equal(t, &extraction.CustomerMatch{Status: CustomerMatchPending}, got.Result.CustomerMatch)
	require.Equal(t, "2027-10-31", got.Result.Derived.EndDate.Value)

	require.NoError(t, f.svc.Process(ctx, c.ID()))
	require.Len(t, f.extractor.requests, 1, "a completed contract is not extracted again")
}

func TestProcess_ExtractErrorLeavesProcessing(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)
	f.extractor.err = ierr.NewError("rate limited").Mark(ierr.ErrTooManyRequests)

	err = f.svc.Process(ctx, c.ID())
	require.True(t, ierr.IsTooManyRequests(err))
	stored, _ := f.contracts.GetByID(ctx, c.ID())
	require.Equal(t, types.ContractExtractionStatusProcessing, stored.ExtractionStatus())
}

func TestProcess_ContractWithoutDocument(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)
	stored, _ := f.contracts.GetByID(ctx, c.ID())
	require.NoError(t, f.contracts.Update(ctx, newBuilderWithoutDocument(stored)))

	err = f.svc.Process(ctx, c.ID())
	require.True(t, ierr.IsInvalidOperation(err))
}

func TestMarkFailed(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)

	long := make([]byte, maxErrorLength+50)
	for i := range long {
		long[i] = 'x'
	}
	require.NoError(t, f.svc.MarkFailed(ctx, c.ID(), string(long)))
	stored, _ := f.contracts.GetByID(ctx, c.ID())
	require.Equal(t, types.ContractExtractionStatusFailed, stored.ExtractionStatus())
	require.Len(t, stored.ExtractionError(), maxErrorLength)

	require.NoError(t, f.svc.Process(ctx, c.ID()))
	require.NoError(t, f.svc.MarkFailed(ctx, c.ID(), "late failure"))
	stored, _ = f.contracts.GetByID(ctx, c.ID())
	require.Equal(t, types.ContractExtractionStatusCompleted, stored.ExtractionStatus(), "a completed contract is never marked failed")
}

func newBuilderWithoutDocument(c *domainContract.Contract) *domainContract.Contract {
	return domainContract.NewBuilder(c).WithDocument(nil).Build()
}
