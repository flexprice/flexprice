package types

// EnvironmentType defines the type of environment.
type EnvironmentType string

const (
	EnvironmentDevelopment EnvironmentType = "development"
	EnvironmentTesting     EnvironmentType = "testing"
	EnvironmentProduction  EnvironmentType = "production"
)
