// Package dlq replays dead-letter messages to their origin topics.
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

// ReplayCountKey stores the number of previous replay attempts.
const ReplayCountKey = "replay_count"

// DefaultGroup tracks replay progress independently of application consumers.
const DefaultGroup = "dlq-replay-tool"

// Options controls a single replay run.
type Options struct {
	SourceTopic    string
	TargetOverride string
	Group          string
	SinceMs        int64
	FromStart      bool
	Max            int
	MaxReplays     int
	DryRun         bool
	idleTimeout    time.Duration
}

// Summary is the outcome of a replay run.
type Summary struct {
	Scanned     int
	Replayed    int
	Skipped     int
	Quarantined int
	ByTarget    map[string]int
	ByReason    map[string]int
}

// Replay drains each partition to the end offset captured when that partition starts.
// It returns partial progress on failure; uncommitted messages may be replayed again.
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

	// Keep autocommit enabled: Sarama needs its background loop to close offset managers.
	// Only handled messages are marked, so autocommit cannot skip unprocessed messages.
	saramaCfg, err := kafka.GetSaramaConfig(&cfg.Kafka)
	if err != nil {
		return sum, fmt.Errorf("build sarama config: %w", err)
	}

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

	var pub message.Publisher
	if !opts.DryRun {
		pub, err = newPublisher(cfg)
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
			return sum, fmt.Errorf("partition %d: %w", p, err)
		}
	}

	log.Info(ctx, "dlq replay: done",
		"scanned", sum.Scanned, "replayed", sum.Replayed,
		"skipped", sum.Skipped, "quarantined", sum.Quarantined,
		"by_target", sum.ByTarget)
	return sum, nil
}

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
	defer pom.Close()

	start, hasWork, err := resolveStart(client, pom, opts, partition, oldest, hwm)
	if err != nil || !hasWork {
		return err
	}

	runErr := drainPartition(ctx, consumer, pom, marshaler, pub, log, opts, partition, start, hwm, sum)

	// Commit handled messages even after a partial failure; leave the failed offset unmarked.
	if !opts.DryRun {
		offsetMgr.Commit()
	}
	return runErr
}

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
			return 0, false, nil
		}
	case opts.FromStart:
		start = oldest
	default:
		start, _ = pom.NextOffset()
		if start < 0 {
			start = oldest
		}
	}

	if start < oldest {
		start = oldest
	}
	if start >= hwm {
		return 0, false, nil
	}
	return start, true, nil
}

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
			log.Info(ctx, "dlq replay: idle timeout before end offset, stopping partition",
				"partition", partition, "source", opts.SourceTopic)
			return nil
		case km := <-pc.Messages():
			if km == nil || km.Offset >= hwm {
				return nil
			}
			if err := handleMessage(ctx, marshaler, pub, log, opts, km, sum); err != nil {
				return err
			}
			if !opts.DryRun {
				pom.MarkOffset(km.Offset+1, "")
			}
			if km.Offset >= hwm-1 {
				return nil
			}
		}
	}
}

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

	replayCount := parseCount(msg.Metadata.Get(ReplayCountKey))
	if replayCount >= opts.MaxReplays {
		log.Info(ctx, "dlq replay: message hit max replays, quarantining",
			"uuid", msg.UUID, "replay_count", replayCount, "max_replays", opts.MaxReplays)
		sum.Quarantined++
		return nil
	}

	if !opts.DryRun {
		for _, key := range []string{
			middleware.ReasonForPoisonedKey,
			middleware.PoisonedTopicKey,
			middleware.PoisonedHandlerKey,
			middleware.PoisonedSubscriberKey,
		} {
			delete(msg.Metadata, key)
		}
		msg.Metadata.Set(ReplayCountKey, strconv.Itoa(replayCount+1))
		if err := pub.Publish(target, msg); err != nil {
			return fmt.Errorf("publish to %s: %w", target, err)
		}
	}
	sum.ByTarget[target]++
	sum.Replayed++
	return nil
}

func newPublisher(cfg *config.Configuration) (message.Publisher, error) {
	saramaCfg, err := kafka.GetSaramaConfig(&cfg.Kafka)
	if err != nil {
		return nil, fmt.Errorf("build sarama config: %w", err)
	}
	saramaCfg.Producer.Return.Successes = true
	saramaCfg.Producer.Return.Errors = true
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
