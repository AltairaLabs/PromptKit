package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// Compaction must reach VAD mode's provider stage.
//
// A voice session is the longest-lived topology there is: without a compactor
// the tool loop grows the context until the provider rejects the turn. The
// default is on, exactly as in the streaming sibling.
func TestVADProviderConfig_CompactsByDefault(t *testing.T) {
	got := vadProviderConfig(&Config{})
	assert.NotNil(t, got.Compactor, "a long-running voice session must compact")
}

// ...and stays off when the caller disabled it.
func TestVADProviderConfig_HonorsCompactionDisabled(t *testing.T) {
	off := false
	got := vadProviderConfig(&Config{CompactionEnabled: &off})
	assert.Nil(t, got.Compactor)
}
