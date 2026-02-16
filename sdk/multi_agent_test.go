package sdk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenMultiAgent_NonExistentPack(t *testing.T) {
	_, err := OpenMultiAgent("/nonexistent/path/to/pack.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve pack path")
}

func TestOpenMultiAgent_NoAgentsSection(t *testing.T) {
	// Create a minimal pack file without agents section
	packPath := createTestPackFile(t, `{
		"schema_version": "2025.1",
		"id": "test-pack",
		"version": "1.0.0",
		"prompts": {
			"chat": {
				"name": "chat",
				"messages": [{"role": "system", "content": "You are helpful."}]
			}
		}
	}`)

	_, err := OpenMultiAgent(packPath, WithSkipSchemaValidation())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pack has no agents section")
}

func TestOpenMultiAgent_NoEntryDefined(t *testing.T) {
	packPath := createTestPackFile(t, `{
		"schema_version": "2025.1",
		"id": "test-pack",
		"version": "1.0.0",
		"prompts": {
			"chat": {
				"name": "chat",
				"messages": [{"role": "system", "content": "You are helpful."}]
			}
		},
		"agents": {
			"entry": "",
			"members": {
				"chat": {
					"description": "A chat agent"
				}
			}
		}
	}`)

	_, err := OpenMultiAgent(packPath, WithSkipSchemaValidation())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entry is required")
}

func TestMultiAgentSession_Close_NilMembers(t *testing.T) {
	// Test that Close handles empty members map gracefully
	session := &MultiAgentSession{
		entry:   &Conversation{config: &config{}},
		members: map[string]*Conversation{},
	}
	// Close on a zero-value Conversation will just set closed=true
	err := session.Close()
	assert.NoError(t, err)
}

func TestMultiAgentSession_Members_ReturnsCopy(t *testing.T) {
	conv1 := &Conversation{config: &config{}}
	conv2 := &Conversation{config: &config{}}
	session := &MultiAgentSession{
		entry: &Conversation{config: &config{}},
		members: map[string]*Conversation{
			"a": conv1,
			"b": conv2,
		},
	}

	members := session.Members()
	assert.Len(t, members, 2)
	assert.Equal(t, conv1, members["a"])
	assert.Equal(t, conv2, members["b"])

	// Mutating the returned map should not affect the session
	delete(members, "a")
	assert.Len(t, session.members, 2)
}

func TestMultiAgentSession_Entry(t *testing.T) {
	entry := &Conversation{config: &config{}}
	session := &MultiAgentSession{
		entry:   entry,
		members: map[string]*Conversation{},
	}
	assert.Equal(t, entry, session.Entry())
}

// createTestPackFile creates a temporary pack JSON file for testing.
func createTestPackFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pack.json")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err, "failed to create test pack file")
	return path
}
