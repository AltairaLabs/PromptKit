package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swapBedrockConstructors saves the current bedrock constructor seams, installs
// the supplied fakes, and returns a restore func for defer.
func swapBedrockConstructors(t *testing.T,
	role, profile func(ctx context.Context, region, arg string) (*credentials.AWSCredential, error),
	def func(ctx context.Context, region string) (*credentials.AWSCredential, error),
) {
	t.Helper()
	origRole := newAWSCredentialWithRole
	origProfile := newAWSCredentialWithProfile
	origDef := newAWSCredential
	newAWSCredentialWithRole = role
	newAWSCredentialWithProfile = profile
	newAWSCredential = def
	t.Cleanup(func() {
		newAWSCredentialWithRole = origRole
		newAWSCredentialWithProfile = origProfile
		newAWSCredential = origDef
	})
}

func TestResolveBedrockCredential_Selection(t *testing.T) {
	t.Run("role ARN selected", func(t *testing.T) {
		called := ""
		var gotRegion, gotArg string
		swapBedrockConstructors(t,
			func(_ context.Context, region, arg string) (*credentials.AWSCredential, error) {
				called, gotRegion, gotArg = "role", region, arg
				return nil, nil
			},
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				called = "profile"
				return nil, nil
			},
			func(_ context.Context, _ string) (*credentials.AWSCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveBedrockCredential(context.Background(), &platformConfig{
			region: "us-west-2", roleARN: "arn:aws:iam::1:role/x",
		})
		require.NoError(t, err)
		assert.Equal(t, "role", called)
		assert.Equal(t, "us-west-2", gotRegion)
		assert.Equal(t, "arn:aws:iam::1:role/x", gotArg)
	})

	t.Run("profile selected when no role", func(t *testing.T) {
		called := ""
		var gotRegion, gotArg string
		swapBedrockConstructors(t,
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				called = "role"
				return nil, nil
			},
			func(_ context.Context, region, arg string) (*credentials.AWSCredential, error) {
				called, gotRegion, gotArg = "profile", region, arg
				return nil, nil
			},
			func(_ context.Context, _ string) (*credentials.AWSCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveBedrockCredential(context.Background(), &platformConfig{
			region: "eu-west-1", awsProfile: "prod",
		})
		require.NoError(t, err)
		assert.Equal(t, "profile", called)
		assert.Equal(t, "eu-west-1", gotRegion)
		assert.Equal(t, "prod", gotArg)
	})

	t.Run("role wins over profile (mis-order guard)", func(t *testing.T) {
		called := ""
		swapBedrockConstructors(t,
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				called = "role"
				return nil, nil
			},
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				called = "profile"
				return nil, nil
			},
			func(_ context.Context, _ string) (*credentials.AWSCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveBedrockCredential(context.Background(), &platformConfig{
			region: "us-west-2", roleARN: "arn:role", awsProfile: "prod",
		})
		require.NoError(t, err)
		assert.Equal(t, "role", called, "roleARN must take precedence over awsProfile")
	})

	t.Run("default when nothing set", func(t *testing.T) {
		called := ""
		var gotRegion string
		swapBedrockConstructors(t,
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				called = "role"
				return nil, nil
			},
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				called = "profile"
				return nil, nil
			},
			func(_ context.Context, region string) (*credentials.AWSCredential, error) {
				called, gotRegion = "default", region
				return nil, nil
			},
		)
		_, err := resolveBedrockCredential(context.Background(), &platformConfig{region: "ap-south-1"})
		require.NoError(t, err)
		assert.Equal(t, "default", called)
		assert.Equal(t, "ap-south-1", gotRegion)
	})

	t.Run("error is wrapped", func(t *testing.T) {
		sentinel := errors.New("boom")
		swapBedrockConstructors(t,
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) {
				return nil, sentinel
			},
			func(_ context.Context, _, _ string) (*credentials.AWSCredential, error) { return nil, nil },
			func(_ context.Context, _ string) (*credentials.AWSCredential, error) { return nil, nil },
		)
		_, err := resolveBedrockCredential(context.Background(), &platformConfig{roleARN: "arn:role"})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "bedrock credentials")
	})
}

