package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

var errEnsureSchedules = errors.New("ensure schedules failed")

type startupRollbackTemporalService struct {
	temporalservice.TemporalService
	stopCalls int
}

func (s *startupRollbackTemporalService) Start(context.Context) error { return nil }

func (s *startupRollbackTemporalService) Stop(context.Context) error {
	s.stopCalls++
	return nil
}

type failingScheduleHandle struct {
	models.ScheduleHandle
}

func (failingScheduleHandle) Describe(context.Context) (*sdkclient.ScheduleDescription, error) {
	return nil, serviceerror.NewNotFound("missing")
}

type failingScheduleTemporalClient struct {
	client.TemporalClient
}

func (failingScheduleTemporalClient) GetScheduleHandle(context.Context, string) models.ScheduleHandle {
	return failingScheduleHandle{}
}

func (failingScheduleTemporalClient) CreateSchedule(context.Context, models.CreateScheduleOptions) (models.ScheduleHandle, error) {
	return nil, errEnsureSchedules
}

func TestStartTemporalWorkerStopsAfterScheduleFailure(t *testing.T) {
	temporalSvc := &startupRollbackTemporalService{}
	temporalClient := failingScheduleTemporalClient{}
	lc := fxtest.NewLifecycle(t)
	startTemporalWorker(lc, logger.NewNoopLogger(), temporalClient, temporalSvc, service.ServiceParams{}, nil)

	err := lc.Start(context.Background())
	if !errors.Is(err, errEnsureSchedules) {
		t.Fatalf("expected original startup error %v, got %v", errEnsureSchedules, err)
	}
	if temporalSvc.stopCalls != 1 {
		t.Fatalf("expected Temporal service Stop once, got %d calls", temporalSvc.stopCalls)
	}
}

func TestProvideAdminHandlersBuildsEveryHandler(t *testing.T) {
	handlers := reflect.ValueOf(provideAdminHandlers(nil, nil, nil, nil))
	for i := range handlers.NumField() {
		if handlers.Field(i).IsNil() {
			t.Errorf("provideAdminHandlers leaves %s nil", handlers.Type().Field(i).Name)
		}
	}
}

// serviceParamsGraph supplies what serviceParamsProviders consume and nothing more, as main's graph does.
func serviceParamsGraph(cfg *config.Configuration, populate *service.ServiceParams) []fx.Option {
	log := logger.NewNoopLogger()
	base := service.ServiceParams{Config: cfg, Logger: log}
	return []fx.Option{
		fx.Supply(fx.Annotated{Name: "base", Target: base}),
		fx.Supply(log),
		fx.Provide(func() postgres.IClient { return nil }),
		fx.Provide(func() *clickhouse.ClickHouseStore { return nil }),
		serviceParamsProviders(),
		fx.Populate(populate),
	}
}

func TestServiceParamsProvidersResolve(t *testing.T) {
	var params service.ServiceParams
	require.NoError(t, fx.ValidateApp(serviceParamsGraph(&config.Configuration{}, &params)...))
}

func TestServiceParamsProvidersSelfBilling(t *testing.T) {
	enabled := config.BillingConfig{
		Enabled: true, Region: "us", APIHost: "https://billing.example.com/v1", APIKey: "sk_test",
		PlanID: "plan_free", DashboardURL: "https://app.example.com",
	}
	tests := []struct {
		name    string
		billing config.BillingConfig
		wantSet bool
	}{
		{"off_when_billing_is_not_configured", config.BillingConfig{}, false},
		{"off_when_billing_is_partly_configured", config.BillingConfig{Enabled: true, Region: "us"}, false},
		{"on_when_billing_is_fully_configured", enabled, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params service.ServiceParams
			app := fxtest.New(t, serviceParamsGraph(&config.Configuration{Billing: tt.billing}, &params)...)
			app.RequireStart().RequireStop()

			assert.NotNil(t, params.RevenueFacts)
			assert.Equal(t, tt.wantSet, params.SelfBilling != nil)
		})
	}
}
