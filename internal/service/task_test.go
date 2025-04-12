package service

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type TaskServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  TaskService
	client   *testutil.MockHTTPClient
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
	s.client = testutil.NewMockHTTPClient()
	s.setupService()
	s.setupTestData()
}

func (s *TaskServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.client.Clear()
}

func (s *TaskServiceSuite) setupService() {
	s.service = NewTaskService(
		ServiceParams{
			Logger:           s.GetLogger(),
			Config:           s.GetConfig(),
			DB:               s.GetDB(),
			Client:           s.client,
			EventRepo:        s.GetStores().EventRepo,
			TaskRepo:         s.GetStores().TaskRepo,
			CustomerRepo:     s.GetStores().CustomerRepo,
			EventPublisher:   s.GetPublisher(),
			WebhookPublisher: s.GetWebhookPublisher(),
			PDFGenerator:     s.GetPDFGenerator(),
			AuthRepo:         s.GetStores().AuthRepo,
			UserRepo:         s.GetStores().UserRepo,
			EnvironmentRepo:  s.GetStores().EnvironmentRepo,
			FeatureRepo:      s.GetStores().FeatureRepo,
			EntitlementRepo:  s.GetStores().EntitlementRepo,
			PaymentRepo:      s.GetStores().PaymentRepo,
			SecretRepo:       s.GetStores().SecretRepo,
			InvoiceRepo:      s.GetStores().InvoiceRepo,
			WalletRepo:       s.GetStores().WalletRepo,
			TenantRepo:       s.GetStores().TenantRepo,
			PlanRepo:         s.GetStores().PlanRepo,
			PriceRepo:        s.GetStores().PriceRepo,
			MeterRepo:        s.GetStores().MeterRepo,
			SubRepo:          s.GetStores().SubscriptionRepo,
		},
	)
}

func (s *TaskServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test task
	s.testData.task = &task.Task{
		ID:         "task_123",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeEvents,
		FileURL:    "https://example.com/test.csv",
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), s.testData.task))

	// Register mock CSV response
	data := [][]string{
		{"event_name", "external_customer_id", "timestamp", "properties.bytes_used", "properties.region", "properties.tier"},
		{"api_call", "cust_ext_123", s.testData.now.Add(-1 * time.Hour).Format(time.RFC3339), "", "", ""},
		{"storage_usage", "cust_ext_123", s.testData.now.Add(-30 * time.Minute).Format(time.RFC3339), "100", "us-east-1", "standard"},
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	s.NoError(writer.WriteAll(data))

	s.client.RegisterResponse("test.csv", testutil.MockResponse{
		StatusCode: http.StatusOK,
		Body:       buf.Bytes(),
		Headers: map[string]string{
			"Content-Type": "text/csv",
		},
	})

	// Create test events
	// Standard events
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

	// Events with properties
	eventsWithProps := []struct {
		name  string
		props map[string]interface{}
	}{
		{
			name: "storage_usage",
			props: map[string]interface{}{
				"bytes_used": float64(100),
				"region":     "us-east-1",
				"tier":       "standard",
			},
		},
		{
			name: "storage_usage",
			props: map[string]interface{}{
				"bytes_used": float64(200),
				"region":     "us-east-1",
				"tier":       "archive",
			},
		},
	}

	for _, e := range eventsWithProps {
		event := &events.Event{
			ID:                 s.GetUUID(),
			TenantID:           s.testData.task.TenantID,
			EventName:          e.name,
			ExternalCustomerID: "cust_ext_123",
			Timestamp:          s.testData.now.Add(-30 * time.Minute),
			Properties:         e.props,
		}
		s.NoError(s.GetStores().EventRepo.InsertEvent(s.GetContext(), event))
		s.testData.events.withProps = append(s.testData.events.withProps, event)
	}
}

