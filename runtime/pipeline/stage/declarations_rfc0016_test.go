package stage

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// declarationRecorder is a tool hook and a provider hook that records the
// declaration each request carried.
type declarationRecorder struct {
	mu      sync.Mutex
	tools   map[string]*packspec.Tool
	prompts []string // Prompt.ID per BeforeCall, "" when nil
}

func newDeclarationRecorder() *declarationRecorder {
	return &declarationRecorder{tools: map[string]*packspec.Tool{}}
}

func (r *declarationRecorder) Name() string { return "declaration-recorder" }

func (r *declarationRecorder) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[req.Name] = req.Declaration
	return hooks.Allow
}

func (r *declarationRecorder) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (r *declarationRecorder) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := ""
	if req.Prompt != nil {
		id = req.Prompt.ID
	}
	r.prompts = append(r.prompts, id)
	return hooks.Allow
}

func (r *declarationRecorder) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// callingProvider calls each named tool on round 1, then answers plainly.
type callingProvider struct {
	*mock.ToolProvider
	calls  []string
	rounds int
}

func (p *callingProvider) SupportsStreaming() bool { return false }

func (p *callingProvider) PredictWithTools(
	context.Context, providers.PredictionRequest, providers.ProviderTools, string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.rounds++
	if p.rounds > 1 {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	calls := make([]types.MessageToolCall, 0, len(p.calls))
	for i, name := range p.calls {
		calls = append(calls, types.MessageToolCall{
			ID: name + "-" + string(rune('a'+i)), Name: name, Args: []byte(`{}`),
		})
	}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

type okExecutor struct{}

func (okExecutor) Name() string { return "local" }
func (okExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

// TestToolHookReceivesTheDeclarationForThisCall — two declared tools and one
// undeclared, called in the same round. Each hook request must carry the
// declaration of the tool it is about, and the undeclared one nil.
func TestToolHookReceivesTheDeclarationForThisCall(t *testing.T) {
	registry := tools.NewRegistry()
	registry.RegisterExecutor(okExecutor{})
	for _, d := range []*tools.ToolDescriptor{
		{Name: "lookup", Mode: "local", InputSchema: []byte(`{"type":"object"}`),
			Declaration: &packspec.Tool{Name: "lookup", Extensions: map[string]any{"acme:tier": "read"}}},
		{Name: "refund", Mode: "local", InputSchema: []byte(`{"type":"object"}`),
			Declaration: &packspec.Tool{Name: "refund", Extensions: map[string]any{"acme:tier": "money"}}},
		{Name: "capability_tool", Mode: "local", InputSchema: []byte(`{"type":"object"}`)},
	} {
		require.NoError(t, registry.Register(d))
	}

	rec := newDeclarationRecorder()
	provider := &callingProvider{
		ToolProvider: mock.NewToolProvider("mock", "m", false, nil),
		calls:        []string{"lookup", "refund", "capability_tool"},
	}
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup", "refund", "capability_tool"}
	stage := NewProviderStageWithTurnState(provider, registry, nil, &ProviderConfig{MaxTokens: 100},
		nil, hooks.NewRegistry(hooks.WithToolHook(rec)), turnState)

	runHandoffTurn(t, stage)

	require.Len(t, rec.tools, 3)
	require.Equal(t, "read", rec.tools["lookup"].Extensions["acme:tier"])
	require.Equal(t, "money", rec.tools["refund"].Extensions["acme:tier"])
	require.Nil(t, rec.tools["capability_tool"], "an undeclared tool must carry nil")
}

// TestProviderHookSeesThePromptActuallyRunning — the turn starts on the
// triage prompt and a workflow handoff moves it to billing mid-turn. The
// provider hook must see each round's own prompt declaration, not the one
// the pipeline was built for.
func TestProviderHookSeesThePromptActuallyRunning(t *testing.T) {
	resolver := &fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN PROMPT", AllowedTools: []string{"workflow__transition"},
			PromptTask: "triage"},
		{Valid: true, SystemPrompt: "DESTINATION PROMPT", AllowedTools: []string{"workflow__transition"},
			PromptTask: "billing"},
	}}

	stage, _, turnState := newHandoffStage(t, resolver)
	turnState.Template = &prompt.Template{TaskType: "triage"}
	rec := newDeclarationRecorder()
	stage.hookRegistry = hooks.NewRegistry(hooks.WithProviderHook(rec))
	stage.config.PromptDeclarations = map[string]*packspec.Prompt{
		"triage":  {ID: "triage"},
		"billing": {ID: "billing"},
	}

	runHandoffTurn(t, stage)

	require.Equal(t, []string{"triage", "billing"}, rec.prompts)
}

// TestProviderHookPromptIsNilWithoutDeclarations — a stage built without
// declarations (no pack) hands hooks nil, never a guessed prompt.
func TestProviderHookPromptIsNilWithoutDeclarations(t *testing.T) {
	stage, _, turnState := newHandoffStage(t, &fakeResolver{sequence: originThenDestination()})
	turnState.Template = &prompt.Template{TaskType: "triage"}
	rec := newDeclarationRecorder()
	stage.hookRegistry = hooks.NewRegistry(hooks.WithProviderHook(rec))

	runHandoffTurn(t, stage)

	require.Equal(t, []string{"", ""}, rec.prompts)
}

// TestValidatorDeclarationReachesTheEventButNotTheMessage — the declaration
// rides on a firing's metadata to reach the validation event, and must not be
// copied into Message.Validations, which is public and persisted.
func TestValidatorDeclarationReachesTheEventButNotTheMessage(t *testing.T) {
	decl := &packspec.Validator{ID: "no-cards", Type: "banned_words"}
	d := hooks.Enforced("blocked", map[string]any{
		"validator_type":                      "banned_words",
		hooks.MetadataKeyValidatorDeclaration: decl,
	})

	v, ok := guardrailValidation(d, hooks.DirectionOutput)

	require.True(t, ok)
	require.NotContains(t, v.Details, hooks.MetadataKeyValidatorDeclaration)
	require.Equal(t, "banned_words", v.Details["validator_type"])
}
