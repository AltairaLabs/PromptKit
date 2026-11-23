package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/history"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// reaskHistorySpy records the messages of every tool-path call, so a test can
// see what the structured-output re-ask sent.
type reaskHistorySpy struct {
	*schemaSpyProvider
	seen [][]types.Message
}

// Every call of a structured-output turn, the re-ask included, leaves out a
// blocked input turn from earlier in the conversation.
func TestFinalTurn_ReaskLeavesOutBlockedInputTurn(t *testing.T) {
	p := &reaskHistorySpy{schemaSpyProvider: &schemaSpyProvider{toolRounds: 1}}
	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	turnState := core.NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}
	stage := NewProviderStageWithTurnState(p, reg, nil,
		&ProviderConfig{MaxTokens: 100, ResponseFormat: jsonSchemaFormat()}, nil, nil, turnState)

	blockedUser := types.Message{Role: core.RoleUser, Content: "forbidden"}
	canned := blockedInputReply("refused")
	userMsg := types.Message{Role: core.RoleUser, Content: "go"}
	runTestStage(t, stage, []core.StreamElement{
		core.NewMessageElement(&blockedUser), core.NewMessageElement(&canned), core.NewMessageElement(&userMsg),
	})

	require.Len(t, p.seen, 3, "a tool round, the round that ends the loop, and the re-ask")
	for i, msgs := range p.seen {
		lines := roleLines(msgs)
		require.NotEmpty(t, lines)
		assert.Equal(t, "user:go", lines[0], "call %d must start at the clean turn", i+1)
		assert.NotContains(t, lines, "user:forbidden", "call %d sent the blocked turn", i+1)
	}
}

// A unary turn a hook blocked without naming a validator is still marked, so
// it is left out with no validation to go on.
func TestProviderStage_BlockedInputIsMarkedWithoutValidation(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
	stage := NewProviderStageWithHooks(providersmock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{MaxTokens: 100}, nil, reg)
	user := types.Message{Role: core.RoleUser, Content: "forbidden"}
	out := runTestStage(t, stage, []core.StreamElement{core.NewMessageElement(&user)})

	var msgs []types.Message
	for _, e := range out {
		if e.Message != nil {
			msgs = append(msgs, *e.Message)
		}
	}
	require.Equal(t, []string{"user:forbidden", "assistant:Not here."}, roleLines(msgs))
	assert.Empty(t, msgs[1].Validations)
	assert.Equal(t, true, msgs[1].Meta[types.MetaBlockedInput])
	assert.Empty(t, history.ExcludeBlockedInputTurns(msgs), "the pair is left out of model context")
}

// What a text turn after such a session sends the provider: neither the
// blocked request nor the reply that answered it.
func TestProviderStage_LateBlockedRequestAndItsReplyAreNotSent(t *testing.T) {
	prov := &requestRecordingProvider{Provider: providersmock.NewProvider("p", "m", false)}
	stage := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, nil)
	history := []types.Message{
		{Role: core.RoleUser, Content: "hello"},
		{Role: core.RoleAssistant, Content: "Hi!"},
		{Role: core.RoleAssistant, Content: "Sure"},
		{Role: core.RoleUser, Content: "a wire transfer please", Meta: map[string]interface{}{
			types.MetaBlockedInput: true, history.BlockedReplyBeforeMetaKey: true,
		}},
		{Role: core.RoleUser, Content: "what time is it"},
	}
	elems := make([]core.StreamElement, len(history))
	for i := range history {
		elems[i] = core.NewMessageElement(&history[i])
	}
	runTestStage(t, stage, elems)

	require.Len(t, prov.seen, 1)
	assert.Equal(t, []string{"user:hello", "assistant:Hi!", "user:what time is it"}, roleLines(prov.seen[0]))
}

// cutThenLateBlocked is a stored duplex history where a barge-in cut the reply
// to a spoken input whose late transcript was then blocked: the reply before
// the blocked request is the cut one, not the answer to the turn before.
func cutThenLateBlocked(next ...types.Message) []types.Message {
	return append([]types.Message{
		{Role: core.RoleUser, Content: "hello"},
		{Role: core.RoleAssistant, Content: "Hi!"},
		interruptedReply("Sure, I'll"),
		{Role: core.RoleUser, Content: "a wire transfer please", Meta: map[string]interface{}{
			types.MetaBlockedInput: true, history.BlockedReplyBeforeMetaKey: true,
		}},
	}, next...)
}

func TestProviderStage_CutReplyBeforeLateBlockedRequestKeepsThePreviousAnswer(t *testing.T) {
	for name, streaming := range map[string]bool{"unary": false, "streaming": true} {
		t.Run(name, func(t *testing.T) {
			prov := &requestRecordingProvider{Provider: providersmock.NewProvider("p", "m", false)}
			var p providers.Provider = prov
			if !streaming {
				p = &nonStreamingProvider{Provider: prov}
			}
			stage := NewProviderStageWithHooks(p, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, nil)
			history := cutThenLateBlocked(types.Message{Role: core.RoleUser, Content: "what time is it"})
			elems := make([]core.StreamElement, len(history))
			for i := range history {
				elems[i] = core.NewMessageElement(&history[i])
			}
			runTestStage(t, stage, elems)

			require.Len(t, prov.seen, 1)
			assert.Equal(t, []string{"user:hello", "assistant:Hi!", "user:what time is it"}, roleLines(prov.seen[0]))
		})
	}
}

func TestFinalTurn_ReaskCutReplyBeforeLateBlockedRequest(t *testing.T) {
	p := &reaskHistorySpy{schemaSpyProvider: &schemaSpyProvider{toolRounds: 1}}
	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	turnState := core.NewTurnState()
	turnState.SystemPrompt = "sys"
	turnState.AllowedTools = []string{"probe"}
	stage := NewProviderStageWithTurnState(p, reg, nil,
		&ProviderConfig{MaxTokens: 100, ResponseFormat: jsonSchemaFormat()}, nil, nil, turnState)
	history := cutThenLateBlocked(types.Message{Role: core.RoleUser, Content: "go"})
	elems := make([]core.StreamElement, len(history))
	for i := range history {
		elems[i] = core.NewMessageElement(&history[i])
	}
	runTestStage(t, stage, elems)

	require.Len(t, p.seen, 3, "a tool round, the round that ends the loop, and the re-ask")
	for i, msgs := range p.seen {
		lines := roleLines(msgs)
		require.GreaterOrEqual(t, len(lines), 3, "call %d", i+1)
		assert.Equal(t, []string{"user:hello", "assistant:Hi!", "user:go"}, lines[:3], "call %d", i+1)
	}
}

// requestRecordingProvider records the messages of every request.
type requestRecordingProvider struct {
	*providersmock.Provider
	seen [][]types.Message
}

func (p *reaskHistorySpy) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest,
	providerTools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.schemaSpyProvider.PredictWithTools(ctx, req, providerTools, choice)
}

func (p *requestRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.Provider.Predict(ctx, req)
}

func (p *requestRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	return p.Provider.PredictStream(ctx, req)
}
