package sdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// declarationsPackJSON declares two of everything a host-facing seam can carry
// — tools, prompts, validators, agents — each with a distinguishing extension,
// so a test can tell which declaration a seam received.
const declarationsPackJSON = `{
  "id": "decl", "name": "Decl", "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "tools": {
    "lookup": {"name": "lookup", "description": "look up",
      "parameters": {"type": "object", "properties": {}},
      "action_scope": {"effect": "read"}, "extensions": {"acme:tier": "read"}},
    "refund": {"name": "refund", "description": "refund",
      "parameters": {"type": "object", "properties": {}},
      "action_scope": {"effect": "external"}, "extensions": {"acme:tier": "money"}}
  },
  "prompts": {
    "chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
      "system_template": "You chat.", "tools": ["lookup", "refund"],
      "extensions": {"acme:prompt": "chat"},
      "validators": [
        {"id": "no-cards", "type": "banned_words", "params": {"words": ["4111"]},
          "extensions": {"acme:control": "PCI"}},
        {"id": "short", "type": "max_length", "params": {"max_characters": 5000},
          "extensions": {"acme:control": "UX"}}]},
    "billing": {"id": "billing", "name": "Billing", "version": "1.0.0",
      "system_template": "You bill.", "extensions": {"acme:prompt": "billing"}}
  },
  "agents": {"entry": "chat", "members": {
    "chat": {"description": "chat agent", "extensions": {"acme:approver": "chat-oncall"}},
    "billing": {"description": "billing agent", "extensions": {"acme:approver": "billing-oncall"}}}}
}`

// toolCallingProvider calls lookup and refund on round 1, then answers.
type toolCallingProvider struct {
	*mock.ToolProvider
	mu     sync.Mutex
	rounds int
}

func (p *toolCallingProvider) SupportsStreaming() bool { return false }

func (p *toolCallingProvider) PredictWithTools(
	context.Context, providers.PredictionRequest, providers.ProviderTools, string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	if round > 1 {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	calls := []types.MessageToolCall{
		{ID: "c1", Name: "lookup", Args: []byte(`{}`)},
		{ID: "c2", Name: "refund", Args: []byte(`{}`)},
	}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

// recordingExecutor records the declaration on each descriptor it executes.
type recordingExecutor struct {
	mu   sync.Mutex
	seen map[string]*packspec.Tool
}

func (e *recordingExecutor) Name() string { return "recording" }

func (e *recordingExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seen[d.Name] = d.Declaration
	return json.RawMessage(`{"ok":true}`), nil
}

// recordingHooks is a tool hook and a provider hook recording declarations.
type recordingHooks struct {
	mu      sync.Mutex
	tools   map[string]*packspec.Tool
	prompts []*packspec.Prompt
}

func (h *recordingHooks) Name() string { return "recording" }

func (h *recordingHooks) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tools[req.Name] = req.Declaration
	return hooks.Allow
}

func (h *recordingHooks) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (h *recordingHooks) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prompts = append(h.prompts, req.Prompt)
	return hooks.Allow
}