func swapVertexConstructors(t *testing.T,
	sa func(ctx context.Context, project, region, keyFile string) (*credentials.GCPCredential, error),
	def func(ctx context.Context, project, region string) (*credentials.GCPCredential, error),
) {
	t.Helper()
	origSA := newGCPCredentialWithServiceAcc
	origDef := newGCPCredential
	newGCPCredentialWithServiceAcc = sa
	newGCPCredential = def
	t.Cleanup(func() {
		newGCPCredentialWithServiceAcc = origSA
		newGCPCredential = origDef
	})
}

func TestResolveVertexCredential_Selection(t *testing.T) {
	t.Run("service account key selected", func(t *testing.T) {
		called := ""
		var gotProject, gotRegion, gotKey string
		swapVertexConstructors(t,
			func(_ context.Context, project, region, keyFile string) (*credentials.GCPCredential, error) {
				called, gotProject, gotRegion, gotKey = "sa", project, region, keyFile
				return nil, nil
			},
			func(_ context.Context, _, _ string) (*credentials.GCPCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveVertexCredential(context.Background(), &platformConfig{
			project: "p", region: "us-central1", serviceAccountKeyPath: "/key.json",
		})
		require.NoError(t, err)
		assert.Equal(t, "sa", called)
		assert.Equal(t, "p", gotProject)
		assert.Equal(t, "us-central1", gotRegion)
		assert.Equal(t, "/key.json", gotKey)
	})

	t.Run("default ADC when no key", func(t *testing.T) {
		called := ""
		var gotProject, gotRegion string
		swapVertexConstructors(t,
			func(_ context.Context, _, _, _ string) (*credentials.GCPCredential, error) {
				called = "sa"
				return nil, nil
			},
			func(_ context.Context, project, region string) (*credentials.GCPCredential, error) {
				called, gotProject, gotRegion = "default", project, region
				return nil, nil
			},
		)
		_, err := resolveVertexCredential(context.Background(), &platformConfig{
			project: "p2", region: "europe-west1",
		})
		require.NoError(t, err)
		assert.Equal(t, "default", called)
		assert.Equal(t, "p2", gotProject)
		assert.Equal(t, "europe-west1", gotRegion)
	})

	t.Run("error is wrapped", func(t *testing.T) {
		sentinel := errors.New("boom")
		swapVertexConstructors(t,
			func(_ context.Context, _, _, _ string) (*credentials.GCPCredential, error) { return nil, nil },
			func(_ context.Context, _, _ string) (*credentials.GCPCredential, error) {
				return nil, sentinel
			},
		)
		_, err := resolveVertexCredential(context.Background(), &platformConfig{project: "p"})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "vertex credentials")
	})
}

func swapAzureConstructors(t *testing.T,
	secret func(ctx context.Context, endpoint, tenantID, clientID, clientSecret string) (*credentials.AzureCredential, error),
	mi func(ctx context.Context, endpoint string, clientID *string) (*credentials.AzureCredential, error),
	def func(ctx context.Context, endpoint string) (*credentials.AzureCredential, error),
) {
	t.Helper()
	origSecret := newAzureCredentialWithClientSecret
	origMI := newAzureCredentialWithManagedIdentity
	origDef := newAzureCredential
	newAzureCredentialWithClientSecret = secret
	newAzureCredentialWithManagedIdentity = mi
	newAzureCredential = def
	t.Cleanup(func() {
		newAzureCredentialWithClientSecret = origSecret
		newAzureCredentialWithManagedIdentity = origMI
		newAzureCredential = origDef
	})
}

func TestResolveAzureCredential_Selection(t *testing.T) {
	t.Run("client secret selected", func(t *testing.T) {
		called := ""
		var gotEndpoint, gotTenant, gotClient, gotSecret string
		swapAzureConstructors(t,
			func(_ context.Context, endpoint, tenantID, clientID, clientSecret string) (*credentials.AzureCredential, error) {
				called = "secret"
				gotEndpoint, gotTenant, gotClient, gotSecret = endpoint, tenantID, clientID, clientSecret
				return nil, nil
			},
			func(_ context.Context, _ string, _ *string) (*credentials.AzureCredential, error) {
				called = "mi"
				return nil, nil
			},
			func(_ context.Context, _ string) (*credentials.AzureCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveAzureCredential(context.Background(), &platformConfig{
			endpoint: "https://ep",
			azureClientSecret: &azureClientSecretConfig{
				tenantID: "t", clientID: "c", clientSecret: "s",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "secret", called)
		assert.Equal(t, "https://ep", gotEndpoint)
		assert.Equal(t, "t", gotTenant)
		assert.Equal(t, "c", gotClient)
		assert.Equal(t, "s", gotSecret)
	})

	t.Run("managed identity selected when no secret", func(t *testing.T) {
		called := ""
		var gotEndpoint string
		var gotClientID *string
		swapAzureConstructors(t,
			func(_ context.Context, _, _, _, _ string) (*credentials.AzureCredential, error) {
				called = "secret"
				return nil, nil
			},
			func(_ context.Context, endpoint string, clientID *string) (*credentials.AzureCredential, error) {
				called, gotEndpoint, gotClientID = "mi", endpoint, clientID
				return nil, nil
			},
			func(_ context.Context, _ string) (*credentials.AzureCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveAzureCredential(context.Background(), &platformConfig{
			endpoint: "https://ep2", managedIdentityClientID: "mi-client",
		})
		require.NoError(t, err)
		assert.Equal(t, "mi", called)
		assert.Equal(t, "https://ep2", gotEndpoint)
		require.NotNil(t, gotClientID)
		assert.Equal(t, "mi-client", *gotClientID)
	})

	t.Run("client secret wins over managed identity (mis-order guard)", func(t *testing.T) {
		called := ""
		swapAzureConstructors(t,
			func(_ context.Context, _, _, _, _ string) (*credentials.AzureCredential, error) {
				called = "secret"
				return nil, nil
			},
			func(_ context.Context, _ string, _ *string) (*credentials.AzureCredential, error) {
				called = "mi"
				return nil, nil
			},
			func(_ context.Context, _ string) (*credentials.AzureCredential, error) {
				called = "default"
				return nil, nil
			},
		)
		_, err := resolveAzureCredential(context.Background(), &platformConfig{
			endpoint:                "https://ep",
			managedIdentityClientID: "mi-client",
			azureClientSecret:       &azureClientSecretConfig{tenantID: "t", clientID: "c", clientSecret: "s"},
		})
		require.NoError(t, err)
		assert.Equal(t, "secret", called, "client secret must take precedence over managed identity")
	})

	t.Run("default when nothing set", func(t *testing.T) {
		called := ""
		var gotEndpoint string
		swapAzureConstructors(t,
			func(_ context.Context, _, _, _, _ string) (*credentials.AzureCredential, error) {
				called = "secret"
				return nil, nil
			},
			func(_ context.Context, _ string, _ *string) (*credentials.AzureCredential, error) {
				called = "mi"
				return nil, nil
			},
			func(_ context.Context, endpoint string) (*credentials.AzureCredential, error) {
				called, gotEndpoint = "default", endpoint
				return nil, nil
			},
		)
		_, err := resolveAzureCredential(context.Background(), &platformConfig{endpoint: "https://ep3"})
		require.NoError(t, err)
		assert.Equal(t, "default", called)
		assert.Equal(t, "https://ep3", gotEndpoint)
	})

	t.Run("error is wrapped", func(t *testing.T) {
		sentinel := errors.New("boom")
		swapAzureConstructors(t,
			func(_ context.Context, _, _, _, _ string) (*credentials.AzureCredential, error) {
				return nil, sentinel
			},
			func(_ context.Context, _ string, _ *string) (*credentials.AzureCredential, error) { return nil, nil },
			func(_ context.Context, _ string) (*credentials.AzureCredential, error) { return nil, nil },
		)
		_, err := resolveAzureCredential(context.Background(), &platformConfig{
			azureClientSecret: &azureClientSecretConfig{tenantID: "t", clientID: "c", clientSecret: "s"},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "azure credentials")
	})
}
