package subscription

// ProcessRenewalDueAlertWorkflowInput is the input for the subscription renewal due alert workflow.
// No specific fields are required - the activity queries all tenants for subscriptions due for renewal.
type ProcessRenewalDueAlertWorkflowInput struct{}

// Validate validates the workflow input
func (i *ProcessRenewalDueAlertWorkflowInput) Validate() error {
	return nil
}

// ProcessRenewalDueAlertWorkflowResult is the result for the subscription renewal due alert workflow
type ProcessRenewalDueAlertWorkflowResult struct {
	Success bool    `json:"success"`
	Error   *string `json:"error,omitempty"`
}

// ProcessRenewalDueAlertActivityInput is the input for the renewal due alert activity
type ProcessRenewalDueAlertActivityInput struct{}

// Validate validates the activity input
func (i *ProcessRenewalDueAlertActivityInput) Validate() error {
	return nil
}

// ProcessRenewalDueAlertActivityResult is the result for the renewal due alert activity
type ProcessRenewalDueAlertActivityResult struct{}
