// Package gcpmarketplace wraps the GCP calls the marketplace integration needs: a Workload Identity
// Federation token exchange (to obtain a Service Control client authenticated as a tenant's service
// account) and Service Control services.report (to report usage). Mirrors
// internal/integration/awsmarketplace in shape — AssumeRole's counterpart is WifSession,
// BatchMeterUsage's counterpart is Report.
package gcpmarketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"golang.org/x/oauth2/google/externalaccount"
	"google.golang.org/api/option"
	servicecontrol "google.golang.org/api/servicecontrol/v1"
)

// servicecontrolScope is the OAuth scope the Service Control API requires.
const servicecontrolScope = "https://www.googleapis.com/auth/cloud-platform"

// awsSubjectTokenType is the STS token type for an AWS-sourced external account credential
// (external_account_authorized_user is a different flow; this is the one gcloud's --aws flag emits).
const awsSubjectTokenType = "urn:ietf:params:aws:token-type:aws4_request"

// UsageReportInput is one usage record to report via services.report. ValueCents is a single int64
// scalar — the client wraps it into metricValueSets[0].metricValues[0].Int64Value; callers never
// touch GCP's nested wire shape directly. OperationID is the idempotency key GCP de-duplicates on
// (the caller sets it to the usage record's own id, so a retry sends an identical operation).
type UsageReportInput struct {
	ServiceName string
	ConsumerID  string
	MetricName  string
	ValueCents  int64
	OperationID string
	StartTime   time.Time
	EndTime     time.Time
}

// ReportResult is GCP's outcome for a services.report call. Accepted mirrors AWS's "a present
// Result is not the same as accepted" discipline: services.report returns HTTP 200 even when it
// rejects the record, so Accepted is derived from len(reportErrors)==0, never from the HTTP status
// alone. ErrorCode/ErrorMessage are only meaningful when Accepted is false. OperationID echoes back
// the operation id this call actually sent (== UsageReportInput.OperationID) — the one and only
// place that value lives, so a caller storing "what we reported as" reads it from here instead of
// re-deriving it a second time from whatever it built the request with.
type ReportResult struct {
	Accepted     bool
	OperationID  string
	ErrorCode    int64
	ErrorMessage string
}

// Client is the set of GCP Marketplace operations the integration uses.
type Client interface {
	// WifSession loads a tenant's Workload Identity Federation credentials JSON and returns a
	// ready-to-use, authenticated Service Control client. It forces the token exchange immediately
	// (rather than deferring to first API use) so a broken pool/provider/binding surfaces here,
	// synchronously — this is what the connection-creation verification step relies on.
	WifSession(ctx context.Context, credentialsJSON string) (*servicecontrol.Service, error)

	// Report reports ONE record via services.report. A nil error means the call itself completed;
	// the caller must still check Accepted, since GCP returns HTTP 200 even for a rejected record.
	Report(ctx context.Context, svc *servicecontrol.Service, in UsageReportInput) (*ReportResult, error)
}

// client is itself the externalaccount.AwsSecurityCredentialsSupplier for every WifSession call: it
// supplies the AWS side of the federation exchange explicitly (Flexprice's own static caller
// identity, config: marketplace.aws.*, assuming the one shared flexprice-gcp-metering-role that
// every tenant's GCP project trusts — config: marketplace.gcp.flexprice_aws_account_id/role_name).
// This is deliberately the same "static creds assume a target role" shape
// awsmarketplace.Client.AssumeRole already uses, instead of letting Google's library fall back to
// the ambient AWS credential chain (env vars, then the EC2/ECS instance-metadata endpoint) — that
// chain's last step is unreachable off EC2/ECS and stalls for seconds before failing, exactly what
// AWSMarketplaceConfig's own doc comment explains AWS Marketplace avoids for the same reason. Using
// an explicit supplier means this works identically in local dev, CI, and prod regardless of what
// host the process happens to run on, and never depends on an IAM role being attached to whatever
// compute is running it.
type client struct {
	awsCfg    config.AWSMarketplaceConfig
	roleArn   string
	region    string
	credCache *aws.CredentialsCache
	logger    *logger.Logger
}

