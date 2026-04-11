package flexpricebilling

// FlexpriceBillingOnboardingInput is the input for the FlexpriceBillingOnboardingWorkflow.
// It carries all information needed to create a billing customer and subscription for a newly
// onboarded Flexprice tenant.
type FlexpriceBillingOnboardingInput struct {
	// TenantID is the new tenant's ID; it becomes the ExternalCustomerID in the billing tenant.
	TenantID             string `json:"tenant_id"`
	TenantName           string `json:"tenant_name"`
	Email                string `json:"email"`
	BillingTenantID      string `json:"billing_tenant_id"`
	BillingEnvironmentID string `json:"billing_environment_id"`
	// BillingPlanID is the BASE plan to assign. If empty, subscription creation is skipped.
	BillingPlanID string `json:"billing_plan_id"`
}

// FlexpriceBillingOnboardingResult is the output of the FlexpriceBillingOnboardingWorkflow.
type FlexpriceBillingOnboardingResult struct {
	CustomerID     string `json:"customer_id"`
	SubscriptionID string `json:"subscription_id"`
	// Status is one of: "processing", "completed", "failed"
	Status       string `json:"status"`
	ErrorSummary string `json:"error_summary,omitempty"`
}
