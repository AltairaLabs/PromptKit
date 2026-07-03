package credentials

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// defaultAWSRegion is the fallback region when none is specified.
const defaultAWSRegion = "us-west-2"

// BedrockModelMapping maps Claude model names to Bedrock model IDs.
var BedrockModelMapping = map[string]string{
	"claude-3-5-sonnet-20241022": "anthropic.claude-3-5-sonnet-20241022-v2:0",
	"claude-3-5-sonnet-20240620": "anthropic.claude-3-5-sonnet-20240620-v1:0",
	"claude-3-opus-20240229":     "anthropic.claude-3-opus-20240229-v1:0",
	"claude-3-sonnet-20240229":   "anthropic.claude-3-sonnet-20240229-v1:0",
	"claude-3-haiku-20240307":    "anthropic.claude-3-haiku-20240307-v1:0",
	"claude-3-5-haiku-20241022":  "anthropic.claude-3-5-haiku-20241022-v1:0",
}

// AWSCredential implements AWS SigV4 signing for Bedrock.
type AWSCredential struct {
	cfg    aws.Config
	region string
}

// NewAWSCredential creates a new AWS credential using the default credential chain.
// This supports IRSA (IAM Roles for Service Accounts), instance profiles, and
// environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY).
func NewAWSCredential(ctx context.Context, region string) (*AWSCredential, error) {
	if region == "" {
		region = defaultAWSRegion
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AWSCredential{
		cfg:    cfg,
		region: region,
	}, nil
}

// NewAWSCredentialWithProfile creates an AWS credential using a named profile
// from the shared credentials/config files (~/.aws/credentials, ~/.aws/config).
func NewAWSCredentialWithProfile(ctx context.Context, region, profile string) (*AWSCredential, error) {
	if region == "" {
		region = defaultAWSRegion
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithSharedConfigProfile(profile),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config with profile %q: %w", profile, err)
	}

	return &AWSCredential{
		cfg:    cfg,
		region: region,
	}, nil
}

// NewAWSCredentialWithRole creates an AWS credential that assumes a role.
func NewAWSCredentialWithRole(ctx context.Context, region, roleARN string) (*AWSCredential, error) {
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create STS client and assume role credentials provider
	stsClient := sts.NewFromConfig(cfg)
	cfg.Credentials = stscreds.NewAssumeRoleProvider(stsClient, roleARN)

	return &AWSCredential{
		cfg:    cfg,
		region: region,
	}, nil
}

// Type returns "aws".
func (c *AWSCredential) Type() string {
	return "aws"
}

// Region returns the configured AWS region.
func (c *AWSCredential) Region() string {
	return c.region
}

// Config returns the AWS config for advanced use cases.
func (c *AWSCredential) Config() aws.Config {
	return c.cfg
}
