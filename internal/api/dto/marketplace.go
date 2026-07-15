package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// allowedMarketplaceProviders are the SecretProvider values this endpoint accepts. AWS is the only
// marketplace wired up today; adding a marketplace later (Azure/GCP) means adding its
// SecretProvider constant in internal/types/secret.go and listing it here — nothing else about
// this validation changes.
var allowedMarketplaceProviders = []types.SecretProvider{
	types.SecretProviderAWSMarketplace,
}

// RegisterMarketplaceAgreementRequest is the request body for POST /v1/marketplace/agreements,
// sent once per buyer purchase to link a Flexprice subscription to its AWS agreement.
type RegisterMarketplaceAgreementRequest struct {
	Provider             types.SecretProvider `json:"provider" validate:"required"` // e.g. "aws_marketplace"
	SubscriptionID       string               `json:"subscription_id" validate:"required"`
	CustomerID           string               `json:"customer_id" validate:"required"`
	CustomerAWSAccountID string               `json:"customer_aws_account_id" validate:"required"`
	LicenseArn           string               `json:"license_arn" validate:"required"`
	PlanID               string               `json:"plan_id" validate:"required"`
	ProductCode          string               `json:"product_code" validate:"required"`
	ConcurrentAgreements bool                 `json:"concurrent_agreements"`
	Dimension            string               `json:"dimension" validate:"required"`
}

// Validate checks the request shape only; subscription existence and uniqueness are validated in the service layer
func (r *RegisterMarketplaceAgreementRequest) Validate() error {
	if r.Provider == "" {
		return ierr.NewError("provider is required").
			WithHint("provider is required (e.g. \"aws_marketplace\")").
			Mark(ierr.ErrValidation)
	}
	if !lo.Contains(allowedMarketplaceProviders, r.Provider) {
		return ierr.NewErrorf("unsupported marketplace provider %q", r.Provider).
			WithHint("Only \"aws_marketplace\" is supported for marketplace agreement registration").
			Mark(ierr.ErrValidation)
	}
	if r.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			Mark(ierr.ErrValidation)
	}
	if r.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			Mark(ierr.ErrValidation)
	}
	if r.CustomerAWSAccountID == "" {
		return ierr.NewError("customer_aws_account_id is required").
			Mark(ierr.ErrValidation)
	}
	if r.LicenseArn == "" {
		return ierr.NewError("license_arn is required").
			Mark(ierr.ErrValidation)
	}
	if r.PlanID == "" {
		return ierr.NewError("plan_id is required").
			Mark(ierr.ErrValidation)
	}
	if r.ProductCode == "" {
		return ierr.NewError("product_code is required").
			Mark(ierr.ErrValidation)
	}
	if r.Dimension == "" {
		return ierr.NewError("dimension is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RegisterMarketplaceAgreementResponse is the response for POST /v1/marketplace/agreements.
type RegisterMarketplaceAgreementResponse struct {
	PlanMappingID         string `json:"plan_mapping_id"`
	SubscriptionMappingID string `json:"subscription_mapping_id"`
	CustomerMappingID     string `json:"customer_mapping_id"`
	Status                string `json:"status"`
}
