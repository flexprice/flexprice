package topicspec

// EnvOverride holds optional per-topic sizing overrides supplied via
// environment variables (see EnvOverridesFromEnv in envoverride.go).
type EnvOverride struct {
	Partitions        *int
	ReplicationFactor *int16
	RetentionMs       *int64
}

// ResolvedTopic is a fully-resolved desired state for one topic, consumed by
// the reconcile package. Names are harvested from the app config (see
// fromconfig.go); sizing comes from env overrides or package defaults.
type ResolvedTopic struct {
	Name              string
	Partitions        int
	ReplicationFactor int16
	RetentionMs       int64
}
