package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordingMessageLog records what the provider stage would persist.
type recordingMessageLog struct{ appended []types.Message }

func (l *recordingMessageLog) LogAppend(
	_ context.Context, _ string, _ int, messages []types.Message,
) (int, error) {
	l.appended = append(l.appended, messages...)
	return len(l.appended), nil
}

func (l *recordingMessageLog) LogLoad(
	_ context.Context, _ string, _ int,
) ([]types.Message, error) {
	return l.appended, nil
}

func (l *recordingMessageLog) LogLen(_ context.Context, _ string) (int, error) {
	return len(l.appended), nil
}

var _ statestore.MessageLog = (*recordingMessageLog)(nil)

// stubSelector is an identity Selector: enough to assert the field was wired.
type stubSelector struct{}

func (*stubSelector) Name() string { return "stub" }

func (*stubSelector) Init(_ selection.SelectorContext) error { return nil }

func (*stubSelector) Select(
	_ context.Context, _ selection.Query, candidates []selection.Candidate,
) ([]string, error) {
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

var _ selection.Selector = (*stubSelector)(nil)

// VAD mode must persist through the message log like every other topology.
//
// It never did. The sibling streaming ProviderConfig in builder.go has always
// set MessageLog and MessageLogConvID; the VAD one set neither, so a voice
// session wrote nothing per turn. IncrementalSaveStage cannot cover for that in
// a long-running session — it drains its input channel before writing, which
// for duplex/VAD means one write at session close.
func TestVADProviderConfig_CarriesTheMessageLog(t *testing.T) {
	log := &recordingMessageLog{}
	cfg := &Config{
		MessageLog:     log,
		ConversationID: "conv-vad-1",
	}

	got := vadProviderConfig(cfg)

	require.NotNil(t, got.MessageLog, "VAD must persist per turn, not only at session close")
	assert.Same(t, log, got.MessageLog)
	assert.Equal(t, "conv-vad-1", got.MessageLogConvID,
		"a log without the conversation id writes to the wrong conversation")
}

// Streaming stays on: the tool loop fires per EndOfTurn rather than once when
// the session closes (#1644). Bundled here because it shares the config that
// lost the message log, and losing it is the same class of silent omission.
func TestVADProviderConfig_StaysStreaming(t *testing.T) {
	got := vadProviderConfig(&Config{})
	assert.True(t, got.Streaming, "VAD is a continuous multi-turn session")
}

// Scalar settings still reach the stage.
func TestVADProviderConfig_CarriesModelSettings(t *testing.T) {
	got := vadProviderConfig(&Config{MaxTokens: 1234, Temperature: 0.25})
	assert.Equal(t, 1234, got.MaxTokens)
	assert.InDelta(t, 0.25, got.Temperature, 0.0001)
}

// Skill tool grants must reach VAD mode's provider stage.
//
// #1957/#1980 made a skill's allowed-tools extend the prompt baseline through
// ProviderConfig.ToolGrants, wired as a live accessor because activation
// happens mid-turn. The streaming sibling in builder.go sets it; VAD did not,
// so a voice session saw a skill activate and then found none of the tools it
// granted. Same silent-omission class as the message log above.
func TestVADProviderConfig_CarriesToolGrants(t *testing.T) {
	cfg := &Config{ToolGrants: func() []string { return []string{"refund"} }}

	got := vadProviderConfig(cfg)

	require.NotNil(t, got.ToolGrants, "a skill activated by voice must grant its tools")
	assert.Equal(t, []string{"refund"}, got.ToolGrants())
}

// The per-turn tool selector must reach VAD mode's provider stage.
func TestVADProviderConfig_CarriesToolSelector(t *testing.T) {
	sel := &stubSelector{}
	cfg := &Config{ToolSelector: sel}

	got := vadProviderConfig(cfg)

	require.NotNil(t, got.ToolSelector, "voice turns narrow tools like every other turn")
	assert.Same(t, sel, got.ToolSelector)
}

// Compaction must reach VAD mode's provider stage, built the same way the
// streaming sibling builds it.
//
// A voice session is the longest-lived topology there is: without a compactor
// the tool loop grows the context until the provider rejects the turn. The
// caller's own strategy wins, as it does everywhere else.
func TestVADProviderConfig_CompactsWithTheCallersStrategy(t *testing.T) {
	mine := &stage.ContextCompactor{BudgetTokens: 4321}

	got := vadProviderConfig(&Config{CompactionStrategy: mine})

	assert.Same(t, mine, got.Compactor,
		"a caller-supplied compactor must reach the voice stage unchanged")
}

// With no strategy supplied, VAD still gets the default compactor and its
// default budget -- not nil, which is what it used to get.
func TestVADProviderConfig_CompactsByDefault(t *testing.T) {
	got := vadProviderConfig(&Config{})

	compactor, ok := got.Compactor.(*stage.ContextCompactor)
	require.True(t, ok, "a long-running voice session must compact, got %T", got.Compactor)
	assert.Equal(t, stage.DefaultBudgetTokens, compactor.BudgetTokens,
		"the default budget is what bounds an unattended voice session")
}

// ...and stays off when the caller disabled it, even having supplied a
// strategy: the switch wins over the strategy.
//
// Asserted against the enabled case built from the same strategy, so the test
// fails both if disabling is ignored and if the switch disables everything.
func TestVADProviderConfig_HonorsCompactionDisabled(t *testing.T) {
	mine := &stage.ContextCompactor{BudgetTokens: 4321}
	off, on := false, true

	disabled := vadProviderConfig(&Config{CompactionEnabled: &off, CompactionStrategy: mine})
	enabled := vadProviderConfig(&Config{CompactionEnabled: &on, CompactionStrategy: mine})

	assert.Same(t, mine, enabled.Compactor, "the same config with the switch on compacts")
	assert.Nil(t, disabled.Compactor, "disabling compaction must beat a supplied strategy")
}

// blockingVoiceHook denies every provider call and records that it ran.
type blockingVoiceHook struct{ beforeCalls int }

func (*blockingVoiceHook) Name() string { return "blocking-voice-hook" }

func (h *blockingVoiceHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	h.beforeCalls++
	return hooks.Deny("blocked in voice")
}

func (*blockingVoiceHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

var _ hooks.ProviderHook = (*blockingVoiceHook)(nil)

// providerStageFrom returns the ProviderStage the builder put in the topology.
func providerStageFrom(t *testing.T, stages []stage.Stage) stage.Stage {
	t.Helper()
	for _, s := range stages {
		if s.Name() == "provider" {
			return s
		}
	}
	t.Fatal("VAD topology has no provider stage")
	return nil
}

// A guardrail must gate a voice turn exactly as it gates a text one.
//
// It never did: builder_vad.go passed nil for the provider stage's hook
// registry, so every provider hook early-returned and a pack's validators:
// silently did nothing in VAD mode (#1944). The streaming sibling in
// builder.go has always passed cfg.HookRegistry — same silent-omission class
// as the message log and the tool grants above.
//
// Asserted by running a turn, not by reading a field: the registry arriving
// and the hook firing are two different things, and #1679 exists because the
// first was once verified without the second.
func TestVADPipeline_RunsProviderHooks(t *testing.T) {
	hook := &blockingVoiceHook{}
	vadConfig := stage.AudioTurnConfig{
		VAD:          &mockVADAnalyzer{},
		TurnDetector: &mockTurnDetector{},
	}
	cfg := &Config{
		Provider:     mock.NewProvider("test-mock", "test-model", false),
		VADConfig:    &vadConfig,
		HookRegistry: hooks.NewRegistry(hooks.WithProviderHook(hook)),
	}

	stages, err := buildVADPipelineStages(cfg, stage.NewTurnState())
	require.NoError(t, err)

	input := make(chan stage.StreamElement, 4)
	input <- stage.NewMessageElement(&types.Message{Role: "user", Content: "say something"})
	input <- stage.NewEndOfTurnElement()
	close(input)
	output := make(chan stage.StreamElement, 32)

	require.NoError(t, providerStageFrom(t, stages).Process(context.Background(), input, output))

	require.Equal(t, 1, hook.beforeCalls,
		"a voice turn must run the provider hooks a text turn runs")

	// Running is not gating. A hook that fires and is then ignored leaves a
	// pack's validators: just as inert, so assert the denial actually took.
	//
	// A bare Deny surfaces as an error element and no assistant turn. The
	// canned-turn contract belongs to the guardrail adapter's Enforced
	// decision, which is a layer above this wiring.
	assistants, errs := drainTurn(output)
	assert.Empty(t, assistants, "a denied turn must not reach the model")
	assert.NotEmpty(t, errs, "the denial has to be visible to the caller")
	assert.Contains(t, errs[0].Error(), "blocked in voice")
}

// drainTurn reads a finished turn's output. Process closes the channel itself,
// so the range terminates without the test closing anything.
func drainTurn(output <-chan stage.StreamElement) ([]string, []error) {
	var assistants []string
	var errs []error
	for e := range output {
		switch {
		case e.Error != nil:
			errs = append(errs, e.Error)
		case e.Message != nil && e.Message.Role == "assistant":
			assistants = append(assistants, e.Message.GetContent())
		}
	}
	return assistants, errs
}

// The control: the same topology with an allowing hook lets the turn through.
// Without it the test above passes for a pipeline that blocks everything —
// including one where the provider was never wired at all.
func TestVADPipeline_AllowingHookLetsTheTurnThrough(t *testing.T) {
	hook := &allowingVoiceHook{}
	vadConfig := stage.AudioTurnConfig{
		VAD:          &mockVADAnalyzer{},
		TurnDetector: &mockTurnDetector{},
	}
	cfg := &Config{
		Provider:     mock.NewProvider("test-mock", "test-model", false),
		VADConfig:    &vadConfig,
		HookRegistry: hooks.NewRegistry(hooks.WithProviderHook(hook)),
	}

	stages, err := buildVADPipelineStages(cfg, stage.NewTurnState())
	require.NoError(t, err)

	input := make(chan stage.StreamElement, 4)
	input <- stage.NewMessageElement(&types.Message{Role: "user", Content: "say something"})
	input <- stage.NewEndOfTurnElement()
	close(input)
	output := make(chan stage.StreamElement, 32)

	require.NoError(t, providerStageFrom(t, stages).Process(context.Background(), input, output))

	require.Equal(t, 1, hook.beforeCalls)
	assistants, errs := drainTurn(output)
	assert.Empty(t, errs)
	require.Len(t, assistants, 1, "an allowed voice turn produces the model's answer")
	assert.NotEmpty(t, assistants[0])
}

// allowingVoiceHook permits every call and records that it ran.
type allowingVoiceHook struct{ beforeCalls int }

func (*allowingVoiceHook) Name() string { return "allowing-voice-hook" }

func (h *allowingVoiceHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	h.beforeCalls++
	return hooks.Allow
}

func (*allowingVoiceHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

var _ hooks.ProviderHook = (*allowingVoiceHook)(nil)
