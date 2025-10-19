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
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
)
