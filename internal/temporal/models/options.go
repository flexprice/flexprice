package models

import (
	"time"

	"go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/types/known/durationpb"
)

// ClientOptions represents configuration options for creating a Temporal client
type ClientOptions struct {
	// Address is the host:port of the Temporal server
	Address string
	// Namespace is the Temporal namespace to use
	Namespace string
	// APIKey is the authentication key for Temporal Cloud
	APIKey string
	// TLS enables TLS for the connection
	TLS bool
	// RetryPolicy defines the default retry policy for workflows
	RetryPolicy *common.RetryPolicy
	// DataConverter is an optional data converter for serialization
	DataConverter converter.DataConverter
}

// DefaultClientOptions returns the default client options
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		RetryPolicy: &common.RetryPolicy{
			InitialInterval:    &durationpb.Duration{Seconds: 1},
			BackoffCoefficient: 2.0,
			MaximumInterval:    &durationpb.Duration{Seconds: 60},
			MaximumAttempts:    5,
		},
	}
}

// WorkerOptions represents configuration options for creating a Temporal worker
type WorkerOptions struct {
	// TaskQueue is the name of the task queue to listen on
	TaskQueue string
	// MaxConcurrentActivityExecutionSize is the maximum number of activities that can be executed concurrently
	MaxConcurrentActivityExecutionSize int
	// MaxConcurrentWorkflowTaskExecutionSize is the maximum number of workflow tasks that can be executed concurrently
	MaxConcurrentWorkflowTaskExecutionSize int
	// WorkerActivitiesPerSecond is the rate limit for activities per second per worker. 0 means unlimited.
	WorkerActivitiesPerSecond float64
	// TaskQueueActivitiesPerSecond is the rate limit for activities per second across all workers for the task queue. 0 means unlimited.
	TaskQueueActivitiesPerSecond float64
	// WorkerStopTimeout is the time to wait for worker to stop gracefully
	WorkerStopTimeout time.Duration
	// EnableLoggingInReplay enables logging in replay mode
	EnableLoggingInReplay bool
	// Interceptors is a list of interceptors to apply to the worker
	Interceptors []interceptor.WorkerInterceptor
}

const (
	DefaultMaxConcurrentActivityExecutionSize     = 10
	DefaultMaxConcurrentWorkflowTaskExecutionSize = 10
	DefaultWorkerActivitiesPerSecond              = 5.0
)

// DefaultWorkerOptions returns the default worker options
func DefaultWorkerOptions() *WorkerOptions {
	return &WorkerOptions{
		MaxConcurrentActivityExecutionSize:     DefaultMaxConcurrentActivityExecutionSize,
		MaxConcurrentWorkflowTaskExecutionSize: DefaultMaxConcurrentWorkflowTaskExecutionSize,
		WorkerActivitiesPerSecond:              DefaultWorkerActivitiesPerSecond,
		WorkerStopTimeout:                      time.Second * 30,
		EnableLoggingInReplay:                  false,
	}
}

// ToSDKOptions converts ClientOptions to Temporal SDK client.Options
func (o *ClientOptions) ToSDKOptions() client.Options {
	return client.Options{
		HostPort:      o.Address,
		Namespace:     o.Namespace,
		DataConverter: o.DataConverter,
		ConnectionOptions: client.ConnectionOptions{
			TLS: nil, // Will be set if TLS is enabled
		},
	}
}

// ToSDKOptions converts WorkerOptions to Temporal SDK worker.Options
func (o *WorkerOptions) ToSDKOptions() worker.Options {
	return worker.Options{
		MaxConcurrentActivityExecutionSize:      o.MaxConcurrentActivityExecutionSize,
		MaxConcurrentWorkflowTaskExecutionSize:  o.MaxConcurrentWorkflowTaskExecutionSize,
		WorkerActivitiesPerSecond:               o.WorkerActivitiesPerSecond,
		TaskQueueActivitiesPerSecond:            o.TaskQueueActivitiesPerSecond,
		WorkerStopTimeout:                       o.WorkerStopTimeout,
		EnableLoggingInReplay:                   o.EnableLoggingInReplay,
		Interceptors:                            o.Interceptors,
	}
}

// CreateScheduleOptions represents options for creating a schedule
type CreateScheduleOptions struct {
	ID      string
	Spec    client.ScheduleSpec
	Overlap enumspb.ScheduleOverlapPolicy
	Action  *client.ScheduleWorkflowAction
	Paused  bool
}

// ToSDKOptions converts CreateScheduleOptions to Temporal SDK client.ScheduleOptions
func (o *CreateScheduleOptions) ToSDKOptions() client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      o.ID,
		Spec:    o.Spec,
		Overlap: o.Overlap,
		Action:  o.Action,
		Paused:  o.Paused,
	}
}
