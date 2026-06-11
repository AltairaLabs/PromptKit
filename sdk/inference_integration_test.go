package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/require"
)

// probeToolCallRepo is a minimal ResponseRepository that:
//   - turn 1: returns a tool_calls response invoking "probe"
//   - turn 2+: returns a plain text response to terminate the loop
type probeToolCallRepo struct{}

func (probeToolCallRepo) GetResponse(_ context.Context, params mock.ResponseParams) (string, error) {
	t, err := probeToolCallRepo{}.GetTurn(context.Background(), params)
	if err != nil {
		return "", err
	}
	return t.Content, nil
}

func (probeToolCallRepo) GetTurn(_ context.Context, params mock.ResponseParams) (*mock.Turn, error) {
	if params.TurnNumber <= 1 {
		return &mock.Turn{
			Type:    "tool_calls",
			Content: "",
			ToolCalls: []mock.ToolCall{
				{Name: "probe", Arguments: map[string]interface{}{}},
			},
		}, nil
	}
	return &mock.Turn{Type: "text", Content: "done"}, nil
}

// TestInferenceRegistry_VisibleDuringSend proves the full path:
//
//	WithClassifier("stub", stubText{})
//	  → config.classifyRegistry
//	  → intpipeline.Config.ClassifyRegistry
//	  → PipelineConfig.ClassifyRegistry
//	  → classify.WithRegistry(execCtx, registry) in StreamPipeline.Execute
//	  → classify.FromContext(ctx) resolves inside OnToolCtx during Send.
//
// The probe tool is called by the mock provider on turn 1, and the OnToolCtx
// handler captures the classify.Registry from context, asserting both that it
// is non-nil and that the "stub" backend registered via WithClassifier resolves.
func TestInferenceRegistry_VisibleDuringSend(t *testing.T) {
	// ---- 1. Build a Conversation with a tool-call-capable mock provider ----
	repo := probeToolCallRepo{}
	mockProv := mock.NewToolProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	// The prompt's Tools list is what PromptAssemblyStage writes into
	// TurnState.AllowedTools; the ProviderStage only sends tools in that
	// list to the provider, so "probe" must appear here.
	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {
				ID:             "chat",
				SystemTemplate: "You are a helpful assistant.",
				Tools:          []string{"probe"},
			},
		},
	}

	// Apply WithClassifier to wire the classify registry onto the config.
	cfg := &config{}
	require.NoError(t, WithClassifier("stub", stubText{})(cfg))
	cfg.provider = mockProv

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         cfg,
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	// ---- 2. Register the "probe" tool descriptor (mode: local → localExecutor) ----
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	require.NoError(t, conv.toolRegistry.Register(&tools.ToolDescriptor{
		Name:        "probe",
		Description: "Registry probe",
		InputSchema: schema,
		Mode:        "local",
	}))

	// ---- 3. Register an OnToolCtx handler that captures classify.FromContext ----
	var seen *classify.Registry
	conv.OnToolCtx("probe", func(ctx context.Context, _ map[string]any) (any, error) {
		seen = classify.FromContext(ctx)
		return "ok", nil
	})

	// ---- 4. Build the pipeline and session ----
	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// ---- 5. Drive one turn; mock returns tool_call "probe" on turn 1 ----
	_, err = conv.Send(context.Background(), "go")
	require.NoError(t, err)

	// ---- 6. Assert the registry was visible in the pipeline context ----
	if seen == nil {
		t.Fatal("classify registry not visible via classify.FromContext during Send")
	}
	if _, err := seen.TextClassifier("stub"); err != nil {
		t.Fatalf("registered 'stub' classifier should resolve: %v", err)
	}
}
