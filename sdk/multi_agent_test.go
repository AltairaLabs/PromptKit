package sdk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
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

func TestMultiAgentSession_Members_ReturnsCopy(t *testing.T) {
	conv1 := &Conversation{config: &config{}}
	conv2 := &Conversation{config: &config{}}
	session := &MultiAgentSession{
		entry: &Conversation{config: &config{}},
		members: map[string]Agent{
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
		members: map[string]Agent{},
	}
	assert.Equal(t, entry, session.Entry())
}

func TestMultiAgentSession_Close_EntryError(t *testing.T) {
	// Use closeErrorCapability (defined in conversation_test.go) to make Close() return an error.
	entryErr := errors.New("entry close failed")
	entry := &Conversation{
		config:       &config{},
		capabilities: []Capability{&closeErrorCapability{name: "failing", closeErr: entryErr}},
	}
	session := &MultiAgentSession{
		entry:   entry,
		members: map[string]Agent{},
	}
	err := session.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, entryErr)
}

func TestMultiAgentSession_Close_MemberError(t *testing.T) {
	memberErr := errors.New("member close failed")
	entry := &Conversation{config: &config{}}
	m1 := &Conversation{
		config:       &config{},
		capabilities: []Capability{&closeErrorCapability{name: "failing", closeErr: memberErr}},
	}
	session := &MultiAgentSession{
		entry:   entry,
		members: map[string]Agent{"m1": m1},
	}
	err := session.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, memberErr)
}

func TestMultiAgentSession_Close_EntryAndMemberErrors(t *testing.T) {
	entryErr := errors.New("entry error")
	memberErr := errors.New("member error")
	entry := &Conversation{
		config:       &config{},
		capabilities: []Capability{&closeErrorCapability{name: "e", closeErr: entryErr}},
	}
	m1 := &Conversation{
		config:       &config{},
		capabilities: []Capability{&closeErrorCapability{name: "m", closeErr: memberErr}},
	}
	session := &MultiAgentSession{
		entry:   entry,
		members: map[string]Agent{"m1": m1},
	}
	err := session.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, entryErr)
	assert.ErrorIs(t, err, memberErr)
}

// Close on an opened multi-agent session closes the entry and every member,
// workflow-backed ones included: none of them takes another turn, and closing
// again is harmless.
func TestMultiAgentSession_Close_ClosesEntryAndMembers(t *testing.T) {
	sess, err := OpenMultiAgent(createTestPackFile(t, stateBackedAgentPackJSON),
		WithSkipSchemaValidation(), WithProvider(mock.NewProvider("m", "m", false)))
	require.NoError(t, err)
	ctx := context.Background()

	// Open: the entry answers.
	_, err = sess.Entry().Send(ctx, "hi")
	require.NoError(t, err)
	require.Len(t, sess.Members(), 1, "members are the non-entry agents")

	require.NoError(t, sess.Close())
	_, err = sess.Entry().Send(ctx, "hi")
	assert.ErrorIs(t, err, ErrConversationClosed)
	for name, m := range sess.Members() {
		_, err := m.Send(ctx, "hi")
		assert.ErrorIs(t, err, ErrWorkflowClosed, "state-backed member %q must be closed", name)
	}
	assert.NoError(t, sess.Close(), "a second Close is harmless")
}

// Complete on an opened multi-agent session completes the entry and every
// member, workflow-backed ones included: each that said something runs its
// conversation evals once. Close runs none, since a closed session may resume.
func TestMultiAgentSession_Complete_CompletesEntryAndMembers(t *testing.T) {
	packJSON := strings.Replace(stateBackedAgentPackJSON, `"prompts": {`,
		`"evals": [{"id": "conv", "type": "conv_stub", "trigger": "on_conversation_complete"}],
	"prompts": {`, 1)
	for name, tc := range map[string]struct {
		finish func(*MultiAgentSession) error
		want   int
	}{
		"close":    {(*MultiAgentSession).Close, 0},
		"complete": {(*MultiAgentSession).Complete, 2},
	} {
		t.Run(name, func(t *testing.T) {
			registry := evals.NewEvalTypeRegistry()
			registry.Register(&stepRecordingHandler{typeName: "conv_stub"})
			log := &completeEvalLog{}
			sess, err := OpenMultiAgent(createTestPackFile(t, packJSON),
				WithSkipSchemaValidation(), WithProvider(mock.NewProvider("m", "m", false)),
				WithEvalRegistry(registry), WithEvalHook(log))
			require.NoError(t, err)
			ctx := context.Background()
			_, err = sess.Entry().Send(ctx, "hi")
			require.NoError(t, err)
			require.Len(t, sess.Members(), 1)
			for _, m := range sess.Members() {
				_, err = m.Send(ctx, "look into it")
				require.NoError(t, err)
			}

			require.NoError(t, tc.finish(sess))
			assert.Equal(t, tc.want, log.counts()["conv"], "conversation evals run once per agent on Complete only")
			err = sess.Complete()
			assert.ErrorIs(t, err, ErrConversationClosed, "a finished session's entry cannot complete again")
			assert.ErrorIs(t, err, ErrWorkflowClosed, "nor its workflow-backed members")
			assert.Equal(t, tc.want, log.counts()["conv"], "the refused Complete ran nothing")
		})
	}
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
