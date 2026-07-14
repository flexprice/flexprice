// Package awsmarketplace wraps the AWS calls the marketplace integration needs: STS AssumeRole
// (to obtain short-lived credentials for a tenant's IAM role) and Marketplace Metering
// BatchMeterUsage (to report usage).
package awsmarketplace

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/marketplacemetering"
	mmtypes "github.com/aws/aws-sdk-go-v2/service/marketplacemetering/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// UsageRecordInput is one usage record to report. CustomerAWSAccountID + LicenseArn identify the
// buyer's agreement. ProductCode is sent only when non-empty; it is left empty for products
// enrolled in AWS Concurrent Agreements, where LicenseArn identifies the product and ProductCode
// is not allowed.
type UsageRecordInput struct {
	CustomerAWSAccountID string
	LicenseArn           string
	ProductCode          string
	Dimension            string
	Quantity             int32
	Timestamp            time.Time
}

// BatchMeterUsageResult is the outcome AWS reports for an accepted usage record.
type BatchMeterUsageResult struct {
	MeteringRecordID string
	Status           string
}

// Client is the set of AWS Marketplace operations the integration uses.
type Client interface {
	// AssumeRole exchanges the tenant's role ARN and external ID for short-lived credentials.
	AssumeRole(ctx context.Context, roleArn, externalID string) (aws.Credentials, error)

	// BatchMeterUsage reports a single usage record. It returns the result when AWS accepts the
	// record, nil when AWS returns it as unprocessed (the caller should retry it later), or an
	// error when the call itself fails.
	BatchMeterUsage(ctx context.Context, creds aws.Credentials, region string, record UsageRecordInput) (*BatchMeterUsageResult, error)
}

type client struct {
	logger *logger.Logger
}

// NewClient builds a stateless AWS Marketplace client. Credentials are passed per call, never held.
func NewClient(log *logger.Logger) Client {
	return &client{logger: log}
}

// AssumeRole obtains short-lived credentials for the tenant's IAM role. It uses the process's
// ambient AWS credentials (Flexprice's own account) as the caller identity, and the tenant's role
// ARN + external ID as the assume target. The role session name identifies this session in the
// tenant's AWS CloudTrail. Errors are returned verbatim so the caller can surface AWS's message;
// the role ARN and external ID are never logged.
func (c *client) AssumeRole(ctx context.Context, roleArn, externalID string) (aws.Credentials, error) {
	if roleArn == "" || externalID == "" {
		return aws.Credentials{}, ierr.NewError("role_arn and external_id are required").
			WithHint("AWS Marketplace connection requires both role_arn and external_id").
			Mark(ierr.ErrValidation)
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return aws.Credentials{}, ierr.WithError(err).
			WithHint("Failed to load AWS SDK configuration").
			Mark(ierr.ErrSystem)
	}

	stsClient := sts.NewFromConfig(cfg)
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleArn, func(o *stscreds.AssumeRoleOptions) {
		o.ExternalID = &externalID
		o.RoleSessionName = "flexprice-marketplace-metering"
	})

	creds, err := provider.Retrieve(ctx)
	if err != nil {
		c.logger.Error(ctx, "aws marketplace assume role failed", "error", err)
		return aws.Credentials{}, ierr.WithError(err).
			WithHint("Failed to assume the provided AWS IAM role. Verify the role ARN, trust policy, and external ID.").
			Mark(ierr.ErrValidation)
	}

	return creds, nil
}

// BatchMeterUsage reports a single usage record to AWS Marketplace Metering. The AWS config is
// built directly from the assumed-role credentials and the connection's region so no ambient
// environment or instance-profile configuration is picked up.
//
// AWS returns an accepted record in Results (with a metering record id) or, if it could not be
// processed, in UnprocessedRecords. An accepted record returns a result; an unprocessed record
// returns nil so the caller retries it on the next run.
func (c *client) BatchMeterUsage(ctx context.Context, creds aws.Credentials, region string, record UsageRecordInput) (*BatchMeterUsageResult, error) {
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.StaticCredentialsProvider{Value: creds},
	}
	meteringClient := marketplacemetering.NewFromConfig(cfg)

	ts := record.Timestamp
	awsRecord := mmtypes.UsageRecord{
		Timestamp: &ts,
		Dimension: aws.String(record.Dimension),
		Quantity:  aws.Int32(record.Quantity),
	}
	if record.CustomerAWSAccountID != "" {
		awsRecord.CustomerAWSAccountId = aws.String(record.CustomerAWSAccountID)
	}
	if record.LicenseArn != "" {
		awsRecord.LicenseArn = aws.String(record.LicenseArn)
	}

	input := &marketplacemetering.BatchMeterUsageInput{UsageRecords: []mmtypes.UsageRecord{awsRecord}}
	if record.ProductCode != "" {
		input.ProductCode = aws.String(record.ProductCode)
	}

	out, err := meteringClient.BatchMeterUsage(ctx, input)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("BatchMeterUsage call failed").
			Mark(ierr.ErrHTTPClient)
	}

	if len(out.Results) == 0 {
		return nil, nil
	}
	res := out.Results[0]
	result := &BatchMeterUsageResult{Status: string(res.Status)}
	if res.MeteringRecordId != nil {
		result.MeteringRecordID = *res.MeteringRecordId
	}
	return result, nil
}
