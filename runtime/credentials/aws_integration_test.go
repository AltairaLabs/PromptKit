package credentials

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBedrockEndpoint(t *testing.T) {
	tests := []struct {
		region   string
		expected string
	}{
		{"us-east-1", "https://bedrock-runtime.us-east-1.amazonaws.com"},
		{"us-west-2", "https://bedrock-runtime.us-west-2.amazonaws.com"},
		{"eu-west-1", "https://bedrock-runtime.eu-west-1.amazonaws.com"},
		{"ap-northeast-1", "https://bedrock-runtime.ap-northeast-1.amazonaws.com"},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := BedrockEndpoint(tt.region)
			if got != tt.expected {
				t.Errorf("BedrockEndpoint(%q) = %q, want %q", tt.region, got, tt.expected)
			}
		})
	}
}

func TestBedrockModelMapping(t *testing.T) {
	expectedModels := []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-5-sonnet-20240620",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
		"claude-3-5-haiku-20241022",
	}

	for _, model := range expectedModels {
		t.Run(model, func(t *testing.T) {
			bedrockID, ok := BedrockModelMapping[model]
			if !ok {
				t.Fatalf("model %q not found in BedrockModelMapping", model)
			}
			if !strings.HasPrefix(bedrockID, "anthropic.") {
				t.Errorf("Bedrock ID %q should start with 'anthropic.'", bedrockID)
			}
			// All Bedrock IDs should contain version suffix like :0
			if !strings.Contains(bedrockID, ":") {
				t.Errorf("Bedrock ID %q should contain version suffix with ':'", bedrockID)
			}
		})
	}

	// Verify count matches
	if len(BedrockModelMapping) != len(expectedModels) {
		t.Errorf("BedrockModelMapping has %d entries, expected %d", len(BedrockModelMapping), len(expectedModels))
	}
}

func TestSignRequest(t *testing.T) {
	creds := &aws.Credentials{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-haiku-20241022-v1%3A0/invoke", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	err = signRequest(req, creds, "us-east-1", "bedrock")
	if err != nil {
		t.Fatalf("signRequest failed: %v", err)
	}

	// Verify Authorization header format
	auth := req.Header.Get("Authorization")
	if auth == "" {
		t.Fatal("Authorization header not set")
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Errorf("Authorization header should start with 'AWS4-HMAC-SHA256', got %q", auth)
	}
	if !strings.Contains(auth, "Credential=AKIAIOSFODNN7EXAMPLE/") {
		t.Error("Authorization header should contain credential with access key ID")
	}
	if !strings.Contains(auth, "/us-east-1/bedrock/aws4_request") {
		t.Error("Authorization header should contain correct credential scope")
	}
	if !strings.Contains(auth, "SignedHeaders=") {
		t.Error("Authorization header should contain SignedHeaders")
	}
	if !strings.Contains(auth, "Signature=") {
		t.Error("Authorization header should contain Signature")
	}

	// Verify required SigV4 headers are set
	if req.Header.Get("X-Amz-Date") == "" {
		t.Error("X-Amz-Date header not set")
	}
	if req.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Error("X-Amz-Content-Sha256 header not set")
	}
}

func TestSignRequest_WithSessionToken(t *testing.T) {
	creds := &aws.Credentials{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:    "FwoGZXIvYXdzEBYaDH1234567890",
	}

	req, err := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	err = signRequest(req, creds, "us-east-1", "bedrock")
	if err != nil {
		t.Fatalf("signRequest failed: %v", err)
	}

	if req.Header.Get("X-Amz-Security-Token") != "FwoGZXIvYXdzEBYaDH1234567890" {
		t.Error("X-Amz-Security-Token header not set correctly")
	}
}

func TestURIEncodePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "/model/test/invoke",
			expected: "/model/test/invoke",
		},
		{
			name:     "path with colon (Bedrock model ID)",
			input:    "/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke",
			expected: "/model/anthropic.claude-3-5-haiku-20241022-v1%3A0/invoke",
		},
		{
			name:     "empty path",
			input:    "",
			expected: "",
		},
		{
			name:     "root path",
			input:    "/",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uriEncodePath(tt.input)
			if got != tt.expected {
				t.Errorf("uriEncodePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestAWSCredential_Apply(t *testing.T) {
	cred := &AWSCredential{
		cfg: aws.Config{
			Region: "us-east-1",
			Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				}, nil
			}),
		},
		region: "us-east-1",
	}

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	// Verify SigV4 headers are set
	auth := req.Header.Get("Authorization")
	assert.True(t, strings.HasPrefix(auth, "AWS4-HMAC-SHA256"), "Authorization should start with AWS4-HMAC-SHA256")
	assert.Contains(t, auth, "Credential=AKIAIOSFODNN7EXAMPLE/")
	assert.Contains(t, auth, "/us-east-1/bedrock/aws4_request")

	assert.NotEmpty(t, req.Header.Get("X-Amz-Date"))
	assert.NotEmpty(t, req.Header.Get("X-Amz-Content-Sha256"))
	// No session token — security token header should be absent
	assert.Empty(t, req.Header.Get("X-Amz-Security-Token"))
}

func TestAWSCredential_Type(t *testing.T) {
	cred := &AWSCredential{region: "us-west-2"}
	assert.Equal(t, "aws", cred.Type())
}

func TestAWSCredential_Region(t *testing.T) {
	cred := &AWSCredential{region: "eu-west-1"}
	assert.Equal(t, "eu-west-1", cred.Region())
}

func TestAWSCredential_Apply_WithSessionToken(t *testing.T) {
	cred := &AWSCredential{
		cfg: aws.Config{
			Region: "us-east-1",
			Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					SessionToken:    "FwoGZXIvYXdzEBYaDH1234567890",
				}, nil
			}),
		},
		region: "us-east-1",
	}

	req, err := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "FwoGZXIvYXdzEBYaDH1234567890", req.Header.Get("X-Amz-Security-Token"))
	assert.NotEmpty(t, req.Header.Get("Authorization"))
}

func TestAWSCredential_Apply_CredentialError(t *testing.T) {
	cred := &AWSCredential{
		cfg: aws.Config{
			Region: "us-east-1",
			Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{}, fmt.Errorf("no credentials available")
			}),
		},
		region: "us-east-1",
	}

	req, err := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader("{}"))
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve AWS credentials")
	assert.Contains(t, err.Error(), "no credentials available")
}

func TestAWSCredential_Config(t *testing.T) {
	cfg := aws.Config{
		Region: "ap-southeast-1",
	}
	cred := &AWSCredential{cfg: cfg, region: "ap-southeast-1"}

	got := cred.Config()
	assert.Equal(t, "ap-southeast-1", got.Region)
}