func (h *recordingHooks) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func openDeclarations(t *testing.T, promptName string, opts ...Option) *Conversation {
	t.Helper()
	path := createTestPackFile(t, declarationsPackJSON)
	prov := &toolCallingProvider{ToolProvider: mock.NewToolProvider("m", "m", false, nil)}
	conv, err := Open(path, promptName, append([]Option{WithProvider(prov)}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// TestToolExecutorAndHookReceiveThisToolsDeclaration runs one turn calling
// both pack tools. The executor and the tool hook each see, per call, the
// declaration of the tool being called — and changing it does not reach the
// loaded pack.
func TestToolExecutorAndHookReceiveThisToolsDeclaration(t *testing.T) {
	rec := &recordingHooks{tools: map[string]*packspec.Tool{}}
	conv := openDeclarations(t, "chat", WithToolHook(rec))
	exec := &recordingExecutor{seen: map[string]*packspec.Tool{}}
	conv.OnToolExecutor("lookup", exec)
	conv.OnToolExecutor("refund", exec)

	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	for _, seen := range []map[string]*packspec.Tool{exec.seen, rec.tools} {
		require.Equal(t, "read", seen["lookup"].Extensions["acme:tier"])
		require.Equal(t, "read", seen["lookup"].ActionScope.Effect)
		require.Equal(t, "money", seen["refund"].Extensions["acme:tier"])
		require.Equal(t, "external", seen["refund"].ActionScope.Effect)
	}

	require.Same(t, rec.tools["refund"], exec.seen["refund"],
		"the executor receives the same declaration the tool hook saw")

	exec.seen["refund"].Extensions["acme:tier"] = "changed"
	rec.tools["refund"].Extensions["acme:tier"] = "changed"
	require.Equal(t, "money", conv.pack.Tools["refund"].Extensions["acme:tier"],
		"a host changing its declaration must not change the loaded pack")
}

// TestProviderHookReceivesThisConversationsPrompt — the pack declares two
// prompts; a conversation opened on billing must hand its provider hooks the
// billing declaration, as a copy.
func TestProviderHookReceivesThisConversationsPrompt(t *testing.T) {
	rec := &recordingHooks{tools: map[string]*packspec.Tool{}}
	conv := openDeclarations(t, "billing", WithProviderHook(rec))

	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.NotEmpty(t, rec.prompts)
	got := rec.prompts[0]
	require.Equal(t, "billing", got.ID)
	require.Equal(t, map[string]any{"acme:prompt": "billing"}, got.Extensions)

	got.Extensions["acme:prompt"] = "changed"
	require.Equal(t, "billing", conv.pack.Prompts["billing"].Extensions["acme:prompt"])
}

// TestValidationEventsCarryTheirValidatorsDeclaration — each of the two pack
// validators reports its own declaration on its validation events.
func TestValidationEventsCarryTheirValidatorsDeclaration(t *testing.T) {
	bus := events.NewEventBus()
	var mu sync.Mutex
	byType := map[string]*packspec.Validator{}
	bus.Subscribe(events.EventValidationStarted, func(e *events.Event) {
		data := e.Data.(*events.ValidationEventData)
		mu.Lock()
		defer mu.Unlock()
		byType[data.ValidatorType] = data.Validator
	})
	conv := openDeclarations(t, "chat", WithEventBus(bus))
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "ok", nil })
	conv.OnTool("refund", func(map[string]any) (any, error) { return "ok", nil })

	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(byType) == 2
	}, 5*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "no-cards", byType["banned_words"].ID)
	require.Equal(t, map[string]any{"acme:control": "PCI"}, byType["banned_words"].Extensions)
	require.Equal(t, "short", byType["max_length"].ID)
	require.Equal(t, map[string]any{"acme:control": "UX"}, byType["max_length"].Extensions)
}

// TestToolRepositoryDescriptorsCarryACopyOfTheDeclaration — the registry path
// (WithToolExecutor and mode-based executors) reads descriptors built here.
func TestToolRepositoryDescriptorsCarryACopyOfTheDeclaration(t *testing.T) {
	var p pack.Pack
	require.NoError(t, json.Unmarshal([]byte(declarationsPackJSON), &p))

	repo := pack.ToToolRepository(&p)
	lookup, err := repo.LoadTool("lookup")
	require.NoError(t, err)
	refund, err := repo.LoadTool("refund")
	require.NoError(t, err)

	require.Equal(t, "read", lookup.Declaration.Extensions["acme:tier"])
	require.Equal(t, "money", refund.Declaration.Extensions["acme:tier"])

	refund.Declaration.Extensions["acme:tier"] = "changed"
	require.Equal(t, "money", p.Tools["refund"].Extensions["acme:tier"])
}

// TestA2AToolsCarryTheCalleeMembersDefinition — each resolved agent tool
// carries the definition of the member it calls, not another member's.
func TestA2AToolsCarryTheCalleeMembersDefinition(t *testing.T) {
	var p pack.Pack
	require.NoError(t, json.Unmarshal([]byte(declarationsPackJSON), &p))

	r := NewAgentToolResolver(&p)
	require.NotNil(t, r)
	descs := r.ResolveAgentTools([]string{"chat", "billing"})

	byName := map[string]*packspec.AgentDef{}
	for _, d := range descs {
		byName[d.Name] = d.Agent
	}
	require.Equal(t, "chat-oncall", byName[tools.QualifyToolName(nsA2A, "chat")].Extensions["acme:approver"])
	require.Equal(t, "billing-oncall", byName[tools.QualifyToolName(nsA2A, "billing")].Extensions["acme:approver"])

	byName[tools.QualifyToolName(nsA2A, "billing")].Extensions["acme:approver"] = "changed"
	require.Equal(t, "billing-oncall", p.Agents.Members["billing"].Extensions["acme:approver"])
}

// TestForkCopiesToolDeclarations — a fork's registry holds its own copies, so
// a hook or executor in the fork cannot change what the parent's see.
func TestForkCopiesToolDeclarations(t *testing.T) {
	conv := openDeclarations(t, "chat")
	fork, err := conv.Fork()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fork.Close() })

	parent := conv.toolRegistry.Get("refund").Declaration
	forked := fork.toolRegistry.Get("refund").Declaration
	require.NotNil(t, forked)
	require.NotSame(t, parent, forked)

	forked.Extensions["acme:tier"] = "changed"
	require.Equal(t, "money", parent.Extensions["acme:tier"])
}
