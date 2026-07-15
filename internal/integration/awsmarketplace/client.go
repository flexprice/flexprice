// Package awsmarketplace wraps the AWS calls the marketplace integration needs: STS AssumeRole
// (to obtain short-lived credentials for a tenant's IAM role) and Marketplace Metering
// BatchMeterUsage (to report usage).
package awsmarketplace

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/marketplacemetering"
	mmtypes "github.com/aws/aws-sdk-go-v2/service/marketplacemetering/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/flexprice/flexprice/internal/config"
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

// AWS's three possible per-record outcomes (marketplacemetering/types.UsageRecordResultStatus).
// A present entry in Results is NOT the same as the record being honored — Status must be checked.
// Only StatusSuccess means AWS actually billed the record:
//   - StatusCustomerNotSubscribed: the buyer has no active agreement (or was suspended); not
//     honored.
//   - StatusDuplicateRecord: AWS already has a DIFFERENT usage record for the same customer,
//     dimension, and timestamp — the existing one, not this one, is what's on file. Also not
//     honored. (This is not "AWS already recorded this exact record, safe to skip" — that case is
//     just a second StatusSuccess, since AWS's own idempotency is on customer+dimension+time+
//     quantity together.)
const (
	StatusSuccess               = string(mmtypes.UsageRecordResultStatusSuccess)
	StatusCustomerNotSubscribed = string(mmtypes.UsageRecordResultStatusCustomerNotSubscribed)
	StatusDuplicateRecord       = string(mmtypes.UsageRecordResultStatusDuplicateRecord)
)

// BatchMeterUsageResult is AWS's outcome for a record present in Results. Status must be checked —
// only StatusSuccess means the record is recorded (billed) on AWS's side.
type BatchMeterUsageResult struct {
	MeteringRecordID string
	Status           string
}

// Client is the set of AWS Marketplace operations the integration uses.
type Client interface {
	// AssumeRole exchanges the tenant's role ARN and external ID for short-lived credentials.
	// duration controls how long the assumed session is valid for. AWS rejects anything below 15
	// minutes (STS: durationSeconds must be >= 900) and anything above the role's own
	// MaxSessionDuration, so valid values are 15m up to that ceiling. Pass 0 to use the SDK default
	// (15 minutes, see stscreds.DefaultDuration).
	AssumeRole(ctx context.Context, roleArn, externalID string, duration time.Duration) (aws.Credentials, error)

	// BatchMeterUsage reports a single usage record. It returns the result for any record present
	// in Results — whose Status the caller must check, since presence alone does not mean AWS
	// accepted it — nil when AWS returns it as unprocessed (the caller should retry it later), or
	// an error when the call itself fails.
	BatchMeterUsage(ctx context.Context, creds aws.Credentials, region string, record UsageRecordInput) (*BatchMeterUsageResult, error)
}

type client struct {
	cfg    config.AWSMarketplaceConfig
	logger *logger.Logger
}

// NewClient builds a stateless AWS Marketplace client. cfg carries Flexprice's own AWS identity —
// the principal that assumes each tenant's role. Tenant credentials are passed per call, never held.
func NewClient(conf *config.Configuration, log *logger.Logger) Client {
	return &client{cfg: conf.Marketplace.AWS, logger: log}
}

// AssumeRole obtains short-lived credentials for the tenant's IAM role. Flexprice's own configured
// credentials (config: aws_marketplace.*) are the caller identity — sts:AssumeRole is an
// authenticated API and the tenant's trust policy names this principal — and the tenant's role ARN
// + external ID are the assume target. The role session name identifies this session in the
// tenant's AWS CloudTrail.
//
// Everything is built explicitly: no LoadDefaultConfig, no ambient credential chain. The chain's
// last step probes the EC2 instance-metadata endpoint, which is unreachable off EC2 and stalls for
// seconds before failing — that must never be in the path of a user-facing connection request.
//
// On failure, the AWS SDK error is not logged or returned — a bad trust policy's AccessDenied
// message embeds the role ARN in its text, so nothing derived from it is surfaced anywhere. The
// role ARN and external ID are never logged.
func (c *client) AssumeRole(ctx context.Context, roleArn, externalID string, duration time.Duration) (aws.Credentials, error) {
	if roleArn == "" || externalID == "" {
		return aws.Credentials{}, ierr.NewError("role_arn and external_id are required").
			WithHint("AWS Marketplace connection requires both role_arn and external_id").
			Mark(ierr.ErrValidation)
	}
	if c.cfg.AccessKeyID == "" || c.cfg.SecretAccessKey == "" || c.cfg.Region == "" {
		return aws.Credentials{}, ierr.NewError("aws marketplace credentials are not configured").
			WithHint("Flexprice's own AWS credentials are missing. Set marketplace.aws.region, marketplace.aws.access_key_id and marketplace.aws.secret_access_key.").
			Mark(ierr.ErrSystem)
	}

	stsClient := sts.NewFromConfig(aws.Config{
		Region: c.cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			c.cfg.AccessKeyID, c.cfg.SecretAccessKey, c.cfg.SessionToken,
		),
	})
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleArn, func(o *stscreds.AssumeRoleOptions) {
		o.ExternalID = &externalID
		o.RoleSessionName = "flexprice-marketplace-metering"
		o.Duration = duration // 0 falls through to the SDK's own default (stscreds.DefaultDuration)
	})

	creds, err := provider.Retrieve(ctx)
	if err != nil {
		// Deliberately not logger.Err(err) and not ierr.WithError(err): AWS's AccessDenied
		// message for a bad trust policy embeds the role ARN in its text. The redacted
		// placeholder still satisfies loglint's "every Error() log carries an error field"
		// invariant without leaking it into logs or API responses.
		c.logger.Error(ctx, "aws marketplace assume role failed", "error", "redacted: aws error message may embed the role arn")
		return aws.Credentials{}, ierr.NewError("aws marketplace assume role failed").
			WithHint("Failed to assume the provided AWS IAM role. Verify the role ARN, trust policy, and external ID.").
			Mark(ierr.ErrValidation)
	}

	return creds, nil
}

// BatchMeterUsage reports a single usage record to AWS Marketplace Metering. The AWS config is
// built directly from the assumed-role credentials and the connection's region so no ambient
// environment or instance-profile configuration is picked up.
//
// AWS returns a processed record in Results (with a Status the caller must check — a present
// result is not the same as an accepted one) or, if it could not be processed at all, in
// UnprocessedRecords, for which this returns nil so the caller retries it on the next run.
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
