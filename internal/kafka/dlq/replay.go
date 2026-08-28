// Package dlq replays messages out of a watermill poison-queue (dead-letter)
// topic back to their origin topic.
//
// Every message the consumer poison-queue middleware writes to a DLQ carries
// watermill's poison metadata — the origin topic lives in the
// middleware.PoisonedTopicKey ("topic_poisoned") header. This package routes
// each message by that header rather than by any hard-coded mapping, so the
// replay target can never drift from what the middleware actually wrote.
//
// Replay is contract-preserving: it uses watermill's DefaultMarshaler in both
// directions, so a republished message is byte-for-byte what a normally
// published one would be (watermill UUID + metadata → kafka headers, keyless
// like every other flexprice producer). The only mutations are (a) the four
// watermill poison headers are stripped, and (b) a replay_count header is
// incremented so a message that keeps failing is quarantined instead of
// looping forever.
//
// Resume semantics: progress is tracked as committed offsets under a dedicated
// consumer group (Options.Group), and an offset is committed only AFTER the
// message is successfully republished. So a second run picks up where the last
// one left off instead of re-replaying the whole topic, and a crash mid-run
// re-replays only the uncommitted tail (at-least-once — safe because handlers
// are idempotent). --since / --from-start override the resume point explicitly.
package dlq

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Shopify/sarama"
	"github.com/ThreeDotsLabs/watermill"
	watermillKafka "github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
)

// ReplayCountKey counts how many times a message has been replayed out of a
// DLQ. It is not a watermill key — we own it — and it is the loop guard: once
// it reaches Options.MaxReplays the message is quarantined (left in the DLQ)
// instead of replayed again.
const ReplayCountKey = "replay_count"

// DefaultGroup is the consumer group under which replay progress (committed
// offsets) is tracked. Dedicated to this tool so it never collides with a real
// consumer's group.
const DefaultGroup = "dlq-replay-tool"

// poisonKeys are the watermill metadata headers stripped before republish so
// the replayed message is indistinguishable from a fresh one.
var poisonKeys = []string{
	middleware.ReasonForPoisonedKey,
	middleware.PoisonedTopicKey,
	middleware.PoisonedHandlerKey,
	middleware.PoisonedSubscriberKey,
}

// Options controls a single replay run.
type Options struct {
	// SourceTopic is the DLQ topic to drain (e.g. "staging_events_dlq").
	SourceTopic string
	// TargetOverride, when set, routes every message to this topic instead of
	// the per-message topic_poisoned header. Use only when the header is
	// missing/wrong; normal replays leave it empty and route per message.
	TargetOverride string
	// Group is the consumer group that tracks committed-offset progress. Empty
	// defaults to DefaultGroup. Change it only to run an independent replay
	// stream over the same topic.
	Group string
	// SinceMs, when > 0, ignores the committed resume point and starts each
	// partition at the first offset at or after this epoch-ms append time
	// (bounds the replay to a specific incident window).
	SinceMs int64
	// FromStart ignores the committed resume point and re-drains each partition
	// from its oldest retained offset. Mutually exclusive with SinceMs (SinceMs
	// wins). Use to deliberately replay a topic in full again.
	FromStart bool
	// Max caps the number of messages replayed (0 = no cap). A safety valve.
	Max int
	// MaxReplays quarantines a message once its replay_count reaches this value
	// (loop guard). Must be >= 1; defaults to 3.
	MaxReplays int
	// DryRun tallies and logs what would happen without producing anything and
	// without committing any offset (so a later real run still sees everything).
	DryRun bool
	// idleTimeout stops draining a partition if no message arrives within this
	// window before the snapshotted end offset is reached (guards against
	// offset gaps from retention/compaction that would otherwise block).
	idleTimeout time.Duration
}

// Summary is the outcome of a replay run.
type Summary struct {
	Scanned     int            // messages read from the DLQ
	Replayed    int            // messages (re)published to a target topic
	Skipped     int            // unroutable (no topic_poisoned and no override)
	Quarantined int            // hit MaxReplays, left in place
	ByTarget    map[string]int // replayed count per destination topic
	ByReason    map[string]int // scanned count per (truncated) poison reason
}

