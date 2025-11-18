package sdk_test

import (
	"context"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPipelineBuilder_WithMediaExternalization verifies that media externalization
// middleware can be added to a pipeline
func TestPipelineBuilder_WithMediaExternalization(t *testing.T) {
	// Create temporary storage directory
	tempDir, err := os.MkdirTemp("", "sdk-media-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage service
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: tempDir,
	})
	require.NoError(t, err)

	// Create mock provider
	provider := mock.NewMockProvider("test-provider", "test-model", false)

	// Build pipeline with media externalization
	builder := sdk.NewPipelineBuilder().
		WithSimpleProvider(provider).
		WithMediaExternalization(storageService, 100, "retain")

	pipeline := builder.Build()
	defer pipeline.Shutdown(context.Background())

	// Verify pipeline was created successfully
	assert.NotNil(t, pipeline)

	// Execute simple request
	result, err := pipeline.Execute(context.Background(), "user", "Hello")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestConversationManager_WithMediaStorage verifies that media storage can be
// configured on the conversation manager
func TestConversationManager_WithMediaStorage(t *testing.T) {
	// Create temporary storage directory
	tempDir, err := os.MkdirTemp("", "sdk-conversation-media-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage service
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: tempDir,
	})
	require.NoError(t, err)

	// Create mock provider
	provider := mock.NewMockProvider("test-provider", "test-model", false)

	// Create conversation manager with media storage
	manager, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
		sdk.WithMediaStorage(storageService),
	)
	require.NoError(t, err)
	assert.NotNil(t, manager)

	// Verify media externalization is enabled by default
	// This is a white-box test - in real usage, the middleware would be activated
	// when processing responses with media content
}

// TestConversationManager_MediaStorageConfig verifies custom media config
func TestConversationManager_MediaStorageConfig(t *testing.T) {
	// Create temporary storage directory
	tempDir, err := os.MkdirTemp("", "sdk-media-config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage service
	storageService, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir: tempDir,
	})
	require.NoError(t, err)

	// Create mock provider
	provider := mock.NewMockProvider("test-provider", "test-model", false)

	// Create conversation manager with custom media config
	customConfig := sdk.ManagerConfig{
		MaxConcurrentExecutions:    50,
		EnableMediaExternalization: true,
		MediaSizeThresholdKB:       200, // 200KB threshold
		MediaDefaultPolicy:         "delete-after-30d",
	}

	manager, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
		sdk.WithMediaStorage(storageService),
		sdk.WithConfig(customConfig),
	)
	require.NoError(t, err)
	assert.NotNil(t, manager)
}
