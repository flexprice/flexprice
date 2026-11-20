package contract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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
	products  *storeProductService
}

func newIngestFixture() *ingestFixture {
	f := &ingestFixture{
		contracts: testutil.NewInMemoryContractStore(),
		store:     newMemStorage(),
		starter:   &fakeStarter{},
		matcher:   &fakeMatcher{configured: true},
		customers: testutil.NewInMemoryCustomerStore(),
		products:  &storeProductService{store: testutil.NewInMemoryProductStore()},
		extractor: &fakeExtractor{out: &extraction.Extraction{
			Result: northwind(), Model: "test/model",
			Usage: &extraction.Usage{Pages: 4, Fields: 61, DurationSeconds: 8},
		}},
	}
	cfg := &config.Configuration{
		FlexpriceS3Imports: config.FlexpriceS3ImportsConfig{KeyPrefix: "/imports/"},
		CustomerMatch:      config.CustomerMatchConfig{Enabled: true},
	}
	svc := NewContractIngestService(f.contracts, nil, nil, NewContractService(f.contracts), nil, &memResolver{store: f.store}, f.extractor, f.matcher, &storeCustomerService{store: f.customers}, f.products, cfg, logger.NewNoopLogger()).(*contractIngestService)
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

// fakeSubscriptions stands in for the subscription service: it records the subscription it creates and activates.
type fakeSubscriptions struct {
	interfaces.SubscriptionService
	created   []dto.CreateSubscriptionRequest
	activated []dto.ActivateDraftSubscriptionRequest
}

func (f *fakeSubscriptions) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	f.created = append(f.created, req)
	return &dto.SubscriptionResponse{Subscription: &subscription.Subscription{ID: "subs_1", CustomerID: req.CustomerID, BaseModel: types.GetDefaultBaseModel(ctx)}}, nil
}

func (f *fakeSubscriptions) ActivateDraftSubscription(_ context.Context, _ string, req dto.ActivateDraftSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	f.activated = append(f.activated, req)
	return &dto.SubscriptionResponse{}, nil
}

// fakeCatalogServices creates customers and products with IDs from their external ID and lookup key.
type fakeCatalogServices struct {
	interfaces.CustomerService
	interfaces.ProductService
}

func (fakeCatalogServices) CreateCustomer(_ context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error) {
	return &dto.CustomerResponse{Customer: &customer.Customer{ID: "cust_" + req.ExternalID}}, nil
}

func (fakeCatalogServices) CreateProduct(_ context.Context, req dto.CreateProductRequest) (*dto.ProductResponse, error) {
	return &dto.ProductResponse{ID: "prod_" + req.LookupKey, LookupKey: req.LookupKey}, nil
}

var (
	termStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	termEnd   = time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
)

type completeFixture struct {
	svc  *contractIngestService
	pobs *testutil.InMemoryPOBStore
	subs *fakeSubscriptions
}

func newCompleteFixture(t *testing.T, ctx context.Context, contract *domainContract.Builder) *completeFixture {
	contracts := testutil.NewInMemoryContractStore()
	pobs := testutil.NewInMemoryPOBStore(contracts)
	subs := &fakeSubscriptions{}
	require.NoError(t, contracts.Create(ctx, contract.WithID("contract_1").WithStatus(types.StatusPublished).Build()))

	pobService := pobsvc.NewPerformanceObligationService(pobs, contracts, nil, nil, nil, nil, nil)
	svc := NewContractIngestService(contracts, testutil.NewMockPostgresClient(nil), subs, NewContractService(contracts), pobService,
		nil, nil, nil, fakeCatalogServices{}, fakeCatalogServices{}, &config.Configuration{}, logger.NewNoopLogger()).(*contractIngestService)
	return &completeFixture{svc: svc, pobs: pobs, subs: subs}
}

func draftContract() *domainContract.Builder {
	return domainContract.NewBuilder(nil).WithContractStatus(types.ContractStatusDraft)
}

func completeRequest(activate bool, pobs ...dto.CreatePOBRequest) *dto.CompleteDraftRequest {
	return &dto.CompleteDraftRequest{
		Contract:               dto.CreateContractRequest{CustomerID: "cust_1", Currency: "usd", TermStart: lo.ToPtr(termStart), TermEnd: termEnd},
		Subscriptions:          []dto.CreateSubscriptionRequest{{PlanID: "plan_1", BillingPeriod: types.BILLING_PERIOD_MONTHLY}},
		PerformanceObligations: pobs,
		Activate:               activate,
	}
}

func platformPOB(amount int64) dto.CreatePOBRequest {
	return dto.CreatePOBRequest{Name: "Platform", ProductID: "prod_platform", RecognitionMethod: types.RecognitionMethodStraightLine,
		ServiceStart: termStart, ServiceEnd: lo.ToPtr(termEnd), AllocatedAmount: decimal.NewFromInt(amount)}
}

