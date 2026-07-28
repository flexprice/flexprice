package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type HubSpotLineItemSyncOperation string

const (
	HubSpotLineItemSyncOperationCreated HubSpotLineItemSyncOperation = "created"
	HubSpotLineItemSyncOperationDeleted HubSpotLineItemSyncOperation = "deleted"
)

func (o HubSpotLineItemSyncOperation) Validate() error {
	allowed := []HubSpotLineItemSyncOperation{
		HubSpotLineItemSyncOperationCreated,
		HubSpotLineItemSyncOperationDeleted,
	}
	if !lo.Contains(allowed, o) {
		return ierr.NewError("invalid operation").
			WithHint("Operation must be one of: created, deleted").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type HubSpotDealSyncWorkflowInput struct {
	SubscriptionID string                       `json:"subscription_id"`
	CustomerID     string                       `json:"customer_id"`
	DealID         string                       `json:"deal_id"`
	TenantID       string                       `json:"tenant_id"`
	EnvironmentID  string                       `json:"environment_id"`
	LineItemID     string                       `json:"line_item_id"`
	Operation      HubSpotLineItemSyncOperation `json:"operation"`
}

// Validate validates the workflow input
func (input *HubSpotDealSyncWorkflowInput) Validate() error {
	if input.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("SubscriptionID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("CustomerID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.DealID == "" {
		return ierr.NewError("deal_id is required").
			WithHint("DealID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("TenantID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("EnvironmentID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.LineItemID == "" {
		return ierr.NewError("line_item_id is required").
			WithHint("LineItemID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if err := input.Operation.Validate(); err != nil {
		return err
	}
	return nil
}