// NewClient builds a GCP Marketplace client. Unlike awsmarketplace.Client, there is nothing
// per-tenant to inject at construction time — the AWS role assumed here is the single one shared
// across every tenant, built once from config, not passed per call.
func NewClient(conf *config.Configuration, log *logger.Logger) Client {
	awsCfg := conf.Marketplace.AWS
	gcpCfg := conf.Marketplace.GCP

	roleArn := ""
	if gcpCfg.FlexpriceAWSAccountID != "" && gcpCfg.FlexpriceAWSRoleName != "" {
		roleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", gcpCfg.FlexpriceAWSAccountID, gcpCfg.FlexpriceAWSRoleName)
	}

	stsClient := sts.NewFromConfig(aws.Config{
		Region: awsCfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			awsCfg.AccessKeyID, awsCfg.SecretAccessKey, awsCfg.SessionToken,
		),
	})
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleArn, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = "flexprice-gcp-marketplace-metering"
	})

	return &client{
		awsCfg:  awsCfg,
		roleArn: roleArn,
		region:  awsCfg.Region,
		// Wrapped in a CredentialsCache (not called directly) so the assumed session is reused
		// across every WifSession call — potentially many, one per GCP connection, in a single
		// reporting-cron run — instead of a fresh AssumeRole per connection. The cache handles its
		// own thread-safe refresh once the session nears expiry.
		credCache: aws.NewCredentialsCache(provider),
		logger:    log,
	}
}

// AwsRegion implements externalaccount.AwsSecurityCredentialsSupplier.
func (c *client) AwsRegion(_ context.Context, _ externalaccount.SupplierOptions) (string, error) {
	return c.region, nil
}

// AwsSecurityCredentials implements externalaccount.AwsSecurityCredentialsSupplier — see the client
// struct's doc comment for why this assumes a role explicitly instead of reading ambient credentials.
func (c *client) AwsSecurityCredentials(ctx context.Context, _ externalaccount.SupplierOptions) (*externalaccount.AwsSecurityCredentials, error) {
	if c.awsCfg.AccessKeyID == "" || c.awsCfg.SecretAccessKey == "" || c.awsCfg.Region == "" {
		return nil, ierr.NewError("aws marketplace credentials are not configured").
			WithHint("Flexprice's own AWS credentials are missing. Set marketplace.aws.region, marketplace.aws.access_key_id and marketplace.aws.secret_access_key.").
			Mark(ierr.ErrSystem)
	}
	if c.roleArn == "" {
		return nil, ierr.NewError("flexprice gcp metering role is not configured").
			WithHint("Set marketplace.gcp.flexprice_aws_account_id and marketplace.gcp.flexprice_aws_role_name.").
			Mark(ierr.ErrSystem)
	}

	creds, err := c.credCache.Retrieve(ctx)
	if err != nil {
		// Deliberately not logging the raw AWS error — an AccessDenied for a bad trust policy can
		// embed the role ARN in its text, the same discipline awsmarketplace.AssumeRole follows.
		c.logger.Error(ctx, "gcp marketplace failed to assume the flexprice gcp metering role",
			"error", "redacted: aws error message may embed the role arn")
		return nil, ierr.NewError("failed to assume the flexprice gcp metering role").
			WithHint("Verify marketplace.aws credentials and the flexprice-gcp-metering role's trust policy.").
			Mark(ierr.ErrSystem)
	}

	return &externalaccount.AwsSecurityCredentials{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
	}, nil
}

