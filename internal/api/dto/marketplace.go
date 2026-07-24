package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// allowedMarketplaceProviders are the SecretProvider values this endpoint accepts. Adding a further
// marketplace (Azure) means adding its SecretProvider constant in internal/types/secret.go, listing
// it here, and adding its own request block below — nothing else about this validation changes.
var allowedMarketplaceProviders = []types.SecretProvider{
	types.SecretProviderAWSMarketplace,
	types.SecretProviderGCPMarketplace,
}

// RegisterMarketplaceAgreementRequest is the request body for POST /v1/marketplace/agreements, sent
// once per buyer purchase to link a Flexprice subscription to its marketplace agreement (AWS) or
// entitlement (GCP). Exactly one of AWS/GCP must be set, matching Provider — this keeps
// provider-specific fields from coexisting as one large bag of optional fields, and adding a further
// marketplace later means a new block, not a redesign of this one.
type RegisterMarketplaceAgreementRequest struct {
	Provider       types.SecretProvider `json:"provider" validate:"required"` // "aws_marketplace" | "gcp_marketplace"
	SubscriptionID string               `json:"subscription_id" validate:"required"`
	CustomerID     string               `json:"customer_id" validate:"required"`
	PlanID         string               `json:"plan_id" validate:"required"`

	AWS *AWSMarketplaceAgreement `json:"aws,omitempty"` // required iff Provider == aws_marketplace
	GCP *GCPMarketplaceAgreement `json:"gcp,omitempty"` // required iff Provider == gcp_marketplace
}

// AWSMarketplaceAgreement carries the AWS-specific identifiers a tenant resolved via ResolveCustomer
// before calling this endpoint.
type AWSMarketplaceAgreement struct {
	ProductCode          string `json:"product_code" validate:"required"`            // -> BatchMeterUsage's ProductCode (omitted when ConcurrentAgreements)
	LicenseArn           string `json:"license_arn" validate:"required"`             // -> BatchMeterUsage's LicenseArn; identifies the buyer's agreement
	CustomerAWSAccountID string `json:"customer_aws_account_id" validate:"required"` // -> BatchMeterUsage's CustomerAWSAccountId
	Dimension            string `json:"dimension" validate:"required"`               // -> BatchMeterUsage's Dimension (always "usage_fee" in the cents model)
	ConcurrentAgreements bool   `json:"concurrent_agreements"`                       // if true, ProductCode is omitted when reporting
}

// Validate checks the request shape only; subscription existence and uniqueness are validated in
// the service layer.
func (a *AWSMarketplaceAgreement) Validate() error {
	if a.ProductCode == "" {
		return ierr.NewError("aws.product_code is required").
			Mark(ierr.ErrValidation)
	}
	if a.LicenseArn == "" {
		return ierr.NewError("aws.license_arn is required").
			Mark(ierr.ErrValidation)
	}
	if a.CustomerAWSAccountID == "" {
		return ierr.NewError("aws.customer_aws_account_id is required").
			Mark(ierr.ErrValidation)
	}
	if a.Dimension == "" {
		return ierr.NewError("aws.dimension is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GCPMarketplaceAgreement carries the GCP-specific identifiers a tenant read off the entitlement
// (via the Procurement API) before calling this endpoint. Flexprice never calls the Procurement API
// itself — the tenant already has everything here by the time it registers the agreement.
type GCPMarketplaceAgreement struct {
	ServiceName      string `json:"service_name" validate:"required"`       // -> services.report URL's service_name; identifies the product
	UsageReportingID string `json:"usage_reporting_id" validate:"required"` // -> services.report's consumerId; identifies the buyer
	MetricName       string `json:"metric_name" validate:"required"`        // -> services.report's metricName (always "{service_name}/usage_fee")
	AccountID        string `json:"account_id" validate:"required"`         // writes the customer mapping; not read in the report payload
}

// Validate checks the request shape only; subscription existence and uniqueness are validated in
// the service layer.
func (g *GCPMarketplaceAgreement) Validate() error {
	if g.ServiceName == "" {
		return ierr.NewError("gcp.service_name is required").
			Mark(ierr.ErrValidation)
	}
	if g.UsageReportingID == "" {
		return ierr.NewError("gcp.usage_reporting_id is required").
			Mark(ierr.ErrValidation)
	}
	if g.MetricName == "" {
		return ierr.NewError("gcp.metric_name is required").
			Mark(ierr.ErrValidation)
	}
	if g.AccountID == "" {
		return ierr.NewError("gcp.account_id is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// Validate checks the request shape only; subscription existence and uniqueness are validated in the service layer
func (r *RegisterMarketplaceAgreementRequest) Validate() error {
	if r.Provider == "" {
		return ierr.NewError("provider is required").
			WithHint("provider is required (\"aws_marketplace\" or \"gcp_marketplace\")").
			Mark(ierr.ErrValidation)
	}
	if !lo.Contains(allowedMarketplaceProviders, r.Provider) {
		return ierr.NewErrorf("unsupported marketplace provider %q", r.Provider).
			WithHint("provider must be one of: aws_marketplace, gcp_marketplace").
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
	if r.PlanID == "" {
		return ierr.NewError("plan_id is required").
			Mark(ierr.ErrValidation)
	}

	switch r.Provider {
	case types.SecretProviderAWSMarketplace:
		if r.AWS == nil {
			return ierr.NewError("aws is required").
				WithHint("\"aws\" block is required when provider is \"aws_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		if r.GCP != nil {
			return ierr.NewError("gcp must not be set").
				WithHint("\"gcp\" block must not be set when provider is \"aws_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		return r.AWS.Validate()
	case types.SecretProviderGCPMarketplace:
		if r.GCP == nil {
			return ierr.NewError("gcp is required").
				WithHint("\"gcp\" block is required when provider is \"gcp_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		if r.AWS != nil {
			return ierr.NewError("aws must not be set").
				WithHint("\"aws\" block must not be set when provider is \"gcp_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		return r.GCP.Validate()
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
