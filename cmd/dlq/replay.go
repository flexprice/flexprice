package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka/dlq"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/spf13/cobra"
)

func newReplayCmd() *cobra.Command {
	var (
		source     string
		target     string
		group      string
		since      string
		fromStart  bool
		maxMsgs    int
		maxReplays int
		timeout    time.Duration
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay messages from a DLQ topic back to their origin topic",
		Long: "Drains a watermill poison-queue topic up to its current end offset and " +
			"routes each message back to the topic named in its topic_poisoned header " +
			"(or --target). Strips the watermill poison headers and increments a " +
			"replay_count guard so a message that keeps failing is quarantined rather " +
			"than looped.\n\n" +
			"Progress is tracked as committed offsets under a consumer group (--group), " +
			"committed only after a successful republish, so a re-run resumes where it " +
			"left off instead of replaying the whole topic. Use --from-start or --since " +
			"to override the resume point. Run with --dry-run first to see the routing " +
			"and reason breakdown (dry-run never commits, so a later real run still sees " +
			"everything).",
		RunE: func(cmd *cobra.Command, args []string) error {
			var sinceMs int64
			if since != "" {
				t, err := time.Parse(time.RFC3339, since)
				if err != nil {
					return fmt.Errorf("parse --since (want RFC3339, e.g. 2026-07-28T06:18:00Z): %w", err)
				}
				sinceMs = t.UnixMilli()
			}

			cfg, err := config.NewConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			log, err := logger.NewLogger(cfg)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			if fromStart && sinceMs > 0 {
				return fmt.Errorf("--from-start and --since are mutually exclusive")
			}

			// A hard wall-clock ceiling so no broker stall can wedge a run
			// forever (the drain honors ctx cancellation). 0 disables it —
			// rely on the K8s Job's activeDeadlineSeconds in that case.
			ctx := context.Background()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			sum, err := dlq.Replay(ctx, cfg, log, dlq.Options{
				SourceTopic:    source,
				TargetOverride: target,
				Group:          group,
				SinceMs:        sinceMs,
				FromStart:      fromStart,
				Max:            maxMsgs,
				MaxReplays:     maxReplays,
				DryRun:         dryRun,
			})
			if sum != nil {
				printSummary(source, dryRun, sum)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "DLQ topic to drain (required), e.g. staging_events_dlq")
	cmd.Flags().StringVar(&target, "target", "", "override destination topic for ALL messages (default: per-message topic_poisoned header)")
	cmd.Flags().StringVar(&group, "group", dlq.DefaultGroup, "consumer group used to track resume offsets")
	cmd.Flags().StringVar(&since, "since", "", "ignore resume point; replay from the first message appended at/after this RFC3339 time")
	cmd.Flags().BoolVar(&fromStart, "from-start", false, "ignore resume point; re-drain each partition from its oldest retained offset")
	cmd.Flags().IntVar(&maxMsgs, "max", 0, "cap number of messages replayed (0 = no cap)")
	cmd.Flags().IntVar(&maxReplays, "max-replays", 3, "quarantine a message once it has been replayed this many times (loop guard)")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "hard wall-clock ceiling for the run (0 = no ceiling; rely on the Job deadline)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show routing + reason breakdown without producing anything (does not commit offsets)")
	_ = cmd.MarkFlagRequired("source")

	return cmd
}

func printSummary(source string, dryRun bool, s *dlq.Summary) {
	verb := "replayed"
	if dryRun {
		verb = "WOULD replay"
	}
	fmt.Printf("\nDLQ replay summary (%s)\n", source)
	fmt.Printf("  scanned:     %d\n", s.Scanned)
	fmt.Printf("  %s:  %d\n", verb, s.Replayed)
	fmt.Printf("  skipped:     %d (unroutable)\n", s.Skipped)
	fmt.Printf("  quarantined: %d (hit max-replays)\n", s.Quarantined)

	if len(s.ByTarget) > 0 {
		fmt.Println("  by target topic:")
		for _, kv := range sortedDesc(s.ByTarget) {
			fmt.Printf("    %-45s %d\n", kv.k, kv.v)
		}
	}
	if len(s.ByReason) > 0 {
		fmt.Println("  by poison reason:")
		for _, kv := range sortedDesc(s.ByReason) {
			fmt.Printf("    %-80s %d\n", kv.k, kv.v)
		}
	}
	fmt.Println()
}

type kv struct {
	k string
	v int
}

func sortedDesc(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].v != out[j].v {
			return out[i].v > out[j].v
		}
		return out[i].k < out[j].k
	})
	return out
}
