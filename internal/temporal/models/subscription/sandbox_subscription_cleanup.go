package subscription

// SandboxSubscriptionCleanupWorkflowResult is the workflow result: counts only (no IDs) to avoid Temporal payload limits.
// To see which subscriptions were terminated: use each TerminateSandboxSubscriptionsBatch activity's result in Temporal UI
// (each returns terminated_subscription_ids for that batch), or the workflow history logs (we log terminated_subscription_ids per batch).
type SandboxSubscriptionCleanupWorkflowResult struct {
	TerminatedCount int `json:"terminated_count"`
	BatchCount      int `json:"batch_count"`
}

// SubToTerminate is a single subscription to terminate. Minimal context for CancelSubscription (tenant, env, user).
type SubToTerminate struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
	CreatedBy      string `json:"created_by"`
}

// BuildSandboxCleanupListInput is the input for BuildSandboxCleanupListActivity (one page per call).
type BuildSandboxCleanupListInput struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// BuildSandboxCleanupListPageResult is one page of subs to terminate. Workflow loops until HasMore is false.
type BuildSandboxCleanupListPageResult struct {
	Items   []SubToTerminate `json:"items"`
	HasMore bool             `json:"has_more"`
}
