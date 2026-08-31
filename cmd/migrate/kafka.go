package main

import (
	"fmt"
	"log"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/kafka/reconcile"
	"github.com/flexprice/flexprice/internal/kafka/topicspec"
	"github.com/spf13/cobra"
)

func newKafkaCmd() *cobra.Command {
	var dryRun bool
	var seedACLs bool

	cmd := &cobra.Command{
		Use:   "kafka",
		Short: "Reconcile Kafka topics against the desired topic spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKafkaMigration(dryRun, seedACLs)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "log intended actions without applying")
	cmd.Flags().BoolVar(&seedACLs, "seed-acls", false, "seed allow-all MSK ACLs (SCRAM only); off by default")

	return cmd
}

// seedACLsEnabled gates seeding to the MSK path: SASL enabled AND a SCRAM
// mechanism. SCRAM alone is not enough — with useSASL=false the admin connects
// unauthenticated, so seeding the MSK-shaped ACLs is wrong (and would fail on a
// real MSK cluster). It must ALSO not key off useSASL alone: OAUTHBEARER (GCP
// Managed Kafka) sets useSASL=true too, and these "kafka-cluster"/User:* ACLs
// do not apply there.
func seedACLsEnabled(useSASL bool, m sarama.SASLMechanism) bool {
	return useSASL && (m == sarama.SASLTypeSCRAMSHA256 || m == sarama.SASLTypeSCRAMSHA512)
}

// seedAllowAllACLs seeds the allow-all ACL safety net using the SAME cluster
// admin the topic reconcile already opened. Gated to SCRAM (MSK); a no-op on
// any other mechanism. Idempotent — CreateACL on an existing binding is a
// broker no-op. Never touches allow.everyone.if.no.acl.found.
func seedAllowAllACLs(sa *reconcile.SaramaAdmin, seedACLs bool, useSASL bool, mechanism sarama.SASLMechanism, dryRun bool) error {
	if !seedACLs {
		log.Printf("kafka-migrate: ACL seed disabled (--seed-acls not set) — skipping")
		return nil
	}
	if !seedACLsEnabled(useSASL, mechanism) {
		log.Printf("kafka-migrate: not a SASL/SCRAM (MSK) cluster (use_sasl=%v mechanism=%q) — skipping ACL seed", useSASL, mechanism)
		return nil
	}
	rules := reconcile.AllowAllACLRules()
	for _, r := range rules {
		res, acl := r.Resource(), r.Acl()
		log.Printf("desired ACL: %v name=%q principal=%s op=%v", res.ResourceType, res.ResourceName, acl.Principal, acl.Operation)
	}
	if dryRun {
		log.Printf("kafka-migrate: dry-run — %d allow-all ACL rules NOT applied", len(rules))
		return nil
	}
	for _, r := range rules {
		res, acl := r.Resource(), r.Acl()
		if err := sa.CreateACL(res, acl); err != nil {
			return fmt.Errorf("create ACL %v/%q: %w", res.ResourceType, res.ResourceName, err)
		}
		log.Printf("OK seeded ACL %v name=%q", res.ResourceType, res.ResourceName)
	}
	log.Printf("kafka-migrate: %d allow-all ACL rules ensured", len(rules))
	return nil
}

func runKafkaMigration(dryRun bool, seedACLs bool) (err error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// FLEXPRICE_KAFKA_TOPICS (JSON), when set, fully replaces config.yaml's
	// kafka.topics_defaults/topics.
	topics := make(map[string]topicspec.ConfigTopic, len(cfg.Kafka.Topics))
	for name, t := range cfg.Kafka.Topics {
		topics[name] = topicspec.ConfigTopic{Partitions: t.Partitions, ReplicationFactor: t.ReplicationFactor, RetentionMs: t.RetentionMs}
	}
	defaults := topicspec.ConfigDefaults{
		ReplicationFactor: cfg.Kafka.TopicsDefaults.ReplicationFactor,
		RetentionMs:       cfg.Kafka.TopicsDefaults.RetentionMs,
	}
	desired, source, err := topicspec.LoadDesired(defaults, topics)
	if err != nil {
		return fmt.Errorf("load desired topics: %w", err)
	}
	env := cfg.Logging.Environment
	log.Printf("kafka-migrate: env=%s topics=%d source=%s dry-run=%v", env, len(desired), source, dryRun)
	if source == "config" {
		// config.yaml carries the base/dev topic names (unprefixed), which are
		// WRONG for a shared prod cluster. Every real deploy must set
		// FLEXPRICE_KAFKA_TOPICS. Make a forgotten env-var loud.
		log.Printf("WARN FLEXPRICE_KAFKA_TOPICS is NOT set — using config.yaml's base topic list. This is correct only for local/dev; a shared prod cluster needs the per-env JSON override or it may create wrong/unprefixed topics. Review the dry-run before applying.")
	}
	for _, d := range desired {
		log.Printf("desired topic: %s partitions=%d rf=%d", d.Name, d.Partitions, d.ReplicationFactor)
	}

	saramaCfg, err := kafka.GetSaramaConfig(&cfg.Kafka)
	if err != nil {
		return fmt.Errorf("build kafka client config: %w", err)
	}

	admin, err := sarama.NewClusterAdmin(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		return fmt.Errorf("connect cluster admin: %w", err)
	}
	defer func() {
		if cerr := admin.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close cluster admin: %w", cerr)
		}
	}()

	saramaAdmin := &reconcile.SaramaAdmin{Admin: admin}

	plan, err := reconcile.Plan(saramaAdmin, desired)
	if err != nil {
		return fmt.Errorf("plan reconcile: %w", err)
	}

	if dryRun {
		for _, act := range plan {
			logKafkaAction(act)
		}
		if err := seedAllowAllACLs(saramaAdmin, seedACLs, cfg.Kafka.UseSASL, cfg.Kafka.SASLMechanism, true); err != nil {
			return err
		}
		return nil
	}

	res, err := reconcile.Apply(saramaAdmin, plan)
	if err != nil {
		return fmt.Errorf("reconcile failed: %w", err)
	}
	if res.SkippedShrink > 0 || res.RFMismatch > 0 || res.RetentionMismatch > 0 {
		log.Printf("WARN reconcile completed with warnings: skipped-shrink=%d rf-mismatch=%d retention-mismatch=%d", res.SkippedShrink, res.RFMismatch, res.RetentionMismatch)
	}
	log.Printf("kafka-migrate done: created=%d grown=%d unchanged=%d skipped-shrink=%d rf-mismatch=%d retention-mismatch=%d",
		res.Created, res.Grown, res.Unchanged, res.SkippedShrink, res.RFMismatch, res.RetentionMismatch)
	if err := seedAllowAllACLs(saramaAdmin, seedACLs, cfg.Kafka.UseSASL, cfg.Kafka.SASLMechanism, false); err != nil {
		return err
	}
	return nil
}