// Replay drains opts.SourceTopic — resuming from the Group's committed offsets —
// up to each partition's end offset at the moment the run starts (messages
// appended mid-run are ignored) and routes each message back to its origin
// topic. It returns a Summary even on a partial run.
func Replay(ctx context.Context, cfg *config.Configuration, log *logger.Logger, opts Options) (*Summary, error) {
	if opts.SourceTopic == "" {
		return nil, fmt.Errorf("dlq replay: SourceTopic is required")
	}
	if opts.Group == "" {
		opts.Group = DefaultGroup
	}
	if opts.MaxReplays < 1 {
		opts.MaxReplays = 3
	}
	if opts.idleTimeout == 0 {
		opts.idleTimeout = 15 * time.Second
	}

	sum := &Summary{ByTarget: map[string]int{}, ByReason: map[string]int{}}

	saramaCfg := kafka.GetSaramaConfig(&cfg.Kafka)
	// Leave AutoCommit ENABLED (its default). The commit-after-publish guarantee
	// is enforced by only ever MarkOffset-ing a message after a successful
	// Publish — autocommit can then only flush offsets for already-republished
	// messages, and we also Commit() explicitly per partition for the tail.
	// Do NOT disable autocommit: sarama only starts the offset manager's
	// background loop when autocommit is on, and that loop is what lets
	// partitionOffsetManager.Close() return — with it off, Close() deadlocks.

	client, err := sarama.NewClient(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		return sum, fmt.Errorf("connect kafka client: %w", err)
	}
	defer client.Close()

	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		return sum, fmt.Errorf("create consumer: %w", err)
	}
	defer consumer.Close()

	offsetMgr, err := sarama.NewOffsetManagerFromClient(opts.Group, client)
	if err != nil {
		return sum, fmt.Errorf("create offset manager (group=%s): %w", opts.Group, err)
	}
	defer offsetMgr.Close()

	// The publisher is created even in dry-run (cheap, and it validates broker
	// auth up front) but Publish is only called on a real run.
	var pub message.Publisher
	if !opts.DryRun {
		pub, err = newPublisher(cfg, log)
		if err != nil {
			return sum, fmt.Errorf("create publisher: %w", err)
		}
		defer pub.Close()
	}

	marshaler := watermillKafka.DefaultMarshaler{}

	partitions, err := client.Partitions(opts.SourceTopic)
	if err != nil {
		return sum, fmt.Errorf("list partitions for %s: %w", opts.SourceTopic, err)
	}

	log.Info(ctx, "dlq replay: starting",
		"source", opts.SourceTopic, "group", opts.Group,
		"target_override", opts.TargetOverride, "partitions", len(partitions),
		"dry_run", opts.DryRun, "from_start", opts.FromStart,
		"since_ms", opts.SinceMs, "max", opts.Max, "max_replays", opts.MaxReplays)

	for _, p := range partitions {
		if opts.Max > 0 && sum.Replayed >= opts.Max {
			break
		}
		if err := replayPartition(ctx, client, consumer, offsetMgr, marshaler, pub, log, opts, p, sum); err != nil {
			// Return what we have so an operator can see partial progress and
			// re-run — resume picks up from the committed offset.
			return sum, fmt.Errorf("partition %d: %w", p, err)
		}
	}

	log.Info(ctx, "dlq replay: done",
		"scanned", sum.Scanned, "replayed", sum.Replayed,
		"skipped", sum.Skipped, "quarantined", sum.Quarantined,
		"by_target", sum.ByTarget)
	return sum, nil
}

