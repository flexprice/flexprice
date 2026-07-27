package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// allowedMarketplaceProviders are the SecretProvider values this endpoint accepts.
var allowedMarketplaceProviders = []types.SecretProvider{
	types.SecretProviderAWSMarketplace,
	types.SecretProviderGCPMarketplace,
	types.SecretProviderAzureMarketplace,
}

// RegisterMarketplaceAgreementRequest is the request body for POST /v1/marketplace/agreements, sent
// once per buyer purchase to link a Flexprice subscription to its marketplace agreement (AWS),
// entitlement (GCP) or subscription (Azure). Exactly one of AWS/GCP/Azure must be set, matching
// Provider — this keeps provider-specific fields from coexisting as one large bag of optional
// fields, and adding a further marketplace later means a new block, not a redesign of this one.
type RegisterMarketplaceAgreementRequest struct {
	Provider       types.SecretProvider `json:"provider" validate:"required"` // "aws_marketplace" | "gcp_marketplace" | "azure_marketplace"
	SubscriptionID string               `json:"subscription_id" validate:"required"`
	CustomerID     string               `json:"customer_id" validate:"required"`
	PlanID         string               `json:"plan_id" validate:"required"`

	AWS   *AWSMarketplaceAgreement   `json:"aws,omitempty"`   // required iff Provider == aws_marketplace
	GCP   *GCPMarketplaceAgreement   `json:"gcp,omitempty"`   // required iff Provider == gcp_marketplace
	Azure *AzureMarketplaceAgreement `json:"azure,omitempty"` // required iff Provider == azure_marketplace
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

// AzureMarketplaceAgreement carries the Azure-specific identifiers a tenant read off the Resolve
// response or the Subscribe webhook before calling this endpoint.
type AzureMarketplaceAgreement struct {
	PlanID               string `json:"plan_id" validate:"required"`                // -> batchUsageEvent's planId; Azure's plan id, distinct from the request's top-level PlanID
	Dimension            string `json:"dimension" validate:"required"`              // -> batchUsageEvent's dimension (always "usage_fee" in the cents model)
	ResourceID           string `json:"resource_id" validate:"required"`            // -> batchUsageEvent's resourceId; the Azure SaaS subscription id
	BeneficiaryAccountID string `json:"beneficiary_account_id" validate:"required"` // writes the customer mapping; not read in the report payload
}

// Validate checks the request shape only; subscription existence and uniqueness are validated in
// the service layer.
func (a *AzureMarketplaceAgreement) Validate() error {
	if a.PlanID == "" {
		return ierr.NewError("azure.plan_id is required").
			Mark(ierr.ErrValidation)
	}
	if a.Dimension == "" {
		return ierr.NewError("azure.dimension is required").
			Mark(ierr.ErrValidation)
	}
	if a.ResourceID == "" {
		return ierr.NewError("azure.resource_id is required").
			Mark(ierr.ErrValidation)
	}
	if a.BeneficiaryAccountID == "" {
		return ierr.NewError("azure.beneficiary_account_id is required").
			Mark(ierr.ErrValidation)
	}
	return nil
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
			WithHint("provider is required (\"aws_marketplace\", \"gcp_marketplace\" or \"azure_marketplace\")").
			Mark(ierr.ErrValidation)
	}
	if !lo.Contains(allowedMarketplaceProviders, r.Provider) {
		return ierr.NewErrorf("unsupported marketplace provider %q", r.Provider).
			WithHint("provider must be one of: aws_marketplace, gcp_marketplace, azure_marketplace").
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
		if r.GCP != nil || r.Azure != nil {
			return ierr.NewError("only aws must be set").
				WithHint("only the \"aws\" block may be set when provider is \"aws_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		return r.AWS.Validate()
	case types.SecretProviderGCPMarketplace:
		if r.GCP == nil {
			return ierr.NewError("gcp is required").
				WithHint("\"gcp\" block is required when provider is \"gcp_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		if r.AWS != nil || r.Azure != nil {
			return ierr.NewError("only gcp must be set").
				WithHint("only the \"gcp\" block may be set when provider is \"gcp_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		return r.GCP.Validate()
	case types.SecretProviderAzureMarketplace:
		if r.Azure == nil {
			return ierr.NewError("azure is required").
				WithHint("\"azure\" block is required when provider is \"azure_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		if r.AWS != nil || r.GCP != nil {
			return ierr.NewError("only azure must be set").
				WithHint("only the \"azure\" block may be set when provider is \"azure_marketplace\"").
				Mark(ierr.ErrValidation)
		}
		return r.Azure.Validate()
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
