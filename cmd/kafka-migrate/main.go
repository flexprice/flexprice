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
	desired := spec.Resolve(env, topicspec.EnvOverridesFromEnv(names))
	log.Printf("kafka-migrate: env=%s topics=%d dry-run=%v", env, len(desired), *dryRun)

	saramaCfg := kafka.GetSaramaConfig(cfg)

	admin, err := sarama.NewClusterAdmin(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		log.Fatalf("connect cluster admin: %v", err)
	}
	defer admin.Close()

	if *dryRun {
		live, err := (&reconcile.SaramaAdmin{Admin: admin}).ListTopics()
		if err != nil {
			log.Fatalf("list topics: %v", err)
		}
		for _, d := range desired {
			cur, ok := live[d.Name]
			switch {
			case !ok:
				log.Printf("WOULD CREATE %s partitions=%d rf=%d", d.Name, d.Partitions, d.ReplicationFactor)
			case int(cur.Partitions) < d.Partitions:
				log.Printf("WOULD GROW %s %d -> %d partitions", d.Name, cur.Partitions, d.Partitions)
			case int(cur.Partitions) > d.Partitions:
				log.Printf("WARN %s has MORE partitions (%d) than desired (%d); will skip", d.Name, cur.Partitions, d.Partitions)
			default:
				log.Printf("OK %s unchanged", d.Name)
			}
		}
		return
	}

	res, err := reconcile.Reconcile(&reconcile.SaramaAdmin{Admin: admin}, desired)
	if err != nil {
		log.Fatalf("reconcile failed: %v", err)
	}
	if res.SkippedShrink > 0 || res.RFMismatch > 0 {
		log.Printf("WARN reconcile completed with warnings: skipped-shrink=%d rf-mismatch=%d", res.SkippedShrink, res.RFMismatch)
	}
	log.Printf("kafka-migrate done: created=%d grown=%d unchanged=%d skipped-shrink=%d rf-mismatch=%d",
		res.Created, res.Grown, res.Unchanged, res.SkippedShrink, res.RFMismatch)
}
