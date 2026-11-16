package integration

import (
	"context"
	"encoding/json"
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
