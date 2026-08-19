package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/worker"
	"go.temporal.io/api/serviceerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type startClient struct {
	temporalclient.TemporalClient
	errors []error
	starts int
	stops  int
	order  *[]string
}

func (c *startClient) Start(context.Context) error {
	c.starts++
	if len(c.errors) == 0 {
		return nil
	}
	err := c.errors[0]
	c.errors = c.errors[1:]
	return err
}

func (c *startClient) Stop(context.Context) error {
	c.stops++
	if c.order != nil {
		*c.order = append(*c.order, "client")
	}
	return nil
}

type stopWorkers struct {
	worker.TemporalWorkerManager
	order *[]string
}

func (m *stopWorkers) StopAllWorkers() error {
	*m.order = append(*m.order, "workers")
	return nil
}

func newTestService(tc temporalclient.TemporalClient, wm worker.TemporalWorkerManager) TemporalService {
	return NewTemporalService(tc, wm, logger.NewNoopLogger(), nil, &config.TemporalConfig{})
}

func TestTemporalServiceStartRetriesOnlyTransientErrors(t *testing.T) {
	tests := []struct {
		name       string
		errors     []error
		wantStarts int
		wantErr    bool
	}{
		{
			name:       "temporal unavailable retries",
			errors:     []error{serviceerror.NewUnavailable("not ready"), nil},
			wantStarts: 2,
		},
		{
			name:       "temporal deadline exceeded retries",
			errors:     []error{serviceerror.NewDeadlineExceeded("not ready"), nil},
			wantStarts: 2,
		},
		{
			name:       "grpc unavailable retries",
			errors:     []error{status.Error(codes.Unavailable, "not ready"), nil},
			wantStarts: 2,
		},
		{
			name:       "grpc deadline exceeded retries",
			errors:     []error{status.Error(codes.DeadlineExceeded, "not ready"), nil},
			wantStarts: 2,
		},
		{
			name:       "permanent error fails immediately",
			errors:     []error{errors.New("invalid namespace"), nil},
			wantStarts: 1,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &startClient{errors: tt.errors}
			service := newTestService(client, nil)
			err := service.Start(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Start() error = %v, wantErr %v", err, tt.wantErr)
			}
			if client.starts != tt.wantStarts {
				t.Fatalf("Start() calls = %d, want %d", client.starts, tt.wantStarts)
			}
		})
	}
}

func TestTemporalServiceStartStopsRetryWhenContextExpires(t *testing.T) {
	client := &startClient{errors: []error{status.Error(codes.Unavailable, "not ready")}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := newTestService(client, nil).Start(ctx)
	if err == nil {
		t.Fatal("Start() error = nil, want context timeout")
	}
	if client.starts != 1 {
		t.Fatalf("Start() calls = %d, want 1 before retry delay is canceled", client.starts)
	}
}

func TestTemporalServiceStopStopsWorkersBeforeClient(t *testing.T) {
	var order []string
	client := &startClient{order: &order}
	service := newTestService(client, &stopWorkers{order: &order})

	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if want := []string{"workers", "client"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("Stop() order = %v, want %v", order, want)
	}
	if client.stops != 1 {
		t.Fatalf("Stop() client calls = %d, want 1", client.stops)
	}
}
