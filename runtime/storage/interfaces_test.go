package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

// mockMediaStorageService is a mock implementation for testing
type mockMediaStorageService struct {
	storeFunc    func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error)
	retrieveFunc func(ctx context.Context, reference storage.Reference) (*types.MediaContent, error)
	deleteFunc   func(ctx context.Context, reference storage.Reference) error
	getURLFunc   func(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error)
}

func (m *mockMediaStorageService) StoreMedia(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, content, metadata)
	}
	return "", nil
}

func (m *mockMediaStorageService) RetrieveMedia(ctx context.Context, reference storage.Reference) (*types.MediaContent, error) {
	if m.retrieveFunc != nil {
		return m.retrieveFunc(ctx, reference)
	}
	return nil, nil
}

func (m *mockMediaStorageService) DeleteMedia(ctx context.Context, reference storage.Reference) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, reference)
	}
	return nil
}

func (m *mockMediaStorageService) GetURL(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error) {
	if m.getURLFunc != nil {
		return m.getURLFunc(ctx, reference, expiry)
	}
	return "", nil
}

func TestMediaStorageServiceInterface(t *testing.T) {
	ctx := context.Background()
	testData := "test-base64-data"

	content := &types.MediaContent{
		Data:     &testData,
		MIMEType: "image/jpeg",
	}

	metadata := storage.MediaMetadata{
		RunID:      "test-run",
		MessageIdx: 0,
		PartIdx:    0,
		MIMEType:   "image/jpeg",
		SizeBytes:  1024,
		Timestamp:  time.Now(),
	}

	t.Run("StoreMedia interface", func(t *testing.T) {
		called := false
		mock := &mockMediaStorageService{
			storeFunc: func(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
				called = true
				assert.NotNil(t, content)
				assert.Equal(t, "test-run", metadata.RunID)
				return storage.Reference("ref-123"), nil
			},
		}

		ref, err := mock.StoreMedia(ctx, content, &metadata)
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, storage.Reference("ref-123"), ref)
	})

	t.Run("RetrieveMedia interface", func(t *testing.T) {
		called := false
		mock := &mockMediaStorageService{
			retrieveFunc: func(ctx context.Context, reference storage.Reference) (*types.MediaContent, error) {
				called = true
				assert.Equal(t, storage.Reference("ref-123"), reference)
				return content, nil
			},
		}

		retrieved, err := mock.RetrieveMedia(ctx, storage.Reference("ref-123"))
		assert.NoError(t, err)
		assert.True(t, called)
		assert.NotNil(t, retrieved)
	})

	t.Run("DeleteMedia interface", func(t *testing.T) {
		called := false
		mock := &mockMediaStorageService{
			deleteFunc: func(ctx context.Context, reference storage.Reference) error {
				called = true
				assert.Equal(t, storage.Reference("ref-123"), reference)
				return nil
			},
		}

		err := mock.DeleteMedia(ctx, storage.Reference("ref-123"))
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("GetURL interface", func(t *testing.T) {
		called := false
		mock := &mockMediaStorageService{
			getURLFunc: func(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error) {
				called = true
				assert.Equal(t, storage.Reference("ref-123"), reference)
				assert.Equal(t, 1*time.Hour, expiry)
				return "file:///path/to/media.jpg", nil
			},
		}

		url, err := mock.GetURL(ctx, storage.Reference("ref-123"), 1*time.Hour)
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, "file:///path/to/media.jpg", url)
	})
}

// mockPolicyHandler is a mock implementation for testing
type mockPolicyHandler struct {
	applyFunc   func(ctx context.Context, filePath string, policyName string) error
	enforceFunc func(ctx context.Context) error
}

func (m *mockPolicyHandler) ApplyPolicy(ctx context.Context, filePath string, policyName string) error {
	if m.applyFunc != nil {
		return m.applyFunc(ctx, filePath, policyName)
	}
	return nil
}

func (m *mockPolicyHandler) EnforcePolicy(ctx context.Context) error {
	if m.enforceFunc != nil {
		return m.enforceFunc(ctx)
	}
	return nil
}

func TestPolicyHandlerInterface(t *testing.T) {
	ctx := context.Background()

	t.Run("ApplyPolicy interface", func(t *testing.T) {
		called := false
		mock := &mockPolicyHandler{
			applyFunc: func(ctx context.Context, filePath string, policyName string) error {
				called = true
				assert.Equal(t, "/path/to/media.jpg", filePath)
				assert.Equal(t, "delete-after-10min", policyName)
				return nil
			},
		}

		err := mock.ApplyPolicy(ctx, "/path/to/media.jpg", "delete-after-10min")
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("EnforcePolicy interface", func(t *testing.T) {
		called := false
		mock := &mockPolicyHandler{
			enforceFunc: func(ctx context.Context) error {
				called = true
				return nil
			},
		}

		err := mock.EnforcePolicy(ctx)
		assert.NoError(t, err)
		assert.True(t, called)
	})
}
