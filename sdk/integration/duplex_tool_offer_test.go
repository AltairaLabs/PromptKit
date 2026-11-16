package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// A realtime session is offered the tools a text turn would be: the prompt's,
// and the capability tools such as memory's (#2697), merged with the host's
// session tools.

// sessionCreatedWith opens the realtime session by sending text and returns
// the config the provider created it with.
func sessionCreatedWith(
	t *testing.T, conv *sdk.Conversation, provider *toolAwareStreamingProvider,
) providers.StreamingInputConfig {
	t.Helper()
	drainDuplexResponse(t, conv)
	require.NoError(t, conv.SendText(context.Background(), "hello"))
	require.Eventually(t, func() bool { return len(provider.createdWith()) == 1 },
		2*time.Second, 5*time.Millisecond, "the session was never created")
	return provider.createdWith()[0]
}

// toolDefByName returns the definition of name in defs.
func toolDefByName(t *testing.T, defs []providers.StreamingToolDefinition, name string) providers.StreamingToolDefinition {
	t.Helper()
	for i := range defs {
		if defs[i].Name == name {
			return defs[i]
		}
	}
	t.Fatalf("tool %q was not offered", name)
	return providers.StreamingToolDefinition{}
}

func jsonObject(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(s), &m))
	return m
}

func TestDuplexToolOffer_MemoryToolsAreOfferedToTheSession(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON, nil, "",
		sdk.WithMemory(memory.NewInMemoryStore(), map[string]string{"user_id": "alice"}))

	created := sessionCreatedWith(t, conv, provider)

	names := make([]string, 0, len(created.Tools))
	for i := range created.Tools {
		names = append(names, created.Tools[i].Name)
	}
	assert.Equal(t,
		[]string{"get_location", "memory__forget", "memory__list", "memory__recall", "memory__remember"}, names)
	assert.Equal(t, providers.StreamingToolDefinition{
		Name: "memory__recall",
		Description: "Search your memories for relevant information. " +
			"Use this to recall facts, preferences, or context from previous conversations.",
		Parameters: jsonObject(t, `{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "What to search for in memory."},
				"types": {"type": "array", "items": {"type": "string"},
					"description": "Filter by memory type (e.g., 'preference', 'episodic')."},
				"limit": {"type": "integer", "description": "Maximum number of results."},
				"min_confidence": {"type": "number", "description": "Minimum confidence threshold (0.0-1.0)."}
			},
			"required": ["query"]
		}`),
	}, toolDefByName(t, created.Tools, "memory__recall"))

	remember := toolDefByName(t, created.Tools, "memory__remember")
	assert.Equal(t, "Store something in memory for future conversations. "+
		"Use this to remember user preferences, important facts, or decisions.", remember.Description)
	assert.Equal(t, "object", remember.Parameters["type"])
	assert.Equal(t, []interface{}{"content"}, remember.Parameters["required"])
	props := remember.Parameters["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"type": "string", "description": "What to remember (natural language)."},
		props["content"])
	assert.Equal(t, map[string]interface{}{"type": "object",
		"description": "Optional structured data to attach to the memory."}, props["metadata"])
	assert.Len(t, props, 5)
}

// A host session tool named like a conversation tool replaces it; a host tool
// the conversation lacks is offered too.
func TestDuplexToolOffer_HostToolWinsOnAClashAndHostOnlyToolIsKept(t *testing.T) {
	hostLocation := providers.StreamingToolDefinition{
		Name: "get_location", Description: "Host location",
		Parameters: map[string]interface{}{"type": "object"},
	}
	cfg := hostToolStreamingConfig()
	cfg.Tools = append([]providers.StreamingToolDefinition{hostLocation}, cfg.Tools...)
	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON, nil, "", sdk.WithStreamingConfig(cfg))

	created := sessionCreatedWith(t, conv, provider)

	assert.Equal(t, []providers.StreamingToolDefinition{hostLocation, hostToolStreamingConfig().Tools[0]},
		created.Tools)
}

// The pack's own tool is offered with its declared schema.
func TestDuplexToolOffer_PackToolIsOfferedWithItsSchema(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON, nil, "")

	created := sessionCreatedWith(t, conv, provider)

	assert.Equal(t, []providers.StreamingToolDefinition{{
		Name:        "get_location",
		Description: "Get the user's GPS location",
		Parameters: jsonObject(t, `{"type": "object",
			"properties": {"accuracy": {"type": "string"}}, "required": ["accuracy"]}`),
	}}, created.Tools)
	assert.Equal(t, types.ContentTypeAudio, created.Config.Type)
}

const duplexRequiredToolPackJSON = `{
	"id": "integration-test-duplex-required-tool",
	"version": "1.0.0",
	"description": "Pack whose prompt requires a tool call",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You look up orders.",
			"tools": ["lookup_order"],
			"tool_policy": {"tool_choice": "required"}
		}
	},
	"tools": {
		"lookup_order": {
			"name": "lookup_order",
			"description": "Look up an order",
			"parameters": {"type": "object", "properties": {"order_id": {"type": "string"}}}
		}
	}
}`