func (s *TaskServiceSuite) TestCreateTask() {
	tests := []struct {
		name    string
		req     dto.CreateTaskRequest
		mockCSV bool
		want    *dto.TaskResponse
		wantErr bool
	}{
		{
			name: "successful_task_creation",
			req: dto.CreateTaskRequest{
				TaskType:   types.TaskTypeImport,
				EntityType: types.EntityTypeEvents,
				FileURL:    "https://example.com/events.csv",
				FileType:   types.FileTypeCSV,
			},
			mockCSV: true,
			wantErr: false,
		},
		{
			name: "invalid_task_type",
			req: dto.CreateTaskRequest{
				TaskType:   "INVALID",
				EntityType: types.EntityTypeEvents,
				FileURL:    "https://example.com/events.csv",
				FileType:   types.FileTypeCSV,
			},
			mockCSV: false,
			wantErr: true,
		},
		{
			name: "invalid_entity_type",
			req: dto.CreateTaskRequest{
				TaskType:   types.TaskTypeImport,
				EntityType: "INVALID",
				FileURL:    "https://example.com/events.csv",
				FileType:   types.FileTypeCSV,
			},
			mockCSV: false,
			wantErr: true,
		},
		{
			name: "empty_file_url",
			req: dto.CreateTaskRequest{
				TaskType:   types.TaskTypeImport,
				EntityType: types.EntityTypeEvents,
				FileURL:    "",
				FileType:   types.FileTypeCSV,
			},
			mockCSV: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.mockCSV {
				data := [][]string{
					{"event_name", "external_customer_id", "timestamp", "properties.bytes_used", "properties.region"},
					{"api_call", "cust_ext_123", s.testData.now.Add(-1 * time.Hour).Format(time.RFC3339), "100", "us-east-1"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))

				s.client.RegisterResponse("events.csv", testutil.MockResponse{
					StatusCode: http.StatusOK,
					Body:       buf.Bytes(),
					Headers: map[string]string{
						"Content-Type": "text/csv",
					},
				})
			}

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
			s.Equal(tt.req.FileURL, resp.FileURL)
			s.Equal(tt.req.FileType, resp.FileType)
			s.Equal(types.TaskStatusPending, resp.TaskStatus)
		})
	}
}

func (s *TaskServiceSuite) TestGetTask() {
	tests := []struct {
		name    string
		id      string
		want    *dto.TaskResponse
		wantErr bool
	}{
		{
			name:    "existing_task",
			id:      s.testData.task.ID,
			wantErr: false,
		},
		{
			name:    "non_existent_task",
			id:      "non_existent",
			wantErr: true,
		},
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
	// Create additional test tasks
	testTasks := []*task.Task{
		{
			ID:         "task_1",
			TaskType:   types.TaskTypeImport,
			EntityType: types.EntityTypeEvents,
			FileURL:    "https://example.com/test1.csv",
			FileType:   types.FileTypeCSV,
			TaskStatus: types.TaskStatusCompleted,
			BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		},
		{
			ID:         "task_2",
			TaskType:   types.TaskTypeImport,
			EntityType: types.EntityTypeEvents,
			FileURL:    "https://example.com/test2.csv",
			FileType:   types.FileTypeCSV,
			TaskStatus: types.TaskStatusFailed,
			BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
		},
	}

	for _, t := range testTasks {
		s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), t))
	}

	completedStatus := types.TaskStatusCompleted
	failedStatus := types.TaskStatusFailed

	tests := []struct {
		name      string
		filter    *types.TaskFilter
		wantCount int
		wantErr   bool
	}{
		{
			name:      "list_all_tasks",
			filter:    &types.TaskFilter{QueryFilter: types.NewDefaultQueryFilter()},
			wantCount: 3, // 2 new + 1 from setupTestData
			wantErr:   false,
		},
		{
			name: "filter_by_status_completed",
			filter: &types.TaskFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				TaskStatus:  &completedStatus,
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "filter_by_status_failed",
			filter: &types.TaskFilter{
				QueryFilter: types.NewDefaultQueryFilter(),
				TaskStatus:  &failedStatus,
			},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.ListTasks(s.GetContext(), tt.filter)
			if tt.wantErr {
				s.Error(err)
				return
			}

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
		{
			name:      "pending_to_processing",
			id:        s.testData.task.ID,
			newStatus: types.TaskStatusProcessing,
			wantErr:   false,
		},
		{
			name:      "processing_to_completed",
			id:        s.testData.task.ID,
			newStatus: types.TaskStatusCompleted,
			wantErr:   false,
		},
		{
			name:      "completed_to_processing",
			id:        s.testData.task.ID,
			newStatus: types.TaskStatusProcessing,
			wantErr:   true, // Invalid transition
		},
		{
			name:      "non_existent_task",
			id:        "non_existent",
			newStatus: types.TaskStatusProcessing,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.UpdateTaskStatus(s.GetContext(), tt.id, tt.newStatus)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			task, err := s.GetStores().TaskRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(tt.newStatus, task.TaskStatus)
		})
	}
}

