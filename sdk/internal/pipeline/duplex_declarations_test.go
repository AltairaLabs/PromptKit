package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
)

// syncPromptHook records the prompt declaration of each BeforeCall; the duplex
// stage calls it from its input goroutine.
type syncPromptHook struct {
	mu      sync.Mutex
	prompts []*packspec.Prompt
}

func (*syncPromptHook) Name() string { return "sync-prompt-hook" }

func (h *syncPromptHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prompts = append(h.prompts, req.Prompt)
	return hooks.Allow
}

func (*syncPromptHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func (h *syncPromptHook) snapshot() []*packspec.Prompt {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*packspec.Prompt(nil), h.prompts...)
}

// The duplex (ASM) topology hands its provider hooks the pack's prompt
// declaration (RFC 0016), as the text and VAD topologies do. Without it a
// governance hook keyed on the prompt sees nil on every realtime turn (#2574).
func TestDuplexPipeline_ProviderHooksSeeThePromptDeclaration(t *testing.T) {
	decl := &packspec.Prompt{ID: "chat", Description: "the chat prompt"}
	hook := &syncPromptHook{}
	cfg := &Config{
		StreamInputProvider: mock.NewStreamingProvider("p", "m", false).WithAutoRespond("hi"),
		StreamInputConfig:   &providers.StreamingInputConfig{},
		HookRegistry:        hooks.NewRegistry(hooks.WithProviderHook(hook)),
		PromptDeclarations:  map[string]*packspec.Prompt{"chat": decl},
	}
	turnState := stage.NewTurnState()
	turnState.Template = &prompt.Template{TaskType: "chat"}

	stages, err := buildProviderStages(cfg, turnState)
	require.NoError(t, err)
	require.Len(t, stages, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan stage.StreamElement, 2)
	output := make(chan stage.StreamElement, 32)
	input <- stage.StreamElement{Text: testutil.Ptr("say something")}
	done := make(chan error, 1)
	go func() { done <- stages[0].Process(ctx, input, output) }()

	require.Eventually(t, func() bool { return len(hook.snapshot()) > 0 }, 2*time.Second, time.Millisecond)
	cancel()
	close(input)
	<-done

	assert.Same(t, decl, hook.snapshot()[0], "the realtime turn's hook must see the pack's prompt declaration")
}