func logKafkaAction(act reconcile.Action) {
	switch act.Kind {
	case reconcile.ActionCreate:
		log.Printf("WOULD CREATE %s partitions=%d rf=%d retention_ms=%d", act.Topic.Name, act.Topic.Partitions, act.Topic.ReplicationFactor, act.Topic.RetentionMs)
	case reconcile.ActionGrow:
		log.Printf("WOULD GROW %s %d -> %d partitions", act.Topic.Name, act.CurrentPartitions, act.Topic.Partitions)
	case reconcile.ActionSkipShrink:
		log.Printf("WARN %s has MORE partitions (%d) than desired (%d); will skip", act.Topic.Name, act.CurrentPartitions, act.Topic.Partitions)
	case reconcile.ActionRFMismatch:
		log.Printf("WARN %s replication-factor mismatch: live=%d desired=%d; will NOT change (warn only)", act.Topic.Name, act.CurrentRF, act.Topic.ReplicationFactor)
	case reconcile.ActionRetentionMismatch:
		log.Printf("WARN %s retention.ms mismatch: live=%d desired=%d; will NOT change (warn only)", act.Topic.Name, act.CurrentRetentionMs, act.Topic.RetentionMs)
	case reconcile.ActionUnchanged:
		log.Printf("OK %s unchanged", act.Topic.Name)
	}
}
