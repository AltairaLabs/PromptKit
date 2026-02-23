package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/generate"
)

// sessionRecordingFixture creates a minimal .recording.json file for testing.
func sessionRecordingFixture(t *testing.T, dir, name string) string {
	t.Helper()

	data := map[string]interface{}{
		"metadata": map[string]interface{}{
			"session_id":  "sess-" + name,
			"provider_id": "test-provider",
			"tags":        []string{"test"},
		},
		"events": []map[string]interface{}{
			{
				"type":      "message",
				"timestamp": time.Now().Format(time.RFC3339),
				"message": map[string]interface{}{
					"role":    "user",
					"content": "Hello from " + name,
				},
			},
			{
				"type":      "message",
				"timestamp": time.Now().Format(time.RFC3339),
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hi back from " + name,
				},
			},
		},
	}

	content, err := json.Marshal(data)
	require.NoError(t, err)

	path := filepath.Join(dir, name+".recording.json")
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

func TestRecordingsAdapter_Name(t *testing.T) {
	adapter := NewRecordingsAdapter("*.recording.json")
	assert.Equal(t, "recordings", adapter.Name())
}

func TestRecordingsAdapter_ListFromFixtures(t *testing.T) {
	dir := t.TempDir()
	sessionRecordingFixture(t, dir, "one")
	sessionRecordingFixture(t, dir, "two")

	adapter := NewRecordingsAdapter(filepath.Join(dir, "*.recording.json"))
	summaries, err := adapter.List(context.Background(), generate.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
}

func TestRecordingsAdapter_ListWithLimit(t *testing.T) {
	dir := t.TempDir()
	sessionRecordingFixture(t, dir, "one")
	sessionRecordingFixture(t, dir, "two")
	sessionRecordingFixture(t, dir, "three")

	adapter := NewRecordingsAdapter(filepath.Join(dir, "*.recording.json"))
	summaries, err := adapter.List(context.Background(), generate.ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
}

func TestRecordingsAdapter_GetSession(t *testing.T) {
	dir := t.TempDir()
	path := sessionRecordingFixture(t, dir, "test")

	adapter := NewRecordingsAdapter(filepath.Join(dir, "*.recording.json"))
	detail, err := adapter.Get(context.Background(), path)
	require.NoError(t, err)

	assert.Equal(t, "sess-test", detail.ID)
	assert.Len(t, detail.Messages, 2)
	assert.Equal(t, "user", detail.Messages[0].Role)
	assert.Nil(t, detail.EvalResults, "recordings should not have eval results")
}

func TestRecordingsAdapter_GetMissingFile(t *testing.T) {
	adapter := NewRecordingsAdapter("*.recording.json")
	_, err := adapter.Get(context.Background(), "/nonexistent/file.recording.json")
	require.Error(t, err)
}
