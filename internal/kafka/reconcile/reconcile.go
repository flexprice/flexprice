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

// Result is a summary of what the reconcile did, for logging.
type Result struct {
	Created       int
	Grown         int
	SkippedShrink int
	RFMismatch    int
	Unchanged     int
}

// Reconcile applies forward-only, non-destructive changes.
func Reconcile(a Admin, desired []topicspec.ResolvedTopic) (Result, error) {
	var res Result
	live, err := a.ListTopics()
	if err != nil {
		return res, fmt.Errorf("list topics: %w", err)
	}

	for _, d := range desired {
		cur, exists := live[d.Name]
		if !exists {
			if err := a.CreateTopic(d.Name, int32(d.Partitions), d.ReplicationFactor); err != nil {
				return res, fmt.Errorf("create topic %s: %w", d.Name, err)
			}
			res.Created++
			continue
		}

		if cur.ReplicationFactor != 0 && cur.ReplicationFactor != d.ReplicationFactor {
			res.RFMismatch++
		}

		switch {
		case int(cur.Partitions) < d.Partitions:
			if err := a.CreatePartitions(d.Name, int32(d.Partitions)); err != nil {
				return res, fmt.Errorf("grow partitions %s: %w", d.Name, err)
			}
			res.Grown++
		case int(cur.Partitions) > d.Partitions:
			res.SkippedShrink++
		default:
			res.Unchanged++
		}
	}
	return res, nil
}
