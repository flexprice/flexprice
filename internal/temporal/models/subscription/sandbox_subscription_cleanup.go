package subscription

// SandboxSubscriptionCleanupWorkflowResult is the workflow result: IDs of subscriptions terminated.
type SandboxSubscriptionCleanupWorkflowResult struct {
	TerminatedSubscriptionIDs []string `json:"terminated_subscription_ids"`
}

// SubToTerminate is a single subscription to terminate. Minimal context for CancelSubscription (tenant, env, user).
type SubToTerminate struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
	CreatedBy      string `json:"created_by"`
}

// BuildSandboxCleanupListResult is the output of BuildSandboxCleanupListActivity: sandboxCleanupList (subs past cleanup window).
type BuildSandboxCleanupListResult struct {
	SubsToTerminate []SubToTerminate `json:"subs_to_terminate"`
}
