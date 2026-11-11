// Command dlq replays dead-letter messages using the configured Kafka cluster.
package main

import (
	"log"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "dlq",
		Short: "Flexprice dead-letter queue operations",
	}

	root.AddCommand(newReplayCmd())

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}
