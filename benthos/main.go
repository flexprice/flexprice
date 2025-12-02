package main

import (
	"context"

	"github.com/warpstreamlabs/bento/public/service"

	// Import all standard Bento components (includes Kafka)
	_ "github.com/warpstreamlabs/bento/public/components/all"

	// Import custom Flexprice output plugin
	_ "github.com/flexprice/flexprice/benthos/output"
)

func main() {
	// Run Bento as a service with CLI support
	// This will read config from -c flag or stdin
	service.RunCLI(context.Background())
}
