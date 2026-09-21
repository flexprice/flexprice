package types

type RunMode string

const (
	// ModeLocal is the mode for running both the API server and the consumer locally
	ModeLocal RunMode = "local"
	// ModeAPI is the mode for running just the API server
	ModeAPI RunMode = "api"
	// ModeConsumer is the mode for running just the consumer
	ModeConsumer RunMode = "consumer"
	// ModeTemporalWorker is the mode for running the temporal worker
	ModeTemporalWorker RunMode = "temporal_worker"
	// ModeInternal serves only the internal router. Public API, Kafka consumers,
	// and Temporal workers are not started.
	ModeInternal RunMode = "internal"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)
