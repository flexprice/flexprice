package task

// ProgressTracker defines the interface for tracking task progress
type ProgressTracker interface {
	Increment(success bool, err error)
	Complete() // Ensures any remaining updates are flushed
}