// replayPartition resolves the start offset for one partition (resume point,
// or a --since / --from-start override), drains it, and commits the resulting
// progress. The offset is committed here — after the drain — and only ever
// reflects messages that handleMessage has already republished/decided on.
func replayPartition(
	ctx context.Context,
	client sarama.Client,
	consumer sarama.Consumer,
	offsetMgr sarama.OffsetManager,
	marshaler watermillKafka.DefaultMarshaler,
	pub message.Publisher,
	log *logger.Logger,
	opts Options,
	partition int32,
	sum *Summary,
) error {
	// Snapshot the end offset now so messages appended during the run are not
	// consumed (a replay must terminate; a live-tailing one never would).
	hwm, err := client.GetOffset(opts.SourceTopic, partition, sarama.OffsetNewest)
	if err != nil {
		return fmt.Errorf("get newest offset: %w", err)
	}
	oldest, err := client.GetOffset(opts.SourceTopic, partition, sarama.OffsetOldest)
	if err != nil {
		return fmt.Errorf("get oldest offset: %w", err)
	}

	pom, err := offsetMgr.ManagePartition(opts.SourceTopic, partition)
	if err != nil {
		return fmt.Errorf("manage partition offsets: %w", err)
	}
	// Close runs after Commit below (deferred, so it is last), flushing cleanly.
	defer pom.Close()

	start, hasWork, err := resolveStart(client, pom, opts, partition, oldest, hwm)
	if err != nil {
		return err
	}
	if !hasWork {
		return nil
	}

	runErr := drainPartition(ctx, consumer, pom, marshaler, pub, log, opts, partition, start, hwm, sum)

	// Flush the marks accumulated during the drain. Every mark is post-publish,
	// so committing them is safe even when runErr != nil (a partial drain still
	// advances the resume point past what it successfully replayed). No-op in
	// dry-run, which never marks.
	if !opts.DryRun {
		offsetMgr.Commit()
	}
	return runErr
}

// resolveStart picks the offset to begin a partition from and reports whether
// there is anything to do. Precedence: --since > --from-start > committed
// resume point. The start is clamped into [oldest, hwm].
func resolveStart(
	client sarama.Client,
	pom sarama.PartitionOffsetManager,
	opts Options,
	partition int32,
	oldest, hwm int64,
) (start int64, hasWork bool, err error) {
	switch {
	case opts.SinceMs > 0:
		start, err = client.GetOffset(opts.SourceTopic, partition, opts.SinceMs)
		if err != nil {
			return 0, false, fmt.Errorf("get offset for since_ms: %w", err)
		}
		if start == -1 {
			return 0, false, nil // nothing appended in-window on this partition
		}
	case opts.FromStart:
		start = oldest
	default:
		// NextOffset returns the config's Initial (a negative sentinel) when
		// this group has never committed for the partition — resume from oldest.
		start, _ = pom.NextOffset()
		if start < 0 {
			start = oldest
		}
	}

	if start < oldest {
		start = oldest // a committed offset trimmed away by retention
	}
	if hwm == 0 || start >= hwm {
		return 0, false, nil // empty partition, or resume point already at the end
	}
	return start, true, nil
}

// drainPartition consumes [start, hwm) and hands each message to handleMessage,
// marking the offset after a successful republish so progress can be committed.
func drainPartition(
	ctx context.Context,
	consumer sarama.Consumer,
	pom sarama.PartitionOffsetManager,
	marshaler watermillKafka.DefaultMarshaler,
	pub message.Publisher,
	log *logger.Logger,
	opts Options,
	partition int32,
	start, hwm int64,
	sum *Summary,
) error {
	pc, err := consumer.ConsumePartition(opts.SourceTopic, partition, start)
	if err != nil {
		return fmt.Errorf("consume partition: %w", err)
	}
	defer pc.Close()

	for {
		if opts.Max > 0 && sum.Replayed >= opts.Max {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.idleTimeout):
			// No message within the idle window although we haven't reached the
			// end offset — treat as end-of-data (offset gaps from retention or
			// compaction) rather than block forever.
			log.Info(ctx, "dlq replay: idle timeout before end offset, stopping partition",
				"partition", partition, "source", opts.SourceTopic)
			return nil
		case km := <-pc.Messages():
			if km == nil {
				return nil
			}
			if err := handleMessage(ctx, marshaler, pub, log, opts, km, sum); err != nil {
				// Hard failure (e.g. publish error): stop without marking this
				// offset so the message is retried from here on the next run.
				return err
			}
			if !opts.DryRun {
				// Terminal decision made (replayed, skipped, or quarantined) —
				// advance the resume point past it.
				pom.MarkOffset(km.Offset+1, "")
			}
			// Stop once we've consumed the last message that existed at start.
			if km.Offset >= hwm-1 {
				return nil
			}
		}
	}
}