func (s *TaskServiceSuite) TestProcessTask() {
	// Create a task for processing
	processTask := &task.Task{
		ID:         "task_process",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeEvents,
		FileURL:    "https://example.com/process.csv",
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), processTask))

	tests := []struct {
		name      string
		id        string
		mockCSV   bool
		csvData   []byte
		wantErr   bool
		wantState types.TaskStatus
	}{
		{
			name:    "process_pending_task",
			id:      processTask.ID,
			mockCSV: true,
			csvData: func() []byte {
				data := [][]string{
					{"event_name", "external_customer_id", "timestamp", "properties.bytes_used", "properties.region", "properties.tier"},
					{"api_call", "cust_ext_123", s.testData.now.Add(-1 * time.Hour).Format(time.RFC3339), "", "", ""},
					{"storage_usage", "cust_ext_123", s.testData.now.Add(-30 * time.Minute).Format(time.RFC3339), "100", "us-east-1", "standard"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))
				return buf.Bytes()
			}(),
			wantErr:   false,
			wantState: types.TaskStatusCompleted,
		},
		{
			name:    "process_task_with_invalid_csv",
			id:      processTask.ID,
			mockCSV: true,
			csvData: func() []byte {
				data := [][]string{
					{"invalid_header1", "invalid_header2"},
					{"data1", "data2"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))
				return buf.Bytes()
			}(),
			wantErr:   true,
			wantState: types.TaskStatusFailed,
		},
		{
			name:    "process_task_with_missing_required_fields",
			id:      processTask.ID,
			mockCSV: true,
			csvData: func() []byte {
				data := [][]string{
					{"event_name", "timestamp", "properties.region"}, // missing external_customer_id
					{"api_call", s.testData.now.Format(time.RFC3339), "us-east-1"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))
				return buf.Bytes()
			}(),
			wantErr:   true,
			wantState: types.TaskStatusFailed,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.mockCSV {
				s.client.RegisterResponse("process.csv", testutil.MockResponse{
					StatusCode: http.StatusOK,
					Body:       tt.csvData,
					Headers: map[string]string{
						"Content-Type": "text/csv",
					},
				})
			}

			err := s.service.ProcessTask(s.GetContext(), tt.id)
			if tt.wantErr {
				s.Error(err)
			} else {
				s.NoError(err)
			}
		})
	}
}