func (c *client) WifSession(ctx context.Context, credentialsJSON string) (*servicecontrol.Service, error) {
	if credentialsJSON == "" {
		return nil, ierr.NewError("credentials_json is required").
			WithHint("GCP Marketplace connection requires credentials_json").
			Mark(ierr.ErrValidation)
	}

	// Only the fields needed to build the token exchange are read here. Deliberately not
	// credential_source: gcloud's --aws flag always bakes in a real EC2/ECS instance-metadata URL
	// there, which this client never uses — the AWS side of the exchange comes from
	// AwsSecurityCredentials above instead. TokenURL is optional; a config without it falls back to
	// GCP's default STS endpoint, which is what gcloud's output already points at anyway.
	var parsed struct {
		Type                           string `json:"type"`
		Audience                       string `json:"audience"`
		TokenURL                       string `json:"token_url"`
		ServiceAccountImpersonationURL string `json:"service_account_impersonation_url"`
	}
	if err := json.Unmarshal([]byte(credentialsJSON), &parsed); err != nil {
		return nil, ierr.NewError("credentials_json is not valid JSON").
			WithHint("GCP Marketplace credentials_json must be the JSON file generated by `gcloud iam workload-identity-pools create-cred-config`.").
			Mark(ierr.ErrValidation)
	}
	if parsed.Type != "external_account" || parsed.Audience == "" || parsed.ServiceAccountImpersonationURL == "" {
		return nil, ierr.NewError("credentials_json is not a workload identity federation config").
			WithHint("GCP Marketplace credentials_json must have \"type\": \"external_account\", plus audience and service_account_impersonation_url — paste the file generated by `gcloud iam workload-identity-pools create-cred-config`, not a service account key.").
			Mark(ierr.ErrValidation)
	}

	tokenSource, err := externalaccount.NewTokenSource(ctx, externalaccount.Config{
		Audience:                       parsed.Audience,
		SubjectTokenType:               awsSubjectTokenType,
		TokenURL:                       parsed.TokenURL,
		ServiceAccountImpersonationURL: parsed.ServiceAccountImpersonationURL,
		Scopes:                         []string{servicecontrolScope},
		AwsSecurityCredentialsSupplier: c,
	})
	if err != nil {
		return nil, ierr.NewError("gcp marketplace credentials_json is invalid").
			WithHint("Verify credentials_json is the file generated by `gcloud iam workload-identity-pools create-cred-config`.").
			Mark(ierr.ErrValidation)
	}

	// Force the AWS -> GCP federated token -> service-account impersonation exchange now. On
	// failure, deliberately don't log the raw error: GCP's WIF errors can embed project/pool IDs,
	// the same discipline awsmarketplace.Client.AssumeRole follows for AWS's AccessDenied messages
	// embedding the role ARN.
	if _, err := tokenSource.Token(); err != nil {
		c.logger.Error(ctx, "gcp marketplace wif exchange failed",
			"error", "redacted: gcp error message may embed project or pool ids")
		return nil, ierr.NewError("gcp marketplace wif exchange failed").
			WithHint("Could not complete the workload identity federation exchange. Verify the workload identity pool, provider, and service account binding.").
			Mark(ierr.ErrValidation)
	}

	svc, err := servicecontrol.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to build the GCP Service Control client").
			Mark(ierr.ErrSystem)
	}

	return svc, nil
}

func (c *client) Report(ctx context.Context, svc *servicecontrol.Service, in UsageReportInput) (*ReportResult, error) {
	req := &servicecontrol.ReportRequest{
		Operations: []*servicecontrol.Operation{
			{
				OperationId:   in.OperationID,
				OperationName: "flexprice/usage_report",
				ConsumerId:    in.ConsumerID,
				StartTime:     in.StartTime.UTC().Format(time.RFC3339),
				EndTime:       in.EndTime.UTC().Format(time.RFC3339),
				MetricValueSets: []*servicecontrol.MetricValueSet{
					{
						MetricName: in.MetricName,
						MetricValues: []*servicecontrol.MetricValue{
							{Int64Value: &in.ValueCents},
						},
					},
				},
			},
		},
	}

	resp, err := svc.Services.Report(in.ServiceName, req).Context(ctx).Do()
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("services.report call failed").
			Mark(ierr.ErrHTTPClient)
	}

	// HTTP 200 does not mean accepted — reportErrors must be inspected. Mirrors
	// awsmarketplace.Client.BatchMeterUsage's "a present Result is not the same as an accepted one."
	if len(resp.ReportErrors) == 0 {
		return &ReportResult{Accepted: true, OperationID: in.OperationID}, nil
	}

	result := &ReportResult{Accepted: false, OperationID: in.OperationID}
	if reportErr := resp.ReportErrors[0]; reportErr.Status != nil {
		result.ErrorCode = reportErr.Status.Code
		result.ErrorMessage = reportErr.Status.Message
	}
	return result, nil
}
