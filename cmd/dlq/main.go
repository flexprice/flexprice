// Command dlq is the operational tool for draining a watermill dead-letter
// (poison) topic back to the origin topic each message came from.
//
// It is deployed as a Kubernetes Job (see infrastructure repo) so it runs
// in-cluster under the same Kafka auth (Workload Identity / OAUTHBEARER on GMK)
// as the consumer, with no bastion or short-lived token handling. Broker and
// auth config come from the standard FLEXPRICE_* env, exactly like the server
// and migrate binaries.
//
//	dlq replay --source staging_events_dlq --dry-run
//	dlq replay --source production_event_processing_dlq --since 2026-07-28T06:18:00Z
package main

import (
	"log"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "dlq",
		Short: "FlexPrice dead-letter queue operations",
	}

	root.AddCommand(newReplayCmd())

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}
