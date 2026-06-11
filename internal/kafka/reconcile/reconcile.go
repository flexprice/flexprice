package reconcile

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/kafka/topicspec"
)

// liveTopic is the subset of cluster state the reconciler needs.
type liveTopic struct {
	Partitions        int32
	ReplicationFactor int16
}

// Admin abstracts the cluster operations so the algorithm is testable.
type Admin interface {
	ListTopics() (map[string]liveTopic, error)
	CreateTopic(name string, partitions int32, rf int16) error
	CreatePartitions(name string, count int32) error
}

// ActionKind enumerates the decisions the reconciler can make for a topic.
type ActionKind int

const (
	ActionCreate     ActionKind = iota // topic absent -> create
	ActionGrow                         // fewer partitions than desired -> grow
	ActionSkipShrink                   // more partitions than desired -> warn, never act
	ActionRFMismatch                   // replication factor differs -> warn, never act
	ActionUnchanged                    // already matches desired
)

// Action is a single planned decision for one topic. It is the shared output
// of Plan(): live apply (Apply) and dry-run logging consume the SAME plan, so
// the two can never drift (e.g. dry-run always surfaces RF mismatches too).
type Action struct {
	Kind ActionKind
	// Topic is the desired sizing this action concerns.
	Topic topicspec.ResolvedTopic
	// Current cluster state. CurrentExists is false for ActionCreate.
	CurrentPartitions int32
	CurrentRF         int16
	CurrentExists     bool
}

// Result is a summary of what was applied, for logging.
type Result struct {
	Created       int
	Grown         int
	SkippedShrink int
	RFMismatch    int
	Unchanged     int
}

// Plan computes the forward-only, non-destructive decisions for each desired
// topic without mutating the cluster.
//
// Create-only semantics: partition counts are hand-tuned per environment and
// live ONLY on the cluster (e.g. staging events=6 vs prod events=100), and are
// never carried in app config/env. So the reconciler NEVER changes the
// partition count of an existing topic — the live count is authoritative.
// Desired partition sizing applies only when CREATING a missing topic. An
// existing topic always yields ActionUnchanged (plus an RF-mismatch warning if
// the replication factors differ — warn only, never altered).
func Plan(a Admin, desired []topicspec.ResolvedTopic) ([]Action, error) {
	live, err := a.ListTopics()
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}

	var plan []Action
	for _, d := range desired {
		cur, exists := live[d.Name]
		if !exists {
			plan = append(plan, Action{Kind: ActionCreate, Topic: d})
			continue
		}

		if cur.ReplicationFactor != 0 && cur.ReplicationFactor != d.ReplicationFactor {
			plan = append(plan, Action{
				Kind:              ActionRFMismatch,
				Topic:             d,
				CurrentPartitions: cur.Partitions,
				CurrentRF:         cur.ReplicationFactor,
				CurrentExists:     true,
			})
		}

		// Existing topic: never touch partitions. Live count is the truth.
		plan = append(plan, Action{
			Kind:              ActionUnchanged,
			Topic:             d,
			CurrentPartitions: cur.Partitions,
			CurrentRF:         cur.ReplicationFactor,
			CurrentExists:     true,
		})
	}
	return plan, nil
}

// Apply executes the only mutating action — create missing topics. Existing
// topics are never mutated (no partition growth, no RF change); RF mismatch is
// counted as a warning only. Returns a Result summary.
func Apply(a Admin, plan []Action) (Result, error) {
	var res Result
	for _, act := range plan {
		switch act.Kind {
		case ActionCreate:
			if err := a.CreateTopic(act.Topic.Name, int32(act.Topic.Partitions), act.Topic.ReplicationFactor); err != nil {
				return res, fmt.Errorf("create topic %s: %w", act.Topic.Name, err)
			}
			res.Created++
		case ActionRFMismatch:
			res.RFMismatch++
		case ActionUnchanged:
			res.Unchanged++
		}
	}
	return res, nil
}

// Reconcile plans then applies forward-only, non-destructive changes: create
// missing, grow partitions, warn (never act) on shrink and RF mismatch. It
// never deletes topics and never touches topics absent from desired.
func Reconcile(a Admin, desired []topicspec.ResolvedTopic) (Result, error) {
	plan, err := Plan(a, desired)
	if err != nil {
		return Result{}, err
	}
	return Apply(a, plan)
}