func TestCompleteDraft_AttachesDraftSubscriptionAndPOBs(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newCompleteFixture(t, ctx, draftContract())

	resp, err := f.svc.CompleteDraft(ctx, "contract_1", completeRequest(false, platformPOB(120)))
	require.NoError(t, err)

	require.Equal(t, types.ContractStatusDraft, resp.ContractStatus)
	require.Equal(t, "cust_1", resp.CustomerID)
	require.Equal(t, "usd", resp.Currency)
	require.True(t, resp.TermStart.Equal(termStart))
	require.True(t, resp.TermEnd.Equal(termEnd))
	require.Len(t, resp.Associations, 1)
	require.Equal(t, "subs_1", resp.Associations[0].EntityID)
	require.Empty(t, f.subs.activated)

	require.Len(t, f.subs.created, 1)
	sub := f.subs.created[0]
	require.Equal(t, types.SubscriptionStatusDraft, sub.SubscriptionStatus)
	require.Equal(t, "cust_1", sub.CustomerID)
	require.Equal(t, "usd", sub.Currency)
	require.True(t, sub.StartDate.Equal(termStart), "the subscription starts with the term")
	require.True(t, sub.EndDate.Equal(termEnd), "the subscription ends with the term")

	pobs, err := f.pobs.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, pobs, 1)
	require.Equal(t, "prod_platform", pobs[0].ProductID())
	require.Equal(t, "120", pobs[0].AllocatedAmount().String())
	require.Equal(t, types.PerformanceObligationStatusDraft, pobs[0].POBStatus())
}

func TestCompleteDraft_CreatesCustomerAndCatalog(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newCompleteFixture(t, ctx, draftContract())
	req := completeRequest(false, dto.CreatePOBRequest{Name: "Agent usage", ProductLookupKey: "agent_usage",
		RecognitionMethod: types.RecognitionMethodAsConsumed, ServiceStart: termStart})
	req.Contract.CustomerID = ""
	req.Customer = &dto.CreateCustomerRequest{ExternalID: "acme", Name: "Acme"}
	req.Products = []dto.CreateProductCatalogRequest{{
		Product: dto.CreateProductRequest{Name: "Agent usage", LookupKey: "agent_usage"},
		Prices: []dto.SubscriptionPriceCreateRequest{
			{Type: types.PRICE_TYPE_USAGE, PriceUnitType: types.PRICE_UNIT_TYPE_FIAT, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
				BillingModel: types.BILLING_MODEL_FLAT_FEE, InvoiceCadence: types.InvoiceCadenceArrear, MeterID: "meter_1", Amount: lo.ToPtr(decimal.NewFromInt(1))},
		},
	}}

	resp, err := f.svc.CompleteDraft(ctx, "contract_1", req)
	require.NoError(t, err)

	require.Equal(t, "cust_acme", resp.CustomerID)
	sub := f.subs.created[0]
	require.Equal(t, "cust_acme", sub.CustomerID)
	require.Len(t, sub.LineItems, 1)
	require.Equal(t, "prod_agent_usage", sub.LineItems[0].Price.ProductID, "the price belongs to the created product")

	pobs, err := f.pobs.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, pobs, 1)
	require.Equal(t, "prod_agent_usage", pobs[0].ProductID(), "the POB names the created product by lookup key")
}

func TestCompleteDraft_ActivatesFromTermStart(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newCompleteFixture(t, ctx, draftContract())

	_, err := f.svc.CompleteDraft(ctx, "contract_1", completeRequest(true))
	require.NoError(t, err)
	require.Len(t, f.subs.activated, 1)
	require.True(t, f.subs.activated[0].StartDate.Equal(termStart))
}

func TestCompleteDraft_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		contract *domainContract.Builder
		req      func(*dto.CompleteDraftRequest)
		twice    bool
		wantErr  func(error) bool
	}{
		{name: "no customer", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) { r.Contract.CustomerID = "" }},
		{name: "customer twice", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			r.Customer = &dto.CreateCustomerRequest{ExternalID: "acme"}
		}},
		{name: "two subscriptions", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			r.Subscriptions = append(r.Subscriptions, r.Subscriptions[0])
		}},
		{name: "activate without term start", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			r.Activate, r.Contract.TermStart = true, nil
		}},
		{name: "term ends before it starts", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			r.Contract.TermEnd = termStart.AddDate(0, 0, -1)
		}},
		{name: "negative allocation", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			r.PerformanceObligations = []dto.CreatePOBRequest{platformPOB(-1)}
		}},
		{name: "POB with product id and lookup key", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			p := platformPOB(0)
			p.ProductLookupKey = "platform"
			r.PerformanceObligations = []dto.CreatePOBRequest{p}
		}},
		{name: "unknown product lookup key", contract: draftContract(), wantErr: ierr.IsValidation, req: func(r *dto.CompleteDraftRequest) {
			p := platformPOB(0)
			p.ProductID, p.ProductLookupKey = "", "missing"
			r.PerformanceObligations = []dto.CreatePOBRequest{p}
		}},
		{name: "currency mismatch", contract: draftContract().WithCurrency("eur"), wantErr: ierr.IsValidation},
		{name: "not a draft", contract: domainContract.NewBuilder(nil).WithContractStatus(types.ContractStatusActive), wantErr: ierr.IsInvalidOperation},
		{name: "already completed", contract: draftContract(), twice: true, wantErr: ierr.IsInvalidOperation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			f := newCompleteFixture(t, ctx, tt.contract)
			req := completeRequest(false)
			if tt.req != nil {
				tt.req(req)
			}
			if tt.twice {
				_, err := f.svc.CompleteDraft(ctx, "contract_1", req)
				require.NoError(t, err)
			}

			_, err := f.svc.CompleteDraft(ctx, "contract_1", req)
			require.True(t, tt.wantErr(err), "got %v", err)
		})
	}
}
