package storage_test

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/stretchr/testify/assert"
)

func TestMediaMetadata(t *testing.T) {
	t.Run("complete metadata", func(t *testing.T) {
		now := time.Now()
		metadata := storage.MediaMetadata{
			RunID:          "run-123",
			ConversationID: "conv-456",
			SessionID:      "session-789",
			MessageIdx:     5,
			PartIdx:        2,
			MIMEType:       "image/jpeg",
			SizeBytes:      102400,
			ProviderID:     "openai",
			Timestamp:      now,
			PolicyName:     "delete-after-10min",
		}

		assert.Equal(t, "run-123", metadata.RunID)
		assert.Equal(t, "conv-456", metadata.ConversationID)
		assert.Equal(t, "session-789", metadata.SessionID)
		assert.Equal(t, 5, metadata.MessageIdx)
		assert.Equal(t, 2, metadata.PartIdx)
		assert.Equal(t, "image/jpeg", metadata.MIMEType)
		assert.Equal(t, int64(102400), metadata.SizeBytes)
		assert.Equal(t, "openai", metadata.ProviderID)
		assert.Equal(t, now, metadata.Timestamp)
		assert.Equal(t, "delete-after-10min", metadata.PolicyName)
	})

	t.Run("minimal metadata", func(t *testing.T) {
		metadata := storage.MediaMetadata{
			RunID:      "run-123",
			MessageIdx: 0,
			PartIdx:    0,
			MIMEType:   "audio/mp3",
			SizeBytes:  204800,
			Timestamp:  time.Now(),
		}

		assert.Equal(t, "run-123", metadata.RunID)
		assert.Empty(t, metadata.ConversationID)
		assert.Empty(t, metadata.SessionID)
		assert.Empty(t, metadata.ProviderID)
		assert.Empty(t, metadata.PolicyName)
	})
}

func TestOrganizationMode(t *testing.T) {
	testCases := []struct {
		name     string
		mode     storage.OrganizationMode
		expected string
	}{
		{
			name:     "by-session",
			mode:     storage.OrganizationBySession,
			expected: "by-session",
		},
		{
			name:     "by-conversation",
			mode:     storage.OrganizationByConversation,
			expected: "by-conversation",
		},
		{
			name:     "by-run",
			mode:     storage.OrganizationByRun,
			expected: "by-run",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, string(tc.mode))
		})
	}
}

func TestStorageReference(t *testing.T) {
	t.Run("string conversion", func(t *testing.T) {
		ref := storage.StorageReference("/path/to/media/file.jpg")
		assert.Equal(t, "/path/to/media/file.jpg", string(ref))
	})

	t.Run("empty reference", func(t *testing.T) {
		ref := storage.StorageReference("")
		assert.Empty(t, string(ref))
	})
}
