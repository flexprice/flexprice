package main

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
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
