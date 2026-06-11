package main

import (
	"flag"
	"log"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/kafka/reconcile"
	"github.com/flexprice/flexprice/internal/kafka/topicspec"
)

func main() {
	specPath := flag.String("spec", "topics.yaml", "path to topics.yaml")
	dryRun := flag.Bool("dry-run", false, "log intended actions without applying")
	flag.Parse()

	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	spec, err := topicspec.Load(*specPath)
	if err != nil {
		log.Fatalf("load topics spec: %v", err)
	}
	env := cfg.Logging.Environment
	names := make([]string, len(spec.Topics))
	for i, t := range spec.Topics {
		names[i] = t.Name
	}
	desired, err := spec.Resolve(env, topicspec.EnvOverridesFromEnv(names))
	if err != nil {
		log.Fatalf("resolve topics spec: %v", err)
	}
	log.Printf("kafka-migrate: env=%s topics=%d dry-run=%v", env, len(desired), *dryRun)

	saramaCfg := kafka.GetSaramaConfig(cfg)

	admin, err := sarama.NewClusterAdmin(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		log.Fatalf("connect cluster admin: %v", err)
	}
	defer admin.Close()

	saramaAdmin := &reconcile.SaramaAdmin{Admin: admin}

	// Plan once; dry-run logs the SAME plan that a live apply would execute,
	// so dry-run can never drift from real behavior (incl. RF-mismatch warns).
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
