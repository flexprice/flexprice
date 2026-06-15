package main

import (
	"flag"
	"log"
	"strings"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/kafka/reconcile"
	"github.com/flexprice/flexprice/internal/kafka/topicspec"
)

func main() {
	specPath := flag.String("spec", "topics.yaml", "path to the baked base topics.yaml (used when FLEXPRICE_KAFKA_TOPICS is unset)")
	dryRun := flag.Bool("dry-run", false, "log intended actions without applying")
	flag.Parse()

	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// FLEXPRICE_KAFKA_TOPICS (JSON), when set, fully replaces the baked file.
	desired, source, err := topicspec.LoadDesired(*specPath)
	if err != nil {
		log.Fatalf("load desired topics: %v", err)
	}
	env := cfg.Logging.Environment
	log.Printf("kafka-migrate: env=%s topics=%d source=%s dry-run=%v", env, len(desired), source, *dryRun)
	if strings.HasPrefix(source, "file:") {
		// The baked file carries the base/dev topic names (unprefixed), which are
		// WRONG for a shared prod cluster. Every real deploy must set
		// FLEXPRICE_KAFKA_TOPICS. Make a forgotten env-var loud.
		log.Printf("WARN FLEXPRICE_KAFKA_TOPICS is NOT set — using the baked base topic list (%s). This is correct only for local/dev; a shared prod cluster needs the per-env JSON override or it may create wrong/unprefixed topics. Review the dry-run before applying.", source)
	}
	for _, d := range desired {
		log.Printf("desired topic: %s partitions=%d rf=%d", d.Name, d.Partitions, d.ReplicationFactor)
	}

	saramaCfg := kafka.GetSaramaConfig(cfg)

	admin, err := sarama.NewClusterAdmin(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		log.Fatalf("connect cluster admin: %v", err)
	}
	defer admin.Close()

	saramaAdmin := &reconcile.SaramaAdmin{Admin: admin}

	plan, err := reconcile.Plan(saramaAdmin, desired)
	if err != nil {
		log.Fatalf("plan reconcile: %v", err)
	}

	if *dryRun {
		for _, act := range plan {
			logAction(act)
		}
		return
	}

	res, err := reconcile.Apply(saramaAdmin, plan)
	if err != nil {
		log.Fatalf("reconcile failed: %v", err)
	}
	if res.SkippedShrink > 0 || res.RFMismatch > 0 {
		log.Printf("WARN reconcile completed with warnings: skipped-shrink=%d rf-mismatch=%d", res.SkippedShrink, res.RFMismatch)
	}
	log.Printf("kafka-migrate done: created=%d grown=%d unchanged=%d skipped-shrink=%d rf-mismatch=%d",
		res.Created, res.Grown, res.Unchanged, res.SkippedShrink, res.RFMismatch)
}

func logAction(act reconcile.Action) {
	switch act.Kind {
	case reconcile.ActionCreate:
		log.Printf("WOULD CREATE %s partitions=%d rf=%d", act.Topic.Name, act.Topic.Partitions, act.Topic.ReplicationFactor)
	case reconcile.ActionGrow:
		log.Printf("WOULD GROW %s %d -> %d partitions", act.Topic.Name, act.CurrentPartitions, act.Topic.Partitions)
	case reconcile.ActionSkipShrink:
		log.Printf("WARN %s has MORE partitions (%d) than desired (%d); will skip", act.Topic.Name, act.CurrentPartitions, act.Topic.Partitions)
	case reconcile.ActionRFMismatch:
		log.Printf("WARN %s replication-factor mismatch: live=%d desired=%d; will NOT change (warn only)", act.Topic.Name, act.CurrentRF, act.Topic.ReplicationFactor)
	case reconcile.ActionUnchanged:
		log.Printf("OK %s unchanged", act.Topic.Name)
	}
}