// A prompt's tool_choice "required" applies to a text turn's first round
// only. A realtime session is not sent it, which would force a tool call
// after every tool result: the reply after the tool's result ends the turn.
func TestDuplexToolOffer_PromptRequiredToolChoiceIsNotSentToTheSession(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexRequiredToolPackJSON,
		[]types.MessageToolCall{lookupOrderCall("call_1")}, "It has shipped")
	conv.OnTool("lookup_order", func(map[string]any) (any, error) { return "shipped", nil })

	got, ended := firstToolResponsesAndEnd(t, conv, provider, "where is my order")
	awaitEnd(t, ended)

	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "call_1", Result: "shipped"}}, got)
	created := provider.createdWith()
	require.Len(t, created, 1)
	assert.Empty(t, created[0].ToolChoice)
	assert.Equal(t, "lookup_order", created[0].Tools[0].Name)
	assert.Len(t, provider.getSessions()[0].receivedResponses, 1, "the session was answered once")
}

// updatingToolSession is a toolAwareMockSession that can change its tools
// mid-session (providers.StreamToolUpdater), recording each update.
type updatingToolSession struct {
	*toolAwareMockSession
	updatesMu sync.Mutex
	updates   [][]string
}

func (s *updatingToolSession) UpdateTools(_ context.Context, defs []providers.StreamingToolDefinition) error {
	names := make([]string, 0, len(defs))
	for i := range defs {
		names = append(names, defs[i].Name)
	}
	s.updatesMu.Lock()
	s.updates = append(s.updates, names)
	s.updatesMu.Unlock()
	return nil
}

func (s *updatingToolSession) toolUpdates() [][]string {
	s.updatesMu.Lock()
	defer s.updatesMu.Unlock()
	return append([][]string(nil), s.updates...)
}

// updatingToolProvider creates one updatingToolSession.
type updatingToolProvider struct {
	*toolAwareStreamingProvider
	sessionMu sync.Mutex
	session   *updatingToolSession
}

func (p *updatingToolProvider) created() *updatingToolSession {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	return p.session
}

func (p *updatingToolProvider) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	inner, err := p.toolAwareStreamingProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, err
	}
	sess := &updatingToolSession{toolAwareMockSession: inner.(*toolAwareMockSession)}
	p.sessionMu.Lock()
	p.session = sess
	p.sessionMu.Unlock()
	return sess, nil
}

const duplexSkillGrantPackJSON = `{
	"id": "integration-test-duplex-skill-grant",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a support agent.",
			"tools": ["lookup_order"]
		}
	},
	"tools": {
		"lookup_order": {
			"name": "lookup_order",
			"description": "Look up an order",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}
		},
		"refund": {
			"name": "refund",
			"description": "Issue a refund",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}
		}
	}
}`

func writeRefundSkillDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "refund-processing")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := "---\nname: refund-processing\ndescription: How to process refunds\n" +
		"allowed-tools:\n  - refund\n---\n\nCall refund with the order id.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600))
	return root
}

// A skill the realtime model activates grants its tools to this
// conversation: the session is given them before the activation's result.
// The session's tool calls run on the session's context, which must carry the
// conversation's active skills, or the activation lands on a set the
// conversation never reads and nothing is granted.
func TestDuplexToolOffer_SkillActivationGivesTheSessionTheGrantedTool(t *testing.T) {
	provider := &updatingToolProvider{toolAwareStreamingProvider: newToolAwareStreamingProvider(
		[]types.MessageToolCall{{ID: "call_1", Name: "skill__activate",
			Args: json.RawMessage(`{"name":"refund-processing"}`)}}, "activated")}
	conv, err := sdk.OpenDuplex(writePackFile(t, duplexSkillGrantPackJSON), "chat",
		sdk.WithProvider(provider), sdk.WithSkipSchemaValidation(),
		sdk.WithSkillsDir(writeRefundSkillDir(t)),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	drainDuplexResponse(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "refund ORD-1"))
	require.Eventually(t, func() bool {
		sess := provider.created()
		return sess != nil && sess.toolResponsesReceived.Load() >= 1
	}, 3*time.Second, 5*time.Millisecond, "the activation was never answered")

	created := provider.createdWith()
	require.Len(t, created, 1)
	assert.NotContains(t, toolDefNamesOf(created[0].Tools), "refund", "refund is offered only once granted")
	assert.Equal(t, [][]string{{"lookup_order", "refund", "skill__activate", "skill__deactivate",
		"skill__read_resource"}}, provider.created().toolUpdates())
}

func toolDefNamesOf(defs []providers.StreamingToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for i := range defs {
		names = append(names, defs[i].Name)
	}
	return names
}
