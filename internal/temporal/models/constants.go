package models

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

const (
	// DefaultTaskQueue is the default task queue name
	DefaultTaskQueue = "default"

	// DefaultNamespace is the default namespace
	DefaultNamespace = "default"

	// DefaultAddress is the default Temporal server address
	DefaultAddress = "localhost:7233"

	// DefaultWorkerStopTimeout is the default timeout for worker graceful shutdown
	DefaultWorkerStopTimeout = time.Second * 30

	// DefaultMaxConcurrentActivities is the default maximum number of concurrent activities
	DefaultMaxConcurrentActivities = 1000

	// DefaultMaxConcurrentWorkflows is the default maximum number of concurrent workflows
	DefaultMaxConcurrentWorkflows = 1000

	// DefaultInitialInterval is the default initial interval for retry policies
	DefaultInitialInterval = time.Second

	// DefaultMaximumInterval is the default maximum interval for retry policies
	DefaultMaximumInterval = time.Minute

	// DefaultBackoffCoefficient is the default backoff coefficient for retry policies
	DefaultBackoffCoefficient = 2.0

	// DefaultMaximumAttempts is the default maximum attempts for retry policies
	DefaultMaximumAttempts = 5
)

// Headers contains common header keys used in Temporal - reusing existing constants where possible
const (
	// HeaderTenantID is the header key for tenant ID
	HeaderTenantID = "tenant-id"

	// HeaderUserID is the header key for user ID
	HeaderUserID = "user-id"

	// HeaderRequestID is the header key for request ID - reusing existing constant
	HeaderRequestID = types.HeaderRequestID

	// HeaderCorrelationID is the header key for correlation ID
	HeaderCorrelationID = "correlation-id"
)
