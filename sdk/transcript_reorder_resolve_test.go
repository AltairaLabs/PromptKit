package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/stretchr/testify/assert"
)

// lateProvider is a streaming provider that declares it emits the input
// transcript late (implements providers.LateInputTranscriber).
type lateProvider struct{ *mock.StreamingProvider }

func (lateProvider) EmitsLateInputTranscription() bool { return true }

func cfgWith(md map[string]any) *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{Metadata: md}
}

func TestResolveTranscriptReorder(t *testing.T) {
	late := lateProvider{mock.NewStreamingProvider("late", "m", false)}
	plain := mock.NewStreamingProvider("plain", "m", false)

	t.Run("auto on: late provider + transcription enabled", func(t *testing.T) {
		on, ph := resolveTranscriptReorder(late, cfgWith(map[string]any{"input_transcription": true}))
		assert.True(t, on)
		assert.Equal(t, defaultTranscriptPlaceholder, ph)
	})

	t.Run("auto off: late provider but transcription disabled", func(t *testing.T) {
		on, _ := resolveTranscriptReorder(late, cfgWith(map[string]any{"input_transcription": false}))
		assert.False(t, on, "no transcript means every turn would be a placeholder — stay off")
	})

	t.Run("auto off: provider does not emit late transcription", func(t *testing.T) {
		on, _ := resolveTranscriptReorder(plain, cfgWith(map[string]any{"input_transcription": true}))
		assert.False(t, on)
	})

	t.Run("override reorder forces on even for a plain provider", func(t *testing.T) {
		on, _ := resolveTranscriptReorder(plain, cfgWith(map[string]any{"input_transcript_ordering": "reorder"}))
		assert.True(t, on)
	})

	t.Run("override passthrough forces off despite auto conditions", func(t *testing.T) {
		on, _ := resolveTranscriptReorder(late, cfgWith(map[string]any{
			"input_transcription":       true,
			"input_transcript_ordering": "passthrough",
		}))
		assert.False(t, on)
	})

	t.Run("placeholder is configurable", func(t *testing.T) {
		_, ph := resolveTranscriptReorder(late, cfgWith(map[string]any{
			"input_transcription":          true,
			"input_transcript_placeholder": "[you spoke]",
		}))
		assert.Equal(t, "[you spoke]", ph)
	})

	t.Run("nil config: off with default placeholder", func(t *testing.T) {
		on, ph := resolveTranscriptReorder(late, nil)
		assert.False(t, on)
		assert.Equal(t, defaultTranscriptPlaceholder, ph)
	})
}