func (s *TaskServiceSuite) TestProcessTaskWithCustomers() {
	// Create a task for processing customers
	customerTask := &task.Task{
		ID:         "task_customer_import",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeCustomers,
		FileURL:    "https://example.com/customers.csv",
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), customerTask))

	// Create a task for testing missing required fields
	customerTaskMissingFields := &task.Task{
		ID:         "task_customer_import_missing_fields",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeCustomers,
		FileURL:    "https://example.com/customers_missing_fields.csv",
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), customerTaskMissingFields))

	// Create a task for testing update functionality
	customerTaskUpdate := &task.Task{
		ID:         "task_customer_import_update",
		TaskType:   types.TaskTypeImport,
		EntityType: types.EntityTypeCustomers,
		FileURL:    "https://example.com/customers_update.csv",
		FileType:   types.FileTypeCSV,
		TaskStatus: types.TaskStatusPending,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().TaskRepo.Create(s.GetContext(), customerTaskUpdate))

	// Create an existing customer for the update test
	existingCustomer := &customer.Customer{
		ID:            "cust_existing_1",
		ExternalID:    "cust_ext_1",
		Name:          "Original Name",
		Email:         "original@example.com",
		Metadata:      map[string]string{"company_size": "50", "industry": "Software"},
		EnvironmentID: "",
		BaseModel: types.BaseModel{
			TenantID:  "tenant_123",
			Status:    types.StatusPublished,
			CreatedAt: s.testData.now,
			UpdatedAt: s.testData.now,
			CreatedBy: "user_123",
			UpdatedBy: "user_123",
		},
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), existingCustomer))

	tests := []struct {
		name      string
		id        string
		fileUrl   string
		mockCSV   bool
		csvData   []byte
		wantErr   bool
		wantState types.TaskStatus
	}{
		{
			name:    "process_customer_import_success",
			id:      customerTask.ID,
			fileUrl: "https://example.com/customers.csv",
			mockCSV: true,
			csvData: func() []byte {
				data := [][]string{
					{"email", "name", "external_id", "metadata.company_size", "metadata.industry"},
					{"customer1@example.com", "Customer 1", "cust_ext_1", "100", "Technology"},
					{"customer2@example.com", "Customer 2", "cust_ext_2", "50", "Healthcare"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))
				return buf.Bytes()
			}(),
			wantErr:   false,
			wantState: types.TaskStatusCompleted,
		},
		{
			name:    "process_customer_import_missing_required_fields",
			id:      customerTaskMissingFields.ID,
			fileUrl: "https://example.com/customers_missing_fields.csv",
			mockCSV: true,
			csvData: func() []byte {
				data := [][]string{
					{"email", "name"},
					{"customer1@example.com", "Customer 1"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))
				return buf.Bytes()
			}(),
			wantErr:   true,
			wantState: types.TaskStatusFailed,
		},
		{
			name:    "process_customer_import_with_update",
			id:      customerTaskUpdate.ID,
			fileUrl: "https://example.com/customers_update.csv",
			mockCSV: true,
			csvData: func() []byte {
				data := [][]string{
					{"id", "email", "name", "external_id", "metadata.company_size", "metadata.industry"},
					{"cust_existing_1", "updated@example.com", "Updated Name", "cust_ext_1", "200", "Technology"},
				}
				var buf bytes.Buffer
				writer := csv.NewWriter(&buf)
				s.NoError(writer.WriteAll(data))
				return buf.Bytes()
			}(),
			wantErr:   false,
			wantState: types.TaskStatusCompleted,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.mockCSV {
				s.client.RegisterResponse(tt.fileUrl, testutil.MockResponse{
					StatusCode: http.StatusOK,
					Body:       tt.csvData,
					Headers: map[string]string{
						"Content-Type": "text/csv",
					},
				})
			}

			err := s.service.ProcessTask(s.GetContext(), tt.id)
			if tt.wantErr {
				s.Error(err)
			} else {
				s.NoError(err)
			}

			// Verify task status
			processedTask, err := s.GetStores().TaskRepo.Get(s.GetContext(), tt.id)
			s.NoError(err)
			s.Equal(tt.wantState, processedTask.TaskStatus)

			// If successful, verify customers were created
			if !tt.wantErr {
				filter := &types.CustomerFilter{
					QueryFilter: types.NewDefaultQueryFilter(),
				}
				customers, err := s.GetStores().CustomerRepo.List(s.GetContext(), filter)
				s.NoError(err)
				s.GreaterOrEqual(len(customers), 2)
			}
		})
	}
}
