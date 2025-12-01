package main

import (
	"context"

	"github.com/benthosdev/benthos/v4/public/service"

	// Import all standard Benthos components (includes Kafka)
	_ "github.com/benthosdev/benthos/v4/public/components/all"

	// Import custom Flexprice output plugin
	_ "github.com/flexprice/flexprice/benthos/output"
)

func main() {
	// Run Benthos as a service with CLI support
	// This will read config from -c flag or stdin
	service.RunCLI(context.Background())
}