// handleMessage routes and (unless dry-run) republishes a single DLQ message.
// It returns a non-nil error only for a hard failure that should stop the drain
// (publish error); routable/skippable/quarantinable outcomes return nil so the
// caller advances past them.
func handleMessage(
	ctx context.Context,
	marshaler watermillKafka.DefaultMarshaler,
	pub message.Publisher,
	log *logger.Logger,
	opts Options,
	km *sarama.ConsumerMessage,
	sum *Summary,
) error {
	sum.Scanned++

	msg, err := marshaler.Unmarshal(km)
	if err != nil {
		// A message we cannot unmarshal cannot be safely re-emitted; count it as
		// skipped and advance rather than aborting the whole drain.
		log.Error(ctx, "dlq replay: unmarshal failed, skipping",
			"error", err, "offset", km.Offset, "partition", km.Partition)
		sum.Skipped++
		return nil
	}

	reason := msg.Metadata.Get(middleware.ReasonForPoisonedKey)
	sum.ByReason[truncate(reason, 80)]++

	target := opts.TargetOverride
	if target == "" {
		target = msg.Metadata.Get(middleware.PoisonedTopicKey)
	}
	if target == "" {
		log.Info(ctx, "dlq replay: message has no topic_poisoned and no override, skipping",
			"uuid", msg.UUID, "offset", km.Offset)
		sum.Skipped++
		return nil
	}

	// Loop guard: quarantine anything that has already been replayed too often.
	replayCount := parseCount(msg.Metadata.Get(ReplayCountKey))
	if replayCount >= opts.MaxReplays {
		log.Info(ctx, "dlq replay: message hit max replays, quarantining",
			"uuid", msg.UUID, "replay_count", replayCount, "max_replays", opts.MaxReplays)
		sum.Quarantined++
		return nil
	}

	// Strip the watermill poison headers and stamp the incremented replay count
	// so the republished message is indistinguishable from a fresh one (except
	// for the loop-guard counter).
	for _, k := range poisonKeys {
		delete(msg.Metadata, k)
	}
	msg.Metadata.Set(ReplayCountKey, strconv.Itoa(replayCount+1))

	if opts.DryRun {
		sum.ByTarget[target]++
		sum.Replayed++ // "would replay"
		return nil
	}

	if err := pub.Publish(target, msg); err != nil {
		return fmt.Errorf("publish to %s: %w", target, err)
	}
	sum.ByTarget[target]++
	sum.Replayed++
	return nil
}

// newPublisher mirrors router.createDLQPublisher: a keyless watermill kafka
// publisher using the DefaultMarshaler, so replayed messages match the wire
// format of every other flexprice producer.
func newPublisher(cfg *config.Configuration, log *logger.Logger) (message.Publisher, error) {
	saramaCfg := kafka.GetSaramaConfig(&cfg.Kafka)
	if saramaCfg != nil {
		saramaCfg.Producer.Return.Successes = true
		saramaCfg.Producer.Return.Errors = true
	}
	return watermillKafka.NewPublisher(
		watermillKafka.PublisherConfig{
			Brokers:               cfg.Kafka.Brokers,
			Marshaler:             watermillKafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaCfg,
		},
		watermill.NewStdLogger(false, false),
	)
}

func parseCount(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func truncate(s string, n int) string {
	if s == "" {
		return "(no reason)"
	}
	if len(s) <= n {
		return s
	}
	return s[:n]
}
